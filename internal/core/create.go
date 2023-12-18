package core

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base32"
	"encoding/base64"
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
	"github.com/vpngen/keydesk/keydesk"
	"github.com/vpngen/wordsgens/namesgenerator"
	"golang.org/x/crypto/ssh"

	dcmgmt "github.com/vpngen/dc-mgmt"
)

const DefaultRealmsPort = 22

const (
	RealmConnectMaxAttempts  = 3
	RealmConnectSleepTimeout = 2 * time.Second
	RealmSelectMaxAttempts   = 3
)

var (
	ErrBrigadeAlreadyLocated = errors.New("brigade already located")
	ErrAttemptLimitExceeded  = errors.New("attempt limit exceeded")
	ErrDraftRealmNotFound    = errors.New("draft realm not found")
)

func ComposeBrigade(ctx context.Context, db *pgxpool.Pool, schema string,
	sshconf *ssh.ClientConfig, tag string,
	partnerID uuid.UUID, brigadeID uuid.UUID,
	fullname string, person *namesgenerator.Person,
) (*dcmgmt.Answer, error) {
	attempts := 0

	for {
		if attempts++; attempts > RealmSelectMaxAttempts {
			return nil, ErrAttemptLimitExceeded
		}

		realmID, addr, err := defineBrigadeRealm(ctx, db, schema, partnerID, brigadeID)
		if err != nil {
			return nil, fmt.Errorf("define realm: %w", err)
		}

		vpnconf, err := callRealmAddBrigade(ctx, sshconf, tag, realmID, addr, brigadeID, fullname, person)
		if err != nil {
			if errors.Is(err, ErrAttemptLimitExceeded) {
				continue
			}

			return nil, fmt.Errorf("call realm add brigade: %w", err)
		}

		if vpnconf.Status != keydesk.AnswerStatusSuccess {
			continue
		}

		if err := promoteBrigadierRealm(ctx, db, schema, brigadeID, realmID); err != nil {
			return nil, fmt.Errorf("promote realm: %w", err)
		}

		return vpnconf, nil
	}
}

