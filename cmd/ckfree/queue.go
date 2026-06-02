package main

import (
	"context"
	"fmt"
	"os"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	EventNotActivated   = "free.not_activated"
	EventLowUsers1d     = "free.low_users_1d"
	EventLowUsers3d     = "free.low_users_3d"
	EventLastChance     = "free.last_chance"
	EventBrigadeDeleted = "free.brigade_deleted"
)

// sqlQueueNotifications batch-inserts push notifications for a set of brigade IDs.
// Inserts only when:
//   - the brigade has a registered telegram ID in free_telegram_ids
//   - the brigade has no active VIP subscription
//
// ON CONFLICT DO NOTHING ensures each (brigade, event) is queued at most once.
const sqlQueueNotifications = `
INSERT INTO head.push_messages (brigade_id, event_type)
SELECT ft.brigade_id, $2
FROM head.free_telegram_ids ft
LEFT JOIN head.brigadier_vip bv ON ft.brigade_id = bv.brigade_id
	AND bv.vip_expire > (NOW() AT TIME ZONE 'UTC')
WHERE ft.brigade_id = ANY($1)
  AND bv.brigade_id IS NULL
ON CONFLICT (brigade_id, event_type) DO NOTHING
`

func queueNotifications(ctx context.Context, db *pgxpool.Pool, brigadeIDs []uuid.UUID, eventType string, silent bool) error {
	if len(brigadeIDs) == 0 {
		return nil
	}

	tag, err := db.Exec(ctx, sqlQueueNotifications, brigadeIDs, eventType)
	if err != nil {
		return fmt.Errorf("exec: %w", err)
	}

	if !silent {
		fmt.Fprintf(os.Stderr, "%s: queued %d new %s notifications\n", LogTag, tag.RowsAffected(), eventType)
	}

	return nil
}

// sqlQueueDeletedBrigades inserts brigade_deleted notifications for all deleted
// free brigades without an active VIP subscription.
const sqlQueueDeletedBrigades = `
INSERT INTO head.push_messages (brigade_id, event_type)
SELECT ft.brigade_id, '` + EventBrigadeDeleted + `'
FROM head.free_telegram_ids ft
JOIN head.deleted_brigadiers d ON ft.brigade_id = d.brigade_id
LEFT JOIN head.brigadier_vip bv ON ft.brigade_id = bv.brigade_id
	AND bv.vip_expire > (NOW() AT TIME ZONE 'UTC')
WHERE bv.brigade_id IS NULL
ON CONFLICT (brigade_id, event_type) DO NOTHING
`

func queueDeletedBrigades(ctx context.Context, db *pgxpool.Pool, silent bool) error {
	tag, err := db.Exec(ctx, sqlQueueDeletedBrigades)
	if err != nil {
		return fmt.Errorf("exec: %w", err)
	}

	if !silent {
		fmt.Fprintf(os.Stderr, "%s: queued %d %s notifications\n", LogTag, tag.RowsAffected(), EventBrigadeDeleted)
	}

	return nil
}
