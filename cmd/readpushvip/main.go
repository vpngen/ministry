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
	"github.com/vpngen/ministry/internal/pgsql"
)

const (
	defaultDatabaseURL    = "postgresql:///vgdept"
	defaultBrigadesSchema = "head"
)

var (
	ErrEmptyAccessToken = errors.New("token not specified")
	ErrInvalidUUID      = errors.New("invalid uuid")
	ErrEventRequired    = errors.New("event type required when using -id")
	ErrPartnerMismatch  = errors.New("partner mismatch")
)

// PushAnswer is the response returned for each pending VIP notification.
type PushAnswer struct {
	TelegramID int64     `json:"telegram_id"`
	RequestID  uuid.UUID `json:"request_id"`
	EventType  string    `json:"event_type"`
	Lang       string    `json:"lang"`
}

func main() {
	var w io.WriteCloser

	chunked, token, requestID, eventType, err := parseArgs()
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

	// -id + -event: mark a previously fetched notification as sent.
	if requestID != uuid.Nil {
		if err := doneNotification(ctx, db, partnerID, requestID, eventType, obfsUUID); err != nil {
			fatal(w, "%s: Can't mark notification as done: %s\n", LogTag, err)
		}

		return
	}

	answ, err := getNotification(ctx, db, partnerID, obfsUUID)
	if err != nil {
		fatal(w, "%s: Can't get notification: %s\n", LogTag, err)
	}

	payload, err := json.MarshalIndent(answ, "", "  ")
	if err != nil {
		fatal(w, "%s: Can't marshal answer: %s\n", LogTag, err)
	}

	if _, err := w.Write(payload); err != nil {
		fatal(w, "%s: Can't write answer: %s\n", LogTag, err)
	}
}

// sqlGetNotification fetches the oldest unsent VIP push notification for brigades
// belonging to the given partner, joining vip_telegram_ids for Telegram contact info.
const sqlGetNotification = `
SELECT
	pm.brigade_id,
	vt.telegram_id,
	vt.lang,
	pm.event_type
FROM
	head.push_messages pm
JOIN
	head.vip_telegram_ids vt ON pm.brigade_id = vt.brigade_id
JOIN
	head.brigadier_partners bp ON bp.brigade_id = pm.brigade_id
WHERE
	bp.partner_id = $1
	AND pm.sent_at IS NULL
	AND pm.event_type LIKE 'vip.%'
	AND pm.last_try < NOW() AT TIME ZONE 'UTC' - INTERVAL '2 MINUTES'
ORDER BY
	pm.last_try ASC
LIMIT 1
`

const sqlUpdateLastTry = `
UPDATE
	head.push_messages
SET
	last_try = NOW() AT TIME ZONE 'UTC'
WHERE
	brigade_id = $1
	AND event_type = $2
`

func getNotification(ctx context.Context, db *pgxpool.Pool, partnerID, obfsUUID uuid.UUID) (*PushAnswer, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}

	defer tx.Rollback(ctx)

	var (
		brigadeID uuid.UUID
		tgID      int64
		lang      string
		eventType string
	)

	if err := tx.QueryRow(ctx, sqlGetNotification, partnerID).Scan(&brigadeID, &tgID, &lang, &eventType); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}

		return nil, fmt.Errorf("query row: %w", err)
	}

	if _, err := tx.Exec(ctx, sqlUpdateLastTry, brigadeID, eventType); err != nil {
		return nil, fmt.Errorf("update last_try: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	var requestID uuid.UUID
	for i := range 16 {
		requestID[i] = brigadeID[i] ^ obfsUUID[i]
	}

	return &PushAnswer{
		TelegramID: tgID,
		RequestID:  requestID,
		EventType:  eventType,
		Lang:       lang,
	}, nil
}

const sqlMarkSent = `
UPDATE
	head.push_messages
SET
	sent_at = NOW() AT TIME ZONE 'UTC'
WHERE
	brigade_id = $1
	AND event_type = $2
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

func doneNotification(ctx context.Context, db *pgxpool.Pool, inPartnerID, requestID uuid.UUID, eventType string, obfsUUID uuid.UUID) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	defer tx.Rollback(ctx)

	var brigadeID uuid.UUID
	for i := range 16 {
		brigadeID[i] = requestID[i] ^ obfsUUID[i]
	}

	var partnerID uuid.UUID
	if err := tx.QueryRow(ctx, sqlGetBrigadePartnerID, brigadeID).Scan(&partnerID); err != nil {
		return fmt.Errorf("get brigade partner: %w", err)
	}

	if partnerID != inPartnerID {
		return fmt.Errorf("%w: %s", ErrPartnerMismatch, inPartnerID)
	}

	if _, err := tx.Exec(ctx, sqlMarkSent, brigadeID, eventType); err != nil {
		return fmt.Errorf("mark sent: %w", err)
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

func parseArgs() (bool, []byte, uuid.UUID, string, error) {
	chunked := flag.Bool("ch", false, "chunked output")
	actDone := flag.String("id", "", "mark notification as sent (obfuscated brigade id)")
	eventType := flag.String("event", "", "event type, required with -id")

	flag.Parse()

	a := flag.Args()
	if len(a) < 1 {
		return false, nil, uuid.Nil, "", fmt.Errorf("access token: %w", ErrEmptyAccessToken)
	}

	token := make([]byte, base64.URLEncoding.WithPadding(base64.NoPadding).DecodedLen(len(a[0])))
	if _, err := base64.URLEncoding.WithPadding(base64.NoPadding).Decode(token, []byte(a[0])); err != nil {
		return false, nil, uuid.Nil, "", fmt.Errorf("access token: %w", err)
	}

	if *actDone == "" {
		return *chunked, token, uuid.Nil, "", nil
	}

	if *eventType == "" {
		return false, nil, uuid.Nil, "", ErrEventRequired
	}

	requestID, err := uuid.Parse(*actDone)
	if err != nil {
		buf, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(*actDone)
		if err != nil {
			return false, nil, uuid.Nil, "", fmt.Errorf("action done: %w: %s", ErrInvalidUUID, err)
		}

		if len(buf) != 16 {
			return false, nil, uuid.Nil, "", fmt.Errorf("action done: %w", ErrInvalidUUID)
		}

		copy(requestID[:], buf)
	}

	return *chunked, token, requestID, *eventType, nil
}
