package berghain

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
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
	validatorPOWTimestamp         = "0000000000000000"
	validatorPOWRandom            = validatorPOWTimestamp + "bh@00000000-0000-4000-8000-000000000000"
	validatorPOWHash              = "0000000000000000000000000000000000000000000000000000000000000000"
	validatorPOWMinSolutionLength = len(validatorPOWRandom + "-" + validatorPOWHash + "-0")
	validatorPOWMaxSolutionLength = validatorPOWMinSolutionLength + 19
)

var validatorPOWChallenge powChallengeTemplate

type powChallengeTemplate struct {
	once sync.Once
	raw  string

	// Slot accessors; valid after init ran.
	Countdown jsonSlot // 1 byte, '0'..'9'
	Random    jsonSlot // 16 hex timestamp chars + 39 support-ID chars
	Sum       jsonSlot // 64 hex chars
	SupportID jsonSlot // 39 support-ID chars, echoed for the challenge page
}

func (t *powChallengeTemplate) init() {
	t.once.Do(func() {
		const (
			countdown = "0"
			echoID    = "bh@00000000-0000-4000-8000-000000000001"
		)
		t.raw = mustJSONEncodeString(struct {
			Countdown json.RawMessage `json:"c"`
			Type      int             `json:"t"`
			Random    string          `json:"r"`
			Hash      string          `json:"s"`
			SupportID string          `json:"i"`
		}{
			Countdown: json.RawMessage(countdown),
			Type:      1,
			Random:    validatorPOWRandom,
			Hash:      validatorPOWHash,
			SupportID: echoID,
		})

		loc := slotLocator{doc: t.raw}
		t.Countdown = loc.next(countdown)
		t.Random = loc.next(validatorPOWRandom)
		t.Sum = loc.next(validatorPOWHash)
		t.SupportID = loc.next(echoID)
	})
}

// Render appends the template to body and returns the rendered document.
// Slot accessors take this document and return writable views into it.
func (t *powChallengeTemplate) Render(body *buffer.SliceBuffer) []byte {
	t.init()
	return renderTemplate(body, t.raw)
}

// This prevents invalid template strings by validatoring them on start
var _ = func() bool {
	h := hashAlgo()
	if len(validatorPOWHash) != hex.EncodedLen(h.Size()) {
		panic("invalid pow hash length")
	}
	if !ValidSupportID([]byte(validatorPOWRandom[len(validatorPOWTimestamp):])) {
		panic("invalid pow support ID placeholder")
	}
	validatorPOWChallenge.init()
	return true
}()

func (powValidator) onNew(b *Berghain, req *ValidatorRequest, resp *ValidatorResponse) error {
	if !ValidSupportID(req.SupportID) {
		return ErrInvalidLength
	}

	h := b.acquireHMAC()
	defer b.releaseHMAC(h)

	lc := b.LevelConfig(req.Identifier.Level)
	tpl := &validatorPOWChallenge
	doc := tpl.Render(resp.Body)

	tpl.Countdown(doc)[0] = byte('0' + lc.Countdown)

	random := tpl.Random(doc)
	var ts [8]byte
	binary.LittleEndian.PutUint64(ts[:], uint64(tc.Now().Add(lc.Duration).Unix()))
	hex.Encode(random[:len(validatorPOWTimestamp)], ts[:])
	copy(random[len(validatorPOWTimestamp):], req.SupportID)

	copy(tpl.SupportID(doc), req.SupportID)

	// Write identifier to hash to ensure uniqueness
	req.Identifier.WriteTo(h)
	h.Write(random)

	hex.Encode(tpl.Sum(doc), h.Sum(nil))

	return nil
}

func (powValidator) isValid(b *Berghain, req *ValidatorRequest, resp *ValidatorResponse) error {
	req.SupportID = nil
	if len(req.Body) < validatorPOWMinSolutionLength || len(req.Body) > validatorPOWMaxSolutionLength {
		return ErrInvalidLength
	}

	body := buffer.NewSliceBufferWithSlice(req.Body)
	randomArea := body.ReadNBytes(len(validatorPOWRandom))
	if separator := body.ReadNBytes(1); len(separator) != 1 || separator[0] != '-' {
		return ErrInvalidLength
	}
	sumArea := body.ReadNBytes(len(validatorPOWHash))
	if separator := body.ReadNBytes(1); len(separator) != 1 || separator[0] != '-' {
		return ErrInvalidLength
	}
	solArea := body.ReadBytes()
	for _, c := range solArea {
		if c < '0' || c > '9' {
			return ErrInvalidLength
		}
	}

	h := b.acquireHMAC()
	defer b.releaseHMAC(h)

	// Write identifier to hash to ensure uniqueness
	req.Identifier.WriteTo(h)
	h.Write(randomArea)

	// we use the response body temporarily as a buffer
	defer resp.Body.Reset()

	ourSum := resp.Body.WriteNBytes(hex.EncodedLen(h.Size()))
	hex.Encode(ourSum, h.Sum(nil))

	if !bytes.Equal(ourSum, sumArea) {
		// invalid hash in solution
		return ErrInvalidHMAC
	}
	resp.Body.Reset()

	supportID := randomArea[len(validatorPOWTimestamp):]
	if !ValidSupportID(supportID) {
		return ErrInvalidLength
	}
	req.SupportID = supportID
	timestampArea := randomArea[:len(validatorPOWTimestamp)]

	expirArea := resp.Body.WriteNBytes(hex.DecodedLen(len(validatorPOWTimestamp)))
	if _, err := hex.Decode(expirArea, timestampArea); err != nil {
		return err
	}

	// Untrusted input is decoded and compared!
	if uint64(tc.Now().Unix()) > binary.LittleEndian.Uint64(expirArea) {
		return ErrExpired
	}

	sha := acquireSHA256()
	defer releaseSHA256(sha)

	sha.Write(randomArea)
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
