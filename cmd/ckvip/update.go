package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const getBrigade = `
SELECT 
	brigade_id
FROM 
	head.brigadiers_ids
WHERE
	brigade_id = $1
`

const addVIP = `
INSERT INTO 
	head.brigadier_vip 
		(brigade_id, vip_expire, vip_users)
	VALUES 
		($1, $2, $3)
ON CONFLICT (brigade_id) DO UPDATE 
	SET 
		vip_expire = EXCLUDED.vip_expire,
		vip_users = EXCLUDED.vip_users
`

const purgeExpired = `
DELETE FROM
	head.brigadier_vip
WHERE
	vip_expire < (NOW() AT TIME ZONE 'UTC' - $1 * INTERVAL '7 DAY')
	AND finalizer = false
`

const purgeVIPTelegramIDs = `
DELETE FROM 
	head.vip_telegram_ids
WHERE 
	brigade_id NOT IN (SELECT brigade_id FROM head.brigadier_vip)
`

const allVipBrigades = `
SELECT 
	b.brigade_id
FROM 
	head.brigadiers b
JOIN 
	head.brigadier_vip bv ON b.brigade_id = bv.brigade_id
WHERE
	bv.vip_expire > (NOW() AT TIME ZONE 'UTC')
`

const clearRenewalNotifications = `
DELETE FROM
	head.push_messages
WHERE
	brigade_id = ANY($1)
	AND event_type IN ('vip.renewal_failed', 'vip.last_chance', 'vip.subscription_expired')
`

const resetExpired = `
UPDATE 
	head.brigadier_vip
SET 
	vip_expire = NOW() AT TIME ZONE 'UTC'
WHERE 
	brigade_id = $1
	AND vip_expire > (NOW() AT TIME ZONE 'UTC')
`

// set new VIP records or update old ones
func updateVIPRecords(ctx context.Context, db *pgxpool.Pool, brigades map[uuid.UUID]VipBrigade, silent bool) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}

	defer tx.Rollback(ctx)

	for _, brigade := range brigades {
		if !silent {
			fmt.Fprintf(os.Stderr, "Brigade: %s (%s), ExpiredAt: %s, UsersCount: %d\n", brigade.BrigadeID, brigade.RawBrigadeID, brigade.ExpiredAt, brigade.UsersCount)
		}

		if err := func() error {
			sp, err := tx.Begin(ctx)
			if err != nil {
				return fmt.Errorf("begin savepoint: %w", err)
			}

			defer sp.Rollback(ctx)

			// set or update existing VIP record
			comm, err := sp.Exec(ctx, addVIP, brigade.BrigadeID, brigade.ExpiredAt, brigade.UsersCount)
			if err != nil {
				var pgErr *pgconn.PgError

				if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.ForeignKeyViolation &&
					pgErr.ConstraintName == "brigadier_vip_brigade_id_fkey" {
					fmt.Fprintf(os.Stderr, "%s: Warning: Brigade %s (%s) does not exist, skipping\n", LogTag, brigade.BrigadeID, brigade.RawBrigadeID)

					return nil
				}

				return fmt.Errorf("set vip: %w", err)
			}

			if comm.RowsAffected() == 0 {
				fmt.Fprintf(os.Stderr, "%s: Warning: No rows affected for VIP brigade: %s\n", LogTag, brigade.BrigadeID)

				return nil
			}

			return sp.Commit(ctx)
		}(); err != nil {
			return err
		}
	}

	rows, err := tx.Query(ctx, allVipBrigades)
	if err != nil {
		return fmt.Errorf("query all vip brigades: %w", err)
	}

	defer rows.Close()

	expired := make([]uuid.UUID, 0)

	var brigadeID uuid.UUID
	if _, err := pgx.ForEachRow(rows, []any{&brigadeID}, func() error {
		if _, ok := brigades[brigadeID]; !ok {
			expired = append(expired, brigadeID)
		}

		return nil
	}); err != nil {
		return fmt.Errorf("foreach: %w", err)
	}

	for _, brigadeID := range expired {
		if _, err := tx.Exec(ctx, resetExpired, brigadeID); err != nil {
			return fmt.Errorf("reset expired: %w", err)
		}

		fmt.Fprintf(os.Stderr, "%s: Brigade %s not in fetched list, set expire to %d hours\n", LogTag, brigadeID, redemtionPeriod)
	}

	// purge expired and deleted VIP records
	if _, err := tx.Exec(ctx, purgeExpired, redemtionPeriod); err != nil {
		return fmt.Errorf("purge expired: %w", err)
	}

	// purge vip telegram IDs for brigades that no longer have a VIP record
	if _, err := tx.Exec(ctx, purgeVIPTelegramIDs); err != nil {
		return fmt.Errorf("purge vip telegram ids: %w", err)
	}

	// clear stale renewal/expiry push notifications for brigades that just renewed
	activeIDs := make([]uuid.UUID, 0, len(brigades))
	for id := range brigades {
		activeIDs = append(activeIDs, id)
	}

	if len(activeIDs) > 0 {
		if _, err := tx.Exec(ctx, clearRenewalNotifications, activeIDs); err != nil {
			return fmt.Errorf("clear renewal notifications: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	return nil
}

var ErrInvalidResponse = errors.New("invalid response")

func fetchPaidUsers(c *http.Client, obfsUUID uuid.UUID, ep string) (map[uuid.UUID]VipBrigade, []byte, error) {
	req, err := http.NewRequest(http.MethodPost, "https://"+ep, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("do request: %w", err)
	}

	defer resp.Body.Close()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("read response body: %w", err)
	}

	// OBFS_UUID="d8fc0859-c1a4-4d29-94e1-4fdea70ff8b8"
	// fmt.Printf("%s\n", payload)

	var userList PaidUsersPesponse
	if err := json.Unmarshal(payload, &userList); err != nil {
		return nil, payload, fmt.Errorf("unmarshal response body: %w", err)
	}

	if userList.Result != "success" {
		return nil, payload, fmt.Errorf("%w: %s", ErrInvalidResponse, userList.Result)
	}

	brigades := make(map[uuid.UUID]VipBrigade, 0)
	for _, u := range userList.Data {
		brigadeID := obfs2uuid(u.UserID, obfsUUID)

		if brigade, ok := brigades[brigadeID]; ok {
			if brigade.ExpiredAt.Before(u.GoodExpiryDateTime) {
				brigade.ExpiredAt = u.GoodExpiryDateTime
			}

			brigade.UsersCount++

			brigades[brigadeID] = brigade

			continue
		}

		brigades[brigadeID] = VipBrigade{
			RawBrigadeID: u.UserID,
			BrigadeID:    brigadeID,
			ExpiredAt:    u.GoodExpiryDateTime,
			UsersCount:   1,
		}
	}

	return brigades, payload, nil
}
