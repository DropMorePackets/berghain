package reputation

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DropMorePackets/berghain/internal/peerserver"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	return New(Config{PeerListen: "127.0.0.1:0"})
}

func Test_stronger(t *testing.T) {
	later := time.Now().Add(4 * time.Hour)
	earlier := time.Now().Add(time.Hour)

	block := peerserver.Entry{Value: actionBlock, ExpiresAt: earlier}
	challenge := peerserver.Entry{Value: actionChallenge, ExpiresAt: later}
	unlimited := peerserver.Entry{Value: actionChallenge}

	if got := stronger(challenge, block); got != block {
		t.Errorf("stronger(challenge, block) = %+v, want the block", got)
	}
	if got := stronger(block, challenge); got != block {
		t.Errorf("stronger(block, challenge) = %+v, want the block", got)
	}
	if got := stronger(challenge, unlimited); got != unlimited {
		t.Errorf("stronger(challenge, unlimited) = %+v, want the unlimited entry", got)
	}
	shorter := peerserver.Entry{Value: actionChallenge, ExpiresAt: earlier}
	if got := stronger(shorter, challenge); got != challenge {
		t.Errorf("stronger(shorter, longer) = %+v, want the longer entry", got)
	}
}

func TestSourceMergeDoesNotClobber(t *testing.T) {
	s := newTestService(t)
	tor := netip.MustParseAddr("192.0.2.1")
	banned := netip.MustParseAddr("192.0.2.2")
	both := netip.MustParseAddr("192.0.2.3")

	s.setSource("static", map[netip.Addr]peerserver.Entry{
		tor:  {Value: actionChallenge},
		both: {Value: actionChallenge},
	})
	s.setSource("crowdsec", map[netip.Addr]peerserver.Entry{
		banned: {Value: actionBlock},
		both:   {Value: actionBlock},
	})

	// A crowdsec update must not clear the static entries and vice versa.
	if e, ok := s.srv.Get(tor); !ok || e.Value != actionChallenge {
		t.Errorf("tor entry = %+v, %v; want challenge", e, ok)
	}
	if e, ok := s.srv.Get(banned); !ok || e.Value != actionBlock {
		t.Errorf("banned entry = %+v, %v; want block", e, ok)
	}
	if e, ok := s.srv.Get(both); !ok || e.Value != actionBlock {
		t.Errorf("contested entry = %+v, %v; want block to win", e, ok)
	}

	// Dropping the crowdsec decisions clears only crowdsec-owned entries.
	s.setSource("crowdsec", map[netip.Addr]peerserver.Entry{})
	if _, ok := s.srv.Get(banned); ok {
		t.Error("banned entry survived crowdsec clearing its source")
	}
	if e, ok := s.srv.Get(both); !ok || e.Value != actionChallenge {
		t.Errorf("contested entry after crowdsec clear = %+v, %v; want static challenge", e, ok)
	}
	if _, ok := s.srv.Get(tor); !ok {
		t.Error("static tor entry lost after crowdsec update")
	}
}

