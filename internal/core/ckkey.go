package core

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vpngen/wordsgens/namesgenerator"
	"github.com/vpngen/wordsgens/seedgenerator"
)

// CheckBrigadier checks brigadier by name and mnemonics
func CheckBrigadier(ctx context.Context, db *pgxpool.Pool, seed string,
	name string, mnemo string, force bool,
) (uuid.UUID, *namesgenerator.Person, bool, time.Time, string, int, time.Time, time.Time, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return uuid.Nil, nil, false, time.Time{}, "", 0, time.Time{}, time.Time{}, fmt.Errorf("begin: %w", err)
	}

	defer tx.Rollback(ctx)

	if force {
		brigadeID, person, deleted, deletedTime, deletionReason, err := checkBrigadierByNameForce(ctx, tx, name)
		if err != nil {
			return uuid.Nil, nil, false, time.Time{}, "", 0, time.Time{}, time.Time{}, fmt.Errorf("check brigadier by key force: %w", err)
		}

		return brigadeID, person, deleted, deletedTime, deletionReason, 0, time.Time{}, time.Time{}, nil
	}

	salt, err := saltByName(ctx, tx, name)
	if err != nil {
		return uuid.Nil, nil, false, time.Time{}, "", 0, time.Time{}, time.Time{}, fmt.Errorf("salt by name: %w", err)
	}

	key := seedgenerator.SeedFromSaltMnemonics(mnemo, seed, salt)

	brigadeID, person, deleted, deletedTime, deletionReason, err := checkBrigadierByKey(ctx, tx, name, key)
	if err != nil {
		return uuid.Nil, nil, false, time.Time{}, "", 0, time.Time{}, time.Time{}, fmt.Errorf("check brigadier: %w", err)
	}

	restoreCount, olderRestore, newerRestore, err := checkLastRestore(ctx, tx, brigadeID)
	if err != nil {
		return uuid.Nil, nil, false, time.Time{}, "", 0, time.Time{}, time.Time{}, fmt.Errorf("check last restore: %w", err)
	}

	return brigadeID, person, deleted, deletedTime, deletionReason, restoreCount, olderRestore, newerRestore, nil
}

func checkLastRestore(ctx context.Context, tx pgx.Tx, brigadeID uuid.UUID) (int, time.Time, time.Time, error) {
	var (
		older, newer pgtype.Timestamp
		count        int
	)

	sqlLastRestore := `
	SELECT
		COUNT(*),MIN(event_time),MAX(event_time)
	FROM 
		head.brigades_actions
	WHERE
		brigade_id=$1
	AND
		event_name='restore_brigade'
	AND
		event_time >= NOW() - INTERVAL '1 month'
	GROUP BY
		brigade_id
	`

	if err := tx.QueryRow(ctx, sqlLastRestore, brigadeID).Scan(&count, &older, &newer); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, time.Time{}, time.Time{}, nil
		}

		return 0, time.Time{}, time.Time{}, fmt.Errorf("last restore: %w", err)
	}

	return count, older.Time, newer.Time, nil
}

func checkBrigadierByKey(ctx context.Context, tx pgx.Tx, name string, key []byte,
) (uuid.UUID, *namesgenerator.Person, bool, time.Time, string, error) {
	var (
		brigadeID      uuid.UUID
		person         namesgenerator.Person
		deletedTime    pgtype.Timestamp
		deletionReason pgtype.Text
	)

	sqlSaltByName := `
	SELECT
		b.brigade_id,
		b.person,
		db.deleted_at,
		db.reason
	FROM head.brigadiers b
	JOIN head.brigadier_keys bk ON bk.brigade_id=b.brigade_id
	LEFT JOIN head.deleted_brigadiers db ON b.brigade_id=db.brigade_id
	WHERE
		b.brigadier=$1
	AND
		bk.key=$2
	`

	if err := tx.QueryRow(ctx,
		sqlSaltByName,
		name,
		key,
	).Scan(
		&brigadeID,
		&person,
		&deletedTime,
		&deletionReason,
	); err != nil {
		return uuid.Nil, nil, false, time.Time{}, "", fmt.Errorf("key query: %w", err)
	}

	return brigadeID, &person, deletedTime.Valid, deletedTime.Time, deletionReason.String, nil
}

func saltByName(ctx context.Context, tx pgx.Tx, name string) ([]byte, error) {
	var salt []byte

	sqlSaltByName := `SELECT
		brigadier_salts.salt
	FROM head.brigadier_salts, head.brigadiers
	WHERE
		brigadiers.brigadier=$1
	AND
		brigadiers.brigade_id=brigadier_salts.brigade_id
	`

	if err := tx.QueryRow(ctx, sqlSaltByName, name).Scan(&salt); err != nil {
		return nil, fmt.Errorf("salt query: %w", err)
	}

	return salt, nil
}

func checkBrigadierByNameForce(ctx context.Context, tx pgx.Tx, name string,
) (uuid.UUID, *namesgenerator.Person, bool, time.Time, string, error) {
	var (
		brigadeID      uuid.UUID
		person         namesgenerator.Person
		deletedTime    pgtype.Timestamp
		deletionReason pgtype.Text
	)

	sqlSaltByName := `
	SELECT
		b.brigade_id,
		b.person,
		db.deleted_at,
		db.reason
	FROM head.brigadiers b
	LEFT JOIN head.deleted_brigadiers db ON b.brigade_id=db.brigade_id
	WHERE
		b.brigadier=$1
	`

	if err := tx.QueryRow(ctx,
		sqlSaltByName,
		name,
	).Scan(
		&brigadeID,
		&person,
		&deletedTime,
		&deletionReason,
	); err != nil {
		return uuid.Nil, nil, false, time.Time{}, "", fmt.Errorf("key query: %w", err)
	}

	return brigadeID, &person, deletedTime.Valid, deletedTime.Time, deletionReason.String, nil
}
