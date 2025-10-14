package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vpngen/keydesk/keydesk"
	"github.com/vpngen/ministry"
	"github.com/vpngen/ministry/internal/core"
	"github.com/vpngen/wordsgens/namesgenerator"
	"golang.org/x/crypto/ssh"

	dcmgmt "github.com/vpngen/dc-mgmt"
)

const defaultSeedExtra = "даблять"

var seedExtra string // extra data for seed

func init() {
	seedExtra = os.Getenv("SEED_EXTRA")
	if seedExtra == "" {
		seedExtra = defaultSeedExtra
	}
}

type tgVipBrigade struct {
	BrigadeID  uuid.UUID             `json:"brigade_id"`
	TelegramID int64                 `json:"telegram_id"`
	Mnemo      string                `json:"mnemo,omitempty"`
	Person     namesgenerator.Person `json:"person,omitempty"`
	Name       string                `json:"name,omitempty"`
}

const sqlNotCreatedVIP = `
SELECT 
	bi.brigade_id,
	vt.telegram_id
FROM 
	head.brigadiers_ids bi
JOIN 
	head.brigadier_vip bv ON bi.brigade_id = bv.brigade_id
LEFT JOIN 
	head.brigadiers b ON bi.brigade_id = b.brigade_id
LEFT JOIN
	head.vip_telegram_ids vt ON bi.brigade_id = vt.brigade_id
WHERE 
	b.brigade_id IS NULL
	AND bv.vip_expire > (NOW() AT TIME ZONE 'UTC')
	AND bv.finalizer = false
`

func getNotCreatedVIP(ctx context.Context, db *pgxpool.Pool) ([]tgVipBrigade, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}

	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, sqlNotCreatedVIP)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}

	defer rows.Close()

	var (
		brigadeID  uuid.UUID
		telegramID pgtype.Int8
	)

	brigades := make([]tgVipBrigade, 0)

	if _, err := pgx.ForEachRow(rows, []any{&brigadeID, &telegramID}, func() error {
		brigades = append(brigades, tgVipBrigade{
			BrigadeID:  brigadeID,
			TelegramID: telegramID.Int64,
		})

		return nil
	}); err != nil {
		return nil, fmt.Errorf("foreach: %w", err)
	}

	return brigades, nil
}

func newCreds(ctx context.Context, db *pgxpool.Pool, sshconf *ssh.ClientConfig, silent bool) error {
	brigades, err := getNotCreatedVIP(ctx, db)
	if err != nil {
		return fmt.Errorf("get not created vip: %w", err)
	}

	if !silent || len(brigades) > 0 {
		fmt.Fprintf(os.Stderr, "%s: Found %d VIP brigades without credentials\n", LogTag, len(brigades))
	}

	for _, brigade := range brigades {
		if brigade.TelegramID == 0 {
			fmt.Fprintf(os.Stderr, "%s: ERROR: Brigade: %s has no telegram ID, can't create credentials\n", LogTag, brigade.BrigadeID)

			continue
		}

		if !silent {
			fmt.Fprintf(os.Stderr, "%s: Creating credentials for brigade: %s: %d\n", LogTag, brigade.BrigadeID, brigade.TelegramID)
		}

		brigadeID, mnemo, fullname, person, err := core.UpdateVIPBrigade(ctx, db, seedExtra, brigade.BrigadeID, "viparize")
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: ERROR: Can't create credentials for brigade: %s: %d: %s\n", LogTag, brigade.BrigadeID, brigade.TelegramID, err)

			continue
		}

		if !silent {
			fmt.Fprintf(os.Stderr, "%s: Created brigade: %s, Name: %s\n", LogTag, brigadeID, fullname)
		}

		if err := composeBrigade(ctx, db, sshconf, brigadeID, mnemo, fullname, person); err != nil {
			fmt.Fprintf(os.Stderr, "%s: ERROR: Can't compose brigade: %s: %d: %s\n", LogTag, brigade.BrigadeID, brigade.TelegramID, err)

			continue
		}
	}

	return nil
}

