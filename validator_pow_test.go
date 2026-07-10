package berghain

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/netip"
	"strconv"
	"testing"
	"time"
)

type powChallenge struct {
	T int    `json:"t"`
	D string `json:"d"`
	R string `json:"r"`
	S string `json:"s"`
}

func decodePOWChallenge(b []byte) (powChallenge, error) {
	var challenge powChallenge
	err := json.NewDecoder(bytes.NewReader(b)).Decode(&challenge)
	return challenge, err
}

func solvePOW(tb testing.TB, b []byte) ([]byte, error) {
	tb.Helper()

	p, err := decodePOWChallenge(b)
	if err != nil {
		return nil, err
	}

	if p.T != 1 && p.T != 2 {
		return nil, fmt.Errorf("invalid challenge type: %d", p.T)
	}

	difficulty, err := strconv.ParseInt(p.D, 16, 0)
	if err != nil {
		return nil, fmt.Errorf("invalid difficulty %q: %w", p.D, err)
	}

	h := NewZeroHasher(hashAlgo())
	for i := 0; true; i++ {
		h.Write([]byte(p.R))
		h.Write([]byte(p.S))

		is := strconv.Itoa(i)
		h.Write([]byte(is))

		if hasLeadingZeroBits(h.Sum(nil), int(difficulty)) {
			solution := p.R + "-" + p.S + "-" + is
			return []byte(solution), nil
		}

		h.Reset()
	}
	panic("unreachable")
}

func powDigest(random, signature, nonce string) []byte {
	h := hashAlgo()
	_, _ = h.Write([]byte(random))
	_, _ = h.Write([]byte(signature))
	_, _ = h.Write([]byte(nonce))
	return h.Sum(nil)
}

