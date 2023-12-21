package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vpngen/wordsgens/namesgenerator"
	"github.com/vpngen/wordsgens/seedgenerator"
)

const maxCollisionAttemts = 1000

var (
	ErrEmptyAccessToken = errors.New("token not specified")
	ErrAccessDenied     = errors.New("access denied")
	ErrMaxCollisions    = errors.New("max collision attempts")
	ErrLabelTooLong     = errors.New("label too long")
)

const brigadeCreationType = "ssh_api"

func createBrigadeEvent(ctx context.Context, tx pgx.Tx, schema string, id uuid.UUID, info string) error {
	sqlCreateBrigadeEvent := `
	INSERT INTO
		%s (brigade_id,	event_name, event_info,	event_time)
	VALUES
		($1, 'create_brigade',	$2, NOW() AT TIME ZONE 'UTC')
	`
	if _, err := tx.Exec(ctx,
		fmt.Sprintf(sqlCreateBrigadeEvent, (pgx.Identifier{schema, "brigades_actions"}.Sanitize())),
		id, info,
	); err != nil {
		return fmt.Errorf("create brigade event: %w", err)
	}

	return nil
}

func storeBrigadierPartner(ctx context.Context, tx pgx.Tx, schema string,
	id uuid.UUID, partnerID uuid.UUID,
) error {
	sqlSelectPartner := `
		SELECT 
			p.partner_id
		FROM 
			%s AS p 					-- partners
		WHERE
			p.partner_id=$1
			AND p.is_active=true
			AND p.open_for_regs=true
		LIMIT 1
		`

	var pid uuid.UUID
	if err := tx.QueryRow(
		ctx,
		fmt.Sprintf(
			sqlSelectPartner,
			pgx.Identifier{schema, "partners"}.Sanitize(),
		),
		partnerID,
	).Scan(&pid); err != nil {
		return fmt.Errorf("select partner: %w", err)
	}

	sqlStorePartnerRelation := `
	INSERT INTO
		%s (brigade_id,	partner_id)
	VALUES
		($1, $2)
	`
	if _, err := tx.Exec(ctx,
		fmt.Sprintf(sqlStorePartnerRelation, (pgx.Identifier{schema, "brigadier_partners"}.Sanitize())),
		id, partnerID,
	); err != nil {
		return fmt.Errorf("store partner relation: %w", err)
	}

	sqlCreatePartnerAction := `
	INSERT INTO
		%s (brigade_id, partner_id, event_name, event_info, event_time)
	VALUES
		($1, $2, 'assign', '', NOW() AT TIME ZONE 'UTC')
	`
	if _, err := tx.Exec(ctx,
		fmt.Sprintf(sqlCreatePartnerAction, (pgx.Identifier{schema, "brigadier_partners_actions"}.Sanitize())),
		id, partnerID,
	); err != nil {
		return fmt.Errorf("create partner action: %w", err)
	}

	return nil
}

func storeBrigadierSaltKey(ctx context.Context, tx pgx.Tx, schema string, id uuid.UUID) (string, error) {
	mnemo, seed, salt, err := seedgenerator.Seed(seedgenerator.ENT64, seedExtra)
	if err != nil {
		return "", fmt.Errorf("gen seed6: %w", err)
	}

	sqlCreateBrigadierSalt := `
	INSERT INTO
		%s (brigade_id,	salt)
	VALUES
		($1, $2)
	`
	if _, err = tx.Exec(ctx,
		fmt.Sprintf(sqlCreateBrigadierSalt, (pgx.Identifier{schema, "brigadier_salts"}.Sanitize())),
		id, salt,
	); err != nil {
		return "", fmt.Errorf("create brigadier salt: %w", err)
	}

	sqlCreateBrigadierKey := `
	INSERT INTO
		%s (brigade_id,	key)
	VALUES
		($1, $2)
	`
	if _, err = tx.Exec(ctx,
		fmt.Sprintf(sqlCreateBrigadierKey, (pgx.Identifier{schema, "brigadier_keys"}.Sanitize())),
		id, seed,
	); err != nil {
		return "", fmt.Errorf("create brigadier key: %w", err)
	}

	return mnemo, nil
}

