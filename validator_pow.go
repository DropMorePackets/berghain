package berghain

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash"
	"net/http"
	"sync"

	"github.com/dropmorepackets/haproxy-go/pkg/buffer"
)

var sha256Pool = sync.Pool{
	New: func() any {
		return NewZeroHasher(hashAlgo())
	},
}

func acquireSHA256() hash.Hash {
	return sha256Pool.Get().(hash.Hash)
}

func releaseSHA256(h hash.Hash) {
	h.Reset()
	sha256Pool.Put(h)
}

type powValidator struct {
	template string
}

const (
	validatorPOWRandom            = "0000000000000000"
	validatorPOWHash              = "0000000000000000000000000000000000000000000000000000000000000000"
	validatorPOWMinSolutionLength = len(validatorPOWRandom + "-" + validatorPOWHash + "-0")

	DefaultPOWDifficulty = 16
	MinPOWDifficulty     = 1
	MaxPOWDifficulty     = 255
)

const hexdigits = "0123456789abcdef"

func powChallengeTemplate(challengeType int) string {
	return mustJSONEncodeString(struct {
		Countdown  int    `json:"c"`
		Type       int    `json:"t"`
		Difficulty string `json:"d"`
		Random     string `json:"r"`
		Hash       string `json:"s"`
	}{
		Type:       challengeType,
		Difficulty: "00",
		Random:     validatorPOWRandom,
		Hash:       validatorPOWHash,
	})
}

var validatorPOWChallengeTemplate = powChallengeTemplate(1)

// This prevents invalid template strings by validatoring them on start
var _ = func() bool {
	h := hashAlgo()
	if len(validatorPOWHash) != hex.EncodedLen(h.Size()) {
		panic("invalid pow hash length")
	}
	return true
}()

func (p powValidator) onNew(b *Berghain, req *ValidatorRequest, resp *ValidatorResponse) error {
	h := b.acquireHMAC()
	defer b.releaseHMAC(h)

	lc := b.LevelConfig(req.Identifier.Level)
	difficulty, err := effectivePOWDifficulty(lc.Difficulty)
	if err != nil {
		return err
	}

	copy(resp.Body.WriteBytes(), p.template)

	resp.Body.AdvanceW(len(`{"c":`))
	// the following conversion is faster than sprintf but also way uglier, I am sorry.
	// 48 is the ASCII code for '0', adding lc.Countdown will give us the single correct digit.
	copy(resp.Body.WriteNBytes(1), []byte{byte(48 + lc.Countdown)})
	resp.Body.AdvanceW(len(`,"t":1,"d":"`))
	difficultyArea := resp.Body.WriteNBytes(2)
	difficultyArea[0] = hexdigits[difficulty>>4]
	difficultyArea[1] = hexdigits[difficulty&0x0f]
	resp.Body.AdvanceW(len(`","r":"`))
	timestampArea := resp.Body.WriteNBytes(len(validatorPOWRandom))
	resp.Body.AdvanceW(len(`","s":"`))
	hexArea := resp.Body.WriteNBytes(hex.EncodedLen(h.Size()))
	resp.Body.AdvanceW(len(`"}`))

	// we use the response body temporarily as a buffer
	expireAt := tc.Now().Add(lc.Duration)
	timestampBuf := resp.Body.WriteNBytes(8)
	resp.Body.AdvanceW(-8) // this should be illegal

	binary.LittleEndian.PutUint64(timestampBuf, uint64(expireAt.Unix()))
	hex.Encode(timestampArea, timestampBuf)

	// Write identifier to hash to ensure uniqueness
	req.Identifier.WriteTo(h)
	h.Write(timestampArea)

	hex.Encode(hexArea, h.Sum(nil))

	return nil
}

func (powValidator) isValid(b *Berghain, req *ValidatorRequest, resp *ValidatorResponse) error {
	difficulty, err := effectivePOWDifficulty(b.LevelConfig(req.Identifier.Level).Difficulty)
	if err != nil {
		return err
	}

	if len(req.Body) < validatorPOWMinSolutionLength {
		// invalid solution data
		return ErrInvalidLength
	}

	body := buffer.NewSliceBufferWithSlice(req.Body)

	timestampArea := body.ReadNBytes(len(validatorPOWRandom))
	body.AdvanceR(1) // Skip padding character
	sumArea := body.ReadNBytes(len(validatorPOWHash))
	body.AdvanceR(1) // Skip padding character
	solArea := body.ReadBytes()

	h := b.acquireHMAC()
	defer b.releaseHMAC(h)

	// Write identifier to hash to ensure uniqueness
	req.Identifier.WriteTo(h)

	h.Write(timestampArea)

	// we use the response body temporarily as a buffer
	defer resp.Body.Reset()

	ourSum := resp.Body.WriteNBytes(hex.EncodedLen(h.Size()))
	hex.Encode(ourSum, h.Sum(nil))

	if !bytes.Equal(ourSum, sumArea) {
		// invalid hash in solution
		return ErrInvalidHMAC
	}
	resp.Body.Reset()

	expirArea := resp.Body.WriteNBytes(hex.DecodedLen(len(validatorPOWRandom)))
	if _, err := hex.Decode(expirArea, timestampArea); err != nil {
		return err
	}

	// Untrusted input is decoded and compared!
	if uint64(tc.Now().Unix()) > binary.LittleEndian.Uint64(expirArea) {
		return ErrExpired
	}

	sha := acquireSHA256()
	defer releaseSHA256(sha)

	sha.Write(timestampArea)
	sha.Write(sumArea)
	sha.Write(solArea)
	sum := sha.Sum(nil)

	if !hasLeadingZeroBits(sum, int(difficulty)) {
		return errInvalidSolution
	}

	return nil
}

// ValidatePOWDifficulty checks an explicit configured difficulty. The zero
// value is reserved for LevelConfig's backward-compatible default and is
// normalized by effectivePOWDifficulty.
func ValidatePOWDifficulty(difficulty int) error {
	if difficulty < MinPOWDifficulty || difficulty > MaxPOWDifficulty {
		return fmt.Errorf("difficulty must be between %d and %d", MinPOWDifficulty, MaxPOWDifficulty)
	}
	return nil
}

func effectivePOWDifficulty(difficulty int) (uint8, error) {
	if difficulty == 0 {
		return DefaultPOWDifficulty, nil
	}
	if err := ValidatePOWDifficulty(difficulty); err != nil {
		return 0, err
	}
	return uint8(difficulty), nil
}

func hasLeadingZeroBits(b []byte, bits int) bool {
	if bits < 0 || bits > len(b)*8 {
		return false
	}

	wholeBytes := bits / 8
	for i := 0; i < wholeBytes; i++ {
		if b[i] != 0 {
			return false
		}
	}

	remainingBits := bits % 8
	return remainingBits == 0 || b[wholeBytes]>>(8-remainingBits) == 0
}

var errInvalidSolution = fmt.Errorf("invalid solution")

func validatorPOW(b *Berghain, req *ValidatorRequest, resp *ValidatorResponse) error {
	return powValidator{template: validatorPOWChallengeTemplate}.run(b, req, resp)
}

func (p powValidator) run(b *Berghain, req *ValidatorRequest, resp *ValidatorResponse) error {
	switch req.Method {
	case http.MethodPost:
		if err := p.isValid(b, req, resp); err != nil {
			return err
		}
		return req.Identifier.ToCookie(b, resp.Token)
	case http.MethodGet:
		return p.onNew(b, req, resp)
	}

	return errInvalidMethod
}