func challengeSignature(tb testing.TB, b *Berghain, identifier RequestIdentifier, random string) string {
	tb.Helper()

	h := b.acquireHMAC()
	defer b.releaseHMAC(h)
	if _, err := identifier.WriteTo(h); err != nil {
		tb.Fatal(err)
	}
	if _, err := h.Write([]byte(random)); err != nil {
		tb.Fatal(err)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func TestValidatePOWDifficulty(t *testing.T) {
	for _, difficulty := range []int{MinPOWDifficulty, DefaultPOWDifficulty, MaxPOWDifficulty} {
		if err := ValidatePOWDifficulty(difficulty); err != nil {
			t.Errorf("ValidatePOWDifficulty(%d) returned %v", difficulty, err)
		}
	}
	for _, difficulty := range []int{-1, 0, MaxPOWDifficulty + 1} {
		if err := ValidatePOWDifficulty(difficulty); err == nil {
			t.Errorf("ValidatePOWDifficulty(%d) unexpectedly succeeded", difficulty)
		}
	}
}

func TestEffectivePOWDifficulty(t *testing.T) {
	if got, err := effectivePOWDifficulty(0); err != nil || got != DefaultPOWDifficulty {
		t.Errorf("effectivePOWDifficulty(0) = %d, %v; want %d, nil", got, err, DefaultPOWDifficulty)
	}
	for _, difficulty := range []int{-1, MaxPOWDifficulty + 1} {
		if _, err := effectivePOWDifficulty(difficulty); err == nil {
			t.Errorf("effectivePOWDifficulty(%d) unexpectedly succeeded", difficulty)
		}
	}
}

func TestHasLeadingZeroBits(t *testing.T) {
	tests := []struct {
		name string
		b    []byte
		bits int
		want bool
	}{
		{name: "zero bits", b: []byte{0xff}, bits: 0, want: true},
		{name: "partial byte", b: []byte{0x7f}, bits: 1, want: true},
		{name: "partial byte set", b: []byte{0x80}, bits: 1, want: false},
		{name: "whole byte", b: []byte{0x00, 0xff}, bits: 8, want: true},
		{name: "whole and partial", b: []byte{0x00, 0x0f}, bits: 12, want: true},
		{name: "whole and partial set", b: []byte{0x00, 0x10}, bits: 12, want: false},
		{name: "too many bits", b: []byte{0x00}, bits: 9, want: false},
		{name: "negative bits", b: []byte{0x00}, bits: -1, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasLeadingZeroBits(tt.b, tt.bits); got != tt.want {
				t.Errorf("hasLeadingZeroBits(%x, %d) = %t, want %t", tt.b, tt.bits, got, tt.want)
			}
		})
	}
}

func TestValidatorPOWDifficulty(t *testing.T) {
	for _, difficulty := range []int{0, 1, 8, 12, MaxPOWDifficulty} {
		t.Run(strconv.Itoa(difficulty), func(t *testing.T) {
			bh := NewBerghain(generateSecret(t))
			bh.Levels = []*LevelConfig{{
				Duration:   time.Minute,
				Type:       ValidationTypePOW,
				Difficulty: difficulty,
			}}
			req, resp := AcquireValidatorRequest(), AcquireValidatorResponse()
			defer ReleaseValidatorRequest(req)
			defer ReleaseValidatorResponse(resp)
			req.Identifier = &RequestIdentifier{
				SrcAddr: netip.MustParseAddr("1.2.3.4"),
				Host:    []byte("example.com"),
				Level:   1,
			}
			req.Method = http.MethodGet

			if err := validatorPOW(bh, req, resp); err != nil {
				t.Fatalf("validatorPOW: %v", err)
			}
			challenge, err := decodePOWChallenge(resp.Body.ReadBytes())
			if err != nil {
				t.Fatal(err)
			}
			wantDifficulty := difficulty
			if wantDifficulty == 0 {
				wantDifficulty = DefaultPOWDifficulty
			}
			if want := fmt.Sprintf("%02x", wantDifficulty); challenge.D != want {
				t.Errorf("difficulty = %q, want %q", challenge.D, want)
			}

			// Avoid attempting intentionally impractical work at the maximum.
			if difficulty == MaxPOWDifficulty {
				return
			}
			solution, err := solvePOW(t, resp.Body.ReadBytes())
			if err != nil {
				t.Fatal(err)
			}
			req.Method = http.MethodPost
			req.Body = solution
			if err := validatorPOW(bh, req, resp); err != nil {
				t.Fatalf("validating solution: %v", err)
			}
		})
	}
}

func TestValidatorPOWRejectsInvalidDirectDifficulty(t *testing.T) {
	for _, difficulty := range []int{-1, MaxPOWDifficulty + 1} {
		bh := NewBerghain(generateSecret(t))
		bh.Levels = []*LevelConfig{{Type: ValidationTypePOW, Difficulty: difficulty}}
		req, resp := AcquireValidatorRequest(), AcquireValidatorResponse()
		req.Identifier = &RequestIdentifier{Level: 1}
		req.Method = http.MethodGet
		if err := validatorPOW(bh, req, resp); err == nil {
			t.Errorf("difficulty %d unexpectedly succeeded", difficulty)
		}
		ReleaseValidatorRequest(req)
		ReleaseValidatorResponse(resp)
	}
}

func TestValidatorPOWBindsWorkToSignature(t *testing.T) {
	const difficulty = 8

	bh := NewBerghain(generateSecret(t))
	bh.Levels = []*LevelConfig{{Duration: time.Minute, Type: ValidationTypePOW, Difficulty: difficulty}}
	firstIdentifier := RequestIdentifier{
		SrcAddr: netip.MustParseAddr("1.2.3.4"),
		Host:    []byte("example.com"),
		Level:   1,
	}
	secondIdentifier := firstIdentifier
	secondIdentifier.SrcAddr = netip.MustParseAddr("1.2.3.5")

	req, resp := AcquireValidatorRequest(), AcquireValidatorResponse()
	defer ReleaseValidatorRequest(req)
	defer ReleaseValidatorResponse(resp)
	req.Identifier = &firstIdentifier
	req.Method = http.MethodGet
	if err := validatorPOW(bh, req, resp); err != nil {
		t.Fatal(err)
	}
	challenge, err := decodePOWChallenge(resp.Body.ReadBytes())
	if err != nil {
		t.Fatal(err)
	}
	secondSignature := challengeSignature(t, bh, secondIdentifier, challenge.R)

	var nonce string
	for i := 0; ; i++ {
		candidate := strconv.Itoa(i)
		if hasLeadingZeroBits(powDigest(challenge.R, challenge.S, candidate), difficulty) &&
			!hasLeadingZeroBits(powDigest(challenge.R, secondSignature, candidate), difficulty) {
			nonce = candidate
			break
		}
	}

	req.Identifier = &secondIdentifier
	req.Method = http.MethodPost
	req.Body = []byte(challenge.R + "-" + secondSignature + "-" + nonce)
	if err := validatorPOW(bh, req, resp); err != errInvalidSolution {
		t.Fatalf("reused work returned %v, want %v", err, errInvalidSolution)
	}
}

func Test_validatorPOW(t *testing.T) {
	bh := NewBerghain(generateSecret(t))

	bh.Levels = []*LevelConfig{
		{
			Duration: time.Minute,
			Type:     ValidationTypePOW,
		},
	}

	req, resp := AcquireValidatorRequest(), AcquireValidatorResponse()
	req.Identifier = &RequestIdentifier{
		SrcAddr: netip.MustParseAddr("1.2.3.4"),
		Host:    []byte("example.com"),
		Level:   1,
	}
	req.Method = http.MethodGet

	if err := validatorPOW(bh, req, resp); err != nil {
		t.Errorf("validator failed: %v", err)
	}

	if resp.Body.Len() != len(validatorPOWChallengeTemplate) {
		t.Errorf("invalid challenge response length: %d != %d", len(validatorPOWChallengeTemplate), resp.Body.Len())
	}

	solution, err := solvePOW(t, resp.Body.ReadBytes())
	if err != nil {
		t.Errorf("while solving pow: %v", err)
	}

	// Do another request but this time as POST and with the solution.
	req.Method = http.MethodPost
	req.Body = solution
	if err := validatorPOW(bh, req, resp); err != nil {
		t.Errorf("validator failed: %v", err)
	}

	if resp.Body.Len() != 0 {
		t.Errorf("invalid response length: %d != %d", 0, resp.Body.Len())
	}

	err = bh.IsValidCookie(*req.Identifier, resp.Token.ReadBytes())
	if err != nil {
		t.Errorf("invalid cookie: %v", err)
	}
}

func Test_validatorPOW_unique(t *testing.T) {
	bh := NewBerghain(generateSecret(t))

	bh.Levels = []*LevelConfig{
		{
			Duration: time.Minute,
			Type:     ValidationTypePOW,
		},
	}

	req, resp := AcquireValidatorRequest(), AcquireValidatorResponse()
	req.Identifier = &RequestIdentifier{
		SrcAddr: netip.MustParseAddr("1.2.3.4"),
		Host:    []byte("example.com"),
		Level:   1,
	}
	req.Method = http.MethodGet

	if err := validatorPOW(bh, req, resp); err != nil {
		t.Errorf("validator failed: %v", err)
	}

	if resp.Body.Len() != len(validatorPOWChallengeTemplate) {
		t.Errorf("invalid challenge response length: %d != %d", len(validatorPOWChallengeTemplate), resp.Body.Len())
	}

	solution, err := solvePOW(t, resp.Body.ReadBytes())
	if err != nil {
		t.Errorf("while solving pow: %v", err)
	}

	// Do another request but this time as POST and with the solution.
	req.Method = http.MethodPost
	req.Body = solution
	req.Identifier.SrcAddr = netip.MustParseAddr("1.2.3.5")
	if err := validatorPOW(bh, req, resp); err == nil {
		t.Errorf("validator should have failed")
	}
}

func Benchmark_validatorPOW_GET(b *testing.B) {
	bh := NewBerghain(generateSecret(b))

	bh.Levels = []*LevelConfig{
		{
			Duration: time.Minute,
			Type:     ValidationTypePOW,
		},
	}

	ri := RequestIdentifier{
		SrcAddr: netip.MustParseAddr("1.2.3.4"),
		Host:    []byte("example.com"),
		Level:   1,
	}

	req := AcquireValidatorRequest()
	defer ReleaseValidatorRequest(req)

	req.Identifier = &ri
	req.Method = http.MethodGet

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			resp := AcquireValidatorResponse()
			if err := validatorPOW(bh, req, resp); err != nil {
				b.Errorf("validator failed: %v", err)
			}

			ReleaseValidatorResponse(resp)
		}
	})
}

func Benchmark_validatorPOW_POST(b *testing.B) {
	bh := NewBerghain(generateSecret(b))

	bh.Levels = []*LevelConfig{
		{
			Duration: time.Minute,
			Type:     ValidationTypePOW,
		},
	}

	ri := RequestIdentifier{
		SrcAddr: netip.MustParseAddr("1.2.3.4"),
		Host:    []byte("example.com"),
		Level:   1,
	}

	req, resp := AcquireValidatorRequest(), AcquireValidatorResponse()
	defer ReleaseValidatorRequest(req)
	req.Method = http.MethodGet

	if err := validatorPOW(bh, req, resp); err != nil {
		b.Errorf("validator failed: %v", err)
	}

	solution, err := solvePOW(b, resp.Body.ReadBytes())
	if err != nil {
		b.Errorf("while solving pow: %v", err)
	}

	ReleaseValidatorResponse(resp)
	req.Identifier = &ri
	req.Method = http.MethodPost
	req.Body = solution

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			resp := AcquireValidatorResponse()

			if err := validatorPOW(bh, req, resp); err != nil {
				b.Errorf("validator failed: %v", err)
			}

			ReleaseValidatorResponse(resp)
		}
	})
}
