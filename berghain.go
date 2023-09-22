package berghain

import (
	"crypto/hmac"
	"crypto/sha256"
	"hash"
	"log"
	"sync"
	"time"
)

type LevelConfig struct {
	Duration time.Duration
	Type     ValidationType
}

type Berghain struct {
	Levels []*LevelConfig

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
		log.Println("level cannot be zero. correcting to 1")
	}

	if level > uint8(len(b.Levels)) {
		log.Printf("level too high. correcting to %d", len(b.Levels))
	}

	level = min(uint8(len(b.Levels)), max(1, level))

	return b.Levels[level-1]
}
