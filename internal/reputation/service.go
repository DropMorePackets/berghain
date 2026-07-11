// Package reputation feeds IP reputation into HAProxy.
//
// It combines multiple sources — the CrowdSec LAPI decision stream and static
// URL feeds — into one desired state and pushes individual-IP entries into
// HAProxy stick-tables live over the peers protocol (internal/peerserver).
// CIDR feeds are written to map files instead, because stick-tables cannot do
// longest-prefix matching.
//
// The same service powers both deployment modes: embedded in the SPOP agent
// (cmd/spop, `reputation:` config section) for the simple single-container
// setup, and standalone (cmd/feedupdater) where scaling or isolation calls
// for it.
package reputation

import (
	"context"
	"log/slog"
	"maps"
	"net"
	"net/http"
	"net/netip"
	"os"
	"sync"
	"time"

	"github.com/DropMorePackets/berghain/internal/peerserver"
)

// Reputation actions, encoded as the gpt0 tag value in the stick-tables.
const (
	actionNone      uint32 = 0 // no entry / cleared
	actionBlock     uint32 = 1 // silent-drop
	actionChallenge uint32 = 3 // raise to the highest challenge level
)

type CrowdSecConfig struct {
	// URL of the local API, e.g. http://crowdsec:8080. Empty disables the source.
	URL string `yaml:"url"`
	// APIKey is a bouncer key (cscli bouncers add berghain). Falls back to the
	// CROWDSEC_API_KEY environment variable so the secret can stay out of files.
	APIKey string `yaml:"api_key"`
	// Interval between decision-stream polls. Default 10s.
	Interval time.Duration `yaml:"interval"`
}

type Config struct {
	// PeerListen is the peers-protocol listen address (e.g. 0.0.0.0:10001).
	// It must match this peer's entry in the HAProxy `peers` section.
	PeerListen string `yaml:"peer_listen"`
	// LocalPeer is our name in the HAProxy peers section. Default berghain_feed.
	LocalPeer string `yaml:"local_peer"`
	// TableV4/TableV6 name the reputation stick-tables. Defaults
	// st_reputation_v4 / st_reputation_v6.
	TableV4 string `yaml:"table_v4"`
	TableV6 string `yaml:"table_v6"`
	// TableExpiry mirrors the `expire` of the HAProxy stick-tables and bounds
	// entries that carry no per-decision duration. Default 24h.
	TableExpiry time.Duration `yaml:"table_expiry"`

	// MapsDir receives the CIDR feed files. Empty disables CIDR feeds.
	MapsDir string `yaml:"maps_dir"`
	// Interval between static feed refreshes. Default 6h.
	Interval time.Duration `yaml:"interval"`
	// Banlist optionally names a local file of individual IPs to block.
	Banlist string `yaml:"banlist"`
	// TorExits toggles the Tor exit-node feed. Default true.
	TorExits *bool `yaml:"tor_exits"`

	CrowdSec CrowdSecConfig `yaml:"crowdsec"`
}

// Enabled reports whether the config activates the service at all.
func (c Config) Enabled() bool {
	return c.PeerListen != ""
}

func (c Config) withDefaults() Config {
	if c.LocalPeer == "" {
		c.LocalPeer = "berghain_feed"
	}
	if c.TableV4 == "" {
		c.TableV4 = "st_reputation_v4"
	}
	if c.TableV6 == "" {
		c.TableV6 = "st_reputation_v6"
	}
	if c.TableExpiry == 0 {
		c.TableExpiry = 24 * time.Hour
	}
	if c.Interval == 0 {
		c.Interval = 6 * time.Hour
	}
	if c.CrowdSec.Interval == 0 {
		c.CrowdSec.Interval = 10 * time.Second
	}
	if c.CrowdSec.APIKey == "" {
		c.CrowdSec.APIKey = os.Getenv("CROWDSEC_API_KEY")
	}
	return c
}

// Service runs the peers listener and the feed loops.
type Service struct {
	cfg    Config
	srv    *peerserver.Server
	client *http.Client

	mu      sync.Mutex
	sources map[string]map[netip.Addr]peerserver.Entry
}

func New(cfg Config) *Service {
	cfg = cfg.withDefaults()
	return &Service{
		cfg:     cfg,
		srv:     peerserver.New(cfg.LocalPeer, cfg.TableV4, cfg.TableV6, cfg.TableExpiry),
		client:  &http.Client{Timeout: 30 * time.Second},
		sources: make(map[string]map[netip.Addr]peerserver.Entry),
	}
}

// Run serves the peers protocol and refreshes all feeds until ctx is done.
func (s *Service) Run(ctx context.Context) error {
	l, err := net.Listen("tcp", s.cfg.PeerListen)
	if err != nil {
		return err
	}
	slog.InfoContext(ctx, "serving reputation over the peers protocol",
		"listen", s.cfg.PeerListen, "peer", s.cfg.LocalPeer)

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		<-ctx.Done()
		_ = l.Close()
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		s.staticFeedLoop(ctx)
	}()

	if s.cfg.CrowdSec.URL != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.crowdsecLoop(ctx)
		}()
	}

	err = s.srv.Serve(l)
	wg.Wait()
	if ctx.Err() != nil {
		return nil // closed by shutdown
	}
	return err
}

// setSource replaces one source's desired state and pushes the merged state of
// all sources to the connected peers. Merging (instead of letting each source
// write directly) prevents a static-feed refresh from clearing CrowdSec
// decisions and vice versa.
func (s *Service) setSource(name string, entries map[netip.Addr]peerserver.Entry) {
	s.mu.Lock()
	s.sources[name] = entries

	merged := make(map[netip.Addr]peerserver.Entry)
	for _, src := range s.sources {
		for a, e := range src {
			if cur, ok := merged[a]; ok {
				e = stronger(cur, e)
			}
			merged[a] = e
		}
	}
	s.mu.Unlock()

	s.srv.ReplaceAll(merged)
	slog.Info("reputation updated", "source", name, "entries", s.srv.Len())
}

// snapshotSource returns a copy of one source's current desired state.
func (s *Service) snapshotSource(name string) map[netip.Addr]peerserver.Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	return maps.Clone(s.sources[name])
}

// stronger picks the entry that must win when two sources disagree about one
// address: a block always beats a challenge, higher challenge levels beat
// lower ones, and for equal actions the longer-lived entry wins.
func stronger(a, b peerserver.Entry) peerserver.Entry {
	if a.Value != b.Value {
		switch {
		case a.Value == actionBlock:
			return a
		case b.Value == actionBlock:
			return b
		case a.Value > b.Value:
			return a
		default:
			return b
		}
	}
	switch {
	case a.ExpiresAt.IsZero():
		return a
	case b.ExpiresAt.IsZero():
		return b
	case a.ExpiresAt.After(b.ExpiresAt):
		return a
	default:
		return b
	}
}
