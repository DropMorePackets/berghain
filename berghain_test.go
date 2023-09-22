package berghain

import (
	"crypto/rand"
	"net/netip"
	"testing"
	"time"
)

func generateSecret(tb testing.TB) []byte {
	tb.Helper()
	b := make([]byte, 32)
	_, err := rand.Read(b)
	if err != nil {
		tb.Fatal(err)
	}
	return b
}

func TestBerghain(t *testing.T) {
	bh := NewBerghain(generateSecret(t))
	bh.Levels = []*LevelConfig{
		{
			Duration: time.Minute,
			Type:     ValidationTypeNone,
		},
	}

	req := AcquireValidatorRequest()
	req.Identifier = &RequestIdentifier{
		SrcAddr: netip.MustParseAddr("1.2.3.4"),
		Host:    []byte("example.com"),
		Level:   1,
	}
	req.Method = "GET"

	resp := AcquireValidatorResponse()
	err := bh.LevelConfig(req.Identifier.Level).Type.RunValidator(bh, req, resp)
	if err != nil {
		t.Errorf("validator failed: %v", err)
	}

	err = bh.IsValidCookie(*req.Identifier, resp.Token.ReadBytes())
	if err != nil {
		t.Errorf("invalid cookie: %v", err)
	}
}
