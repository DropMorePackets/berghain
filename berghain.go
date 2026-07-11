package berghain

import (
	"crypto/hmac"
	"crypto/sha256"
	"hash"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

type LevelConfig struct {
	Countdown int
	Duration  time.Duration
	Type      ValidationType

	// Captcha configuration, required for the turnstile, hcaptcha and
	// recaptcha validation types.
	CaptchaSitekey string
	CaptchaSecret  string
	// CaptchaVerifyURL overrides the provider siteverify endpoint,
	// e.g. for regional endpoints or tests.
	CaptchaVerifyURL string
	// CaptchaSkipHostnameCheck disables binding the provider-reported
	// hostname to the request identity. Provider test keys report a
	// fixed hostname, so tests need this; production setups do not.
	CaptchaSkipHostnameCheck bool

	captchaBodyOnce sync.Once
	captchaBody     []byte
}

type Berghain struct {
	Levels         []*LevelConfig
	TrustedDomains []string

	// HTTPClient is used for captcha siteverify requests.
	// Defaults to a client with a 5 second timeout.
	HTTPClient *http.Client

	secret []byte
	hmac   sync.Pool
}

var hashAlgo = sha256.New

func NewBerghain(secret []byte) *Berghain {
	return &Berghain{
		secret: secret,
		hmac: sync.Pool{
			New: func() any {
				return NewZeroHasher(hmac.New(hashAlgo, secret))
			},
		},
	}
}

var defaultHTTPClient = &http.Client{Timeout: 5 * time.Second}

func (b *Berghain) httpClient() *http.Client {
	if b.HTTPClient != nil {
		return b.HTTPClient
	}
	return defaultHTTPClient
}

func (b *Berghain) acquireHMAC() hash.Hash {
	return b.hmac.Get().(hash.Hash)
}

func (b *Berghain) releaseHMAC(h hash.Hash) {
	h.Reset()
	b.hmac.Put(h)
}

func (b *Berghain) LevelConfig(level uint8) *LevelConfig {

	if level == 0 {
		slog.Warn("level cannot be zero, correcting", "old", 0, "new", 1)
	}

	if level > uint8(len(b.Levels)) {
		slog.Warn("level too high, correcting", "old", level, "new", len(b.Levels))
	}

	level = min(uint8(len(b.Levels)), max(1, level))

	return b.Levels[level-1]
}
