package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/netip"
	"strings"
	"sync"

	"github.com/dropmorepackets/haproxy-go/pkg/buffer"
	"github.com/dropmorepackets/haproxy-go/pkg/encoding"

	"github.com/DropMorePackets/berghain"
)

type frontend struct {
	bh *berghain.Berghain
}

func readExpectedKVEntry(ctx context.Context, m *encoding.Message, k *encoding.KVEntry, name string) error {
	// read frontend
	if !m.KV.Next(k) {
		if err := m.KV.Error(); err != nil {
			slog.ErrorContext(ctx, "error while reading KV", "error", err)
			return errors.New("generic error while reading KV")
		}

		slog.ErrorContext(ctx, "missing SPOP argument", "want", name, "have", "nil")
		return errors.New("missing SPOP argument while reading KV")
	}

	if !k.NameEquals(name) {
		slog.ErrorContext(ctx, "invalid SPOP argument order", "want", name, "have", k.NameBytes())
		return errors.New("invalid SPOP argument order while reading KV")
	}

	return nil
}

func addOptionalSessionToContext(ctx context.Context, m *encoding.Message, k *encoding.KVEntry) (context.Context, []byte) {
	if !m.KV.Next(k) {
		if err := m.KV.Error(); err != nil {
			slog.ErrorContext(ctx, "error while reading optional session argument", "error", err)
		}
		return ctx, nil
	}

	if !k.NameEquals("session") {
		slog.WarnContext(ctx, "ignoring unexpected optional SPOP argument", "have", k.NameBytes())
		return ctx, nil
	}

	if id := k.ValueBytes(); berghain.ValidSupportID(id) {
		return context.WithValue(ctx, "session", string(id)), id
	}

	slog.DebugContext(ctx, "ignoring invalid session id")
	return ctx, nil
}

const hostBufferLength = 256

var hostBufPool = sync.Pool{
	New: func() any {
		return buffer.NewSliceBuffer(hostBufferLength)
	},
}

func acquireHostBuf() *buffer.SliceBuffer {
	return hostBufPool.Get().(*buffer.SliceBuffer)
}

func releaseHostBuf(b *buffer.SliceBuffer) {
	b.Reset()
	hostBufPool.Put(b)
}

// hostWithoutPort strips a port from an unambiguous Host authority. Bracketed
// IPv6 literals retain their brackets, while unbracketed values containing
// multiple colons and malformed ports are left untouched.
func hostWithoutPort(host []byte) []byte {
	if len(host) == 0 {
		return host
	}

	if host[0] == '[' {
		closingBracket := bytes.IndexByte(host, ']')
		if closingBracket < 0 || closingBracket == len(host)-1 {
			return host
		}
		if host[closingBracket+1] != ':' || !validPort(host[closingBracket+2:]) {
			return host
		}
		return host[:closingBracket+1]
	}

	if bytes.Count(host, []byte{':'}) != 1 {
		return host
	}
	separator := bytes.IndexByte(host, ':')
	if separator == 0 || !validPort(host[separator+1:]) {
		return host
	}
	return host[:separator]
}

func validPort(port []byte) bool {
	if len(port) == 0 {
		return false
	}

	value := 0
	for _, digit := range port {
		if digit < '0' || digit > '9' {
			return false
		}
		value = value*10 + int(digit-'0')
		if value > 65535 {
			return false
		}
	}
	return true
}

// URI hosts are case-insensitive. Canonicalizing ASCII case keeps challenge
// identities stable if a client changes the case of a DNS name or IP literal.
func normalizeHost(host []byte) []byte {
	host = hostWithoutPort(host)
	for i, c := range host {
		if c < 'A' || c > 'Z' {
			continue
		}
		normalized := bytes.Clone(host)
		for j := i; j < len(normalized); j++ {
			if normalized[j] >= 'A' && normalized[j] <= 'Z' {
				normalized[j] += 'a' - 'A'
			}
		}
		return normalized
	}
	return host
}

func getTrustedDomain(host []byte, td []string) []byte {
	h := string(host)
	for _, d := range td {
		if len(d) == 0 || len(h) < len(d) {
			continue
		}

		start := len(h) - len(d)
		if strings.EqualFold(h[start:], d) && (start == 0 || h[start-1] == '.') {
			return host[start:]
		}
	}

	return nil
}

func getDomainAttr(host []byte) string {
	if bytes.Contains(host, []byte(".")) {
		return "domain=" + string(host) + ";"
	} else {
		return ""
	}
}

