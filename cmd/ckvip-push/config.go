package main

import (
	"flag"
)

const (
	LogTag             = "ckvip-push"
	defaultDatabaseURL = "postgresql:///vgdept"
)

const (
	buyVIPKeyDays     = 7
	renewalFailedDays = 1
	lastChanceDays    = 3
)

type config struct {
	debug  bool
	silent bool
	dbURL  string
}

func parseArgs() (config, error) {
	cfg := config{}

	dbURL := flagEnv("DB_URL", defaultDatabaseURL)
	cfg.dbURL = dbURL

	debug := flag.Bool("debug", false, "Debug")
	silent := flag.Bool("s", false, "Silent")

	flag.Parse()

	cfg.debug = *debug
	cfg.silent = *silent && !*debug

	return cfg, nil
}
