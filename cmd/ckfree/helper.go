package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net/netip"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vpngen/ministry/internal/core"
	"golang.org/x/crypto/ssh"
)

const (
	notActivatedHours = 12
	lowUsers1dDays    = 1
	lowUsers3dDays    = 3
	lastChanceDays    = 6 // deletion happens at 7 days, so 6 = 1 day before
	minActiveUsers    = 10
	maxResultRows     = 10000
)

const sqlActiveRealms = `
SELECT realm_id, control_ip
FROM head.realms
WHERE is_active = true
`

func getActiveRealms(ctx context.Context, db *pgxpool.Pool) ([]Realm, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}

	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, sqlActiveRealms)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}

	defer rows.Close()

	var (
		realmID uuid.UUID
		ip      netip.Addr
	)

	realms := make([]Realm, 0)

	if _, err := pgx.ForEachRow(rows, []any{&realmID, &ip}, func() error {
		realms = append(realms, Realm{
			RealmID:   realmID,
			ControlIP: netip.AddrPortFrom(ip, core.DefaultRealmsPort),
		})

		return nil
	}); err != nil {
		return nil, fmt.Errorf("foreach: %w", err)
	}

	return realms, nil
}

// dialRealm opens one SSH connection to a realm with retry on transient failures.
func dialRealm(ctx context.Context, sshconf *ssh.ClientConfig, addr netip.AddrPort) (*ssh.Client, error) {
	var (
		client   *ssh.Client
		err      error
		attempts int
	)

	for {
		client, err = ssh.Dial("tcp", addr.String(), sshconf)
		if err == nil {
			return client, nil
		}

		if attempts++; attempts > core.RealmConnectMaxAttempts {
			return nil, core.ErrAttemptLimitExceeded
		}

		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("ssh dial: %w", ctx.Err())
		case <-time.After(core.RealmConnectSleepTimeout):
		}
	}
}

// runGetWasted runs one getwasted command on an already-open SSH client
// (new session, same connection) and returns the parsed brigade UUIDs.
func runGetWasted(client *ssh.Client, cmd string) ([]uuid.UUID, error) {
	session, err := client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("ssh session: %w", err)
	}

	defer session.Close()

	var b, e bytes.Buffer

	session.Stdout = &b
	session.Stderr = &e

	defer func() {
		switch errstr := e.String(); errstr {
		case "":
		default:
			fmt.Fprintf(os.Stderr, "%s: SSH StdErr for %q:\n", LogTag, cmd)
			for _, line := range strings.Split(errstr, "\n") {
				fmt.Fprintf(os.Stderr, "%s: | %s\n", LogTag, line)
			}
		}
	}()

	if err := session.Run(cmd); err != nil {
		return nil, fmt.Errorf("ssh run: %w", err)
	}

	var ids []uuid.UUID

	scanner := bufio.NewScanner(&b)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		id, err := uuid.Parse(line)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: skip invalid uuid %q: %s\n", LogTag, line, err)

			continue
		}

		ids = append(ids, id)
	}

	return ids, scanner.Err()
}
