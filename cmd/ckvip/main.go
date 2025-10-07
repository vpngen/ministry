package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base32"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/netip"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vpngen/ministry/internal/core"
	"github.com/vpngen/ministry/internal/pgsql"
	"golang.org/x/crypto/ssh"

	sshVng "github.com/vpngen/ministry/internal/ssh"
)

const (
	LogTag = "ckvip"
)

const (
	redemtionPeriod = 24 // hours
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

	db, err := pgsql.CreateDBPool(cfg.dbURL)
	if err != nil {
		log.Fatalf("Can't create db pool: %s\n", err)
	}

	sshconf, err := sshVng.CreateSSHConfig(cfg.sshKeyFn, sshkeyRemoteUsername, sshVng.SSHDefaultTimeOut)
	if err != nil {
		log.Fatalf("Can't create ssh configs: %s\n", err)
	}

	ctx := context.Background()

	fmt.Fprintf(os.Stderr, "VIP Endpoint: %s\n", cfg.vipEndpoint)
	fmt.Fprintf(os.Stderr, "OBFS UUID: %s\n", cfg.obfsUUID)
	fmt.Fprintf(os.Stderr, "DB URL: %s\n", cfg.dbURL)

	brigades, err := fetchPaidUsers(c, cfg.obfsUUID, cfg.vipEndpoint)
	if err != nil {
		log.Fatalf("Can't fetch paid users: %s\n", err)
	}

	goods := 0
	for _, brigade := range brigades {
		fmt.Fprintf(os.Stderr, "Brigade: %s (%s), ExpiredAt: %s, UsersCount: %d\n", brigade.BrigadeID, brigade.RawBrigadeID, brigade.ExpiredAt, brigade.UsersCount)
		goods += brigade.UsersCount
	}

	fmt.Fprintf(os.Stderr, "Fetched %d paid users\n", len(brigades))
	fmt.Fprintf(os.Stderr, "Total %d goods\n", goods)

	if err := updateVIPRecords(ctx, db, cfg.obfsUUID, brigades); err != nil {
		log.Fatalf("Can't update VIP records: %s\n", err)
	}

	// try to set vip brigade
	if err := viparize(ctx, db, sshconf); err != nil {
		log.Fatalf("Can't set VIP brigades: %s\n", err)
	}

	// try to restore deleted vip brigade
	if err := viparizeDeleted(ctx, db, sshconf); err != nil {
		log.Fatalf("Can't restore deleted VIP brigades: %s\n", err)
	}

	// try to unset vip brigade
	if err := unviparize(ctx, db, sshconf); err != nil {
		log.Fatalf("Can't unset VIP brigades: %s\n", err)
	}

	fmt.Fprintf(os.Stderr, "Done\n")
}

const sqlBrigadesToUnVIParize = `
SELECT 
	b.brigade_id
FROM 
	head.brigadiers b
JOIN 
	head.brigadier_vip bv ON b.brigade_id = bv.brigade_id
LEFT JOIN
	head.deleted_brigadiers d ON b.brigade_id = d.brigade_id
WHERE 
	d.brigade_id IS NULL
	AND v.vip_expire < (NOW() AT TIME ZONE 'UTC' - $1 * INTERVAL '1 HOUR')
	AND bv.finalizer = true
`

func getBrigadesToUnVIParize(ctx context.Context, db *pgxpool.Pool) ([]uuid.UUID, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}

	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, sqlBrigadesToUnVIParize, redemtionPeriod)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}

	defer rows.Close()

	var brigadeID uuid.UUID
	brigades := make([]uuid.UUID, 0)

	if _, err := pgx.ForEachRow(rows, []any{&brigadeID}, func() error {
		brigades = append(brigades, brigadeID)

		return nil
	}); err != nil {
		return nil, fmt.Errorf("foreach: %w", err)
	}

	return brigades, nil
}

const sqlBrigadesToVIParize = `
SELECT 
	b.brigade_id
FROM 
	head.brigadiers b
JOIN 
	head.brigadier_vip bv ON b.brigade_id = bv.brigade_id
LEFT JOIN
	head.deleted_brigadiers d ON b.brigade_id = d.brigade_id
WHERE 
	d.brigade_id IS NULL
	AND v.vip_expire > (NOW() AT TIME ZONE 'UTC')
	AND bv.finalizer = false
`

func getBrigadesToVIParize(ctx context.Context, db *pgxpool.Pool) ([]uuid.UUID, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}

	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, sqlBrigadesToVIParize)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}

	defer rows.Close()

	var brigadeID uuid.UUID
	brigades := make([]uuid.UUID, 0)

	if _, err := pgx.ForEachRow(rows, []any{&brigadeID}, func() error {
		brigades = append(brigades, brigadeID)

		return nil
	}); err != nil {
		return nil, fmt.Errorf("foreach: %w", err)
	}

	return brigades, nil
}

