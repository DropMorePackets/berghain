package main

import (
	"context"
	"flag"
	"log"
	"log/slog"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"sync"

	"github.com/dropmorepackets/haproxy-go/pkg/encoding"
	"github.com/dropmorepackets/haproxy-go/spop"
)

var (
	configPath string
)

func main() {
	var (
		logLevelArg string
		pprofArg    bool
	)

	flag.StringVar(&configPath, "config", "config.yaml", "Config file to load")
	flag.StringVar(&logLevelArg, "loglevel", "info", "Logging level")
	flag.BoolVar(&pprofArg, "pprof", false, "Enable pprof listener")
	flag.Parse()

	log.SetFlags(log.LstdFlags | log.Lshortfile)

	var logLevel slog.Level
	if err := logLevel.UnmarshalText([]byte(logLevelArg)); err != nil {
		Fatal("invalid log level, cannot proceed")
	}

	slog.SetDefault(slog.New(&logHandler{
		TextHandler: slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: logLevel,
		}),
	}))

	// Optionally start a http server to serve the default pprof handlers.
	if pprofArg {
		go http.ListenAndServe(":9001", nil)
		slog.Info("Listening for pprof requests", "type", "tcp", "address", "*:9001")
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)

	ctx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()

	ec := make(chan error)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		ec <- runBerghain(&wg, ctx)
	}()

	select {
	case <-c:
		cancelFunc()
	case err := <-ec:
		Fatal("closing due to fatal error", "error", err)
		return
	}

	go func() {
		// wait for everything to shut down
		wg.Wait()
		close(c)
	}()

	<-c
}

func runBerghain(wg *sync.WaitGroup, ctx context.Context) error {
	defer wg.Done()

	cfg := loadConfig()
	if len(cfg.Secret) != 32 {
		Fatal("provided secret has invalid length", "have", len(cfg.Secret), "need", 32)
	}

	b := instance{
		c: map[string]*frontend{
			defaultFrontend: {bh: cfg.Default.AsBerghain(cfg.Secret)},
		},
	}

	for fName, config := range cfg.Frontend {
		b.c[fName] = &frontend{bh: config.AsBerghain(cfg.Secret)}
	}

	network, address := ParseListener(cfg.Listen)
	listen, err := net.Listen(network, address)
	if err != nil {
		return err
	}
	defer listen.Close()

	slog.InfoContext(ctx, "Listening for SPOP requests", "type", network, "address", address)

	a := &spop.Agent{
		Handler:     &b,
		BaseContext: ctx,
	}

	return a.Serve(listen)
}

type instance struct {
	c map[string]*frontend
}

const defaultFrontend = "default"

func (i *instance) Frontend(b []byte) *frontend {
	// TODO: does this alloc?
	f := i.c[string(b)]

	if f == nil {
		f = i.c[defaultFrontend]
		if f == nil {
			Fatal("failed fetching default frontend")
		}
	}

	return f
}

func (i *instance) HandleSPOE(ctx context.Context, w *encoding.ActionWriter, m *encoding.Message) {
	const SPOEMessageNameValidate, SPOEMessageNameChallenge = "validate", "challenge"

	k := encoding.AcquireKVEntry()
	defer encoding.ReleaseKVEntry(k)

	// read frontend
	if !m.KV.Next(k) {
		if err := m.KV.Error(); err != nil {
			slog.ErrorContext(ctx, "error while reading KV", "error", err)
			return
		}

		slog.ErrorContext(ctx, "failed reading fronted argument")
	}

	if !k.NameEquals("frontend") {
		slog.ErrorContext(ctx, "invalid SPOP argument order", "error", "expected frontend")
	}

	f := i.Frontend(k.ValueBytes())

	h := string(m.NameBytes())

	ctx = context.WithValue(ctx, "handler", h)

	switch h {
	case SPOEMessageNameValidate:
		f.HandleSPOEValidate(ctx, w, m)
	case SPOEMessageNameChallenge:
		f.HandleSPOEChallenge(ctx, w, m)
	}
}
