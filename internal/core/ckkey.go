package core

import (
	"context"
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
func CheckBrigadier(ctx context.Context, db *pgxpool.Pool, schema string, seed string,
	name string, mnemo string,
) (uuid.UUID, uuid.UUID, *namesgenerator.Person, bool, time.Time, string, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return uuid.Nil, uuid.Nil, nil, false, time.Time{}, "", fmt.Errorf("begin: %w", err)
	}

	defer tx.Rollback(ctx)

	salt, err := saltByName(ctx, tx, schema, name)
	if err != nil {
		return uuid.Nil, uuid.Nil, nil, false, time.Time{}, "", fmt.Errorf("salt by name: %w", err)
	}

	key := seedgenerator.SeedFromSaltMnemonics(mnemo, seed, salt)

	brigadeID, partnerID, person, deleted, deletedTime, deletionReason, err := checkBrigadierByKey(ctx, tx, schema, name, key)
	if err != nil {
		return uuid.Nil, uuid.Nil, nil, false, time.Time{}, "", fmt.Errorf("check brigadier: %w", err)
	}

	return brigadeID, partnerID, person, deleted, deletedTime, deletionReason, nil
}

func checkBrigadierByKey(ctx context.Context, tx pgx.Tx, schema string, name string, key []byte,
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
	FROM %s b
	JOIN %s bk ON bk.brigade_id=b.brigade_id
	JOIN %s bp ON b.brigade_id=bp.brigade_id
	LEFT JOIN %s db ON b.brigade_id=db.brigade_id
	WHERE
		b.brigadier=$1
	AND
		bk.key=$2
	`

	if err := tx.QueryRow(ctx,
		fmt.Sprintf(sqlSaltByName,
			pgx.Identifier{schema, "brigadiers"}.Sanitize(),
			pgx.Identifier{schema, "brigadier_keys"}.Sanitize(),
			pgx.Identifier{schema, "brigadier_partners"}.Sanitize(),
			pgx.Identifier{schema, "deleted_brigadiers"}.Sanitize(),
		),
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

func saltByName(ctx context.Context, tx pgx.Tx, schema, name string) ([]byte, error) {
	var salt []byte

	sqlSaltByName := `SELECT
		brigadier_salts.salt
	FROM %s, %s
	WHERE
		brigadiers.brigadier=$1
	AND
		brigadiers.brigade_id=brigadier_salts.brigade_id
	`

	if err := tx.QueryRow(ctx, fmt.Sprintf(sqlSaltByName,
		pgx.Identifier{schema, "brigadier_salts"}.Sanitize(),
		pgx.Identifier{schema, "brigadiers"}.Sanitize()),
		name,
	).Scan(&salt); err != nil {
		return nil, fmt.Errorf("salt query: %w", err)
	}

	return salt, nil
}
