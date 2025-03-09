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
	name string, mnemo string,
) (uuid.UUID, uuid.UUID, *namesgenerator.Person, bool, time.Time, string, time.Time, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return uuid.Nil, uuid.Nil, nil, false, time.Time{}, "", time.Time{}, fmt.Errorf("begin: %w", err)
	}

	defer tx.Rollback(ctx)

	salt, err := saltByName(ctx, tx, name)
	if err != nil {
		return uuid.Nil, uuid.Nil, nil, false, time.Time{}, "", time.Time{}, fmt.Errorf("salt by name: %w", err)
	}

	key := seedgenerator.SeedFromSaltMnemonics(mnemo, seed, salt)

	brigadeID, partnerID, person, deleted, deletedTime, deletionReason, err := checkBrigadierByKey(ctx, tx, name, key)
	if err != nil {
		return uuid.Nil, uuid.Nil, nil, false, time.Time{}, "", time.Time{}, fmt.Errorf("check brigadier: %w", err)
	}

	lastRestore, err := checkLastRestore(ctx, tx, brigadeID)
	if err != nil {
		return uuid.Nil, uuid.Nil, nil, false, time.Time{}, "", time.Time{}, fmt.Errorf("check last restore: %w", err)
	}

	return brigadeID, partnerID, person, deleted, deletedTime, deletionReason, lastRestore, nil
}

func checkLastRestore(ctx context.Context, tx pgx.Tx, brigadeID uuid.UUID) (time.Time, error) {
	var lastRestore pgtype.Timestamp

	sqlLastRestore := `
	SELECT
		event_time
	FROM 
		head.brigades_actions
	WHERE
		brigade_id=$1
	AND
		event_name='restore_brigade'
	ORDER BY
		event_time DESC
	LIMIT 1
	`

	if err := tx.QueryRow(ctx, sqlLastRestore, brigadeID).Scan(&lastRestore); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return time.Time{}, nil
		}

		return time.Time{}, fmt.Errorf("last restore: %w", err)
	}

	return lastRestore.Time, nil
}

func checkBrigadierByKey(ctx context.Context, tx pgx.Tx, name string, key []byte,
) (uuid.UUID, uuid.UUID, *namesgenerator.Person, bool, time.Time, string, error) {
	var (
		brigadeID      uuid.UUID
		partnerID      uuid.UUID
		person         namesgenerator.Person
		deletedTime    pgtype.Timestamp
		deletionReason pgtype.Text
	)

	sqlSaltByName := `
	SELECT
		b.brigade_id,
		b.person,
		bp.partner_id,
		db.deleted_at,
		db.reason
	FROM head.brigadiers b
	JOIN head.brigadier_keys bk ON bk.brigade_id=b.brigade_id
	JOIN head.brigadier_partners bp ON b.brigade_id=bp.brigade_id
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
		&partnerID,
		&deletedTime,
		&deletionReason,
	); err != nil {
		return uuid.Nil, uuid.Nil, nil, false, time.Time{}, "", fmt.Errorf("key query: %w", err)
	}

	return brigadeID, partnerID, &person, deletedTime.Valid, deletedTime.Time, deletionReason.String, nil
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
