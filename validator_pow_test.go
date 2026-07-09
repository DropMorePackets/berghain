package berghain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/netip"
	"strconv"
	"testing"
	"time"
)

func solvePOW(tb testing.TB, b []byte) ([]byte, error) {
	tb.Helper()

	type powChallenge struct {
		T int    `json:"t"`
		D string `json:"d"`
		R string `json:"r"`
		S string `json:"s"`
	}

	var p powChallenge
	if err := json.NewDecoder(bytes.NewReader(b)).Decode(&p); err != nil {
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

func Test_hasLeadingZeroBits(t *testing.T) {
	tests := []struct {
		b    []byte
		bits int
		want bool
	}{
		{[]byte{0xff}, 0, true},
		{[]byte{0x7f}, 1, true},
		{[]byte{0x80}, 1, false},
		{[]byte{0x00}, 8, true},
		{[]byte{0x01}, 8, false},
		{[]byte{0x00, 0x0f}, 12, true},
		{[]byte{0x00, 0x10}, 12, false},
		{[]byte{0x00, 0x00}, 16, true},
		{[]byte{0x00, 0x00, 0x80}, 17, false},
		{[]byte{0x00, 0x00, 0x00}, 17, true},
	}
	for _, tt := range tests {
		if got := hasLeadingZeroBits(tt.b, tt.bits); got != tt.want {
			t.Errorf("hasLeadingZeroBits(%x, %d) = %v, want %v", tt.b, tt.bits, got, tt.want)
		}
	}
}

func Test_effectivePOWDifficulty(t *testing.T) {
	for in, want := range map[int]int{-5: 16, 0: 16, 1: 1, 16: 16, 200: 200} {
		if got := effectivePOWDifficulty(in); got != want {
			t.Errorf("effectivePOWDifficulty(%d) = %d, want %d", in, got, want)
		}
	}
}

// A POW level with Difficulty unset (Go zero value) must fall back to the
// historic 16-bit target, not a zero-work "accept any nonce" challenge.
func Test_validatorPOW_defaultDifficulty(t *testing.T) {
	bh := NewBerghain(generateSecret(t))
	bh.Levels = []*LevelConfig{
		{Duration: time.Minute, Type: ValidationTypePOW}, // Difficulty intentionally unset
	}

	req, resp := AcquireValidatorRequest(), AcquireValidatorResponse()
	req.Identifier = &RequestIdentifier{
		SrcAddr: netip.MustParseAddr("1.2.3.4"),
		Host:    []byte("example.com"),
		Level:   1,
	}
	req.Method = http.MethodGet
	if err := validatorPOW(bh, req, resp); err != nil {
		t.Fatalf("validator failed: %v", err)
	}

	body := resp.Body.ReadBytes()
	var adv struct {
		D string `json:"d"`
	}
	if err := json.Unmarshal(body, &adv); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if adv.D != "10" {
		t.Errorf("advertised difficulty = %q, want 10 (16 bits default)", adv.D)
	}

	solution, err := solvePOW(t, body)
	if err != nil {
		t.Fatalf("solve: %v", err)
	}
	req.Method = http.MethodPost
	req.Body = solution
	if err := validatorPOW(bh, req, resp); err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	if err := bh.IsValidCookie(*req.Identifier, resp.Token.ReadBytes()); err != nil {
		t.Errorf("invalid cookie: %v", err)
	}
}

func Test_validatorPOW_difficulty(t *testing.T) {
	// 1 exercises the sub-byte remainder path, 8 the whole-byte path, 12 both.
	for _, difficulty := range []int{1, 8, 12} {
		difficulty := difficulty
		t.Run(fmt.Sprintf("bits=%d", difficulty), func(t *testing.T) {
			bh := NewBerghain(generateSecret(t))
			bh.Levels = []*LevelConfig{
				{
					Duration:   time.Minute,
					Type:       ValidationTypePOW,
					Difficulty: difficulty,
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
				t.Fatalf("validator failed: %v", err)
			}

			body := resp.Body.ReadBytes()

			var adv struct {
				D string `json:"d"`
			}
			if err := json.Unmarshal(body, &adv); err != nil {
				t.Fatalf("decoding challenge: %v", err)
			}
			if want := fmt.Sprintf("%02x", difficulty); adv.D != want {
				t.Errorf("advertised difficulty = %q, want %q", adv.D, want)
			}

			solution, err := solvePOW(t, body)
			if err != nil {
				t.Fatalf("while solving pow: %v", err)
			}

			req.Method = http.MethodPost
			req.Body = solution
			if err := validatorPOW(bh, req, resp); err != nil {
				t.Fatalf("validator failed: %v", err)
			}

			if err := bh.IsValidCookie(*req.Identifier, resp.Token.ReadBytes()); err != nil {
				t.Errorf("invalid cookie: %v", err)
			}
		})
	}
}

func Test_validatorPOW(t *testing.T) {
	bh := NewBerghain(generateSecret(t))

	bh.Levels = []*LevelConfig{
		{
			Duration:   time.Minute,
			Type:       ValidationTypePOW,
			Difficulty: 16,
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
			Duration:   time.Minute,
			Type:       ValidationTypePOW,
			Difficulty: 16,
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
			Duration:   time.Minute,
			Type:       ValidationTypePOW,
			Difficulty: 16,
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
			Duration:   time.Minute,
			Type:       ValidationTypePOW,
			Difficulty: 16,
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
