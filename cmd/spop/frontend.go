package main

import (
	"context"
	"fmt"
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

func readExpectedKVEntry(m *encoding.Message, k *encoding.KVEntry, name string) {
	// read frontend
	if !m.KV.Next(k) {
		if err := m.KV.Error(); err != nil {
			panic(fmt.Sprintf("error while reading KV: %v", err))
		}

		panic(fmt.Sprintf("missing SPOP argument: expected %s, got nil", name))
	}

	if !k.NameEquals(name) {
		panic(fmt.Sprintf("invalid SPOP argument order: expected %s, got %s", name, k.NameBytes()))
	}
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

	readExpectedKVEntry(m, k, "level")
	ri.Level = uint8(k.ValueInt())
	ctx = context.WithValue(ctx, "level", int(ri.Level))
	if ri.Level == 0 {
		// berghain is disabled, just exit early and ignore everything...
		return
	}

	readExpectedKVEntry(m, k, "src")
	// AddrFromSlice copies the underlying data
	addr, ok := netip.AddrFromSlice(k.ValueBytes())
	if !ok {
		panic("cant read netip.Address from message")
	}
	ctx = context.WithValue(ctx, "src", addr.String())
	ri.SrcAddr = addr

	readExpectedKVEntry(m, k, "host")
	host := k.ValueBytes()
	ctx = context.WithValue(ctx, "host", string(host))
	if len(host) > hostBufferLength {
		panic("host length too big")
	}

	hostBuf := acquireHostBuf()
	defer releaseHostBuf(hostBuf)
	copy(hostBuf.WriteNBytes(len(k.ValueBytes())), k.ValueBytes())
	ri.Host = hostBuf.ReadBytes()

	readExpectedKVEntry(m, k, "cookie")
	err := f.bh.IsValidCookie(ri, k.ValueBytes())
	if err != nil {
		slog.DebugContext(ctx, "cookie not valid", "error", err)
	}
	isValidCookie := err == nil

	if err := w.SetBool(encoding.VarScopeTransaction, "valid", isValidCookie); err != nil {
		panic(fmt.Sprintf("failed setting action %s: %v", "valid", err))
	}
}

func (f *frontend) HandleSPOEChallenge(_ context.Context, w *encoding.ActionWriter, m *encoding.Message) {
	k := encoding.AcquireKVEntry()
	defer encoding.ReleaseKVEntry(k)

	var ri berghain.RequestIdentifier

	readExpectedKVEntry(m, k, "level")
	ri.Level = uint8(k.ValueInt())
	if ri.Level == 0 {
		// berghain is disabled, just exit early and ignore everything...
		return
	}

	readExpectedKVEntry(m, k, "src")
	// AddrFromSlice copies the underlying data
	addr, ok := netip.AddrFromSlice(k.ValueBytes())
	if !ok {
		panic("cant read netip.Address from message")
	}
	ri.SrcAddr = addr

	readExpectedKVEntry(m, k, "host")
	if len(k.ValueBytes()) > hostBufferLength {
		panic("host length too big")
	}

	hostBuf := acquireHostBuf()
	defer releaseHostBuf(hostBuf)
	copy(hostBuf.WriteNBytes(len(k.ValueBytes())), k.ValueBytes())
	ri.Host = hostBuf.ReadBytes()

	req := berghain.AcquireValidatorRequest()
	defer berghain.ReleaseValidatorRequest(req)

	req.Identifier = &ri

	readExpectedKVEntry(m, k, "method")
	switch {
	case string(k.ValueBytes()) == http.MethodGet:
		req.Method = http.MethodGet
	case string(k.ValueBytes()) == http.MethodPost:
		req.Method = http.MethodPost
	default:
		_ = w.SetString(encoding.VarScopeTransaction, "response", "unsupported request method")
		return
	}

	readExpectedKVEntry(m, k, "body")
	req.Body = k.ValueBytes()

	resp := berghain.AcquireValidatorResponse()
	defer berghain.ReleaseValidatorResponse(resp)

	err := f.bh.LevelConfig(ri.Level).Type.RunValidator(f.bh, req, resp)
	if err != nil {
		panic(fmt.Errorf("validator failed: %v", err))
	}

	_ = w.SetStringBytes(encoding.VarScopeTransaction, "response", resp.Body.ReadBytes())
	if resp.Token.Len() > 0 {
		_ = w.SetStringBytes(encoding.VarScopeTransaction, "token", resp.Token.ReadBytes())
	}
}
