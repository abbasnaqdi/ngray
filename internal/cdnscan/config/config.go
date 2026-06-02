package config

import (
	"os"
	"time"

	"github.com/BurntSushi/toml"
)

type Config struct {
	// Phase 1: TCP port-443 discovery
	DiscoverWorkers int    `toml:"discover_workers"`
	DiscoverTimeout string `toml:"discover_timeout"`
	MasscanRate     int    `toml:"masscan_rate"`

	// Phase 2: CDN fronting verification
	VerifyWorkers int    `toml:"verify_workers"`
	VerifyTimeout string `toml:"verify_timeout"`

	// Parsed durations (not in TOML)
	DiscoverTimeoutDur time.Duration
	VerifyTimeoutDur   time.Duration
}

func defaults() Config {
	return Config{
		// 100 workers × 6 CDNs = 600 max concurrent TCP sockets.
		// Raise discover_workers in akamai-scanner.toml on high-spec machines.
		DiscoverWorkers: 100,
		DiscoverTimeout: "3s",
		MasscanRate:     5000,
		// Verification (TLS + HTTP) is heavier — keep per-CDN count low.
		VerifyWorkers: 25,
		VerifyTimeout: "10s",
	}
}

func Load() *Config {
	cfg := defaults()

	if raw, err := os.ReadFile("akamai-scanner.toml"); err == nil {
		toml.Unmarshal(raw, &cfg) //nolint:errcheck
	}

	if d, err := time.ParseDuration(cfg.DiscoverTimeout); err == nil {
		cfg.DiscoverTimeoutDur = d
	} else {
		cfg.DiscoverTimeoutDur = 3 * time.Second
	}
	if d, err := time.ParseDuration(cfg.VerifyTimeout); err == nil {
		cfg.VerifyTimeoutDur = d
	} else {
		cfg.VerifyTimeoutDur = 10 * time.Second
	}

	return &cfg
}
