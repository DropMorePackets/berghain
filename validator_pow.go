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
	// template is the pre-encoded challenge JSON copied into the response body.
	// POW variants differ only by the "t" (challenge type) value it carries.
	template string
}

const (
	validatorPOWRandom            = "0000000000000000"
	validatorPOWHash              = "0000000000000000000000000000000000000000000000000000000000000000"
	validatorPOWMinSolutionLength = len(validatorPOWRandom + "-" + validatorPOWHash + "-0")
)

const hexdigits = "0123456789abcdef"

// powChallengeTemplate builds the pre-encoded challenge JSON for a POW variant.
// Only the "t" value varies; every mutable field stays fixed-width so onNew can
// overwrite it by offset. Difficulty is a fixed-width two-hex-char byte (leading
// zero bits, 0-255) so the hand-packed template keeps its field offsets.
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
		Random:     "0000000000000000",
		Hash:       "0000000000000000000000000000000000000000000000000000000000000000",
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

	copy(resp.Body.WriteBytes(), p.template)

	resp.Body.AdvanceW(len(`{"c":`))
	// the following conversion is faster than sprintf but also way uglier, I am sorry.
	// 48 is the ASCII code for '0', adding lc.Countdown will give us the single correct digit.
	copy(resp.Body.WriteNBytes(1), []byte{byte(48 + lc.Countdown)})
	resp.Body.AdvanceW(len(`,"t":1,"d":"`))
	// Write the difficulty as a fixed-width two-hex-char byte (leading zero bits).
	// This is advisory for the client; the server enforces the difficulty itself.
	difficulty := effectivePOWDifficulty(lc.Difficulty)
	diffArea := resp.Body.WriteNBytes(2)
	diffArea[0] = hexdigits[byte(difficulty)>>4]
	diffArea[1] = hexdigits[byte(difficulty)&0x0f]
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
	// req.Body should be at least validatorPOWMinSolutionLength. A minimal valid
	// solution (single-digit nonce) has exactly that length, so reject only when
	// strictly shorter — otherwise low-difficulty levels with a tiny nonce fail.
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
	sha.Write(solArea)
	sum := sha.Sum(nil)

	if !hasLeadingZeroBits(sum, effectivePOWDifficulty(b.LevelConfig(req.Identifier.Level).Difficulty)) {
		return errInvalidSolution
	}

	return nil
}

// defaultPOWDifficulty preserves the historic 16-bit (two zero byte) target for
// levels that leave Difficulty unset — e.g. a LevelConfig constructed directly
// rather than via the YAML parser. Without this a zero difficulty would accept
// any nonce, silently disabling the proof of work.
const defaultPOWDifficulty = 16

func effectivePOWDifficulty(d int) int {
	if d <= 0 {
		return defaultPOWDifficulty
	}
	return d
}

// hasLeadingZeroBits reports whether b begins with at least bits zero bits.
// It is zero-allocation and does not read past bits/8 (rounded up) bytes of b.
func hasLeadingZeroBits(b []byte, bits int) bool {
	n := bits >> 3
	for i := 0; i < n; i++ {
		if b[i] != 0 {
			return false
		}
	}
	if r := bits & 7; r != 0 {
		if b[n]>>(8-r) != 0 {
			return false
		}
	}
	return true
}

var errInvalidSolution = fmt.Errorf("invalid solution")

func validatorPOW(b *Berghain, req *ValidatorRequest, resp *ValidatorResponse) error {
	p := powValidator{template: validatorPOWChallengeTemplate}

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
