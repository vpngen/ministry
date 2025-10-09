package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http/httputil"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/vpngen/ministry"
	"github.com/vpngen/ministry/internal/core"
	"github.com/vpngen/ministry/internal/pgsql"
	"github.com/vpngen/wordsgens/namesgenerator"
)

const (
	defaultDatabaseURL    = "postgresql:///vgdept"
	defaultBrigadesSchema = "head"
)

const brigadeCreationType = "ssh_api"

const (
	maxStartLabelLen = 64
)

var (
	ErrEmptyAccessToken = errors.New("token not specified")
	ErrLabelTooLong     = errors.New("label too long")
)

func main() {
	var w io.WriteCloser

	person, name, chunked, token, label, labelID, fv, err := parseArgs()
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

	brigadeID, err := core.RequestVIPBrigade(ctx, db, partnerID, brigadeCreationType, person, name, label, labelID, fv)
	if err != nil {
		fatal(w, "%s: Can't create brigade: %s\n", LogTag, err)
	}

	var resUUID uuid.UUID
	for i := range 16 {
		resUUID[i] = brigadeID[i] ^ obfsUUID[i]
	}

	answ := ministry.VIPReserve{
		RequestID: resUUID,
	}

	payload, err := json.Marshal(answ)
	if err != nil {
		fatal(w, "%s: Can't marshal answer: %s\n", LogTag, err)
	}

	if _, err := w.Write(payload); err != nil {
		fatal(w, "%s: Can't write answer: %s\n", LogTag, err)
	}
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

func parseArgs() (*namesgenerator.Person, string, bool, []byte, string, string, int64, error) {
	chunked := flag.Bool("ch", false, "chunked output")
	label := flag.String("l", "", "label")
	labelID := flag.String("lu", "", "label UUID")
	labelTime := flag.Int("lt", 0, "first visit")
	customName := flag.String("name", "", "custom brigadier fullname")
	forcePerson := flag.String("p", "", "force person")

	flag.Parse()

	if *label != "" && len(*label) > maxStartLabelLen {
		return nil, "", false, nil, "", "", 0, fmt.Errorf("label: %w", ErrLabelTooLong)
	}

	id := *labelID
	if id == "" {
		id = uuid.New().String()
	}

	firstVisit := *labelTime
	if firstVisit <= 0 {
		firstVisit = int(time.Now().Unix())
	}

	a := flag.Args()
	if len(a) < 1 {
		return nil, "", false, nil, "", "", 0, fmt.Errorf("access token: %w", ErrEmptyAccessToken)
	}

	token := make([]byte, base64.URLEncoding.WithPadding(base64.NoPadding).DecodedLen(len(a[0])))
	_, err := base64.URLEncoding.WithPadding(base64.NoPadding).Decode(token, []byte(a[0]))
	if err != nil {
		return nil, "", false, nil, "", "", 0, fmt.Errorf("access token: %w", err)
	}

	var person *namesgenerator.Person
	if *forcePerson != "" {
		buf, err := base64.StdEncoding.WithPadding(base64.StdPadding).DecodeString(*forcePerson)
		if err != nil {
			return nil, "", false, nil, "", "", 0, fmt.Errorf("force person: %w", err)
		}

		if err := json.Unmarshal(buf, &person); err != nil {
			return nil, "", false, nil, "", "", 0, fmt.Errorf("force person: %w", err)
		}
	}

	var fullname string
	if *customName != "" {
		buf, err := base64.StdEncoding.WithPadding(base64.StdPadding).DecodeString(*customName)
		if err != nil {
			return nil, "", false, nil, "", "", 0, fmt.Errorf("custom name: %w", err)
		}

		fullname = string(buf)
	}

	return person, fullname, *chunked, token, *label, id, int64(firstVisit), nil
}
