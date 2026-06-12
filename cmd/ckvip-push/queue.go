package main

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	EventBuyVIPKey      = "vip.buy_vip_key"
	EventRenewalFailed  = "vip.renewal_failed"
	EventLastChance     = "vip.last_chance"
)

// sqlQueueBuyVIPKey queues a "buy VIP key" upsell notification 7 days after the
// first time a brigade became VIP (earliest 'begin' action), while the subscription
// is still active. Skips deleted brigades and already-queued entries.
const sqlQueueBuyVIPKey = `
INSERT INTO head.push_messages (brigade_id, event_type)
SELECT vt.brigade_id, $1
FROM head.vip_telegram_ids vt
JOIN head.brigadier_vip bv ON vt.brigade_id = bv.brigade_id
JOIN (
    SELECT brigade_id, MIN(event_time) AS first_begin
    FROM head.brigadier_vip_actions
    WHERE event_name = 'begin'
    GROUP BY brigade_id
) fa ON vt.brigade_id = fa.brigade_id
LEFT JOIN head.deleted_brigadiers d ON vt.brigade_id = d.brigade_id
WHERE d.brigade_id IS NULL
  AND bv.vip_expire > NOW() AT TIME ZONE 'UTC'
  AND fa.first_begin < NOW() AT TIME ZONE 'UTC' - $2 * INTERVAL '1 DAY'
ON CONFLICT (brigade_id, event_type) DO NOTHING
`

func queueBuyVIPKey(ctx context.Context, db *pgxpool.Pool, silent bool) error {
	tag, err := db.Exec(ctx, sqlQueueBuyVIPKey, EventBuyVIPKey, buyVIPKeyDays)
	if err != nil {
		return fmt.Errorf("exec: %w", err)
	}

	if !silent {
		fmt.Fprintf(os.Stderr, "%s: queued %d new %s notifications\n", LogTag, tag.RowsAffected(), EventBuyVIPKey)
	}

	return nil
}

// sqlQueueRenewalFailed queues a "renewal failed" notification 1+ days after
// vip_expire. ON CONFLICT DO NOTHING ensures it is sent at most once per brigade.
const sqlQueueRenewalFailed = `
INSERT INTO head.push_messages (brigade_id, event_type)
SELECT bv.brigade_id, $1
FROM head.brigadier_vip bv
JOIN head.vip_telegram_ids vt ON bv.brigade_id = vt.brigade_id
LEFT JOIN head.deleted_brigadiers d ON bv.brigade_id = d.brigade_id
WHERE d.brigade_id IS NULL
  AND bv.vip_expire < NOW() AT TIME ZONE 'UTC' - $2 * INTERVAL '1 DAY'
ON CONFLICT (brigade_id, event_type) DO NOTHING
`

func queueRenewalFailed(ctx context.Context, db *pgxpool.Pool, silent bool) error {
	tag, err := db.Exec(ctx, sqlQueueRenewalFailed, EventRenewalFailed, renewalFailedDays)
	if err != nil {
		return fmt.Errorf("exec: %w", err)
	}

	if !silent {
		fmt.Fprintf(os.Stderr, "%s: queued %d new %s notifications\n", LogTag, tag.RowsAffected(), EventRenewalFailed)
	}

	return nil
}

// sqlQueueLastChance queues a "last chance" notification 3+ days after vip_expire.
// ON CONFLICT DO NOTHING ensures it is sent at most once per brigade.
const sqlQueueLastChance = `
INSERT INTO head.push_messages (brigade_id, event_type)
SELECT bv.brigade_id, $1
FROM head.brigadier_vip bv
JOIN head.vip_telegram_ids vt ON bv.brigade_id = vt.brigade_id
LEFT JOIN head.deleted_brigadiers d ON bv.brigade_id = d.brigade_id
WHERE d.brigade_id IS NULL
  AND bv.vip_expire < NOW() AT TIME ZONE 'UTC' - $2 * INTERVAL '1 DAY'
ON CONFLICT (brigade_id, event_type) DO NOTHING
`

func queueLastChance(ctx context.Context, db *pgxpool.Pool, silent bool) error {
	tag, err := db.Exec(ctx, sqlQueueLastChance, EventLastChance, lastChanceDays)
	if err != nil {
		return fmt.Errorf("exec: %w", err)
	}

	if !silent {
		fmt.Fprintf(os.Stderr, "%s: queued %d new %s notifications\n", LogTag, tag.RowsAffected(), EventLastChance)
	}

	return nil
}