func composeBrigade(ctx context.Context, db *pgxpool.Pool, sshconf *ssh.ClientConfig, brigadeID uuid.UUID, mnemo, fullname string, person *namesgenerator.Person) error {
	vpnconf, err := core.ComposeBrigade(ctx, db, sshconf, LogTag, true, brigadeID, fullname, person)
	if err != nil {
		return fmt.Errorf("compose brigade: %w", err)
	}

	answ := ministry.Answer{
		Answer: dcmgmt.Answer{
			Answer: keydesk.Answer{
				Code:    http.StatusCreated,
				Desc:    http.StatusText(http.StatusCreated),
				Status:  keydesk.AnswerStatusSuccess,
				Configs: vpnconf.Answer.Configs,
			},
			KeydeskIPv6: vpnconf.KeydeskIPv6,
			FreeSlots:   vpnconf.FreeSlots,
		},
		Mnemo:  mnemo,
		Name:   fullname,
		Person: *person,
	}

	payload, err := json.Marshal(answ)
	if err != nil {
		return fmt.Errorf("marshal answer: %w", err)
	}

	if err := updateVPNConf(ctx, db, brigadeID, string(payload)); err != nil {
		return fmt.Errorf("update vpnconf: %w", err)
	}

	return nil
}

const sqlUpdateVPNConf = `
UPDATE 
	head.vip_messages
SET 
	finalizer = true,
	last_try = NOW() AT TIME ZONE 'UTC' - INTERVAL '1 HOUR',
	vpnconfig = $1
WHERE 
	brigade_id = $2
`

func updateVPNConf(ctx context.Context, db *pgxpool.Pool, brigadeID uuid.UUID, payload string) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}

	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, sqlUpdateVPNConf, payload, brigadeID); err != nil {
		return fmt.Errorf("exec: %w", err)
	}

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

const sqlNotPromotedVIP = `
SELECT 
	b.brigade_id,
	vt.telegram_id,
	vm.mnemo,
	b.brigadier,
	b.person
FROM 
	head.brigadiers b
JOIN 
	head.brigadier_vip bv ON b.brigade_id = bv.brigade_id
LEFT JOIN
	head.vip_telegram_ids vt ON b.brigade_id = vt.brigade_id
LEFT JOIN
	head.vip_messages vm ON b.brigade_id = vm.brigade_id AND vm.finalizer = false
LEFT JOIN
	head.brigadier_realms br ON b.brigade_id = br.brigade_id AND br.featured = true
WHERE 
	bv.vip_expire > (NOW() AT TIME ZONE 'UTC')
	AND bv.finalizer = false
	AND br.brigade_id IS NULL
`

func getNotPromotedVIP(ctx context.Context, db *pgxpool.Pool) ([]tgVipBrigade, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}

	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, sqlNotPromotedVIP)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}

	defer rows.Close()

	var (
		brigadeID   uuid.UUID
		telegramID  pgtype.Int8
		mnemo, name string
		person      namesgenerator.Person
	)

	brigades := make([]tgVipBrigade, 0)

	if _, err := pgx.ForEachRow(rows, []any{&brigadeID, &telegramID, &mnemo, &name, &person}, func() error {
		brigades = append(brigades, tgVipBrigade{
			BrigadeID:  brigadeID,
			TelegramID: telegramID.Int64,
			Mnemo:      mnemo,
			Name:       name,
			Person:     person,
		})
		return nil
	}); err != nil {
		return nil, fmt.Errorf("foreach: %w", err)
	}

	return brigades, nil
}

func nextTryNewBrigade(ctx context.Context, db *pgxpool.Pool, sshconf *ssh.ClientConfig, silent bool) error {
	brigades, err := getNotPromotedVIP(ctx, db)
	if err != nil {
		return fmt.Errorf("get not promoted vip: %w", err)
	}

	for _, brigade := range brigades {
		if brigade.TelegramID == 0 {
			fmt.Fprintf(os.Stderr, "%s: ERROR: Brigade: %s has no telegram ID\n", LogTag, brigade.BrigadeID)

			continue
		}

		if brigade.Mnemo == "" {
			fmt.Fprintf(os.Stderr, "%s: ERROR: Brigade: %s has no mnemo\n", LogTag, brigade.BrigadeID)

			continue
		}

		if !silent {
			fmt.Fprintf(os.Stderr, "%s: Promoting brigade: %s\n", LogTag, brigade.BrigadeID)
		}

		if err := composeBrigade(ctx, db, sshconf, brigade.BrigadeID, brigade.Mnemo, brigade.Name, &brigade.Person); err != nil {
			fmt.Fprintf(os.Stderr, "%s: ERROR: Can't compose brigade: %s: %s\n", LogTag, brigade.BrigadeID, err)

			continue
		}

	}

	return nil
}