const sqlDeletedBrigadesToVIParize = `
SELECT 
	b.brigade_id
FROM 
	head.brigadiers b
JOIN 
	head.brigadier_vip bv ON b.brigade_id = bv.brigade_id
LEFT JOIN
	head.deleted_brigadiers d ON b.brigade_id = d.brigade_id
WHERE 
	d.brigade_id IS NOT NULL
	AND v.vip_expire > (NOW() AT TIME ZONE 'UTC')
	AND bv.finalizer = false
`

func getDeletedBrigadesToVIParize(ctx context.Context, db *pgxpool.Pool) ([]uuid.UUID, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}

	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, sqlDeletedBrigadesToVIParize)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}

	defer rows.Close()

	var brigadeID uuid.UUID
	brigades := make([]uuid.UUID, 0)

	if _, err := pgx.ForEachRow(rows, []any{&brigadeID}, func() error {
		brigades = append(brigades, brigadeID)

		return nil
	}); err != nil {
		return nil, fmt.Errorf("foreach: %w", err)
	}

	return brigades, nil
}

var ErrServiceTemporarilyUnavailable = errors.New("service temporarily unavailable")

func viparize(ctx context.Context, db *pgxpool.Pool, ssh *ssh.ClientConfig) error {
	brigades, err := getBrigadesToVIParize(ctx, db)
	if err != nil {
		return fmt.Errorf("get brigades to viparize: %w", err)
	}

	for _, brigadeID := range brigades {
		fmt.Fprintf(os.Stderr, "Set VIP for brigade: %s\n", brigadeID)

		_, addr, err := fetchBrigadeRealm(ctx, db, brigadeID)
		if err != nil {
			return fmt.Errorf("fetch realm: %w", err)
		}

		if err := callRealmViparizeBrigadier(ctx, ssh, LogTag, addr, true, brigadeID); err != nil {
			fmt.Fprintf(os.Stderr, "Can't call realm viparize brigadier: %s\n", err)

			continue
		}

		if err := setFinalizer(ctx, db, brigadeID); err != nil {
			fmt.Fprintf(os.Stderr, "Can't set finalizer: %s\n", err)

			continue
		}

		fmt.Fprintf(os.Stderr, "Brigade %s VIP set\n", brigadeID)
	}

	return nil
}

func unviparize(ctx context.Context, db *pgxpool.Pool, ssh *ssh.ClientConfig) error {
	brigades, err := getBrigadesToUnVIParize(ctx, db)
	if err != nil {
		return fmt.Errorf("get brigades to viparize: %w", err)
	}

	for _, brigadeID := range brigades {
		fmt.Fprintf(os.Stderr, "Set VIP for brigade: %s\n", brigadeID)

		_, addr, err := fetchBrigadeRealm(ctx, db, brigadeID)
		if err != nil {
			return fmt.Errorf("fetch realm: %w", err)
		}

		if err := callRealmViparizeBrigadier(ctx, ssh, LogTag, addr, false, brigadeID); err != nil {
			fmt.Fprintf(os.Stderr, "Can't call realm unviparize brigadier: %s\n", err)

			continue
		}

		if err := dropFinalizer(ctx, db, brigadeID); err != nil {
			fmt.Fprintf(os.Stderr, "Can't set finalizer: %s\n", err)

			continue
		}

		fmt.Fprintf(os.Stderr, "Brigade %s VIP unset\n", brigadeID)
	}

	return nil
}

func viparizeDeleted(ctx context.Context, db *pgxpool.Pool, ssh *ssh.ClientConfig) error {
	brigades, err := getDeletedBrigadesToVIParize(ctx, db)
	if err != nil {
		return fmt.Errorf("get deleted brigades to viparize: %w", err)
	}

	for _, brigadeID := range brigades {
		fmt.Fprintf(os.Stderr, "Restore deleted brigade: %s\n", brigadeID)

		_, addr, err := core.DefineBrigadeRealm(ctx, db, brigadeID)
		if err != nil {
			return fmt.Errorf("fetch realm: %w", err)
		}

		fmt.Fprintf(os.Stderr, "Set VIP for brigade: %s\n", brigadeID)

		if err := callRealmViparizeBrigadier(ctx, ssh, LogTag, addr, true, brigadeID); err != nil {
			fmt.Fprintf(os.Stderr, "Can't call realm viparize brigadier: %s\n", err)

			continue
		}

		if err := setFinalizer(ctx, db, brigadeID); err != nil {
			fmt.Fprintf(os.Stderr, "Can't set finalizer: %s\n", err)

			continue
		}

		fmt.Fprintf(os.Stderr, "Brigade %s VIP set\n", brigadeID)
	}

	return nil
}

const sqlSetFinalizer = `
UPDATE 
	head.brigadier_vip
SET 
	finalizer = true
WHERE 
	brigade_id = $1
`

