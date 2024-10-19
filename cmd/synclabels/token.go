package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrAccessDenied = errors.New("access denied")

func checkToken(db *pgxpool.Pool, token []byte) (uuid.UUID, error) {
	ctx := context.Background()

	tx, err := db.Begin(ctx)
	if err != nil {
		return uuid.Nil, fmt.Errorf("begin: %w", err)
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
		pgx.Identifier{"head", "partners_tokens"}.Sanitize(),
		pgx.Identifier{"head", "partners"}.Sanitize()),
		token,
	).Scan(&id); err != nil {
		return uuid.Nil, ErrAccessDenied
	}

	return id, nil
}
