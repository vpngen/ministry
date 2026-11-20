package main

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Money is a monetary amount in minor units (e.g. cents), avoiding float
// precision issues once real billing math lands.
type Money = int64

// KeyIssuer issues a new key for a brigade at the given tier. The real
// implementation will call the external Socket registry once its API is
// published; until then, stubKeyIssuer exercises the same DB write path.
type KeyIssuer interface {
	IssueKey(ctx context.Context, brigadeID uuid.UUID, tier string) (socketKeyID string, err error)
}

// BillingQuoter returns the checkout quote for upgrading a brigade to tier.
// The real implementation will call the (not yet built) billing system;
// until then, stubBillingQuoter returns a fixed fake quote.
type BillingQuoter interface {
	GetCheckoutQuote(ctx context.Context, brigadeID uuid.UUID, tier string) (
		proratedCost, nextPeriodCost Money, nextInvoiceDate time.Time, err error)
}

// stubKeyIssuer writes a fake socket_key_id into brigadier_key_tier, proving
// out the full request -> DB write -> status transition path without a real
// Socket call.
type stubKeyIssuer struct {
	db     *pgxpool.Pool
	schema string
}

func (s *stubKeyIssuer) IssueKey(ctx context.Context, brigadeID uuid.UUID, tier string) (string, error) {
	socketKeyID := "stub-" + uuid.New().String()

	sqlUpsert := `
	INSERT INTO %s (brigade_id, tier, socket_key_id)
	VALUES ($1, $2, $3)
	ON CONFLICT (brigade_id) DO UPDATE SET tier = $2, socket_key_id = $3
	`

	if _, err := s.db.Exec(ctx, fmt.Sprintf(sqlUpsert, pgx.Identifier{s.schema, "brigadier_key_tier"}.Sanitize()), brigadeID, tier, socketKeyID); err != nil {
		return "", fmt.Errorf("upsert key tier: %w", err)
	}

	return socketKeyID, nil
}

// stubBillingQuoter returns a fixed fake quote so the (future) checkout UI
// has something deterministic to render against.
type stubBillingQuoter struct{}

func (s *stubBillingQuoter) GetCheckoutQuote(ctx context.Context, brigadeID uuid.UUID, tier string) (Money, Money, time.Time, error) {
	const stubProratedCost Money = 0
	const stubNextPeriodCost Money = 0

	return stubProratedCost, stubNextPeriodCost, time.Now().AddDate(0, 1, 0), nil
}