const addSetVipAction = `
INSERT INTO 
	head.brigadier_vip_actions
		(brigade_id, event_name, event_info, event_time)
	VALUES 
		($1, $2, $3, NOW() AT TIME ZONE 'UTC')
`

func setFinalizer(ctx context.Context, db *pgxpool.Pool, brigadeID uuid.UUID) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}

	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, sqlSetFinalizer, brigadeID); err != nil {
		return fmt.Errorf("exec finalizer: %w", err)
	}

	if _, err := tx.Exec(ctx, addSetVipAction, brigadeID, "begin", ""); err != nil {
		return fmt.Errorf("exec action: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	return nil
}

const sqlDropFinalizer = `
UPDATE 
	head.brigadier_vip
SET 
	finalizer = false
WHERE 
	brigade_id = $1
`

func dropFinalizer(ctx context.Context, db *pgxpool.Pool, brigadeID uuid.UUID) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}

	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, sqlDropFinalizer, brigadeID); err != nil {
		return fmt.Errorf("exec finalizer: %w", err)
	}

	if _, err := tx.Exec(ctx, addSetVipAction, brigadeID, "end", ""); err != nil {
		return fmt.Errorf("exec action: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	return nil
}

func callRealmViparizeBrigadier(ctx context.Context, sshconf *ssh.ClientConfig, tag string,
	addr netip.AddrPort, vip bool, brigadeID uuid.UUID,
) error {
	bid := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(brigadeID[:])

	cmd := fmt.Sprintf("vipon -ch -id %s", bid)
	if !vip {
		cmd = fmt.Sprintf("vipoff -ch -id %s", bid)
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

	r := bufio.NewReader(httputil.NewChunkedReader(&b))

	if _, err := io.ReadAll(r); err != nil {
		return fmt.Errorf("chunk read: %w", err)
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

const addVIP = `
INSERT INTO 
	head.brigadier_vip 
		(brigade_id, vip_expire, vip_users)
	VALUES 
		($1, $2, $3)
ON CONFLICT (brigade_id) DO UPDATE 
	SET 
		vip_expire = EXCLUDED.vip_expire,
		vip_users = EXCLUDED.vip_users
`

const purgeExpired = `
DELETE FROM 
	head.brigadier_vip 
WHERE 
	vip_expire < (NOW() AT TIME ZONE 'UTC' - $1 * INTERVAL '1 HOUR')
	AND finalizer = false
`

// set new VIP records or update old ones
func updateVIPRecords(ctx context.Context, db *pgxpool.Pool, obfsUUID uuid.UUID, brigades map[uuid.UUID]VipBrigade) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}

	defer tx.Rollback(ctx)

	for obfsUUID, brigade := range brigades {
		fmt.Fprintf(os.Stderr, "Brigade: %s (%s), ExpiredAt: %s, UsersCount: %d\n", brigade.BrigadeID, obfsUUID, brigade.ExpiredAt, brigade.UsersCount)

		// purge expired and deleted VIP records
		if _, err := tx.Exec(ctx, purgeExpired, redemtionPeriod); err != nil {
			return fmt.Errorf("purge expired: %w", err)
		}

		// set or update existing VIP record
		if _, err := tx.Exec(ctx, addVIP, brigade.BrigadeID, brigade.ExpiredAt, brigade.UsersCount); err != nil {
			return fmt.Errorf("set vip: %w", err)
		}
	}

	return nil
}

func obfs2uuid(in, obfsUUID uuid.UUID) uuid.UUID {
	var id uuid.UUID

	for i := range len(in) {
		id[i] = in[i] ^ obfsUUID[i]
	}

	return id
}

func fetchPaidUsers(c *http.Client, obfsUUID uuid.UUID, ep string) (map[uuid.UUID]VipBrigade, error) {
	req, err := http.NewRequest(http.MethodPost, "https://"+ep, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}

	defer resp.Body.Close()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	// OBFS_UUID="d8fc0859-c1a4-4d29-94e1-4fdea70ff8b8"
	// fmt.Printf("%s\n", payload)

	var userList PaidUsersPesponse
	if err := json.Unmarshal(payload, &userList); err != nil {
		return nil, fmt.Errorf("unmarshal response body: %w", err)
	}

	brigades := make(map[uuid.UUID]VipBrigade, 0)
	for _, u := range userList.Data {
		brigadeID := obfs2uuid(u.UserID, obfsUUID)

		if brigade, ok := brigades[brigadeID]; ok {
			if brigade.ExpiredAt.Before(u.GoodExpiryDateTime) {
				brigade.ExpiredAt = u.GoodExpiryDateTime
			}

			brigade.UsersCount++

			brigades[brigadeID] = brigade

			continue
		}

		brigades[brigadeID] = VipBrigade{
			RawBrigadeID: u.UserID,
			BrigadeID:    brigadeID,
			ExpiredAt:    u.GoodExpiryDateTime,
			UsersCount:   1,
		}
	}

	return brigades, nil
}
