package berghain

import (
	"encoding/json"
	"net/http"
	"net/netip"
	"testing"
	"time"
)

func Test_validatorPOWWorker(t *testing.T) {
	bh := NewBerghain(generateSecret(t))

	bh.Levels = []*LevelConfig{
		{
			Duration:   time.Minute,
			Type:       ValidationTypePOWWorker,
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

	if err := validatorPOWWorker(bh, req, resp); err != nil {
		t.Fatalf("validator failed: %v", err)
	}

	body := resp.Body.ReadBytes()

	// The worker challenge must advertise type 2 (otherwise byte-identical to POW).
	var adv struct {
		T int `json:"t"`
	}
	if err := json.Unmarshal(body, &adv); err != nil {
		t.Fatalf("decoding challenge: %v", err)
	}
	if adv.T != 2 {
		t.Errorf("challenge type = %d, want 2", adv.T)
	}

	solution, err := solvePOW(t, body)
	if err != nil {
		t.Fatalf("while solving pow: %v", err)
	}

	req.Method = http.MethodPost
	req.Body = solution
	if err := validatorPOWWorker(bh, req, resp); err != nil {
		t.Fatalf("validator failed: %v", err)
	}

	if err := bh.IsValidCookie(*req.Identifier, resp.Token.ReadBytes()); err != nil {
		t.Errorf("invalid cookie: %v", err)
	}
}
