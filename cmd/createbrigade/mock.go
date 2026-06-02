package main

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vpngen/ministry/internal/core"
)

// checkTokenOrMock behaves like checkToken in normal mode.  In mock mode it
// additionally seeds a mock partner + token into the DB when the token is not
// found, so the rest of the brigade creation flow can proceed on an empty DB.
func checkTokenOrMock(ctx context.Context, db *pgxpool.Pool, schema string, token []byte) (uuid.UUID, bool, error) {
	id, ok, _ := checkToken(ctx, db, schema, token)
	if ok {
		return id, true, nil
	}

	// Token not found (or DB error) — seed a mock partner that owns this token.
	if err := ensureMockPartner(ctx, db, schema, token); err != nil {
		return uuid.Nil, false, fmt.Errorf("ensure mock partner: %w", err)
	}

	// Retry after seeding.
	return checkToken(ctx, db, schema, token)
}

// ensureMockPartner upserts a mock partner (fixed UUID) that is active and
// open for registrations, then links the given token to it.
func ensureMockPartner(ctx context.Context, db *pgxpool.Pool, schema string, token []byte) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}

	defer tx.Rollback(ctx)

	// Upsert mock partner.
	sqlUpsertPartner := `
INSERT INTO head.partners (partner_id, partner, is_active, open_for_regs)
VALUES ($1, 'mock-partner', true, true)
ON CONFLICT (partner_id) DO UPDATE
    SET is_active = true, open_for_regs = true
`
	if _, err := tx.Exec(ctx, sqlUpsertPartner, core.MockPartnerID); err != nil {
		return fmt.Errorf("upsert mock partner: %w", err)
	}

	// Upsert token — use ON CONFLICT on the token column to be idempotent.
	sqlUpsertToken := `
INSERT INTO head.partners_tokens (partner_id, token, name)
VALUES ($1, $2, 'mock-token')
ON CONFLICT (token) DO NOTHING
`
	if _, err := tx.Exec(ctx, sqlUpsertToken, core.MockPartnerID, token); err != nil {
		return fmt.Errorf("upsert mock token: %w", err)
	}

	return tx.Commit(ctx)
}
