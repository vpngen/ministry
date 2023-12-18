package core

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base32"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http/httputil"
	"net/netip"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	dcmgmt "github.com/vpngen/dc-mgmt"
	"golang.org/x/crypto/ssh"
)

var ErrServiceTemporarilyUnavailable = errors.New("service temporarily unavailable")

// ReplaceBrigadier - replaces brigadier.
func ReplaceBrigadier(ctx context.Context, db *pgxpool.Pool, schema string, tag string,
	sshconf *ssh.ClientConfig,
	brigadeID uuid.UUID,
) (*dcmgmt.Answer, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}

	defer tx.Rollback(ctx)

	realmID, addr, active, err := fetchBrigadeRealm(ctx, tx, schema, brigadeID)
	if err != nil {
		return nil, fmt.Errorf("fetch realm: %w", err)
	}

	if !active {
		return nil, ErrServiceTemporarilyUnavailable
	}

	vpnconf, err := callRealmReplaceBrigadier(ctx, sshconf, tag, realmID, addr, brigadeID)
	if err != nil {
		return nil, fmt.Errorf("call realm replace brigadier: %w", err)
	}

	return vpnconf, nil
}

func fetchBrigadeRealm(ctx context.Context, tx pgx.Tx, schema string, brigadeID uuid.UUID,
) (uuid.UUID, netip.AddrPort, bool, error) {
	sqlSelectRealm := `
	SELECT
		r.realm_id, r.control_ip, r.is_active
	FROM
		%s AS b					   	-- brigadiers
		JOIN %s AS br ON br.brigade_id=b.brigade_id	-- brigadier_realms
		JOIN %s AS r ON r.realm_id=br.realm_id		-- realms
	WHERE
		b.brigade_id=$1
	AND
		br.featured IS TRUE
	`

	var (
		id     uuid.UUID
		ip     netip.Addr
		active bool
	)
	if err := tx.QueryRow(ctx, fmt.Sprintf(sqlSelectRealm,
		pgx.Identifier{schema, "brigadiers"}.Sanitize(),
		pgx.Identifier{schema, "brigadier_realms"}.Sanitize(),
		pgx.Identifier{schema, "realms"}.Sanitize(),
	),
		brigadeID,
	).Scan(&id, &ip, &active); err != nil {
		return uuid.Nil, netip.AddrPort{}, false, fmt.Errorf("select: %w", err)
	}

	return id, netip.AddrPortFrom(ip, DefaultRealmsPort), active, nil
}

func callRealmReplaceBrigadier(ctx context.Context, sshconf *ssh.ClientConfig, tag string,
	realmID uuid.UUID, addr netip.AddrPort,
	brigadeID uuid.UUID,
) (*dcmgmt.Answer, error) {
	cmd := fmt.Sprintf("replacebrigadier -ch -j -id %s",
		base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(brigadeID[:]),
	)

	fmt.Fprintf(os.Stderr, "%s: %s#%s -> %s\n", tag, sshconf.User, addr, cmd)

	var (
		client   *ssh.Client
		err      error
		attempts int
	)

	for {
		client, err = ssh.Dial("tcp", addr.String(), sshconf)
		if err != nil {
			if attempts++; attempts > RealmConnectMaxAttempts {
				return nil, ErrAttemptLimitExceeded
			}

			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("ssh dial: %w", ctx.Err())
			case <-time.After(RealmConnectSleepTimeout):
			}

			continue
		}

		defer client.Close()

		break
	}

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
			fmt.Fprintf(os.Stderr, "%s: SSH Session StdErr: empty\n", tag)
		default:
			fmt.Fprintf(os.Stderr, "%s: SSH Session StdErr:\n", tag)
			for _, line := range strings.Split(errstr, "\n") {
				fmt.Fprintf(os.Stderr, "%s: | %s\n", tag, line)
			}
		}
	}()

	if err := session.Run(cmd); err != nil {
		return nil, fmt.Errorf("ssh run: %w", err)
	}

	r := bufio.NewReader(httputil.NewChunkedReader(&b))

	payload, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("chunk read: %w", err)
	}

	wgconf := &dcmgmt.Answer{}
	if err := json.Unmarshal(payload, &wgconf); err != nil {
		return nil, fmt.Errorf("json unmarshal: %w", err)
	}

	return wgconf, nil
}
