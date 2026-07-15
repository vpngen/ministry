package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	dcmgmt "github.com/vpngen/dc-mgmt"
	"github.com/vpngen/ministry/internal/core"
	"github.com/vpngen/wordsgens/namesgenerator"
	"golang.org/x/crypto/ssh"
)

const sqlBrigadesToVIParize = `
SELECT
	b.brigade_id,
	vt.telegram_id,
	vt.lang
FROM
	head.brigadiers b
JOIN
	head.brigadier_vip bv ON b.brigade_id = bv.brigade_id
LEFT JOIN
	head.deleted_brigadiers d ON b.brigade_id = d.brigade_id
LEFT JOIN
	head.vip_messages vm ON b.brigade_id = vm.brigade_id AND vm.finalizer = false
LEFT JOIN
	head.free_telegram_ids vt ON b.brigade_id = vt.brigade_id
WHERE
	d.brigade_id IS NULL
	AND vm.brigade_id IS NULL
	AND bv.vip_expire > (NOW() AT TIME ZONE 'UTC')
	AND bv.finalizer = false
`

type brigadeToVIParize struct {
	BrigadeID  uuid.UUID
	TelegramID int64
	Lang       string
}

func getBrigadesToVIParize(ctx context.Context, db *pgxpool.Pool) ([]brigadeToVIParize, error) {
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

	var (
		brigadeID  uuid.UUID
		telegramID pgtype.Int8
		lang       pgtype.Text
	)

	brigades := make([]brigadeToVIParize, 0)

	if _, err := pgx.ForEachRow(rows, []any{&brigadeID, &telegramID, &lang}, func() error {
		brigades = append(brigades, brigadeToVIParize{
			BrigadeID:  brigadeID,
			TelegramID: telegramID.Int64,
			Lang:       lang.String,
		})

		return nil
	}); err != nil {
		return nil, fmt.Errorf("foreach: %w", err)
	}

	return brigades, nil
}

const sqlDeletedBrigadesToVIParize = `
SELECT 
	b.brigade_id,
	b.brigadier,
	b.person
FROM 
	head.brigadiers b
JOIN 
	head.brigadier_vip bv ON b.brigade_id = bv.brigade_id
LEFT JOIN
	head.deleted_brigadiers d ON b.brigade_id = d.brigade_id
LEFT JOIN
	head.vip_messages vm ON b.brigade_id = vm.brigade_id AND vm.finalizer = false
WHERE 
	d.brigade_id IS NOT NULL
	AND vm.brigade_id IS NULL
	AND bv.vip_expire > (NOW() AT TIME ZONE 'UTC')
	AND bv.finalizer = false
`

type xBrigade struct {
	BrigadeID uuid.UUID
	Name      string
	Person    namesgenerator.Person
}

func getDeletedBrigadesToVIParize(ctx context.Context, db *pgxpool.Pool) ([]xBrigade, error) {
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

	var (
		brigadeID uuid.UUID
		name      string
		person    namesgenerator.Person
	)

	brigades := make([]xBrigade, 0)

	if _, err := pgx.ForEachRow(rows, []any{&brigadeID, &name, &person}, func() error {
		brigades = append(brigades, xBrigade{
			BrigadeID: brigadeID,
			Name:      name,
			Person:    person,
		})

		return nil
	}); err != nil {
		return nil, fmt.Errorf("foreach: %w", err)
	}

	return brigades, nil
}

