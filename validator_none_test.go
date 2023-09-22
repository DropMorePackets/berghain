package berghain

import (
	"bytes"
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

	err := validatorNone(bh, req, resp)
	if err != nil {
		t.Errorf("validator failed: %v", err)
	}

	if !bytes.Equal(resp.Body.ReadBytes(), []byte(validatorNoneResponse)) {
		t.Errorf("invalid response: %s != %s", validatorNoneResponse, resp.Body.ReadBytes())
	}

	err = bh.IsValidCookie(*req.Identifier, resp.Token.ReadBytes())
	if err != nil {
		t.Errorf("invalid cookie: %v", err)
	}
}
