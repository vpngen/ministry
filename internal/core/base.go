package core

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vpngen/wordsgens/namesgenerator"
	"github.com/vpngen/wordsgens/seedgenerator"
)

const maxCollisionAttemts = 1000

var ErrMaxCollisions = errors.New("max collision attempts")

func createBrigadeEvent(ctx context.Context, tx pgx.Tx, id uuid.UUID, info string) error {
	sqlCreateBrigadeEvent := `
	INSERT INTO
		head.brigades_actions (brigade_id,	event_name, event_info,	event_time)
	VALUES
		($1, 'create_brigade',	$2, NOW() AT TIME ZONE 'UTC')
	`
	if _, err := tx.Exec(ctx, sqlCreateBrigadeEvent, id, info); err != nil {
		return fmt.Errorf("create brigade event: %w", err)
	}

	return nil
}

func storeBrigadierPartner(ctx context.Context, tx pgx.Tx,
	id uuid.UUID, partnerID uuid.UUID,
) error {
	sqlSelectPartner := `
		SELECT 
			p.partner_id
		FROM 
			head.partners AS p 					-- partners
		WHERE
			p.partner_id=$1
			AND p.is_active=true
			AND p.open_for_regs=true
		LIMIT 1
		`

	var pid uuid.UUID
	if err := tx.QueryRow(ctx, sqlSelectPartner, partnerID).Scan(&pid); err != nil {
		return fmt.Errorf("select partner: %w", err)
	}

	sqlStorePartnerRelation := `
	INSERT INTO
		head.brigadier_partners (brigade_id,	partner_id)
	VALUES
		($1, $2)
	`
	if _, err := tx.Exec(ctx, sqlStorePartnerRelation, id, partnerID); err != nil {
		return fmt.Errorf("store partner relation: %w", err)
	}

	sqlCreatePartnerAction := `
	INSERT INTO
		head.brigadier_partners_actions (brigade_id, partner_id, event_name, event_info, event_time)
	VALUES
		($1, $2, 'assign', '', NOW() AT TIME ZONE 'UTC')
	`
	if _, err := tx.Exec(ctx, sqlCreatePartnerAction, id, partnerID); err != nil {
		return fmt.Errorf("create partner action: %w", err)
	}

	return nil
}

func storeBrigadierSaltKey(ctx context.Context, tx pgx.Tx, seedExtra string, id uuid.UUID) (string, error) {
	mnemo, seed, salt, err := seedgenerator.Seed(seedgenerator.ENT64, seedExtra)
	if err != nil {
		return "", fmt.Errorf("gen seed6: %w", err)
	}

	sqlCreateBrigadierSalt := `
	INSERT INTO
		head.brigadier_salts (brigade_id,	salt)
	VALUES
		($1, $2)
	`
	if _, err = tx.Exec(ctx, sqlCreateBrigadierSalt, id, salt); err != nil {
		return "", fmt.Errorf("create brigadier salt: %w", err)
	}

	sqlCreateBrigadierKey := `
	INSERT INTO
		head.brigadier_keys (brigade_id,	key)
	VALUES
		($1, $2)
	`
	if _, err = tx.Exec(ctx, sqlCreateBrigadierKey, id, seed); err != nil {
		return "", fmt.Errorf("create brigadier key: %w", err)
	}

	return mnemo, nil
}

func defineBrigadierPerson(ctx context.Context, tx pgx.Tx, id uuid.UUID,
	forcePerson *namesgenerator.Person, customName string,
) (string, *namesgenerator.Person, error) {
	sqlCreateBrigadier := `
	INSERT INTO
		head.brigadiers (brigade_id,	brigadier, person)
	VALUES
		($1, $2, $3)
	`

	cnt := 0
	for {
		var (
			fullname string
			person   *namesgenerator.Person
		)

		if cnt++; cnt > maxCollisionAttemts {
			return "", nil, fmt.Errorf("create brigadier: %w: %d", ErrMaxCollisions, cnt)
		}

		switch customName {
		case "":
			fullnameNew, personNew, err := namesgenerator.PhysicsAwardeeShort()
			if err != nil {
				return "", nil, fmt.Errorf("physics generate: %s", err)
			}

			fullname = fullnameNew
			person = &personNew
		default:
			fullname = customName
			if forcePerson != nil {
				person = forcePerson
				break
			}

			person = &namesgenerator.Person{
				Name: fullname,
				Desc: "Я люблю делать то, что не пройдет цензуру",
				URL:  "https://vpngen.com",
			}
		}

		if err := func() error {
			stx, e := tx.Begin(ctx)
			if e != nil {
				return fmt.Errorf("sub begin: %w", e)
			}

			defer stx.Rollback(ctx)

			if _, e := tx.Exec(ctx, sqlCreateBrigadier, id, fullname, person); e != nil {
				return fmt.Errorf("insert id: %w", e)
			}

			if e := stx.Commit(ctx); e != nil {
				return fmt.Errorf("sub commit: %w", e)
			}

			return nil
		}(); err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) {
				switch pgErr.ConstraintName {
				case "brigadiers_brigadier_key":
					if customName != "" {
						return "", nil, fmt.Errorf("create custom brigadier: %w: %s", pgErr, customName)
					}

					continue
				default:
					return "", nil, fmt.Errorf("create brigadier: %w", pgErr)
				}
			}
		}

		return fullname, person, nil
	}
}

