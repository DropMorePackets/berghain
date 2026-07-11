package berghain

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"net/netip"
	"sync"

	"github.com/dropmorepackets/haproxy-go/pkg/buffer"
)

// 03|3778206500000000|1edb6858c727c3519825ac8a8777d94282fe476c4d3e0b6a7247dc5fa2d4ed7f
// uint8 +  uint64 + sha256 (32byte) = 41 byte
// this encoded into hex = 82 byte
// adding two spacers = 2 byte
// total = 82 bytes
const encodedCookieSize = 84

var cookieBufferPool = sync.Pool{
	New: func() any {
		return buffer.NewSliceBuffer(encodedCookieSize)
	},
}

func AcquireCookieBuffer() *buffer.SliceBuffer {
	return cookieBufferPool.Get().(*buffer.SliceBuffer)
}

func ReleaseCookieBuffer(b *buffer.SliceBuffer) {
	b.Reset()
	cookieBufferPool.Put(b)
}

type RequestIdentifier struct {
	SrcAddr netip.Addr
	Host    []byte
	Level   uint8
}

func (ri RequestIdentifier) WriteTo(h io.Writer) (int64, error) {
	raw := AcquireCookieBuffer()
	defer ReleaseCookieBuffer(raw)

	var written int64
	// Write Host to the hash
	n, err := h.Write(ri.Host)
	written += int64(n)
	if err != nil {
		return written, err
	}

	// Write SrcAddr first to the buffer and then to the hash.
	// netip.AsSlice does an allocation we want to avoid.
	addrSlice := raw.WriteBytes()[:0] // reset length to zero
	addrSlice = ri.SrcAddr.AppendTo(addrSlice)
	n, err = h.Write(addrSlice)
	written += int64(n)
	if err != nil {
		return written, err
	}
	raw.Reset()

	// Write Level to the buffer and to the hash
	raw.WriteNBytes(1)[0] = ri.Level
	n, err = h.Write(raw.ReadBytes())
	written += int64(n)
	if err != nil {
		return written, err
	}

	return written, nil
}

var identityCookie cookieTemplate

// The cookie document is level|expiry|sum, all hex encoded.
type cookieTemplate struct {
	once sync.Once
	raw  string

	// Slot accessors; valid after init ran.
	Level  bufSlot // 2 hex chars
	Expiry bufSlot // 16 hex chars
	Sum    bufSlot // 64 hex chars
}

func (t *cookieTemplate) init() {
	t.once.Do(func() {
		const (
			level  = "00"
			expiry = "0000000000000000"
			sum    = "0000000000000000000000000000000000000000000000000000000000000000"
		)
		t.raw = level + "|" + expiry + "|" + sum
		if len(t.raw) != encodedCookieSize {
			panic("buftemplate: cookie template does not match encodedCookieSize")
		}

		loc := slotLocator{doc: t.raw}
		t.Level = loc.next(level)
		t.Expiry = loc.next(expiry)
		t.Sum = loc.next(sum)
	})
}

// Render appends the template to body and returns the rendered document.
// Slot accessors take this document and return writable views into it.
func (t *cookieTemplate) Render(body *buffer.SliceBuffer) []byte {
	t.init()
	return renderTemplate(body, t.raw)
}

