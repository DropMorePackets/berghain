package berghain

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"
)

type captchaVerdict struct {
	Success    bool     `json:"success"`
	Hostname   string   `json:"hostname"`
	ErrorCodes []string `json:"error-codes,omitempty"`
}

func newCaptchaBerghain(tb testing.TB, verifyURL string) *Berghain {
	tb.Helper()

	bh := NewBerghain(generateSecret(tb))
	bh.Levels = []*LevelConfig{
		{
			Duration:         time.Minute,
			Type:             ValidationTypeTurnstile,
			CaptchaSitekey:   "sitekey-under-test",
			CaptchaSecret:    "secret-under-test",
			CaptchaVerifyURL: verifyURL,
		},
	}

	return bh
}

func newCaptchaIdentifier() *RequestIdentifier {
	return &RequestIdentifier{
		SrcAddr: netip.MustParseAddr("1.2.3.4"),
		Host:    []byte("example.com"),
		Level:   1,
	}
}

func newSiteverifyStub(t *testing.T, verdict captchaVerdict, wantToken string) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("unexpected siteverify method: %s", r.Method)
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("parsing siteverify form: %v", err)
		}
		if got := r.PostForm.Get("secret"); got != "secret-under-test" {
			t.Errorf("unexpected siteverify secret: %q", got)
		}
		if got := r.PostForm.Get("response"); got != wantToken {
			t.Errorf("unexpected siteverify response token: %q", got)
		}
		if got := r.PostForm.Get("remoteip"); got != "1.2.3.4" {
			t.Errorf("unexpected siteverify remoteip: %q", got)
		}
		if err := json.NewEncoder(w).Encode(verdict); err != nil {
			t.Errorf("encoding siteverify verdict: %v", err)
		}
	}))
}

func Test_validatorCaptcha_GET(t *testing.T) {
	bh := newCaptchaBerghain(t, "http://invalid.invalid")

	req, resp := AcquireValidatorRequest(), AcquireValidatorResponse()
	defer ReleaseValidatorRequest(req)
	defer ReleaseValidatorResponse(resp)

	req.Identifier = newCaptchaIdentifier()
	req.Method = http.MethodGet

	if err := validatorCaptcha(bh, req, resp); err != nil {
		t.Fatalf("validator failed: %v", err)
	}

	var challenge struct {
		Countdown int    `json:"c"`
		Type      int    `json:"t"`
		Sitekey   string `json:"k"`
	}
	if err := json.NewDecoder(bytes.NewReader(resp.Body.ReadBytes())).Decode(&challenge); err != nil {
		t.Fatalf("decoding challenge: %v", err)
	}

	if challenge.Type != 3 {
		t.Errorf("invalid challenge type: %d != 3", challenge.Type)
	}
	if challenge.Sitekey != "sitekey-under-test" {
		t.Errorf("invalid challenge sitekey: %q", challenge.Sitekey)
	}
	if challenge.Countdown != 0 {
		t.Errorf("invalid challenge countdown: %d != 0", challenge.Countdown)
	}
	if resp.Token.Len() != 0 {
		t.Errorf("challenge must not issue a token")
	}
}

func Test_validatorCaptcha_POST(t *testing.T) {
	const token = "widget-response-token"

	stub := newSiteverifyStub(t, captchaVerdict{Success: true, Hostname: "example.com"}, token)
	defer stub.Close()

	bh := newCaptchaBerghain(t, stub.URL)

	req, resp := AcquireValidatorRequest(), AcquireValidatorResponse()
	defer ReleaseValidatorRequest(req)
	defer ReleaseValidatorResponse(resp)

	req.Identifier = newCaptchaIdentifier()
	req.Method = http.MethodPost
	req.Body = []byte(token)

	if err := validatorCaptcha(bh, req, resp); err != nil {
		t.Fatalf("validator failed: %v", err)
	}

	if err := bh.IsValidCookie(*req.Identifier, resp.Token.ReadBytes()); err != nil {
		t.Errorf("invalid cookie: %v", err)
	}
}

func Test_validatorCaptcha_POST_subdomain(t *testing.T) {
	const token = "widget-response-token"

	// trusted_domains may collapse the identity host to a domain suffix
	// while the provider reports the full page hostname.
	stub := newSiteverifyStub(t, captchaVerdict{Success: true, Hostname: "foo.example.com"}, token)
	defer stub.Close()

	bh := newCaptchaBerghain(t, stub.URL)

	req, resp := AcquireValidatorRequest(), AcquireValidatorResponse()
	defer ReleaseValidatorRequest(req)
	defer ReleaseValidatorResponse(resp)

	req.Identifier = newCaptchaIdentifier()
	req.Method = http.MethodPost
	req.Body = []byte(token)

	if err := validatorCaptcha(bh, req, resp); err != nil {
		t.Fatalf("validator failed: %v", err)
	}

	if err := bh.IsValidCookie(*req.Identifier, resp.Token.ReadBytes()); err != nil {
		t.Errorf("invalid cookie: %v", err)
	}
}

