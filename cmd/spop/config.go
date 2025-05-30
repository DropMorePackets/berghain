package main

import (
	"encoding/base64"
	"os"
	"strings"
	"time"

	"github.com/goccy/go-yaml"

	"github.com/DropMorePackets/berghain"
)

type Config struct {
	Secret   Secret                    `yaml:"secret"`
	Listen   string                    `yaml:"listen"`
	Default  FrontendConfig            `yaml:"default"`
	Frontend map[string]FrontendConfig `yaml:"frontend"`
}

type Secret []byte

func init() {
	yaml.RegisterCustomUnmarshaler((*Secret).UnmarshalYAML)
}

func (s *Secret) UnmarshalYAML(b []byte) error {
	ba, err := base64.StdEncoding.DecodeString(string(b))
	if err != nil {
		return err
	}
	*s = ba
	return nil
}

type FrontendConfig []LevelConfig

func (fc FrontendConfig) AsBerghain(s []byte) *berghain.Berghain {
	b := berghain.NewBerghain(s)
	for _, c := range fc {
		b.Levels = append(b.Levels, c.AsLevelConfig())
	}
	return b
}

type LevelConfig struct {
	Countdown *int          `yaml:"countdown"`
	Duration  time.Duration `yaml:"duration"`
	Type      string        `yaml:"type"`
}

func (c LevelConfig) AsLevelConfig() *berghain.LevelConfig {
	var lc berghain.LevelConfig

	lc.Duration = c.Duration

	if c.Countdown == nil {
		// no level specific countdown was provided
		lc.Countdown = 3
	} else if *c.Countdown > 9 {
		// template string currently only allows one digit
		//   and JavaScript does not allow zero padding of integers in JSON
		Fatal("countdown too high, cannot proceed", "countdown_have", *c.Countdown, "countdown_max", 9)
	} else {
		lc.Countdown = *c.Countdown
	}

	switch c.Type {
	case "none":
		lc.Type = berghain.ValidationTypeNone
	case "pow":
		lc.Type = berghain.ValidationTypePOW
	default:
		Fatal("unknown validation type", "validator", c.Type)
	}

	return &lc
}

func loadConfig() Config {
	if configPath == "" {
		Fatal("missing config path", "path", configPath)
	}

	f, err := os.Open(configPath)
	if err != nil {
		Fatal("failed opening config", "path", configPath, "error", err)
	}

	var c Config
	if err := yaml.NewDecoder(f).Decode(&c); err != nil {
		Fatal("failed reading config", "path", configPath, "error", err)
	}

	return c
}

// ParseListener parses the listen string and returns the network type and address
func ParseListener(listen string) (network, address string) {

	// old default
	if listen == "" {
		return "unix", "./spop.sock"
	}

	if strings.HasPrefix(listen, "unix://") {
		return "unix", strings.TrimPrefix(listen, "unix://")
	}

	if strings.HasPrefix(listen, "tcp://") {
		return "tcp", strings.TrimPrefix(listen, "tcp://")
	}

	// old default behaviour
	return "unix", listen
}
