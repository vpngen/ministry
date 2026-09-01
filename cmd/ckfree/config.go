package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

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

	// Brigades created with one of these start labels (createbrigade -l,
	// stored in head.start_labels) get NO free-brigade lifecycle
	// notifications - conference/campaign cohorts promised no nagging.
	// Same concept (and env var) as the deletion guard in
	// scripts/delete_brigadier.sh. Space-separated; PROTECTED_LABELS=""
	// disables the exemption.
	defaultProtectedLabels = "global-gathering"
)

type config struct {
	debug           bool
	silent          bool
	mock            bool      // MOCK=true env: skip SSH, only run queueDeletedBrigades
	mockID          uuid.UUID // -mock-id flag: SSH normally but pass -mock <id> to getwasted
	dbURL           string
	sshKeyFn        string
	protectedLabels []string
}

func parseArgs() (config, error) {
	cfg := config{}

	dbURL := os.Getenv("DB_URL")
	if dbURL == "" {
		dbURL = defaultDatabaseURL
	}

	cfg.dbURL = dbURL
	cfg.mock = os.Getenv("MOCK") == "true"

	// Explicit-empty disables the exemption; unset takes the default.
	labels, ok := os.LookupEnv("PROTECTED_LABELS")
	if !ok {
		labels = defaultProtectedLabels
	}

	cfg.protectedLabels = strings.Fields(labels)

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
