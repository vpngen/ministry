package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/google/uuid"
	"github.com/vpngen/ministry/internal/pgsql"
	sshVng "github.com/vpngen/ministry/internal/ssh"
	"golang.org/x/crypto/ssh"
)

func main() {
	cfg, err := parseArgs()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Can't parse args: %s\n", err)
		os.Exit(1)
	}

	ctx := context.Background()

	var sshconf *ssh.ClientConfig
	if !cfg.mock {
		sshconf, err = sshVng.CreateSSHConfig(cfg.sshKeyFn, sshkeyRemoteUsername, sshVng.SSHDefaultTimeOut)
		if err != nil {
			log.Fatalf("%s: Can't create ssh config: %s\n", LogTag, err)
		}
	}

	db, err := pgsql.CreateDBPool(cfg.dbURL)
	if err != nil {
		log.Fatalf("%s: Can't create db pool: %s\n", LogTag, err)
	}

	// brigade_deleted is sourced from ministry DB directly, no SSH needed.
	if err := queueDeletedBrigades(ctx, db, cfg.silent); err != nil {
		log.Fatalf("%s: Can't queue deleted brigades: %s\n", LogTag, err)
	}

	if cfg.mock {
		if !cfg.silent {
			fmt.Fprintf(os.Stderr, "%s: Mock mode, skipping SSH realm scan\n", LogTag)
		}

		return
	}

	realms, err := getActiveRealms(ctx, db)
	if err != nil {
		log.Fatalf("%s: Can't get active realms: %s\n", LogTag, err)
	}

	if !cfg.silent {
		fmt.Fprintf(os.Stderr, "%s: Found %d active realms\n", LogTag, len(realms))
	}

	// Each step maps to one getwasted command and the notification event type it produces.
	// All steps are run on the same SSH connection per realm to avoid repeated handshakes.
	type step struct {
		cmd       string
		eventType string
	}

	mockSuffix := ""
	if cfg.mockID != uuid.Nil {
		mockSuffix = fmt.Sprintf(" -mock %s", cfg.mockID)

		if !cfg.silent {
			fmt.Fprintf(os.Stderr, "%s: mock-id mode, injecting brigade %s into all getwasted commands\n", LogTag, cfg.mockID)
		}
	}

	steps := []step{
		{
			cmd:       fmt.Sprintf("getwasted notvisited -d %d -n %d%s", notActivatedDays, maxResultRows, mockSuffix),
			eventType: EventNotActivated,
		},
		{
			cmd:       fmt.Sprintf("getwasted inactive -x %d -d %d -n %d%s", minActiveUsers, lowUsers1dDays, maxResultRows, mockSuffix),
			eventType: EventLowUsers1d,
		},
		{
			cmd:       fmt.Sprintf("getwasted inactive -x %d -d %d -n %d%s", minActiveUsers, lowUsers3dDays, maxResultRows, mockSuffix),
			eventType: EventLowUsers3d,
		},
		{
			cmd:       fmt.Sprintf("getwasted inactive -x %d -d %d -n %d%s", minActiveUsers, lastChanceDays, maxResultRows, mockSuffix),
			eventType: EventLastChance,
		},
	}

	// Collect brigade IDs from all realms first, grouped by event type.
	// One SSH client per realm; all steps run as separate sessions on that client.
	collected := make(map[string][]uuid.UUID)

	for _, realm := range realms {
		if !cfg.silent {
			fmt.Fprintf(os.Stderr, "%s: connecting to realm %s (%s)\n", LogTag, realm.RealmID, realm.ControlIP)
		}

		client, err := dialRealm(ctx, sshconf, realm.ControlIP)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: dial realm %s: %s\n", LogTag, realm.RealmID, err)

			continue
		}

		for _, s := range steps {
			ids, err := runGetWasted(client, s.cmd)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s: %s realm %s: %s\n", LogTag, s.eventType, realm.RealmID, err)

				continue
			}

			if !cfg.silent {
				fmt.Fprintf(os.Stderr, "%s: realm %s: %d %s brigades\n", LogTag, realm.RealmID, len(ids), s.eventType)
			}

			collected[s.eventType] = append(collected[s.eventType], ids...)
		}

		client.Close()
	}

	// One batch insert per event type. ON CONFLICT DO NOTHING ensures brigades that
	// already have this notification queued or sent are silently skipped.
	for _, s := range steps {
		if err := queueNotifications(ctx, db, collected[s.eventType], s.eventType, cfg.silent); err != nil {
			fmt.Fprintf(os.Stderr, "%s: queue %s: %s\n", LogTag, s.eventType, err)
		}
	}

	if !cfg.silent {
		fmt.Fprintf(os.Stderr, "%s: Done\n", LogTag)
	}
}