func Test_validatorCaptcha_POST_rejected(t *testing.T) {
	const token = "widget-response-token"

	stub := newSiteverifyStub(t, captchaVerdict{
		Success:    false,
		ErrorCodes: []string{"invalid-input-response"},
	}, token)
	defer stub.Close()

	bh := newCaptchaBerghain(t, stub.URL)

	req, resp := AcquireValidatorRequest(), AcquireValidatorResponse()
	defer ReleaseValidatorRequest(req)
	defer ReleaseValidatorResponse(resp)

	req.Identifier = newCaptchaIdentifier()
	req.Method = http.MethodPost
	req.Body = []byte(token)

	if err := validatorCaptcha(bh, req, resp); !errors.Is(err, errCaptchaRejected) {
		t.Fatalf("expected rejected token error, got: %v", err)
	}

	if resp.Token.Len() != 0 {
		t.Errorf("rejected token must not issue a cookie")
	}
}

func Test_validatorCaptcha_POST_hostMismatch(t *testing.T) {
	const token = "widget-response-token"

	stub := newSiteverifyStub(t, captchaVerdict{Success: true, Hostname: "evil.example.org"}, token)
	defer stub.Close()

	bh := newCaptchaBerghain(t, stub.URL)

	req, resp := AcquireValidatorRequest(), AcquireValidatorResponse()
	defer ReleaseValidatorRequest(req)
	defer ReleaseValidatorResponse(resp)

	req.Identifier = newCaptchaIdentifier()
	req.Method = http.MethodPost
	req.Body = []byte(token)

	if err := validatorCaptcha(bh, req, resp); !errors.Is(err, errCaptchaHostMismatch) {
		t.Fatalf("expected hostname mismatch error, got: %v", err)
	}

	if resp.Token.Len() != 0 {
		t.Errorf("mismatched hostname must not issue a cookie")
	}
}

func Test_validatorCaptcha_POST_skipHostnameCheck(t *testing.T) {
	const token = "widget-response-token"

	// Provider test keys report a fixed hostname unrelated to the page.
	stub := newSiteverifyStub(t, captchaVerdict{Success: true, Hostname: "unrelated.example.org"}, token)
	defer stub.Close()

	bh := newCaptchaBerghain(t, stub.URL)
	bh.Levels[0].CaptchaSkipHostnameCheck = true

	req, resp := AcquireValidatorRequest(), AcquireValidatorResponse()
	defer ReleaseValidatorRequest(req)
	defer ReleaseValidatorResponse(resp)

	req.Identifier = newCaptchaIdentifier()
	req.Method = http.MethodPost
	req.Body = []byte(token)

	if err := validatorCaptcha(bh, req, resp); err != nil {
		t.Fatalf("validator failed: %v", err)
	}

	if err := bh.IsValidCookie(*req.Identifier, resp.Token.ReadBytes()); err != nil {
		t.Errorf("invalid cookie: %v", err)
	}
}

func Test_validatorCaptcha_POST_unavailable(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer stub.Close()

	bh := newCaptchaBerghain(t, stub.URL)

	req, resp := AcquireValidatorRequest(), AcquireValidatorResponse()
	defer ReleaseValidatorRequest(req)
	defer ReleaseValidatorResponse(resp)

	req.Identifier = newCaptchaIdentifier()
	req.Method = http.MethodPost
	req.Body = []byte("widget-response-token")

	if err := validatorCaptcha(bh, req, resp); !errors.Is(err, errCaptchaUnavailable) {
		t.Fatalf("expected unavailable error on provider 5xx, got: %v", err)
	}

	// A dead provider must fail closed as well.
	stub.Close()
	if err := validatorCaptcha(bh, req, resp); !errors.Is(err, errCaptchaUnavailable) {
		t.Fatalf("expected unavailable error on connection failure, got: %v", err)
	}

	if resp.Token.Len() != 0 {
		t.Errorf("unavailable provider must not issue a cookie")
	}
}

func Test_validatorCaptcha_POST_invalidBody(t *testing.T) {
	bh := newCaptchaBerghain(t, "http://invalid.invalid")

	req, resp := AcquireValidatorRequest(), AcquireValidatorResponse()
	defer ReleaseValidatorRequest(req)
	defer ReleaseValidatorResponse(resp)

	req.Identifier = newCaptchaIdentifier()
	req.Method = http.MethodPost

	req.Body = nil
	if err := validatorCaptcha(bh, req, resp); !errors.Is(err, ErrEmpty) {
		t.Errorf("expected empty body error, got: %v", err)
	}

	req.Body = bytes.Repeat([]byte{'A'}, validatorCaptchaMaxTokenLength+1)
	if err := validatorCaptcha(bh, req, resp); !errors.Is(err, ErrInvalidLength) {
		t.Errorf("expected invalid length error, got: %v", err)
	}
}

func Test_captchaHostnameMatches(t *testing.T) {
	tests := []struct {
		hostname string
		host     string
		want     bool
	}{
		{"example.com", "example.com", true},
		{"EXAMPLE.com", "example.com", true},
		{"foo.example.com", "example.com", true},
		{"foo.bar.example.com", "example.com", true},
		{"example.com", "foo.example.com", false},
		{"fooexample.com", "example.com", false},
		{"example.org", "example.com", false},
		{"", "example.com", false},
		{"example.com", "", false},
		{".example.com", "example.com", true},
	}

	for _, tt := range tests {
		if got := captchaHostnameMatches(tt.hostname, []byte(tt.host)); got != tt.want {
			t.Errorf("captchaHostnameMatches(%q, %q) = %v, want %v", tt.hostname, tt.host, got, tt.want)
		}
	}
}