func callRealmAddBrigade(ctx context.Context, sshconf *ssh.ClientConfig, tag string,
	realmID uuid.UUID, addr netip.AddrPort,
	brigadeUUID uuid.UUID, fullname string, person *namesgenerator.Person,
) (*dcmgmt.Answer, error) {
	fullnameEncoded := base64.StdEncoding.WithPadding(base64.StdPadding).EncodeToString([]byte(fullname))
	personEncoded := base64.StdEncoding.WithPadding(base64.StdPadding).EncodeToString([]byte(person.Name))
	descEncoded := base64.StdEncoding.WithPadding(base64.StdPadding).EncodeToString([]byte(person.Desc))
	urlEncoded := base64.StdEncoding.WithPadding(base64.StdPadding).EncodeToString([]byte(person.URL))

	brigadeID := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(brigadeUUID[:])

	cmd := fmt.Sprintf("addbrigade -ch -j -id %s -name %s -person %s -desc %s -url %s",
		brigadeID,
		fullnameEncoded,
		personEncoded,
		descEncoded,
		urlEncoded,
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

func defineBrigadeRealm(ctx context.Context, db *pgxpool.Pool, schema string,
	partnerID uuid.UUID, brigadeID uuid.UUID,
) (uuid.UUID, netip.AddrPort, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return uuid.Nil, netip.AddrPort{}, fmt.Errorf("begin: %w", err)
	}

	defer tx.Rollback(ctx)

	ok, err := isBrigadeLocated(ctx, tx, schema, brigadeID)
	if err != nil {
		return uuid.Nil, netip.AddrPort{}, fmt.Errorf("check realm: %w", err)
	}

	if ok {
		return uuid.Nil, netip.AddrPort{}, ErrBrigadeAlreadyLocated
	}

	realmID, addr, err := selectBrigadeRealm(ctx, tx, schema, partnerID, brigadeID)
	if err != nil {
		return uuid.Nil, netip.AddrPort{}, fmt.Errorf("get realm: %w", err)
	}

	if err := storeBrigadierDraftRealm(ctx, tx, schema, brigadeID, realmID); err != nil {
		return uuid.Nil, netip.AddrPort{}, fmt.Errorf("store realm: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, netip.AddrPort{}, fmt.Errorf("commit: %w", err)
	}

	return realmID, addr, nil
}

func storeBrigadierDraftRealm(ctx context.Context, tx pgx.Tx, schema string,
	brigadeID uuid.UUID, realmID uuid.UUID,
) error {
	sqlStoreRealmRelation := `
	INSERT INTO
		%s (brigade_id,	realm_id, featured, draft)
	VALUES
		($1, $2, false, true)
	`
	if _, err := tx.Exec(ctx,
		fmt.Sprintf(sqlStoreRealmRelation, (pgx.Identifier{schema, "brigadier_realms"}.Sanitize())),
		brigadeID, realmID,
	); err != nil {
		return fmt.Errorf("insert rel: %w", err)
	}

	sqlCreateRealmAction := `
	INSERT INTO
		%s (brigade_id, realm_id, event_name, event_info, event_time)
	VALUES
		($1, $2, 'order', 'draft', NOW() AT TIME ZONE 'UTC')
	`
	if _, err := tx.Exec(ctx,
		fmt.Sprintf(sqlCreateRealmAction, (pgx.Identifier{schema, "brigadier_realms_actions"}.Sanitize())),
		brigadeID, realmID,
	); err != nil {
		return fmt.Errorf("insert action: %w", err)
	}

	return nil
}

func promoteBrigadierRealm(ctx context.Context, db *pgxpool.Pool, schema string,
	brigadeID uuid.UUID, realmID uuid.UUID,
) error {
	fmt.Fprintf(os.Stderr, "promoteBrigadierRealm: %s %s\n", brigadeID, realmID)

	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}

	defer tx.Rollback(ctx)

	sqlStoreRealmRelation := `
	UPDATE
		%s
	SET
		draft=false,
		featured=true
	WHERE
		brigade_id=$1
		AND realm_id=$2
	`
	ct, err := tx.Exec(ctx,
		fmt.Sprintf(sqlStoreRealmRelation, (pgx.Identifier{schema, "brigadier_realms"}.Sanitize())),
		brigadeID, realmID,
	)
	if err != nil {
		return fmt.Errorf("update rel: %w", err)
	}

	if ct.RowsAffected() == 0 {
		return fmt.Errorf("update rel: %w", ErrDraftRealmNotFound)
	}

	sqlCreateRealmAction := `
	INSERT INTO
		%s (brigade_id, realm_id, event_name, event_info, event_time)
	VALUES
		($1, $2, 'modify', 'promote', NOW() AT TIME ZONE 'UTC')
	`
	if _, err := tx.Exec(ctx,
		fmt.Sprintf(sqlCreateRealmAction, (pgx.Identifier{schema, "brigadier_realms_actions"}.Sanitize())),
		brigadeID, realmID,
	); err != nil {
		return fmt.Errorf("insert action: %w", err)
	}

	if err := undeleteBrigadier(ctx, tx, schema, brigadeID); err != nil {
		return fmt.Errorf("undelete: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	return nil
}

func undeleteBrigadier(ctx context.Context, tx pgx.Tx, schema string, brigadeID uuid.UUID) error {
	sqlUndeleteBrigadier := `
	DELETE FROM
		%s
	WHERE
		brigade_id=$1
	`

	comm, err := tx.Exec(ctx,
		fmt.Sprintf(sqlUndeleteBrigadier, (pgx.Identifier{schema, "deleted_brigadiers"}.Sanitize())),
		brigadeID,
	)
	if err != nil {
		return fmt.Errorf("delete: %w", err)
	}

	event := "create_brigade"
	if comm.RowsAffected() > 0 {
		event = "restore_brigade"
	}

	sqlCreateRealmAction := `
	INSERT INTO
		%s (brigade_id, event_name, event_info, event_time)
	VALUES
		($1, $2, '', NOW() AT TIME ZONE 'UTC')
	`
	if _, err := tx.Exec(ctx,
		fmt.Sprintf(sqlCreateRealmAction, (pgx.Identifier{schema, "brigades_actions"}.Sanitize())),
		brigadeID, event,
	); err != nil {
		return fmt.Errorf("insert action: %w", err)
	}

	return nil
}

func isBrigadeLocated(ctx context.Context, tx pgx.Tx, schema string,
	brigadeID uuid.UUID,
) (bool, error) {
	sqlCheckRealm := `
	SELECT
		COUNT(br.realm_id)
	FROM
		%s AS br
		JOIN %s AS r ON r.realm_id=br.realm_id
	WHERE
		br.brigade_id=$1
		AND br.draft = false
	`

	var n int
	if err := tx.QueryRow(ctx,
		fmt.Sprintf(sqlCheckRealm,
			pgx.Identifier{schema, "brigadier_realms"}.Sanitize(),
			pgx.Identifier{schema, "realms"}.Sanitize(),
		),
		brigadeID,
	).Scan(&n); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}

		return false, fmt.Errorf("select: %w", err)
	}

	if n == 0 {
		return false, nil
	}

	return true, nil
}

func selectBrigadeRealm(ctx context.Context, tx pgx.Tx, schema string,
	partnerID uuid.UUID, brigadeID uuid.UUID,
) (uuid.UUID, netip.AddrPort, error) {
	sqlSelectRealm := `
	SELECT
		r.realm_id, r.control_ip
	FROM
		%s AS b					   	-- brigadiers
		JOIN %s AS bp ON bp.brigade_id=b.brigade_id 	-- brigadier_partners
		JOIN %s AS pr ON pr.partner_id = bp.partner_id	-- partners_realms
		JOIN %s AS r ON r.realm_id=pr.realm_id		-- realms
		LEFT JOIN %s AS br ON br.realm_id=pr.realm_id AND br.brigade_id=b.brigade_id	-- brigadier_realms
	WHERE
		b.brigade_id=$1
		AND pr.partner_id=$2
		AND r.is_active=true
		AND r.open_for_regs=true
		AND r.free_slots>0
		AND br.realm_id IS NULL
	ORDER BY RANDOM() LIMIT 1
	`

	var (
		id uuid.UUID
		ip netip.Addr
	)
	if err := tx.QueryRow(ctx, fmt.Sprintf(sqlSelectRealm,
		pgx.Identifier{schema, "brigadiers"}.Sanitize(),
		pgx.Identifier{schema, "brigadier_partners"}.Sanitize(),
		pgx.Identifier{schema, "partners_realms"}.Sanitize(),
		pgx.Identifier{schema, "realms"}.Sanitize(),
		pgx.Identifier{schema, "brigadier_realms"}.Sanitize(),
	),
		brigadeID,
		partnerID,
	).Scan(&id, &ip); err != nil {
		return uuid.Nil, netip.AddrPort{}, fmt.Errorf("select: %w", err)
	}

	return id, netip.AddrPortFrom(ip, DefaultRealmsPort), nil
}
