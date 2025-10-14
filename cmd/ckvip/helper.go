package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base32"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vpngen/ministry/internal/core"
	"golang.org/x/crypto/ssh"
)

var ErrServiceTemporarilyUnavailable = errors.New("service temporarily unavailable")

func callRealmViparizeBrigadier(ctx context.Context, sshconf *ssh.ClientConfig, tag string,
	addr netip.AddrPort, vip bool, brigadeID uuid.UUID,
) error {
	bid := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(brigadeID[:])

	cmd := fmt.Sprintf("vipon -id %s", bid)
	if !vip {
		cmd = fmt.Sprintf("vipoff -id %s", bid)
	}

	fmt.Fprintf(os.Stderr, "%s: %s#%s -> %s\n", tag, sshconf.User, addr, cmd)

	var (
		client   *ssh.Client
		err      error
		attempts int
	)

	for {
		client, err = ssh.Dial("tcp", addr.String(), sshconf)
		if err != nil {
			if attempts++; attempts > core.RealmConnectMaxAttempts {
				return core.ErrAttemptLimitExceeded
			}

			select {
			case <-ctx.Done():
				return fmt.Errorf("ssh dial: %w", ctx.Err())
			case <-time.After(core.RealmConnectSleepTimeout):
			}

			continue
		}

		defer client.Close()

		break
	}

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("ssh session: %w", err)
	}

	defer session.Close()

	var b, e bytes.Buffer

	session.Stdout = &b
	session.Stderr = &e

	defer func() {
		switch errstr := e.String(); errstr {
		case "":
			fmt.Fprintf(os.Stderr, "%s: SSH Session StdErr: empty\n", tag)
		default:
			fmt.Fprintf(os.Stderr, "%s: SSH Session StdErr:\n", tag)
			for _, line := range strings.Split(errstr, "\n") {
				fmt.Fprintf(os.Stderr, "%s: | %s\n", tag, line)
			}
		}
	}()

	if err := session.Run(cmd); err != nil {
		return fmt.Errorf("ssh run: %w", err)
	}

	r := bufio.NewReader(&b)

	if _, err := io.ReadAll(r); err != nil {
		return fmt.Errorf("read: %w", err)
	}

	return nil
}

func fetchBrigadeRealm(ctx context.Context, db *pgxpool.Pool, brigadeID uuid.UUID) (uuid.UUID, netip.AddrPort, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return uuid.Nil, netip.AddrPort{}, fmt.Errorf("begin: %w", err)
	}

	defer tx.Rollback(ctx)

	realmID, addr, active, err := core.FetchBrigadeRealm(ctx, tx, brigadeID)
	if err != nil {
		return realmID, addr, fmt.Errorf("fetch realm: %w", err)
	}

	if !active {
		return realmID, addr, ErrServiceTemporarilyUnavailable
	}

	return realmID, addr, nil
}

func obfs2uuid(in, obfsUUID uuid.UUID) uuid.UUID {
	var id uuid.UUID

	for i := range len(in) {
		id[i] = in[i] ^ obfsUUID[i]
	}

	return id
}