func TestPollCrowdSec(t *testing.T) {
	var startups []string
	lapi := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/decisions/stream" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("X-Api-Key") != "test-key" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		startup := r.URL.Query().Get("startup")
		startups = append(startups, startup)
		w.Header().Set("Content-Type", "application/json")

		if startup == "true" {
			w.Write([]byte(`{"new": [
				{"scope": "Ip", "value": "198.51.100.1", "type": "ban", "duration": "3h59m57s"},
				{"scope": "Ip", "value": "198.51.100.2", "type": "captcha", "duration": "1h"},
				{"scope": "Range", "value": "203.0.113.0/24", "type": "ban", "duration": "4h"}
			], "deleted": []}`))
			return
		}
		w.Write([]byte(`{"new": [
			{"scope": "Ip", "value": "198.51.100.3", "type": "unknown-custom", "duration": "1h"}
		], "deleted": [
			{"scope": "Ip", "value": "198.51.100.1", "type": "ban", "duration": "-1s"}
		]}`))
	}))
	defer lapi.Close()

	s := New(Config{
		PeerListen: "127.0.0.1:0",
		CrowdSec:   CrowdSecConfig{URL: lapi.URL, APIKey: "test-key"},
	})
	ctx := context.Background()

	if err := s.pollCrowdSec(ctx, true); err != nil {
		t.Fatal(err)
	}

	if e, ok := s.srv.Get(netip.MustParseAddr("198.51.100.1")); !ok || e.Value != actionBlock {
		t.Errorf("ban decision = %+v, %v; want block", e, ok)
	} else if until := time.Until(e.ExpiresAt); until <= 3*time.Hour || until > 4*time.Hour {
		t.Errorf("ban decision expiry in %v, want just under 4h", until)
	}
	if e, ok := s.srv.Get(netip.MustParseAddr("198.51.100.2")); !ok || e.Value != actionChallenge {
		t.Errorf("captcha decision = %+v, %v; want challenge", e, ok)
	}
	if _, ok := s.srv.Get(netip.MustParseAddr("203.0.113.0")); ok {
		t.Error("range decision must be skipped, not applied to its base address")
	}

	if err := s.pollCrowdSec(ctx, false); err != nil {
		t.Fatal(err)
	}

	if _, ok := s.srv.Get(netip.MustParseAddr("198.51.100.1")); ok {
		t.Error("deleted decision still present after delta poll")
	}
	if e, ok := s.srv.Get(netip.MustParseAddr("198.51.100.3")); !ok || e.Value != actionBlock {
		t.Errorf("unknown remediation type = %+v, %v; want fail-closed block", e, ok)
	}
	if e, ok := s.srv.Get(netip.MustParseAddr("198.51.100.2")); !ok || e.Value != actionChallenge {
		t.Errorf("untouched decision after delta = %+v, %v; want unchanged challenge", e, ok)
	}

	if len(startups) != 2 || startups[0] != "true" || startups[1] != "false" {
		t.Errorf("startup params = %v, want [true false]", startups)
	}
}

func TestRefreshStatic(t *testing.T) {
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/torbulkexitlist":
			w.Write([]byte("# comment\n192.0.2.10\n192.0.2.11\n"))
		case "/ips-v4":
			w.Write([]byte("203.0.113.0/24\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer feed.Close()

	origTor, origCIDR := torExitSource, cidrSources
	t.Cleanup(func() { torExitSource, cidrSources = origTor, origCIDR })
	torExitSource.urls = []string{feed.URL + "/torbulkexitlist"}
	cidrSources = []cidrSource{{name: "cloudflare", urls: []string{feed.URL + "/ips-v4"}, outFile: "cloudflare-ips.lst"}}

	mapsDir := t.TempDir()
	banlist := filepath.Join(t.TempDir(), "banlist.txt")
	if err := os.WriteFile(banlist, []byte("192.0.2.11\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := New(Config{PeerListen: "127.0.0.1:0", MapsDir: mapsDir, Banlist: banlist})
	s.refreshStatic(context.Background())

	if e, ok := s.srv.Get(netip.MustParseAddr("192.0.2.10")); !ok || e.Value != actionChallenge {
		t.Errorf("tor exit = %+v, %v; want challenge", e, ok)
	}
	// The banlist entry overlaps the tor list; the block must win.
	if e, ok := s.srv.Get(netip.MustParseAddr("192.0.2.11")); !ok || e.Value != actionBlock {
		t.Errorf("banlisted tor exit = %+v, %v; want block", e, ok)
	}

	b, err := os.ReadFile(filepath.Join(mapsDir, "cloudflare-ips.lst"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "203.0.113.0/24\n"; !strings.Contains(string(b), want) {
		t.Errorf("cidr map file %q does not contain %q", b, want)
	}
}
