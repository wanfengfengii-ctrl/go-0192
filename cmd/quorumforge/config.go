package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
)

// config holds runtime configuration resolved from flags and environment.
type config struct {
	Addr   string
	DBPath string
	Seed   bool
}

// parseConfig resolves configuration, preferring flags over environment.
func parseConfig(args []string) (config, error) {
	fs := flag.NewFlagSet("quorumforge", flag.ContinueOnError)
	addr := fs.String("addr", envOr("QUORUMFORGE_ADDR", ":8080"), "HTTP listen address")
	dbPath := fs.String("db", envOr("QUORUMFORGE_DB", "quorumforge.db"), "SQLite database path")
	seed := fs.Bool("seed", envBool("QUORUMFORGE_SEED", true), "seed a demo policy and participants on startup")

	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	if *addr == "" {
		return config{}, fmt.Errorf("listen address must not be empty")
	}
	return config{Addr: *addr, DBPath: *dbPath, Seed: *seed}, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}
