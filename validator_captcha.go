package berghain

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type captchaValidator struct {
}

func captchaVerifyURL(t ValidationType) string {
	switch t {
	case ValidationTypeTurnstile:
		return "https://challenges.cloudflare.com/turnstile/v0/siteverify"
	case ValidationTypeHCaptcha:
		return "https://api.hcaptcha.com/siteverify"
	case ValidationTypeReCaptcha:
		return "https://www.google.com/recaptcha/api/siteverify"
	default:
		return ""
	}
}

// Providers accept tokens of a few kilobytes; reCAPTCHA tokens are the
// largest at around two to three kilobytes.
const validatorCaptchaMaxTokenLength = 8 << 10

// The provider verdict is a small JSON document; limit reads defensively.
const validatorCaptchaMaxVerdictLength = 64 << 10

var (
	errCaptchaRejected     = fmt.Errorf("captcha token rejected")
	errCaptchaHostMismatch = fmt.Errorf("captcha hostname mismatch")
	errCaptchaUnavailable  = fmt.Errorf("captcha provider unavailable")
)

// captchaChallengeBody returns the static challenge response for a captcha
// level. Unlike POW, the challenge embeds no per-request state: the security
// binding happens when the solved token is exchanged for a cookie.
func (lc *LevelConfig) captchaChallengeBody() []byte {
	lc.captchaBodyOnce.Do(func() {
		body, err := json.Marshal(struct {
			Countdown int    `json:"c"`
			Type      int    `json:"t"`
			Sitekey   string `json:"k"`
		}{
			Countdown: lc.Countdown,
			Type:      int(lc.Type) - 1, // the web protocol counts types from zero
			Sitekey:   lc.CaptchaSitekey,
		})
		if err != nil {
			panic(err)
		}
		lc.captchaBody = body
	})
	return lc.captchaBody
}

func (captchaValidator) onNew(b *Berghain, req *ValidatorRequest, resp *ValidatorResponse) error {
	lc := b.LevelConfig(req.Identifier.Level)

	body := lc.captchaChallengeBody()
	if len(body) > len(resp.Body.WriteBytes()) {
		return fmt.Errorf("captcha challenge body exceeds response buffer: %d bytes", len(body))
	}
	copy(resp.Body.WriteNBytes(len(body)), body)

	return nil
}

func (captchaValidator) isValid(b *Berghain, req *ValidatorRequest, _ *ValidatorResponse) error {
	if len(req.Body) == 0 {
		return ErrEmpty
	}
	if len(req.Body) > validatorCaptchaMaxTokenLength {
		return ErrInvalidLength
	}

	lc := b.LevelConfig(req.Identifier.Level)

	verifyURL := lc.CaptchaVerifyURL
	if verifyURL == "" {
		verifyURL = captchaVerifyURL(lc.Type)
	}

	form := url.Values{
		"secret":   {lc.CaptchaSecret},
		"response": {string(req.Body)},
		"remoteip": {req.Identifier.SrcAddr.String()},
	}

	httpResp, err := b.httpClient().PostForm(verifyURL, form)
	if err != nil {
		// Fail closed: the client is told the challenge failed and can retry.
		return fmt.Errorf("%w: %v", errCaptchaUnavailable, err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: status %d", errCaptchaUnavailable, httpResp.StatusCode)
	}

	var verdict struct {
		Success    bool     `json:"success"`
		Hostname   string   `json:"hostname"`
		ErrorCodes []string `json:"error-codes"`
	}
	if err := json.NewDecoder(io.LimitReader(httpResp.Body, validatorCaptchaMaxVerdictLength)).Decode(&verdict); err != nil {
		return fmt.Errorf("%w: %v", errCaptchaUnavailable, err)
	}

	if !verdict.Success {
		return fmt.Errorf("%w: %s", errCaptchaRejected, strings.Join(verdict.ErrorCodes, ", "))
	}

	if !lc.CaptchaSkipHostnameCheck && !captchaHostnameMatches(verdict.Hostname, req.Identifier.Host) {
		return errCaptchaHostMismatch
	}

	return nil
}

// captchaHostnameMatches accepts the exact identity host or any of its
// subdomains: trusted_domains may have collapsed the identity host to a
// domain suffix while the provider reports the full page hostname.
func captchaHostnameMatches(hostname string, host []byte) bool {
	if hostname == "" || len(host) == 0 {
		return false
	}
	if len(hostname) == len(host) {
		return strings.EqualFold(hostname, string(host))
	}
	prefixLen := len(hostname) - len(host)
	if prefixLen < 1 || hostname[prefixLen-1] != '.' {
		return false
	}
	return strings.EqualFold(hostname[prefixLen:], string(host))
}

func validatorCaptcha(b *Berghain, req *ValidatorRequest, resp *ValidatorResponse) error {
	var c captchaValidator

	switch req.Method {
	case http.MethodPost:
		if err := c.isValid(b, req, resp); err != nil {
			return err
		}
		return req.Identifier.ToCookie(b, resp.Token)
	case http.MethodGet:
		return c.onNew(b, req, resp)
	}

	return errInvalidMethod
}
