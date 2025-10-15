package main

import (
	"context"
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http/httputil"
	"os"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vpngen/ministry"
	"github.com/vpngen/ministry/internal/pgsql"
)

const (
	defaultDatabaseURL    = "postgresql:///vgdept"
	defaultBrigadesSchema = "head"
)

var (
	ErrEmptyAccessToken = errors.New("token not specified")
	ErrInvalidUUID      = errors.New("invalid uuid")
	ErrPartnerMismatch  = errors.New("partner mismatch")
)

func main() {
	var w io.WriteCloser

	chunked, token, inUUID, err := parseArgs()
	if err != nil {
		log.Fatalf("%s: Can't parse args: %s\n", LogTag, err)
	}

	switch chunked {
	case true:
		w = httputil.NewChunkedWriter(os.Stdout)
		defer w.Close()
	default:
		w = os.Stdout
	}

	obfsUUID, dbURL, err := readConfigs()
	if err != nil {
		fatal(w, "Can't read configs: %s\n", err)
	}

	db, err := pgsql.CreateDBPool(dbURL)
	if err != nil {
		fatal(w, "%s: Can't create db pool: %s\n", LogTag, err)
	}

	ctx := context.Background()

	partnerID, ok, err := checkToken(ctx, db, defaultBrigadesSchema, token)
	if err != nil || !ok {
		if err != nil {
			fatal(w, "%s: Can't check token: %s\n", LogTag, err)
		}

		fatal(w, "%s: Access denied\n", LogTag)
	}

	if inUUID != uuid.Nil {
		if err := doneMessage(ctx, db, partnerID, inUUID, obfsUUID); err != nil {
			fatal(w, "%s: Can't mark message as done: %s\n", LogTag, err)
		}

		return
	}

	answ, err := getMessage(ctx, db, partnerID, obfsUUID)
	if err != nil {
		fatal(w, "%s: Can't get message: %s\n", LogTag, err)
	}

	payload, err := json.MarshalIndent(answ, "", "  ")
	if err != nil {
		fatal(w, "%s: Can't marshal answer: %s\n", LogTag, err)
	}

	if _, err := w.Write(payload); err != nil {
		fatal(w, "%s: Can't write answer: %s\n", LogTag, err)
	}
}

const sqlGetMessage = `
SELECT 
	vm.brigade_id,
	vt.telegram_id,
	vm.vpnconfig
FROM 
	head.vip_messages vm
JOIN
	head.vip_telegram_ids vt ON vm.brigade_id = vt.brigade_id
JOIN
	head.brigadier_partners bp ON bp.brigade_id = vm.brigade_id
WHERE
	bp.partner_id = $1
	AND vm.finalizer = true
	AND vm.vpnconfig != ''
	AND vm.last_try < NOW() AT TIME ZONE 'UTC' - INTERVAL '2 MINUTES'
ORDER BY
	vm.last_try DESC
LIMIT 1
`

const sqlUpdateMessage = `
UPDATE 
	head.vip_messages
SET 
	last_try = NOW() AT TIME ZONE 'UTC'
WHERE 
	brigade_id = $1
`

func getMessage(ctx context.Context, db *pgxpool.Pool, partnerID, obfsUUID uuid.UUID) (*ministry.VIPAnswer, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}

	defer tx.Rollback(ctx)

	var (
		msg       ministry.Answer
		payload   string
		tgID      int64
		brigadeID uuid.UUID
	)

	if err := tx.QueryRow(ctx, sqlGetMessage, partnerID).Scan(&brigadeID, &tgID, &payload); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}

		return nil, fmt.Errorf("query row: %w", err)
	}

	if err := json.Unmarshal([]byte(payload), &msg); err != nil {
		return nil, fmt.Errorf("unmarshal payload: %w", err)
	}

	if _, err := tx.Exec(ctx, sqlUpdateMessage, brigadeID); err != nil {
		return nil, fmt.Errorf("update message: %w", err)
	}

	var outUUID uuid.UUID
	for i := range 16 {
		outUUID[i] = brigadeID[i] ^ obfsUUID[i]
	}

	answ := &ministry.VIPAnswer{
		Answer:     msg,
		TelegramID: tgID,
		RequestID:  outUUID,
	}

	return answ, nil
}

const sqlDoneMessage = `
DELETE FROM 
	head.vip_messages
WHERE
	brigade_id = $1
`

const sqlDeleteTelegramID = `
DELETE FROM 
	head.vip_telegram_ids
WHERE
	brigade_id = $1
`

const sqlGetBrigadePartnerID = `
SELECT 
	bp.partner_id	
FROM
	head.brigadier_partners bp
WHERE
	bp.brigade_id = $1
LIMIT 1
`

func doneMessage(ctx context.Context, db *pgxpool.Pool, inPartnerID, inUUID, obfsUUID uuid.UUID) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	defer tx.Rollback(ctx)

	var brigadeID uuid.UUID
	for i := range 16 {
		brigadeID[i] = inUUID[i] ^ obfsUUID[i]
	}

	var partnerID uuid.UUID
	if err := tx.QueryRow(ctx, sqlGetBrigadePartnerID, brigadeID).Scan(&partnerID); err != nil {
		return fmt.Errorf("get brigade partner id: %w", err)
	}

	if partnerID != inPartnerID {
		return fmt.Errorf("%w: %s", ErrPartnerMismatch, inPartnerID.String())
	}

	if _, err := tx.Exec(ctx, sqlDoneMessage, brigadeID); err != nil {
		return fmt.Errorf("exec: %w", err)
	}

	if _, err := tx.Exec(ctx, sqlDeleteTelegramID, brigadeID); err != nil {
		return fmt.Errorf("exec delete tg id: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	return nil
}

func readConfigs() (uuid.UUID, string, error) {
	dbURL := os.Getenv("DB_URL")
	if dbURL == "" {
		dbURL = defaultDatabaseURL
	}

	obfsKey := os.Getenv("OBFS_UUID")

	obfsUUID, err := uuid.Parse(obfsKey)
	if err != nil {
		return uuid.Nil, dbURL, fmt.Errorf("parse obfs uuid: %w", err)
	}

	obfsUUID[6] &= 0x0F
	obfsUUID[8] &= 0x3F

	return obfsUUID, dbURL, nil
}

func parseArgs() (bool, []byte, uuid.UUID, error) {
	chunked := flag.Bool("ch", false, "chunked output")
	actDone := flag.String("id", "", "action done")

	flag.Parse()

	a := flag.Args()
	if len(a) < 1 {
		return false, nil, uuid.Nil, fmt.Errorf("access token: %w", ErrEmptyAccessToken)
	}

	token := make([]byte, base64.URLEncoding.WithPadding(base64.NoPadding).DecodedLen(len(a[0])))
	if _, err := base64.URLEncoding.WithPadding(base64.NoPadding).Decode(token, []byte(a[0])); err != nil {
		return false, nil, uuid.Nil, fmt.Errorf("access token: %w", err)
	}

	if *actDone == "" {
		return *chunked, token, uuid.Nil, nil
	}

	var inUUID uuid.UUID
	inUUID, err := uuid.Parse(*actDone)
	if err != nil {
		buf, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(*actDone)
		if err != nil {
			return false, nil, uuid.Nil, fmt.Errorf("action done: %w:%s", ErrInvalidUUID, err.Error())
		}

		if len(buf) != 16 {
			return false, nil, uuid.Nil, fmt.Errorf("action done: %w", ErrInvalidUUID)
		}
	}

	return *chunked, token, inUUID, nil
}
