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
		R string `json:"r"`
		S string `json:"s"`
	}

	var p powChallenge
	if err := json.NewDecoder(bytes.NewReader(b)).Decode(&p); err != nil {
		return nil, err
	}

	if p.T != 1 {
		return nil, fmt.Errorf("invalid challenge type: %d", p.T)
	}

	h := NewZeroHasher(hashAlgo())
	for i := 0; true; i++ {
		h.Write([]byte(p.R))

		is := strconv.Itoa(i)
		h.Write([]byte(is))

		if bytes.HasPrefix(h.Sum(nil), []byte{0x00, 0x00}) {
			solution := p.R + "-" + p.S + "-" + is
			return []byte(solution), nil
		}

		h.Reset()
	}
	panic("unreachable")
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