func defineBrigadeID(ctx context.Context, tx pgx.Tx) (uuid.UUID, time.Time, error) {
	sqlInsertID := `INSERT INTO head.brigadiers_ids (brigade_id, created_at) VALUES ($1, $2::TIMESTAMP WITHOUT TIME ZONE AT TIME ZONE 'UTC')`

	now := time.Now().UTC()
	id := uuid.New()

	cnt := 0
	for {
		if cnt++; cnt > maxCollisionAttemts {
			return id, now, fmt.Errorf("create brigadier: %w: %d", ErrMaxCollisions, cnt)
		}

		err := func() error {
			stx, e := tx.Begin(ctx)
			if e != nil {
				return fmt.Errorf("sub begin: %w", e)
			}

			defer stx.Rollback(ctx)

			if _, e := tx.Exec(ctx, sqlInsertID, id, now); e != nil {
				return fmt.Errorf("insert id: %w", e)
			}

			if e := stx.Commit(ctx); e != nil {
				return fmt.Errorf("sub commit: %w", e)
			}

			return nil
		}()
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) {
				switch pgErr.ConstraintName {
				case "brigadiers_ids_brigade_id_key":
					id = uuid.New()
					continue
				default:
					return id, now, fmt.Errorf("create brigadier: %w", pgErr)
				}
			}
		}

		break
	}

	return id, now, nil
}

func storeBrigadierLabel(ctx context.Context, tx pgx.Tx,
	id uuid.UUID, pid uuid.UUID, now time.Time, label string, labelID string, firstVisit int64,
) error {
	fv := time.Unix(firstVisit, 0)

	sql := `
	INSERT INTO
		head.start_labels (brigade_id, created_at, label, label_id, first_visit, update_time, partner_id)
	VALUES
		($1, $2::TIMESTAMP WITHOUT TIME ZONE AT TIME ZONE 'UTC', $3, $4, $5::TIMESTAMP WITHOUT TIME ZONE AT TIME ZONE 'UTC', NOW() AT TIME ZONE 'UTC', $6)
	ON CONFLICT (label_id, partner_id, first_visit) DO UPDATE
		SET brigade_id=$1, created_at=$2::TIMESTAMP WITHOUT TIME ZONE AT TIME ZONE 'UTC', label=$3, first_visit=$5::TIMESTAMP WITHOUT TIME ZONE AT TIME ZONE 'UTC', update_time=NOW() AT TIME ZONE 'UTC', partner_id=$6
	`
	if _, err := tx.Exec(ctx, sql, id, now, label, labelID, fv, pid); err != nil {
		return fmt.Errorf("store label: %w", err)
	}

	return nil
}

func CreateBrigade(ctx context.Context, db *pgxpool.Pool,
	seedExtra string, partnerID uuid.UUID, creationInfo string,
	forcePerson *namesgenerator.Person, customName string,
	label string, labelID string, firstVisit int64,
) (uuid.UUID, string, string, *namesgenerator.Person, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return uuid.Nil, "", "", nil, fmt.Errorf("begin: %w", err)
	}

	defer tx.Rollback(ctx)

	id, now, err := defineBrigadeID(ctx, tx)
	if err != nil {
		return uuid.Nil, "", "", nil, fmt.Errorf("select brigade id: %w", err)
	}

	fullname, person, err := defineBrigadierPerson(ctx, tx, id, forcePerson, customName)
	if err != nil {
		return uuid.Nil, "", "", nil, fmt.Errorf("select brigadier person: %w", err)
	}

	mnemo, err := storeBrigadierSaltKey(ctx, tx, seedExtra, id)
	if err != nil {
		return uuid.Nil, "", "", nil, fmt.Errorf("store brigade salt key: %w", err)
	}

	if err := createBrigadeEvent(ctx, tx, id, creationInfo); err != nil {
		return uuid.Nil, "", "", nil, fmt.Errorf("create brigade event: %w", err)
	}

	if err := storeBrigadierPartner(ctx, tx, id, partnerID); err != nil {
		return uuid.Nil, "", "", nil, fmt.Errorf("store brigadier partner: %w", err)
	}

	if err := storeBrigadierLabel(ctx, tx, id, partnerID, now, label, labelID, firstVisit); err != nil {
		return uuid.Nil, "", "", nil, fmt.Errorf("store brigadier label: %w", err)
	}

	err = tx.Commit(ctx)
	if err != nil {
		return id, "", "", nil, fmt.Errorf("commit: %w", err)
	}

	return id, mnemo, fullname, person, nil
}

