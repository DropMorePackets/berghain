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

func TestSourceLogID(t *testing.T) {
	secret := generateSecret(t)
	bh := NewBerghain(secret)
	address := netip.MustParseAddr("192.0.2.1")

	got := bh.SourceLogID(address)
	if got != bh.SourceLogID(address) {
		t.Fatal("SourceLogID is not deterministic")
	}
	if len(got) != 32 {
		t.Fatalf("SourceLogID length = %d, want 32", len(got))
	}
	if got == address.String() {
		t.Fatal("SourceLogID returned the plaintext address")
	}
	if got == bh.SourceLogID(netip.MustParseAddr("192.0.2.2")) {
		t.Fatal("SourceLogID collided for distinct addresses")
	}
	if got == NewBerghain(generateSecret(t)).SourceLogID(address) {
		t.Fatal("SourceLogID does not depend on the secret")
	}
	if invalid := bh.SourceLogID(netip.Addr{}); invalid != "" {
		t.Fatalf("SourceLogID(invalid) = %q, want empty", invalid)
	}
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
	req.SupportID = []byte("bh@123e4567-e89b-12d3-a456-426614174000")

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
