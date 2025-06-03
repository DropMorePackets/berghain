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
}

const (
	validatorPOWRandom            = "0000000000000000"
	validatorPOWHash              = "0000000000000000000000000000000000000000000000000000000000000000"
	validatorPOWMinSolutionLength = len(validatorPOWRandom + "-" + validatorPOWHash + "-0")
)

var validatorPOWChallengeTemplate = mustJSONEncodeString(struct {
	Countdown int    `json:"c"`
	Type      int    `json:"t"`
	Random    string `json:"r"`
	Hash      string `json:"s"`
}{
	// Only strings have to be set, as the default is zero for ints.
	// We do set the Type here because it is static anyway...
	Type:   1,
	Random: "0000000000000000",
	Hash:   "0000000000000000000000000000000000000000000000000000000000000000",
})

// This prevents invalid template strings by validatoring them on start
var _ = func() bool {
	h := hashAlgo()
	if len(validatorPOWHash) != hex.EncodedLen(h.Size()) {
		panic("invalid pow hash length")
	}
	return true
}()

func (powValidator) onNew(b *Berghain, req *ValidatorRequest, resp *ValidatorResponse) error {
	h := b.acquireHMAC()
	defer b.releaseHMAC(h)

	lc := b.LevelConfig(req.Identifier.Level)

	copy(resp.Body.WriteBytes(), validatorPOWChallengeTemplate)

	resp.Body.AdvanceW(len(`{"c":`))
	// the following conversion is faster than sprintf but also way uglier, I am sorry.
	// 48 is the ASCII code for '0', adding lc.Countdown will give us the single correct digit.
	copy(resp.Body.WriteNBytes(1), []byte{byte(48 + lc.Countdown)})
	resp.Body.AdvanceW(len(`,"t":1,"r":"`))
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
	// req.Body should be at least validatorPOWMinSolutionLength
	if len(req.Body) <= validatorPOWMinSolutionLength {
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

	if !bytes.HasPrefix(sum, []byte{0x00, 0x00}) {
		return errInvalidSolution
	}

	return nil
}

var errInvalidSolution = fmt.Errorf("invalid solution")

func validatorPOW(b *Berghain, req *ValidatorRequest, resp *ValidatorResponse) error {
	var p powValidator

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
