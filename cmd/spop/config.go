package main

import (
	"encoding/base64"
	"log"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/fionera/berghain"
)

type Config struct {
	Secret   Secret                    `yaml:"secret"`
	Listen   string                    `yaml:"listen"`
	Default  FrontendConfig            `yaml:"default"`
	Frontend map[string]FrontendConfig `yaml:"frontend"`
}

type Secret []byte

func (s *Secret) UnmarshalYAML(node *yaml.Node) error {
	ba, err := base64.StdEncoding.DecodeString(node.Value)
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
	Duration time.Duration `yaml:"duration"`
	Type     string        `yaml:"type"`
}

func (c LevelConfig) AsLevelConfig() *berghain.LevelConfig {
	var lc berghain.LevelConfig

	lc.Duration = c.Duration

	switch c.Type {
	case "none":
		lc.Type = berghain.ValidationTypeNone
	case "pow":
		lc.Type = berghain.ValidationTypePOW
	default:
		log.Fatalf("unknown validation type: %s", c.Type)
	}

	return &lc
}

func loadConfig() Config {
	if configPath == "" {
		log.Fatal("missing config path")
	}

	f, err := os.Open(configPath)
	if err != nil {
		log.Fatalf("failed opening config: %v", err)
	}

	var c Config
	if err := yaml.NewDecoder(f).Decode(&c); err != nil {
		log.Fatalf("failed reading config: %v", err)
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
