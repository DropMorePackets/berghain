//go:build e2e

// Package e2e exercises the complete browser challenge flow against a running
// Berghain and HAProxy stack.
package e2e

import (
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

const (
	defaultBaseURL      = "http://localhost:18080"
	defaultTurnstileURL = "http://localhost:18081"
	backendBody         = "Berghain E2E backend reached"
)

func baseURL() string {
	if value := os.Getenv("BERGHAIN_E2E_BASE_URL"); value != "" {
		return strings.TrimRight(value, "/")
	}
	return defaultBaseURL
}

func turnstileURL() string {
	if value := os.Getenv("BERGHAIN_E2E_TURNSTILE_URL"); value != "" {
		return strings.TrimRight(value, "/")
	}
	return defaultTurnstileURL
}

func requireChallengePage(t *testing.T, url string) {
	t.Helper()

	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Get(url + "/")
	if err != nil {
		t.Fatalf("request challenge page: %v", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		t.Fatalf("read challenge page: %v", err)
	}
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("expected challenge status 403, got %d: %q", response.StatusCode, body)
	}
	if !strings.Contains(string(body), "Request on Hold") {
		t.Fatalf("expected challenge page, got: %q", body)
	}
}

func TestBrowserSolvesChallenge(t *testing.T) {
	solveChallengeInBrowser(t, baseURL())
}

// TestBrowserSolvesTurnstileChallenge drives the full captcha flow with
// Cloudflare's always-passing dummy keys, so it needs egress to the real
// Turnstile script and siteverify endpoints.
func TestBrowserSolvesTurnstileChallenge(t *testing.T) {
	solveChallengeInBrowser(t, turnstileURL())
}

func solveChallengeInBrowser(t *testing.T, url string) {
	t.Helper()

	requireChallengePage(t, url)

	options := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	options = append(options, chromedp.NoSandbox)
	if binary := os.Getenv("CHROME_BIN"); binary != "" {
		options = append(options, chromedp.ExecPath(binary))
	}

	allocatorContext, cancelAllocator := chromedp.NewExecAllocator(context.Background(), options...)
	defer cancelAllocator()
	browserContext, cancelBrowser := chromedp.NewContext(allocatorContext)
	defer cancelBrowser()
	browserContext, cancelTimeout := context.WithTimeout(browserContext, 90*time.Second)
	defer cancelTimeout()

	var title string
	if err := chromedp.Run(browserContext,
		chromedp.Navigate(url+"/"),
		chromedp.Title(&title),
	); err != nil {
		t.Fatalf("navigate to challenge page: %v", err)
	}
	if title != "Request on Hold" {
		t.Fatalf("expected browser challenge page, got title %q", title)
	}

	deadline := time.Now().Add(75 * time.Second)
	var body string
	for time.Now().Before(deadline) {
		err := chromedp.Run(browserContext,
			chromedp.Evaluate(`document.body ? document.body.textContent : ""`, &body),
		)
		if err == nil && strings.Contains(body, backendBody) {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}

	t.Fatalf("browser did not reach the backend after solving the challenge; last body: %q", body)
}
