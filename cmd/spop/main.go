package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"sync"

	"github.com/dropmorepackets/haproxy-go/pkg/encoding"
	"github.com/dropmorepackets/haproxy-go/spop"
)

var configPath string
var debug bool

func main() {
	flag.StringVar(&configPath, "config", "config.yaml", "Config file to load")
	flag.BoolVar(&debug, "debug", false, "Enable debug mode")
	flag.Parse()

	log.SetFlags(log.LstdFlags | log.Lshortfile)

	// In debug mode start a http server to serve the default pprof handlers.
	if debug {
		go http.ListenAndServe(":9001", nil)
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
		log.Printf("closing due to fatal error: %v", err)
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
		log.Fatalf("provided secret has invalid length: 32 != %d", len(cfg.Secret))
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

	log.Printf("Listening on %s://%s", network, address)

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
			panic("failed fetching default frontend")
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
			panic(fmt.Sprintf("error while reading KV: %v", err))
		}

		panic("failed reading fronted argument")
	}

	if !k.NameEquals("frontend") {
		panic("Invalid SPOP argument order: expected frontend")
	}

	f := i.Frontend(k.ValueBytes())

	switch {
	case bytes.Equal(m.NameBytes(), []byte(SPOEMessageNameValidate)):
		f.HandleSPOEValidate(ctx, w, m)
	case bytes.Equal(m.NameBytes(), []byte(SPOEMessageNameChallenge)):
		f.HandleSPOEChallenge(ctx, w, m)
	}
}
