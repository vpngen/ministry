package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/vpngen/ministry/internal/pgsql"
)

func main() {
	cfg, err := parseArgs()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Can't parse args: %s\n", err)
		os.Exit(1)
	}

	db, err := pgsql.CreateDBPool(cfg.dbURL)
	if err != nil {
		log.Fatalf("%s: Can't create db pool: %s\n", LogTag, err)
	}

	ctx := context.Background()

	if err := queueBuyVIPKey(ctx, db, cfg.silent); err != nil {
		log.Fatalf("%s: Can't queue buy_vip_key notifications: %s\n", LogTag, err)
	}

	if err := queueRenewalFailed(ctx, db, cfg.silent); err != nil {
		log.Fatalf("%s: Can't queue renewal_failed notifications: %s\n", LogTag, err)
	}

	if err := queueLastChance(ctx, db, cfg.silent); err != nil {
		log.Fatalf("%s: Can't queue last_chance notifications: %s\n", LogTag, err)
	}

	if !cfg.silent {
		fmt.Fprintf(os.Stderr, "%s: Done\n", LogTag)
	}
}

func flagEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}

	return def
}
