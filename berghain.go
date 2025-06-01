package berghain

import (
	"crypto/hmac"
	"crypto/sha256"
	"hash"
	"log/slog"
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
