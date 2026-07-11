package main

import (
	"testing"
	"time"

	"github.com/goccy/go-yaml"
)

func TestConfigReputationBlock(t *testing.T) {
	var cfg Config
	err := yaml.Unmarshal([]byte(`
secret: JMal0XJRROOMsMdPqggG2tR56CTkpgN3r47GgUN/WSQ=
reputation:
  peer_listen: 0.0.0.0:10001
  crowdsec:
    url: http://crowdsec:8080
    interval: 30s
`), &cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Reputation.Enabled() {
		t.Error("reputation block with peer_listen must enable the service")
	}
	if cfg.Reputation.CrowdSec.URL != "http://crowdsec:8080" {
		t.Errorf("crowdsec url = %q", cfg.Reputation.CrowdSec.URL)
	}
	if cfg.Reputation.CrowdSec.Interval != 30*time.Second {
		t.Errorf("crowdsec interval = %v, want 30s", cfg.Reputation.CrowdSec.Interval)
	}
}

func TestConfigReputationAbsent(t *testing.T) {
	var cfg Config
	err := yaml.Unmarshal([]byte(`
secret: JMal0XJRROOMsMdPqggG2tR56CTkpgN3r47GgUN/WSQ=
`), &cfg)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Reputation.Enabled() {
		t.Error("absent reputation block must leave the service disabled")
	}
}
