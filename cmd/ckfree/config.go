package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/google/uuid"
	sshVng "github.com/vpngen/ministry/internal/ssh"
)

const (
	LogTag = "ckfree"
)

const (
	sshkeyDefaultPath    = "/etc/vgdept"
	sshkeyRemoteUsername = "_valera_"
	defaultDatabaseURL   = "postgresql:///vgdept"
)

type config struct {
	debug    bool
	silent   bool
	mock     bool      // MOCK=true env: skip SSH, only run queueDeletedBrigades
	mockID   uuid.UUID // -mock-id flag: SSH normally but pass -mock <id> to getwasted
	dbURL    string
	sshKeyFn string
}

func parseArgs() (config, error) {
	cfg := config{}

	dbURL := os.Getenv("DB_URL")
	if dbURL == "" {
		dbURL = defaultDatabaseURL
	}

	cfg.dbURL = dbURL
	cfg.mock = os.Getenv("MOCK") == "true"

	debug := flag.Bool("debug", false, "Debug")
	silent := flag.Bool("s", false, "Silent")
	mockID := flag.String("mock-id", "", "Pass -mock <brigade_id> to every getwasted command (for testing)")

	flag.Parse()

	cfg.debug = *debug
	cfg.silent = *silent && !*debug

	if *mockID != "" {
		id, err := uuid.Parse(*mockID)
		if err != nil {
			return cfg, fmt.Errorf("parse -mock-id: %w", err)
		}

		cfg.mockID = id
	}

	// SSH key is needed for real realm scans (both normal and mock-id modes).
	if !cfg.mock {
		sshKeyFilename, err := sshVng.LookupForSSHKeyfile(os.Getenv("SSH_KEY"), sshkeyDefaultPath)
		if err != nil {
			return cfg, fmt.Errorf("lookup for ssh key: %w", err)
		}

		cfg.sshKeyFn = sshKeyFilename
	}

	return cfg, nil
}