func (f *frontend) HandleSPOEValidate(ctx context.Context, w *encoding.ActionWriter, m *encoding.Message) {
	k := encoding.AcquireKVEntry()
	defer encoding.ReleaseKVEntry(k)

	var ri berghain.RequestIdentifier

	if err := readExpectedKVEntry(ctx, m, k, "level"); err != nil {
		return
	}
	ri.Level = uint8(k.ValueInt())
	ctx = context.WithValue(ctx, "level", int(ri.Level))
	if ri.Level == 0 {
		// berghain is disabled, just exit early and ignore everything...
		return
	}

	if err := readExpectedKVEntry(ctx, m, k, "src"); err != nil {
		return
	}
	// AddrFromSlice copies the underlying data
	addr, ok := netip.AddrFromSlice(k.ValueBytes())
	if !ok {
		slog.ErrorContext(ctx, "cant read netip.Address from message")
		return
	}
	ctx = context.WithValue(ctx, "src", addr.String())
	ri.SrcAddr = addr

	if err := readExpectedKVEntry(ctx, m, k, "host"); err != nil {
		return
	}
	host := normalizeHost(k.ValueBytes())
	ctx = context.WithValue(ctx, "host", string(host))
	if len(host) > hostBufferLength {
		slog.ErrorContext(ctx, "host length too big")
		return
	}

	td := getTrustedDomain(host, f.bh.TrustedDomains)
	if td != nil {
		host = td
	}

	hostBuf := acquireHostBuf()
	defer releaseHostBuf(hostBuf)

	copy(hostBuf.WriteNBytes(len(host)), host)
	ri.Host = hostBuf.ReadBytes()

	if err := readExpectedKVEntry(ctx, m, k, "cookie"); err != nil {
		return
	}
	err := f.bh.IsValidCookie(ri, k.ValueBytes())
	if err != nil {
		slog.DebugContext(ctx, "cookie not valid", "error", err)
	}
	isValidCookie := err == nil

	if err := w.SetBool(encoding.VarScopeTransaction, "valid", isValidCookie); err != nil {
		slog.ErrorContext(ctx, "failed setting action 'valid'", "error", err)
		return
	}
}

func (f *frontend) HandleSPOEChallenge(ctx context.Context, w *encoding.ActionWriter, m *encoding.Message) {
	k := encoding.AcquireKVEntry()
	defer encoding.ReleaseKVEntry(k)

	var ri berghain.RequestIdentifier

	if err := readExpectedKVEntry(ctx, m, k, "level"); err != nil {
		return
	}
	ri.Level = uint8(k.ValueInt())
	if ri.Level == 0 {
		// berghain is disabled, just exit early and ignore everything...
		return
	}

	if err := readExpectedKVEntry(ctx, m, k, "src"); err != nil {
		return
	}
	// AddrFromSlice copies the underlying data
	addr, ok := netip.AddrFromSlice(k.ValueBytes())
	if !ok {
		slog.ErrorContext(ctx, "cant read netip.Address from message")
		return
	}
	ri.SrcAddr = addr

	if err := readExpectedKVEntry(ctx, m, k, "host"); err != nil {
		return
	}
	host := normalizeHost(k.ValueBytes())
	if len(host) > hostBufferLength {
		slog.ErrorContext(ctx, "host length too big")
		return
	}

	td := getTrustedDomain(host, f.bh.TrustedDomains)
	if td != nil {
		host = td
	}

	_ = w.SetString(encoding.VarScopeTransaction, "domain", getDomainAttr(host))

	hostBuf := acquireHostBuf()
	defer releaseHostBuf(hostBuf)

	copy(hostBuf.WriteNBytes(len(host)), host)
	ri.Host = hostBuf.ReadBytes()

	req := berghain.AcquireValidatorRequest()
	defer berghain.ReleaseValidatorRequest(req)

	req.Identifier = &ri

	if err := readExpectedKVEntry(ctx, m, k, "method"); err != nil {
		return
	}
	switch {
	case string(k.ValueBytes()) == http.MethodGet:
		req.Method = http.MethodGet
	case string(k.ValueBytes()) == http.MethodPost:
		req.Method = http.MethodPost
	default:
		_ = w.SetString(encoding.VarScopeTransaction, "response", "unsupported request method")
		return
	}

	if err := readExpectedKVEntry(ctx, m, k, "body"); err != nil {
		return
	}
	req.Body = k.ValueBytes()
	ctx, req.SupportID = addOptionalSessionToContext(ctx, m, k)

	resp := berghain.AcquireValidatorResponse()
	defer berghain.ReleaseValidatorResponse(resp)

	err := f.bh.LevelConfig(ri.Level).Type.RunValidator(f.bh, req, resp)
	if berghain.ValidSupportID(req.SupportID) {
		ctx = context.WithValue(ctx, "session", string(req.SupportID))
	}
	if err != nil {
		slog.ErrorContext(ctx, "validator failed", "error", err)
	}

	_ = w.SetStringBytes(encoding.VarScopeTransaction, "response", resp.Body.ReadBytes())
	if resp.Token.Len() > 0 {
		_ = w.SetStringBytes(encoding.VarScopeTransaction, "token", resp.Token.ReadBytes())
	}
}
