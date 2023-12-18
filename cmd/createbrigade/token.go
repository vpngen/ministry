package main

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func checkToken(ctx context.Context, db *pgxpool.Pool, schema string, token []byte) (uuid.UUID, bool, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("begin: %w", err)
	}

	defer tx.Rollback(ctx)

	sqlCheckToken := `
	SELECT
		t.partner_id
	FROM
		%s AS t
		JOIN %s AS p ON p.partner_id=t.partner_id
	WHERE
		p.is_active=true
		AND t.token=$1
	LIMIT 1
	`

	var id uuid.UUID
	if err = tx.QueryRow(ctx, fmt.Sprintf(sqlCheckToken,
		pgx.Identifier{schema, "partners_tokens"}.Sanitize(),
		pgx.Identifier{schema, "partners"}.Sanitize()),
		token,
	).Scan(&id); err != nil {
		return uuid.Nil, false, ErrAccessDenied
	}

	return id, true, nil
}
