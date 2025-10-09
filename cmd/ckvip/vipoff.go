package main

import (
	"context"
	"fmt"
	"os"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/ssh"
)

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
	AND bv.vip_expire < (NOW() AT TIME ZONE 'UTC' - $1 * INTERVAL '1 HOUR')
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

func unviparize(ctx context.Context, db *pgxpool.Pool, sshconf *ssh.ClientConfig, silent bool) error {
	brigades, err := getBrigadesToUnVIParize(ctx, db)
	if err != nil {
		return fmt.Errorf("get brigades to viparize: %w", err)
	}

	if !silent || len(brigades) > 0 {
		fmt.Fprintf(os.Stderr, "%s: Found %d brigades to unviparize\n", LogTag, len(brigades))
	}

	for _, brigadeID := range brigades {
		fmt.Fprintf(os.Stderr, "%s: Set VIP for brigade: %s\n", LogTag, brigadeID)

		_, addr, err := fetchBrigadeRealm(ctx, db, brigadeID)
		if err != nil {
			return fmt.Errorf("fetch realm: %w", err)
		}

		if err := callRealmViparizeBrigadier(ctx, sshconf, LogTag, addr, false, brigadeID); err != nil {
			fmt.Fprintf(os.Stderr, "%s: Can't call realm unviparize brigadier: %s\n", LogTag, err)

			continue
		}

		if err := dropFinalizer(ctx, db, brigadeID); err != nil {
			fmt.Fprintf(os.Stderr, "%s: Can't set finalizer: %s\n", LogTag, err)

			continue
		}

		fmt.Fprintf(os.Stderr, "%s: Brigade %s VIP unset\n", LogTag, brigadeID)
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
