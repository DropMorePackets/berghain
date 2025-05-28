package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/netip"
	"sync"

	"github.com/dropmorepackets/haproxy-go/pkg/buffer"
	"github.com/dropmorepackets/haproxy-go/pkg/encoding"

	"github.com/fionera/berghain"
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
	host := k.ValueBytes()
	ctx = context.WithValue(ctx, "host", string(host))
	if len(host) > hostBufferLength {
		slog.ErrorContext(ctx, "host length too big")
		return
	}

	hostBuf := acquireHostBuf()
	defer releaseHostBuf(hostBuf)
	copy(hostBuf.WriteNBytes(len(k.ValueBytes())), k.ValueBytes())
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
	}
	ri.SrcAddr = addr

	if err := readExpectedKVEntry(ctx, m, k, "host"); err != nil {
		return
	}
	if len(k.ValueBytes()) > hostBufferLength {
		slog.ErrorContext(ctx, "host length too big")
	}

	hostBuf := acquireHostBuf()
	defer releaseHostBuf(hostBuf)
	copy(hostBuf.WriteNBytes(len(k.ValueBytes())), k.ValueBytes())
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

	resp := berghain.AcquireValidatorResponse()
	defer berghain.ReleaseValidatorResponse(resp)

	err := f.bh.LevelConfig(ri.Level).Type.RunValidator(f.bh, req, resp)
	if err != nil {
		slog.ErrorContext(ctx, "validator failed", "error", err)
	}

	_ = w.SetStringBytes(encoding.VarScopeTransaction, "response", resp.Body.ReadBytes())
	if resp.Token.Len() > 0 {
		_ = w.SetStringBytes(encoding.VarScopeTransaction, "token", resp.Token.ReadBytes())
	}
}