const sqlStoreTGID = `
	INSERT INTO
		head.vip_telegram_ids (brigade_id, telegram_id)
	VALUES
		($1, $2)
	ON CONFLICT (brigade_id) DO UPDATE
		SET telegram_id = EXCLUDED.telegram_id
	`

func storeVIPTelegramID(ctx context.Context, tx pgx.Tx, id uuid.UUID, tgID int64) error {
	if _, err := tx.Exec(ctx, sqlStoreTGID, id, tgID); err != nil {
		return fmt.Errorf("store vip telegram id: %w", err)
	}

	return nil
}

const sqlFetchTGID = `
SELECT 
	brigade_id
FROM 
	head.vip_telegram_ids
WHERE 
	telegram_id = $1
LIMIT 1
`

func fetchVIPByTelegramID(ctx context.Context, tx pgx.Tx, telegram_id int64) (uuid.UUID, error) {
	var id uuid.UUID
	if err := tx.QueryRow(ctx, sqlFetchTGID, telegram_id).Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, nil
		}

		return uuid.Nil, fmt.Errorf("fetch vip telegram id: %w", err)
	}

	return id, nil
}

func RequestVIPBrigade(ctx context.Context, db *pgxpool.Pool,
	partnerID uuid.UUID, creationInfo string,
	forcePerson *namesgenerator.Person, customName string,
	label string, labelID string, firstVisit int64,
	tgID int64,
) (uuid.UUID, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return uuid.Nil, fmt.Errorf("begin: %w", err)
	}

	defer tx.Rollback(ctx)

	// check if tgID already has reserved brigade
	brigadeID, err := fetchVIPByTelegramID(ctx, tx, tgID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("fetch vip by telegram id: %w", err)
	}

	if brigadeID != uuid.Nil {
		return brigadeID, nil
	}

	id, now, err := defineBrigadeID(ctx, tx)
	if err != nil {
		return uuid.Nil, fmt.Errorf("select brigade id: %w", err)
	}

	if err := createBrigadeEvent(ctx, tx, id, creationInfo); err != nil {
		return uuid.Nil, fmt.Errorf("create brigade event: %w", err)
	}

	if err := storeBrigadierPartner(ctx, tx, id, partnerID); err != nil {
		return uuid.Nil, fmt.Errorf("store brigadier partner: %w", err)
	}

	if err := storeBrigadierLabel(ctx, tx, id, partnerID, now, label, labelID, firstVisit); err != nil {
		return uuid.Nil, fmt.Errorf("store brigadier label: %w", err)
	}

	if err := storeVIPTelegramID(ctx, tx, id, tgID); err != nil {
		return uuid.Nil, fmt.Errorf("store vip telegram id: %w", err)
	}

	err = tx.Commit(ctx)
	if err != nil {
		return id, fmt.Errorf("commit: %w", err)
	}

	return id, nil
}

func UpdateVIPBrigade(ctx context.Context, db *pgxpool.Pool,
	seedExtra string, id uuid.UUID, creationInfo string,
) (uuid.UUID, string, string, *namesgenerator.Person, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return uuid.Nil, "", "", nil, fmt.Errorf("begin: %w", err)
	}

	defer tx.Rollback(ctx)

	fullname, person, err := defineBrigadierPerson(ctx, tx, id, nil, "")
	if err != nil {
		return uuid.Nil, "", "", nil, fmt.Errorf("select brigadier person: %w", err)
	}

	mnemo, err := storeBrigadierSaltKey(ctx, tx, seedExtra, id)
	if err != nil {
		return uuid.Nil, "", "", nil, fmt.Errorf("store brigade salt key: %w", err)
	}

	if err := storeVIPMnemo(ctx, tx, id, mnemo); err != nil {
		return uuid.Nil, "", "", nil, fmt.Errorf("store vip memo: %w", err)
	}

	err = tx.Commit(ctx)
	if err != nil {
		return id, "", "", nil, fmt.Errorf("commit: %w", err)
	}

	return id, mnemo, fullname, person, nil
}

const sqlVIPStoreMnemo = `
INSERT INTO
	head.vip_messages
	(brigade_id, mnemo)
VALUES
	($1, $2)
ON CONFLICT (brigade_id) DO UPDATE
	SET mnemo = EXCLUDED.mnemo
`

func storeVIPMnemo(ctx context.Context, tx pgx.Tx, id uuid.UUID, mnemo string) error {
	if _, err := tx.Exec(ctx, sqlVIPStoreMnemo, id, mnemo); err != nil {
		return fmt.Errorf("store vip mnemo: %w", err)
	}

	return nil
}
