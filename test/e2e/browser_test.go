//go:build e2e

// Package e2e contains a real-browser end-to-end test: it drives headless
// Chrome against a running Berghain + HAProxy stack and asserts that the actual
// client bundle solves a challenge and reaches the backend (issue #9).
//
// It expects the stack to be reachable at $BERGHAIN_E2E_BASE_URL (default
// http://localhost:8080). The CI e2e job builds the web bundle, starts the
// agent + HAProxy, then runs `go test -tags e2e`. Locally you can point it at
// `docker compose up`.
package e2e

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

const browserUA = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120 Safari/537.36"

func baseURL() string {
	if u := os.Getenv("BERGHAIN_E2E_BASE_URL"); u != "" {
		return u
	}
	return "http://localhost:8080"
}

// hit makes one GET with a browser User-Agent (so it is not tarpitted).
func hit(base string) {
	c := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest(http.MethodGet, base+"/", nil)
	if err != nil {
		return
	}
	req.Header.Set("User-Agent", browserUA)
	if resp, err := c.Do(req); err == nil {
		resp.Body.Close()
	}
}

func TestBrowserSolvesChallenge(t *testing.T) {
	base := baseURL()

	// Skip cleanly if nothing is serving, so `go test -tags e2e` never hangs.
	// Use a browser UA so the probe is not caught by the tarpit.
	probe := &http.Client{Timeout: 3 * time.Second}
	probeReq, _ := http.NewRequest(http.MethodGet, base+"/", nil)
	probeReq.Header.Set("User-Agent", browserUA)
	if _, err := probe.Do(probeReq); err != nil {
		t.Skipf("no server reachable at %s: %v", base, err)
	}

	// Keep the request rate above the challenge threshold for the whole test so
	// a challenge is genuinely required (otherwise the rate window could decay
	// and Berghain would be bypassed, giving a false pass). Gentle enough not to
	// trip the per-second burst limiter.
	stop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				hit(base)
			}
		}
	}()
	defer close(stop)

	// Prime the rate so the first browser navigation is already challenged.
	for i := 0; i < 12; i++ {
		hit(base)
		time.Sleep(80 * time.Millisecond)
	}

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.NoSandbox,
		chromedp.UserAgent(browserUA),
	)
	if bin := os.Getenv("CHROME_BIN"); bin != "" {
		opts = append(opts, chromedp.ExecPath(bin))
	}
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(), opts...)
	defer cancelAlloc()
	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()
	ctx, cancelTO := context.WithTimeout(ctx, 90*time.Second)
	defer cancelTO()

	// Surface client console output / exceptions to help diagnose failures.
	chromedp.ListenTarget(ctx, func(ev interface{}) {
		switch e := ev.(type) {
		case *runtime.EventConsoleAPICalled:
			var parts []string
			for _, a := range e.Args {
				parts = append(parts, string(a.Value))
			}
			t.Logf("console.%s: %s", e.Type, strings.Join(parts, " "))
		case *runtime.EventExceptionThrown:
			t.Logf("exception: %s", e.ExceptionDetails.Text)
		}
	})

	// The first response must be the challenge interstitial.
	var title string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		chromedp.Title(&title),
	); err != nil {
		t.Fatalf("initial navigation failed: %v", err)
	}
	if !strings.Contains(title, "Request on Hold") {
		t.Fatalf("expected the challenge interstitial, got page title %q (was the rate threshold tripped?)", title)
	}
	t.Logf("challenge interstitial served (title=%q); waiting for the client to solve it...", title)

	// The client JS solves the challenge, reloads with the clearance cookie, and
	// the backend ("Hello World!") is reached. Poll the body until it appears.
	deadline := time.Now().Add(75 * time.Second)
	for {
		// Evaluate fresh each poll so the client's window.location.reload() does
		// not leave us holding a stale node reference. Use textContent (not
		// innerText) — headless Chrome returns "" for innerText of a text/plain
		// backend response.
		var body string
		_ = chromedp.Run(ctx, chromedp.Evaluate(`document.body ? document.body.textContent : ""`, &body))
		if strings.Contains(body, "Hello World!") {
			t.Log("browser reached the backend: the client solved the challenge end-to-end")
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("browser never reached the backend; last body: %q", body)
		}
		time.Sleep(1 * time.Second)
	}
}
