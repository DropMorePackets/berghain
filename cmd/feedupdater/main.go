// Command feedupdater runs the Berghain IP reputation service standalone.
//
// It is a thin wrapper around internal/reputation: it fetches CrowdSec
// decisions and static feeds and pushes individual-IP reputation into HAProxy
// stick-tables over the peers protocol. Simple deployments can skip this
// binary entirely and enable the same service inside cmd/spop via the
// `reputation:` config section; this command exists for setups that scale or
// isolate the feed daemon separately.
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"

	"github.com/DropMorePackets/berghain/internal/reputation"
)

func main() {
	var (
		cfg         reputation.Config
		logLevelArg string
	)

	flag.StringVar(&cfg.PeerListen, "peer-listen", "127.0.0.1:10001", "listen address for the HAProxy peers protocol")
	flag.StringVar(&cfg.LocalPeer, "local-peer", "", "this peer's name in the HAProxy peers section (default berghain_feed)")
	flag.StringVar(&cfg.TableV4, "table-v4", "", "IPv4 reputation stick-table name (default st_reputation_v4)")
	flag.StringVar(&cfg.TableV6, "table-v6", "", "IPv6 reputation stick-table name (default st_reputation_v6)")
	flag.DurationVar(&cfg.TableExpiry, "table-expiry", 0, "stick-table expire value, bounds entries without their own duration (default 24h)")
	flag.StringVar(&cfg.MapsDir, "maps-dir", "", "directory to write CIDR map/ACL files into; empty disables CIDR feeds")
	flag.DurationVar(&cfg.Interval, "interval", 0, "static feed refresh interval (default 6h)")
	flag.StringVar(&cfg.Banlist, "banlist", "", "optional file of individual IPs to block (one per line)")
	flag.StringVar(&cfg.CrowdSec.URL, "crowdsec-url", "", "CrowdSec LAPI URL (e.g. http://crowdsec:8080); empty disables the source")
	flag.StringVar(&cfg.CrowdSec.APIKey, "crowdsec-api-key", "", "CrowdSec bouncer API key (or CROWDSEC_API_KEY env)")
	flag.DurationVar(&cfg.CrowdSec.Interval, "crowdsec-interval", 0, "CrowdSec decision-stream poll interval (default 10s)")
	torExits := flag.Bool("tor-exits", true, "challenge Tor exit nodes")
	flag.StringVar(&logLevelArg, "loglevel", "info", "Logging level")
	flag.Parse()

	cfg.TorExits = torExits

	var logLevel slog.Level
	if err := logLevel.UnmarshalText([]byte(logLevelArg)); err != nil {
		slog.Error("invalid log level, cannot proceed", "loglevel", logLevelArg)
		os.Exit(1)
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel})))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := reputation.New(cfg).Run(ctx); err != nil {
		slog.Error("reputation service failed", "error", err)
		os.Exit(1)
	}
}