func viparize(ctx context.Context, db *pgxpool.Pool, sshconf *ssh.ClientConfig, testTelegramIDs map[int64]struct{}, silent bool) error {
	brigades, err := getBrigadesToVIParize(ctx, db)
	if err != nil {
		return fmt.Errorf("get brigades to viparize: %w", err)
	}

	if !silent || len(brigades) > 0 {
		fmt.Fprintf(os.Stderr, "%s: Found %d brigades to viparize\n", LogTag, len(brigades))
	}

	for _, brigade := range brigades {
		fmt.Fprintf(os.Stderr, "%s: Set VIP for brigade: %s\n", LogTag, brigade.BrigadeID)

		if _, isTest := testTelegramIDs[brigade.TelegramID]; !isTest {
			_, addr, err := fetchBrigadeRealm(ctx, db, brigade.BrigadeID)
			if err != nil {
				return fmt.Errorf("fetch realm: %w", err)
			}

			if err := callRealmViparizeBrigadier(ctx, sshconf, LogTag, addr, true, brigade.BrigadeID); err != nil {
				fmt.Fprintf(os.Stderr, "%s: Can't call realm viparize brigadier: %s\n", LogTag, err)

				continue
			}
		}

		if err := setFinalizer(ctx, db, brigade.BrigadeID); err != nil {
			fmt.Fprintf(os.Stderr, "%s: Can't set finalizer: %s\n", LogTag, err)

			continue
		}

		// This brigade already has a working config - there's nothing new to
		// generate, just a short "your brigade is now VIP" notification to send.
		if brigade.TelegramID != 0 {
			lang := brigade.Lang
			if lang == "" {
				lang = "ru"
			}

			// readmsgs delivers off vip_telegram_ids regardless of which path
			// discovered the telegram link, so make sure it's populated here too.
			if err := linkAndNotifyVIPUpgrade(ctx, db, brigade.BrigadeID, brigade.TelegramID, lang); err != nil {
				fmt.Fprintf(os.Stderr, "%s: Can't queue VIP upgrade notification: %s\n", LogTag, err)
			}
		}

		fmt.Fprintf(os.Stderr, "%s: Brigade %s VIP set\n", LogTag, brigade.BrigadeID)
	}

	return nil
}

func viparizeDeleted(ctx context.Context, db *pgxpool.Pool, sshconf *ssh.ClientConfig, mock bool, debug bool, silent bool) error {
	brigades, err := getDeletedBrigadesToVIParize(ctx, db)
	if err != nil {
		return fmt.Errorf("get deleted brigades to viparize: %w", err)
	}

	if !silent || len(brigades) > 0 {
		fmt.Fprintf(os.Stderr, "%s: Found %d deleted brigades to viparize\n", LogTag, len(brigades))
	}

	for _, brigade := range brigades {
		fmt.Fprintf(os.Stderr, "%s: Restore deleted brigade: %s:%s\n", LogTag, brigade.Name, brigade.BrigadeID)

		var (
			vpnconf *dcmgmt.Answer
			err     error
		)

		vpnconf, err = core.ComposeBrigade(ctx, db, sshconf, LogTag, true, false, brigade.BrigadeID, brigade.Name, &brigade.Person)

		if err != nil {
			return fmt.Errorf("compose brigade: %w", err)
		}

		if err := setFinalizer(ctx, db, brigade.BrigadeID); err != nil {
			fmt.Fprintf(os.Stderr, "%s: Can't set finalizer: %s\n", LogTag, err)

			continue
		}

		jsonData, err := json.MarshalIndent(vpnconf, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: Can't marshal vpnconf: %s\n", LogTag, err)
		}

		if debug {
			fmt.Fprintf(os.Stderr, "%s: VPNCONF: %s\n", LogTag, jsonData)
		}

		fmt.Fprintf(os.Stderr, "%s: Brigade %s VIP set\n", LogTag, brigade.BrigadeID)
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

const sqlStoreVIPUpgradeNotifyMessage = `
INSERT INTO
	head.vip_messages (brigade_id, finalizer, vip_upgrade_notify)
VALUES
	($1, true, true)
ON CONFLICT (brigade_id) DO UPDATE
	SET finalizer = true, vip_upgrade_notify = true
`

// linkAndNotifyVIPUpgrade links the telegram_id (discovered via
// free_telegram_ids) into head.vip_telegram_ids and queues a plain "your
// brigade is now VIP" notice in one transaction. Both must succeed together:
// by this point setFinalizer has already flipped bv.finalizer to true, so a
// partial failure here would drop the brigade out of getBrigadesToVIParize's
// candidate set permanently, with no notification ever queued and no retry.
func linkAndNotifyVIPUpgrade(ctx context.Context, db *pgxpool.Pool, brigadeID uuid.UUID, telegramID int64, lang string) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}

	defer tx.Rollback(ctx)

	if err := core.StoreVIPTelegramID(ctx, tx, brigadeID, telegramID, lang); err != nil {
		return fmt.Errorf("link vip telegram id: %w", err)
	}

	if _, err := tx.Exec(ctx, sqlStoreVIPUpgradeNotifyMessage, brigadeID); err != nil {
		return fmt.Errorf("store notify message: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	return nil
}
