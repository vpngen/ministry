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
//   - the brigade was not created with a protected start label ($3, see
//     PROTECTED_LABELS) - those cohorts are exempt from the whole
//     free-brigade lifecycle (an empty $3 matches nothing, disabling the
//     exemption)
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
  AND NOT EXISTS (
	SELECT 1 FROM head.start_labels sl
	WHERE sl.brigade_id = ft.brigade_id AND sl.label = ANY($3)
  )
ON CONFLICT (brigade_id, event_type) DO NOTHING
`

func queueNotifications(ctx context.Context, db *pgxpool.Pool, brigadeIDs []uuid.UUID, eventType string, protectedLabels []string, silent bool) error {
	if len(brigadeIDs) == 0 {
		return nil
	}

	tag, err := db.Exec(ctx, sqlQueueNotifications, brigadeIDs, eventType, protectedLabels)
	if err != nil {
		return fmt.Errorf("exec: %w", err)
	}

	if !silent {
		fmt.Fprintf(os.Stderr, "%s: queued %d new %s notifications\n", LogTag, tag.RowsAffected(), eventType)
	}

	return nil
}

// sqlQueueDeletedBrigades inserts brigade_deleted notifications for all deleted
// free brigades without an active VIP subscription and without a protected
// start label ($1) - a label-protected brigade cannot be auto-deleted anymore
// (delete_brigadier.sh refuses), so this only matters for pre-protection
// deletions, but it keeps "no ckfree notifications at all" literally true.
const sqlQueueDeletedBrigades = `
INSERT INTO head.push_messages (brigade_id, event_type)
SELECT ft.brigade_id, '` + EventBrigadeDeleted + `'
FROM head.free_telegram_ids ft
JOIN head.deleted_brigadiers d ON ft.brigade_id = d.brigade_id
LEFT JOIN head.brigadier_vip bv ON ft.brigade_id = bv.brigade_id
	AND bv.vip_expire > (NOW() AT TIME ZONE 'UTC')
WHERE bv.brigade_id IS NULL
  AND NOT EXISTS (
	SELECT 1 FROM head.start_labels sl
	WHERE sl.brigade_id = ft.brigade_id AND sl.label = ANY($1)
  )
ON CONFLICT (brigade_id, event_type) DO NOTHING
`

func queueDeletedBrigades(ctx context.Context, db *pgxpool.Pool, protectedLabels []string, silent bool) error {
	tag, err := db.Exec(ctx, sqlQueueDeletedBrigades, protectedLabels)
	if err != nil {
		return fmt.Errorf("exec: %w", err)
	}

	if !silent {
		fmt.Fprintf(os.Stderr, "%s: queued %d %s notifications\n", LogTag, tag.RowsAffected(), EventBrigadeDeleted)
	}

	return nil
}
