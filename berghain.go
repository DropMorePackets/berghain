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
}

type Berghain struct {
	Levels         []*LevelConfig
	TrustedDomains []string

	secret        []byte
	hmac          sync.Pool
	sourceLogHMAC sync.Pool
}

var hashAlgo = sha256.New

const sourceLogPurpose = "berghain/source-log/v1"

func NewBerghain(secret []byte) *Berghain {
	keyDeriver := hmac.New(hashAlgo, secret)
	_, _ = keyDeriver.Write([]byte(sourceLogPurpose))
	sourceLogKey := keyDeriver.Sum(nil)

	return &Berghain{
		secret: secret,
		hmac: sync.Pool{
			New: func() any {
				return NewZeroHasher(hmac.New(hashAlgo, secret))
			},
		},
		sourceLogHMAC: sync.Pool{
			New: func() any {
				return hmac.New(hashAlgo, sourceLogKey)
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

// SourceLogID returns a stable keyed pseudonym for an address. It prevents
// accidental plaintext IP logging, but it is not anonymization: a holder of the
// configuration secret can enumerate the IPv4 address space.
func (b *Berghain) SourceLogID(address netip.Addr) string {
	if !address.IsValid() {
		return ""
	}

	h := b.sourceLogHMAC.Get().(hash.Hash)
	defer func() {
		h.Reset()
		b.sourceLogHMAC.Put(h)
	}()

	var raw [16]byte
	if address.Is4() {
		value := address.As4()
		copy(raw[:], value[:])
		_, _ = h.Write(raw[:4])
	} else {
		value := address.As16()
		copy(raw[:], value[:])
		_, _ = h.Write(raw[:])
	}

	digest := h.Sum(nil)
	var encoded [32]byte
	hex.Encode(encoded[:], digest[:16])
	return string(encoded[:])
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
