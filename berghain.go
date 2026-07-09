package berghain

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"log/slog"
	"net/netip"
	"sync"
	"time"
)

type LevelConfig struct {
	Countdown int
	Duration  time.Duration
	Type      ValidationType

	// Difficulty is the number of leading zero bits a POW solution hash must
	// have. It is only used by POW-style validators and is ignored otherwise.
	Difficulty int
}

type Berghain struct {
	Levels         []*LevelConfig
	TrustedDomains []string

	secret []byte
	hmac   sync.Pool
}

var hashAlgo = sha256.New

func NewBerghain(secret []byte) *Berghain {
	return &Berghain{
		secret: secret,
		hmac: sync.Pool{
			New: func() any {
				return NewZeroHasher(hmac.New(hashAlgo, secret))
			},
		},
	}
}

func (b *Berghain) acquireHMAC() hash.Hash {
	return b.hmac.Get().(hash.Hash)
}

func (b *Berghain) releaseHMAC(h hash.Hash) {
	h.Reset()
	b.hmac.Put(h)
}

// HashSource returns a keyed, non-reversible hex digest of the given source
// address, suitable for correlating log lines without ever writing an IP in
// plaintext. It reuses the HMAC secret, so the mapping cannot be reversed even
// by the operator, honoring Berghain's privacy design.
func (b *Berghain) HashSource(a netip.Addr) string {
	h := b.acquireHMAC()
	defer b.releaseHMAC(h)

	raw := AcquireCookieBuffer()
	defer ReleaseCookieBuffer(raw)

	// netip.AsSlice would allocate; append into the pooled buffer instead.
	addrSlice := raw.WriteBytes()[:0]
	addrSlice = a.AppendTo(addrSlice)
	_, _ = h.Write(addrSlice)

	return hex.EncodeToString(h.Sum(nil)[:8])
}

func (b *Berghain) LevelConfig(level uint8) *LevelConfig {

	if level == 0 {
		slog.Warn("level cannot be zero, correcting", "old", 0, "new", 1)
	}

	if level > uint8(len(b.Levels)) {
		slog.Warn("level too high, correcting", "old", level, "new", len(b.Levels))
	}

	level = min(uint8(len(b.Levels)), max(1, level))

	return b.Levels[level-1]
}