func (ri RequestIdentifier) ToCookie(b *Berghain, enc *buffer.SliceBuffer) error {
	raw := AcquireCookieBuffer()
	defer ReleaseCookieBuffer(raw)

	h := b.acquireHMAC()
	defer b.releaseHMAC(h)

	tpl := &identityCookie
	doc := tpl.Render(enc)

	// Write Host to the hash
	if _, err := h.Write(ri.Host); err != nil {
		return err
	}

	// Write SrcAddr first to the buffer and then to the hash.
	// netip.AsSlice does an allocation we want to avoid.
	addrSlice := raw.WriteBytes()[:0] // reset length to zero
	addrSlice = ri.SrcAddr.AppendTo(addrSlice)
	if _, err := h.Write(addrSlice); err != nil {
		return err
	}
	raw.Reset()

	// Write Level to the buffer, the hash, and its output slot.
	raw.WriteNBytes(1)[0] = ri.Level
	if _, err := h.Write(raw.ReadBytes()); err != nil {
		return err
	}
	hex.Encode(tpl.Level(doc), raw.ReadBytes())
	raw.Reset()

	// Calculate the expiration of the cookie, write it to the hash and its slot.
	expireAt := tc.Now().Add(b.LevelConfig(ri.Level).Duration)
	binary.LittleEndian.PutUint64(raw.WriteNBytes(8), uint64(expireAt.Unix()))
	if _, err := h.Write(raw.ReadBytes()); err != nil {
		return err
	}
	hex.Encode(tpl.Expiry(doc), raw.ReadBytes())
	raw.Reset()

	// Finally generate the sum into its slot.
	hex.Encode(tpl.Sum(doc), h.Sum(nil))

	return nil
}

var (
	ErrEmpty         = fmt.Errorf("empty")
	ErrInvalidLength = fmt.Errorf("invalid length")
	ErrLevelTooLow   = fmt.Errorf("cookie level too low")
	ErrExpired       = fmt.Errorf("expired")
	ErrInvalidHMAC   = fmt.Errorf("invalid hmac")
)

func (b *Berghain) IsValidCookie(ri RequestIdentifier, cookie []byte) error {
	lc := len(cookie)

	if lc == 0 {
		// cookie either not set or set with empty value
		return ErrEmpty
	}

	if lc != encodedCookieSize {
		return ErrInvalidLength
	}

	dec := AcquireCookieBuffer()
	defer ReleaseCookieBuffer(dec)

	h := b.acquireHMAC()
	defer b.releaseHMAC(h)

	if _, err := h.Write(ri.Host); err != nil {
		return err
	}

	// Write SrcAddr first to the buffer and then to the hash.
	// netip.AsSlice does an allocation we want to avoid.
	addrSlice := dec.WriteBytes()[:0] // reset capacity to zero
	addrSlice = ri.SrcAddr.AppendTo(addrSlice)
	if _, err := h.Write(addrSlice); err != nil {
		return err
	}
	dec.Reset()

	cookieBuf := buffer.NewSliceBufferWithSlice(cookie)

	cookieLevel := cookieBuf.ReadNBytes(hex.EncodedLen(1))
	cookieBuf.AdvanceR(1) // Separator
	levelArea := dec.WriteNBytes(hex.DecodedLen(len(cookieLevel)))
	if _, err := hex.Decode(levelArea, cookieLevel); err != nil {
		return err
	}

	// Untrusted input is compared!
	if ri.Level > dec.ReadBytes()[0] {
		return ErrLevelTooLow
	}

	if _, err := h.Write(dec.ReadBytes()); err != nil {
		return err
	}
	dec.Reset()

	cookieExpiration := cookieBuf.ReadNBytes(hex.EncodedLen(8))
	cookieBuf.AdvanceR(1) // Separator
	expirArea := dec.WriteNBytes(hex.DecodedLen(len(cookieExpiration)))
	if _, err := hex.Decode(expirArea, cookieExpiration); err != nil {
		return err
	}

	// Untrusted input is decoded and compared!
	if uint64(tc.Now().Unix()) > binary.LittleEndian.Uint64(dec.ReadBytes()) {
		return ErrExpired
	}

	if _, err := h.Write(dec.ReadBytes()); err != nil {
		return err
	}
	dec.Reset()

	cookieSum := cookieBuf.ReadBytes()
	sumArea := dec.WriteNBytes(hex.DecodedLen(len(cookieSum)))
	if _, err := hex.Decode(sumArea, cookieSum); err != nil {
		return err
	}

	if !bytes.Equal(h.Sum(nil), dec.ReadBytes()) {
		return ErrInvalidHMAC
	}

	return nil
}
