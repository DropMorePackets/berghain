package berghain

import (
	"net/http"
	"net/netip"
	"testing"
	"time"
)

func TestValidatorPOWWorker(t *testing.T) {
	bh := NewBerghain(generateSecret(t))
	bh.Levels = []*LevelConfig{{
		Duration:   time.Minute,
		Type:       ValidationTypePOWWorker,
		Difficulty: 8,
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

	if err := ValidationTypePOWWorker.RunValidator(bh, req, resp); err != nil {
		t.Fatalf("creating worker challenge: %v", err)
	}

	challenge, err := decodePOWChallenge(resp.Body.ReadBytes())
	if err != nil {
		t.Fatalf("decoding worker challenge: %v", err)
	}
	if challenge.T != 2 {
		t.Errorf("challenge type = %d, want 2", challenge.T)
	}
	if challenge.D != "08" {
		t.Errorf("challenge difficulty = %q, want %q", challenge.D, "08")
	}
	if resp.Body.Len() != len(validatorPOWWorkerChallengeTemplate) {
		t.Errorf("challenge response length = %d, want %d", resp.Body.Len(), len(validatorPOWWorkerChallengeTemplate))
	}

	solution, err := solvePOW(t, resp.Body.ReadBytes())
	if err != nil {
		t.Fatalf("solving worker challenge: %v", err)
	}

	req.Method = http.MethodPost
	req.Body = solution
	if err := ValidationTypePOWWorker.RunValidator(bh, req, resp); err != nil {
		t.Fatalf("validating worker challenge: %v", err)
	}
	if err := bh.IsValidCookie(*req.Identifier, resp.Token.ReadBytes()); err != nil {
		t.Errorf("validating issued cookie: %v", err)
	}
}
