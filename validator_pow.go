package berghain

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"hash"
	"math/rand"
	"net/http"
	"sync"

	"github.com/dropmorepackets/haproxy-go/pkg/buffer"
)

var randPool = sync.Pool{
	New: func() interface{} {
		return rand.New(rand.NewSource(rand.Int63()))
	},
}

func writeRandomASCIIBytes(b []byte) {
	r := randPool.Get().(*rand.Rand)
	defer randPool.Put(r)

	r.Read(b)

	for i := 0; i < len(b); i++ {
		b[i] = 65 + (b[i] % 25)
	}
}

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
	validatorPOWRandom            = "00000000"
	validatorPOWHash              = "0000000000000000000000000000000000000000000000000000000000000000"
	validatorPOWChallengeTemplate = `{"t": 1, "r": "` + validatorPOWRandom + `", "s": "` + validatorPOWHash + `"}`
	validatorPOWMinSolutionLength = len(validatorPOWRandom) + 1 + len(validatorPOWHash) + 1 + 1
)

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

	copy(resp.Body.WriteBytes(), validatorPOWChallengeTemplate)
	resp.Body.AdvanceW(15)
	randArea := resp.Body.WriteNBytes(len(validatorPOWRandom))
	resp.Body.AdvanceW(9)
	hexArea := resp.Body.WriteNBytes(hex.EncodedLen(h.Size()))
	resp.Body.AdvanceW(2)

	writeRandomASCIIBytes(randArea)
	h.Write(randArea)
	hex.Encode(hexArea, h.Sum(nil))

	return nil
}

func (powValidator) isValid(b *Berghain, req *ValidatorRequest, resp *ValidatorResponse) bool {
	// req.Body should look like this:
	// NCUEKLGC-5d2702c936458bf9b962617673f0825ee3b51a84a42fc9f591d8c67516442a2f-61764
	if len(req.Body) <= validatorPOWMinSolutionLength {
		// invalid solution data
		return false
	}

	body := buffer.NewSliceBufferWithSlice(req.Body)

	randArea := body.ReadNBytes(len(validatorPOWRandom))
	body.AdvanceR(1) // Skip padding character
	sumArea := body.ReadNBytes(len(validatorPOWHash))
	body.AdvanceR(1) // Skip padding character
	solArea := body.ReadBytes()

	h := b.acquireHMAC()
	defer b.releaseHMAC(h)

	h.Write(randArea)

	// we use the response body temporarily as a buffer
	defer resp.Body.Reset()

	ourSum := resp.Body.WriteNBytes(hex.EncodedLen(h.Size()))
	hex.Encode(ourSum, h.Sum(nil))

	if !bytes.Equal(ourSum, sumArea) {
		// invalid hash in solution
		return false
	}

	sha := acquireSHA256()
	defer releaseSHA256(sha)

	sha.Write(randArea)
	sha.Write(solArea)
	sum := sha.Sum(nil)

	return bytes.HasPrefix(sum, []byte{0x00, 0x00})
}

var errInvalidSolution = fmt.Errorf("invalid solution")

func validatorPOW(b *Berghain, req *ValidatorRequest, resp *ValidatorResponse) error {
	var p powValidator

	switch req.Method {
	case http.MethodPost:
		if p.isValid(b, req, resp) {
			return req.Identifier.ToCookie(b, resp.Token)
		}
		return errInvalidSolution
	case http.MethodGet:
		return p.onNew(b, req, resp)
	}

	return errInvalidMethod
}
