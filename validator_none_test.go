package berghain

import (
	"encoding/json"
	"net/http"
	"net/netip"
	"testing"
	"time"
)

func Test_validatorNone(t *testing.T) {
	bh := NewBerghain(generateSecret(t))

	bh.Levels = []*LevelConfig{
		{
			Duration: time.Minute,
			Type:     ValidationTypeNone,
		},
	}

	req, resp := AcquireValidatorRequest(), AcquireValidatorResponse()
	req.Identifier = &RequestIdentifier{
		SrcAddr: netip.MustParseAddr("1.2.3.4"),
		Host:    []byte("example.com"),
		Level:   1,
	}
	req.Method = http.MethodGet
	req.SupportID = []byte("bh@123e4567-e89b-12d3-a456-426614174000")

	err := validatorNone(bh, req, resp)
	if err != nil {
		t.Errorf("validator failed: %v", err)
	}

	var challenge struct {
		Countdown int    `json:"c"`
		Type      int    `json:"t"`
		SupportID string `json:"i"`
	}
	if err := json.Unmarshal(resp.Body.ReadBytes(), &challenge); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if challenge.Type != 0 || challenge.SupportID != string(req.SupportID) {
		t.Errorf("invalid response: %+v", challenge)
	}

	err = bh.IsValidCookie(*req.Identifier, resp.Token.ReadBytes())
	if err != nil {
		t.Errorf("invalid cookie: %v", err)
	}
}
