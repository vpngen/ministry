package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/vpngen/ministry/internal/pgsql"

	sshVng "github.com/vpngen/ministry/internal/ssh"
)

const (
	LogTag = "ckvip"
)

const (
	redemtionPeriod = 4 // hours
)

func main() {
	cfg, err := parseArgs()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Can't parse args: %s\n", err)
		os.Exit(1)
	}

	c := &http.Client{
		Timeout:   time.Minute,
		Transport: NewBearerAuthTransport(&cfg.jwtKeydeskIssuer, nil),
	}

	ctx := context.Background()

	if !cfg.silent {
		fmt.Fprintf(os.Stderr, "%s: VIP Endpoint: %s\n", LogTag, cfg.vipEndpoint)
		fmt.Fprintf(os.Stderr, "%s: OBFS UUID: %s\n", LogTag, cfg.obfsUUID)
		fmt.Fprintf(os.Stderr, "%s: DB URL: %s\n", LogTag, cfg.dbURL)
	}

	brigades, raw, err := fetchPaidUsers(c, cfg.obfsUUID, cfg.vipEndpoint)
	if cfg.debug && raw != nil {
		fmt.Fprintf(os.Stdout, "%s\n", raw)
	}

	if err != nil {
		log.Fatalf("%s: Can't fetch paid users: %s\n", LogTag, err)
	}

	goods := 0
	for _, brigade := range brigades {
		if cfg.debug {
			fmt.Fprintf(os.Stderr, "Found brigade: %s (%s), ExpiredAt: %s, UsersCount: %d\n", brigade.BrigadeID, brigade.RawBrigadeID, brigade.ExpiredAt, brigade.UsersCount)
		}

		goods += brigade.UsersCount
	}

	if !cfg.silent {
		fmt.Fprintf(os.Stderr, "Fetched %d paid users\n", len(brigades))
		fmt.Fprintf(os.Stderr, "Total %d goods\n", goods)
	}

	if cfg.onlyfetch {
		fmt.Fprintf(os.Stderr, "Only fetch mode, exiting\n")
		return
	}

	sshconf, err := sshVng.CreateSSHConfig(cfg.sshKeyFn, sshkeyRemoteUsername, sshVng.SSHDefaultTimeOut)
	if err != nil {
		log.Fatalf("%s: Can't create ssh configs: %s\n", LogTag, err)
	}

	db, err := pgsql.CreateDBPool(cfg.dbURL)
	if err != nil {
		log.Fatalf("%s: Can't create db pool: %s\n", LogTag, err)
	}

	if len(brigades) > 0 {
		if err := updateVIPRecords(ctx, db, brigades, cfg.silent); err != nil {
			log.Fatalf("%s: Can't update VIP records: %s\n", LogTag, err)
		}
	}

	// try to set vip brigade
	if !cfg.silent {
		fmt.Fprintf(os.Stderr, "%s: Try to set VIP brigades\n", LogTag)
	}

	if err := viparize(ctx, db, sshconf, cfg.silent); err != nil {
		log.Fatalf("%s: Can't set VIP brigades: %s\n", LogTag, err)
	}

	// try to restore deleted vip brigade
	if !cfg.silent {
		fmt.Fprintf(os.Stderr, "%s: Try to restore deleted VIP brigades\n", LogTag)
	}

	if err := viparizeDeleted(ctx, db, sshconf, cfg.debug, cfg.silent); err != nil {
		log.Fatalf("%s: Can't restore deleted VIP brigades: %s\n", LogTag, err)
	}

	// try to create credentials for VIP brigades
	if !cfg.silent {
		fmt.Fprintf(os.Stderr, "%s: Try to create credentials for VIP brigades\n", LogTag)
	}

	if err := newCreds(ctx, db, sshconf, cfg.silent); err != nil {
		fmt.Fprintf(os.Stderr, "%s: Can't create credentials for VIP brigades: %s\n", LogTag, err)
	}

	if !cfg.silent {
		fmt.Fprintf(os.Stderr, "%s: Try to set VIP brigades\n", LogTag)
	}

	if err := nextTryNewBrigade(ctx, db, sshconf, cfg.silent); err != nil {
		fmt.Fprintf(os.Stderr, "%s: Can't set VIP brigades: %s\n", LogTag, err)
	}

	// try to unset vip brigade
	if !cfg.silent {
		fmt.Fprintf(os.Stderr, "%s: Try to unset VIP brigades\n", LogTag)
	}

	if err := unviparize(ctx, db, sshconf, cfg.silent); err != nil {
		log.Fatalf("%s: Can't unset VIP brigades: %s\n", LogTag, err)
	}

	if !cfg.silent {
		fmt.Fprintf(os.Stderr, "%s: Done\n", LogTag)
	}
}