func defineBrigadierPerson(ctx context.Context, tx pgx.Tx, schema string, id uuid.UUID,
) (string, *namesgenerator.Person, error) {
	sqlCreateBrigadier := `
	INSERT INTO
		%s (brigade_id,	brigadier, person)
	VALUES
		($1, $2, $3)
	`
	sql := fmt.Sprintf(
		sqlCreateBrigadier,
		pgx.Identifier{schema, "brigadiers"}.Sanitize(),
	)

	cnt := 0
	for {

		if cnt++; cnt > maxCollisionAttemts {
			return "", nil, fmt.Errorf("create brigadier: %w: %d", ErrMaxCollisions, cnt)
		}

		fullname, person, err := namesgenerator.PhysicsAwardeeShort()
		if err != nil {
			return "", nil, fmt.Errorf("physics generate: %s", err)
		}

		if _, err = tx.Exec(ctx, sql, id, fullname, person); err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) {
				switch pgErr.ConstraintName {
				case "brigadiers_brigadier_key":
					continue
				default:
					return "", nil, fmt.Errorf("create brigadier: %w", pgErr)
				}
			}
		}

		return fullname, &person, nil
	}
}

func defineBrigadeID(ctx context.Context, tx pgx.Tx, schema string) (uuid.UUID, error) {
	sqlInsertID := `INSERT INTO %s (brigade_id, created_at) VALUES ($1, NOW() AT TIME ZONE 'UTC')`
	sql := fmt.Sprintf(sqlInsertID, pgx.Identifier{schema, "brigadiers_ids"}.Sanitize())

	id := uuid.New()

	cnt := 0
	for {
		if cnt++; cnt > maxCollisionAttemts {
			return id, fmt.Errorf("create brigadier: %w: %d", ErrMaxCollisions, cnt)
		}

		if _, err := tx.Exec(ctx, sql, id); err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) {
				switch pgErr.ConstraintName {
				case "brigadiers_ids_brigade_id_key":
					id = uuid.New()
					continue
				default:
					return id, fmt.Errorf("create brigadier: %w", pgErr)
				}
			}
		}

		break
	}

	return id, nil
}

func storeBrigadierLabel(ctx context.Context, tx pgx.Tx, schema string,
	id uuid.UUID, label string,
) error {
	sql := `
	INSERT INTO
		%s (brigade_id,	label)
	VALUES
		($1, $2)
	`
	if _, err := tx.Exec(ctx,
		fmt.Sprintf(sql, (pgx.Identifier{schema, "start_labels"}.Sanitize())),
		id, label,
	); err != nil {
		return fmt.Errorf("store label: %w", err)
	}

	return nil
}

func createBrigade(ctx context.Context, db *pgxpool.Pool, schema string,
	partnerID uuid.UUID, creationInfo string, label string,
) (uuid.UUID, string, string, *namesgenerator.Person, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return uuid.Nil, "", "", nil, fmt.Errorf("begin: %w", err)
	}

	defer tx.Rollback(ctx)

	id, err := defineBrigadeID(ctx, tx, schema)
	if err != nil {
		return uuid.Nil, "", "", nil, fmt.Errorf("select brigade id: %w", err)
	}

	fullname, person, err := defineBrigadierPerson(ctx, tx, schema, id)
	if err != nil {
		return uuid.Nil, "", "", nil, fmt.Errorf("select brigadier person: %w", err)
	}

	mnemo, err := storeBrigadierSaltKey(ctx, tx, schema, id)
	if err != nil {
		return uuid.Nil, "", "", nil, fmt.Errorf("store brigade salt key: %w", err)
	}

	if err := createBrigadeEvent(ctx, tx, schema, id, creationInfo); err != nil {
		return uuid.Nil, "", "", nil, fmt.Errorf("create brigade event: %w", err)
	}

	if err := storeBrigadierPartner(ctx, tx, schema, id, partnerID); err != nil {
		return uuid.Nil, "", "", nil, fmt.Errorf("store brigadier partner: %w", err)
	}

	if label != "" {
		if err := storeBrigadierLabel(ctx, tx, schema, id, label); err != nil {
			return uuid.Nil, "", "", nil, fmt.Errorf("store brigadier label: %w", err)
		}
	}

	err = tx.Commit(ctx)
	if err != nil {
		return id, "", "", nil, fmt.Errorf("commit: %w", err)
	}

	return id, mnemo, fullname, person, nil
}
