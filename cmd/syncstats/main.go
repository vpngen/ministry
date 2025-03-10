package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http/httputil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	sshVng "github.com/vpngen/ministry/internal/ssh"
	"golang.org/x/crypto/ssh"
)

const (
	sshkeyRemoteUsername = "vgstats"
	sshkeyDefaultPath    = "/etc/vgdept"
	sshTimeOut           = time.Duration(80 * time.Second)
)

const (
	maxPostgresqlNameLen = 63
	defaultDatabaseURL   = "postgresql:///vgdept"
	defaultHeadSchema    = "head"
)

// UpdateTimeResultVersion - is a version of UpdateTimeResult struct.
const UpdateTimeResultVersion = 1

// UpdateTimeResult - is a struct for last update time result.
type UpdateTimeResult struct {
	Version                   int       `json:"version"`
	UpdateTimeIDs             time.Time `json:"ids_update_time"`
	UpdateTimePartners        time.Time `json:"partners_update_time"`
	UpdateTimeActionsPartners time.Time `json:"actions_partners_update_time"`
	UpdateTimeRealms          time.Time `json:"realms_update_time"`
	UpdateTimeActionsRealms   time.Time `json:"actions_realms_update_time"`
	UpdateTimeActions         time.Time `json:"actions_update_time"`
	UpdateTimeStartLabels     time.Time `json:"start_labels_update_time"`
}

// IDsUpdate - is a struct of a table brigades_ids.
type IDsUpdate struct {
	BrigadeID  string    `json:"brigade_id"`
	UpdateTime time.Time `json:"update_time"`
}

// RealmsUpdate - is a struct of a table brigades_realms.
type PartnersUpdate struct {
	PartnerID   string    `json:"partner_id"`
	PartnerName string    `json:"partner_name"`
	UpdateTime  time.Time `json:"update_time"`
}

// ActionsPartnersUpdate - is a struct of a table brigades_actions.
type ActionsPartnersUpdate struct {
	BrigadeID  string    `json:"brigade_id"`
	PartnerID  string    `json:"partner_id"`
	EventName  string    `json:"event_name"`
	EventInfo  string    `json:"event_info"`
	EventTime  time.Time `json:"event_time"`
	UpdateTime time.Time `json:"update_time"`
}

// RealmsUpdate - is a struct of a table brigades_realms.
type RealmsUpdate struct {
	RealmID    string    `json:"realm_id"`
	RealmName  string    `json:"realm_name"`
	UpdateTime time.Time `json:"update_time"`
}

// ActionsRealmsUpdate - is a struct of a table brigades_actions.
type ActionsRealmsUpdate struct {
	BrigadeID  string    `json:"brigade_id"`
	RealmID    string    `json:"realm_id"`
	EventName  string    `json:"event_name"`
	EventInfo  string    `json:"event_info"`
	EventTime  time.Time `json:"event_time"`
	UpdateTime time.Time `json:"update_time"`
}

// ActionsUpdate - is a struct of a table brigades_actions.
type ActionsUpdate struct {
	BrigadeID  string    `json:"brigade_id"`
	EventName  string    `json:"event_name"`
	EventInfo  string    `json:"event_info"`
	EventTime  time.Time `json:"event_time"`
	UpdateTime time.Time `json:"update_time"`
}

// StartLabelsUpdate - is a struct of a table start_labels.
type StartLabelsUpdate struct {
	BrigadeID  string    `json:"brigade_id,omitempty"`
	PartnerID  string    `json:"partner_id"`
	LabelID    string    `json:"label_id"`
	Label      string    `json:"label"`
	CreatedAt  time.Time `json:"created_at,omitempty"`
	FirstVisit time.Time `json:"first_visit"`
	UpdateTime time.Time `json:"update_time"`
}

// UpdatesPackVersion - is a version of IDUpdatesPack struct.
const UpdatesPackVersion = 1

// UpdatesPack - is a update pack for brigades_ids table.
type UpdatesPack struct {
	Version                int                     `json:"version"`
	RealmsUpdates          []RealmsUpdate          `json:"updates_realms"`
	RealmsActionsUpdates   []ActionsRealmsUpdate   `json:"updates_realms_actions"`
	PartnersUpdates        []PartnersUpdate        `json:"updates_partners"`
	PartnersActionsUpdates []ActionsPartnersUpdate `json:"updates_partners_actions"`
	IDsUpdates             []IDsUpdate             `json:"updates_ids"`
	ActionsUpdates         []ActionsUpdate         `json:"updates_actions"`
	StartLabelsUpdates     []StartLabelsUpdate     `json:"updates_start_labels"`
	UpdatesFrom            UpdateTimeResult        `json:"updates_from"`
	UpdateTime             time.Time               `json:"update_time"`
}

var LogTag = setLogTag()

const defaultLogTag = "syncstats"

func setLogTag() string {
	executable, err := os.Executable()
	if err != nil {
		return defaultLogTag
	}

	return filepath.Base(executable)
}

func main() {
	var addr string

	dryRun, addr2, err := parseArgs()
	if err != nil {
		log.Fatalf("Parse args: %s", err)
	}

	sshKeyFilename, addr1, dbURL, schema, err := readConfigs()
	if err != nil {
		log.Fatalf("Can't read configs: %s\n", err)
	}

	sshconf, err := sshVng.CreateSSHConfig(sshKeyFilename, sshkeyRemoteUsername, sshVng.SSHDefaultTimeOut)
	if err != nil {
		log.Fatalf("%s: Can't create ssh configs: %s\n", LogTag, err)
	}

	db, err := createDBPool(dbURL)
	if err != nil {
		log.Fatalf("%s: Can't create db pool: %s\n", LogTag, err)
	}

	switch {
	case addr1 != "":
		addr = addr1
	case addr2 != "":
		addr = addr2
	default:
		log.Fatalf("Stats server address is not set")
	}

	fmt.Fprintf(os.Stderr, "Fetching last updates from %s\n", addr)

	lastUpdates, err := fetchLastUpdates(sshconf, addr)
	if err != nil {
		log.Fatalf("Can't fetch last updates: %s", err)
	}

	now := time.Now().UTC()

	realms, err := syncRealms(db, schema, lastUpdates.UpdateTimeRealms)
	if err != nil {
		log.Fatalf("Sync realms: %s", err)
	}

	log.Println(realms)

	partners, err := syncPartners(db, schema, lastUpdates.UpdateTimePartners)
	if err != nil {
		log.Fatalf("Sync partners: %s", err)
	}

	log.Println(partners)

	ids, err := syncIDs(db, schema, lastUpdates.UpdateTimeIDs)
	if err != nil {
		log.Fatalf("Sync IDs: %s", err)
	}

	log.Println(ids)

	actions, err := syncActions(db, schema, lastUpdates.UpdateTimeActions)
	if err != nil {
		log.Fatalf("Sync actions: %s", err)
	}

	log.Println(actions)

	realmsActions, err := syncRealmsActions(db, schema, lastUpdates.UpdateTimeActionsRealms)
	if err != nil {
		log.Fatalf("Sync realms actions: %s", err)
	}

	log.Println(realmsActions)

	partnersActions, err := syncPartnersActions(db, schema, lastUpdates.UpdateTimeActionsPartners)
	if err != nil {
		log.Fatalf("Sync partners actions: %s", err)
	}

	log.Println(partnersActions)

	startLabels, err := syncStartLabels(db, schema, lastUpdates.UpdateTimeStartLabels)
	if err != nil {
		log.Fatalf("Sync start labels: %s", err)
	}

	pack := &UpdatesPack{
		Version:                UpdatesPackVersion,
		RealmsUpdates:          realms,
		RealmsActionsUpdates:   realmsActions,
		PartnersUpdates:        partners,
		PartnersActionsUpdates: partnersActions,
		IDsUpdates:             ids,
		ActionsUpdates:         actions,
		StartLabelsUpdates:     startLabels,
		UpdatesFrom:            lastUpdates,
		UpdateTime:             now,
	}

	if dryRun {
		buf, err := json.MarshalIndent(pack, "", "  ")
		if err != nil {
			log.Fatalf("Marshal updates pack: %s", err)
		}

		fmt.Println(string(buf))

		return
	}

	if err := applyUpdates(sshconf, addr, pack); err != nil {
		log.Fatalf("Apply updates: %s", err)
	}
}

func applyUpdates(sshConfig *ssh.ClientConfig, addr string, updates *UpdatesPack) error {
	client, err := ssh.Dial("tcp", fmt.Sprintf("%s:22", addr), sshConfig)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}

	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("new session: %w", err)
	}

	defer session.Close()

	var b, e bytes.Buffer

	session.Stdout = &b
	session.Stderr = &e

	defer func() {
		switch errstr := e.String(); errstr {
		case "":
			fmt.Fprintf(os.Stderr, "%s: SSH Session StdErr: empty\n", LogTag)
		default:
			fmt.Fprintf(os.Stderr, "%s: SSH Session StdErr:\n", LogTag)
			for _, line := range strings.Split(errstr, "\n") {
				fmt.Fprintf(os.Stderr, "%s: | %s\n", LogTag, line)
			}
		}
	}()

	stdin, err := session.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}

	if err := session.Start("sync_ids -ch sync"); err != nil {
		return fmt.Errorf("start: %w", err)
	}

	if err := json.NewEncoder(httputil.NewChunkedWriter(stdin)).Encode(updates); err != nil {
		return fmt.Errorf("encode: %w", err)
	}

	if err := session.Wait(); err != nil {
		return fmt.Errorf("wait: %w", err)
	}

	return nil
}

func syncPartnersActions(dbPool *pgxpool.Pool, schema string, lastUpdate time.Time) ([]ActionsPartnersUpdate, error) {
	fmt.Fprintf(os.Stderr, "Requst partners actions updates from: %s\n", lastUpdate.Format(time.RFC3339Nano))

	updates, err := queryPartnersActionsUpdates(dbPool, schema, lastUpdate)
	if err != nil {
		return nil, fmt.Errorf("query partners actions updates: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Actions partners updates: %d\n", len(updates))

	return updates, nil
}

func queryPartnersActionsUpdates(db *pgxpool.Pool, schema string, lastUpdate time.Time) ([]ActionsPartnersUpdate, error) {
	ctx := context.Background()

	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}

	defer tx.Rollback(ctx)

	sqlGetActions := `
	SELECT 
		brigade_id, partner_id, event_name, event_info, event_time, update_time 
	FROM 
		%s 
	WHERE 
		update_time >= $1
	`
	rows, err := tx.Query(
		ctx,
		fmt.Sprintf(
			sqlGetActions,
			(pgx.Identifier{schema, "brigadier_partners_actions"}).Sanitize(),
		),
		lastUpdate,
	)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}

	defer rows.Close()

	var (
		updates    = []ActionsPartnersUpdate{}
		brigadeID  string
		partnerID  string
		eventName  string
		eventInfo  string
		eventTime  time.Time
		updateTime time.Time
	)

	_, err = pgx.ForEachRow(
		rows,
		[]any{
			&brigadeID,
			&partnerID,
			&eventName,
			&eventInfo,
			&eventTime,
			&updateTime,
		},
		func() error {
			updates = append(
				updates,
				ActionsPartnersUpdate{
					BrigadeID:  brigadeID,
					PartnerID:  partnerID,
					EventName:  eventName,
					EventInfo:  eventInfo,
					EventTime:  eventTime,
					UpdateTime: updateTime,
				},
			)

			return nil
		},
	)
	if err != nil {
		return nil, fmt.Errorf("for each row: %w", err)
	}

	return updates, nil
}

func syncRealmsActions(dbPool *pgxpool.Pool, schema string, lastUpdate time.Time) ([]ActionsRealmsUpdate, error) {
	fmt.Fprintf(os.Stderr, "Requst realms actions updates from: %s\n", lastUpdate.Format(time.RFC3339Nano))

	updates, err := queryRealmsActionsUpdates(dbPool, schema, lastUpdate)
	if err != nil {
		return nil, fmt.Errorf("query realms actions updates: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Actions realms updates: %d\n", len(updates))

	return updates, nil
}

func queryRealmsActionsUpdates(db *pgxpool.Pool, schema string, lastUpdate time.Time) ([]ActionsRealmsUpdate, error) {
	ctx := context.Background()

	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}

	defer tx.Rollback(ctx)

	sqlGetActions := `
	SELECT 
		brigade_id, realm_id, event_name, event_info, event_time, update_time 
	FROM 
		%s 
	WHERE 
		update_time >= $1
	`
	rows, err := tx.Query(
		ctx,
		fmt.Sprintf(
			sqlGetActions,
			(pgx.Identifier{schema, "brigadier_realms_actions"}).Sanitize(),
		),
		lastUpdate,
	)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}

	defer rows.Close()

	var (
		updates    = []ActionsRealmsUpdate{}
		brigadeID  string
		realmID    string
		eventName  string
		eventInfo  string
		eventTime  time.Time
		updateTime time.Time
	)

	_, err = pgx.ForEachRow(
		rows,
		[]any{
			&brigadeID,
			&realmID,
			&eventName,
			&eventInfo,
			&eventTime,
			&updateTime,
		},
		func() error {
			updates = append(
				updates,
				ActionsRealmsUpdate{
					BrigadeID:  brigadeID,
					RealmID:    realmID,
					EventName:  eventName,
					EventInfo:  eventInfo,
					EventTime:  eventTime,
					UpdateTime: updateTime,
				},
			)

			return nil
		},
	)
	if err != nil {
		return nil, fmt.Errorf("for each row: %w", err)
	}

	return updates, nil
}

func syncActions(dbPool *pgxpool.Pool, schema string, lastUpdate time.Time) ([]ActionsUpdate, error) {
	fmt.Fprintf(os.Stderr, "Requst actions updates from: %s\n", lastUpdate.Format(time.RFC3339Nano))

	updates, err := queryActionsUpdates(dbPool, schema, lastUpdate)
	if err != nil {
		return nil, fmt.Errorf("query actions updates: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Actions updates: %d\n", len(updates))

	return updates, nil
}

func queryActionsUpdates(db *pgxpool.Pool, schema string, lastUpdate time.Time) ([]ActionsUpdate, error) {
	ctx := context.Background()

	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}

	defer tx.Rollback(ctx)

	sqlGetActions := `
	SELECT 
		brigade_id, event_name, event_info, event_time, update_time 
	FROM 
		%s 
	WHERE 
		update_time >= $1
	`
	rows, err := tx.Query(
		ctx,
		fmt.Sprintf(
			sqlGetActions,
			(pgx.Identifier{schema, "brigades_actions"}).Sanitize(),
		),
		lastUpdate,
	)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}

	defer rows.Close()

	var (
		updates    = []ActionsUpdate{}
		brigadeID  string
		eventName  string
		eventInfo  string
		eventTime  time.Time
		updateTime time.Time
	)

	_, err = pgx.ForEachRow(
		rows,
		[]any{
			&brigadeID,
			&eventName,
			&eventInfo,
			&eventTime,
			&updateTime,
		},
		func() error {
			updates = append(
				updates,
				ActionsUpdate{
					BrigadeID:  brigadeID,
					EventName:  eventName,
					EventInfo:  eventInfo,
					EventTime:  eventTime,
					UpdateTime: updateTime,
				},
			)

			return nil
		},
	)
	if err != nil {
		return nil, fmt.Errorf("for each row: %w", err)
	}

	return updates, nil
}

func syncPartners(dbPool *pgxpool.Pool, schema string, lastUpdate time.Time) ([]PartnersUpdate, error) {
	fmt.Fprintf(os.Stderr, "Requst partners updates from: %s\n", lastUpdate.Format(time.RFC3339Nano))

	updates, err := queryPartnersUpdates(dbPool, schema, lastUpdate)
	if err != nil {
		return nil, fmt.Errorf("query partners updates: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Realms updates: %d\n", len(updates))

	return updates, nil
}

func queryPartnersUpdates(db *pgxpool.Pool, schema string, lastUpdate time.Time) ([]PartnersUpdate, error) {
	ctx := context.Background()

	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}

	defer tx.Rollback(ctx)

	sqlGetPartners := `
	SELECT 
		partner_id, partner, update_time 
	FROM 
		%s 
	WHERE 
		update_time >= $1
	`
	rows, err := tx.Query(
		ctx,
		fmt.Sprintf(
			sqlGetPartners,
			(pgx.Identifier{schema, "partners"}).Sanitize(),
		),
		lastUpdate,
	)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}

	defer rows.Close()

	var (
		updates     = []PartnersUpdate{}
		partnerID   string
		partnerName string
		updateTime  time.Time
	)

	_, err = pgx.ForEachRow(
		rows,
		[]any{
			&partnerID,
			&partnerName,
			&updateTime,
		},
		func() error {
			updates = append(
				updates,
				PartnersUpdate{
					PartnerID:   partnerID,
					PartnerName: partnerName,
					UpdateTime:  updateTime,
				},
			)

			return nil
		},
	)
	if err != nil {
		return nil, fmt.Errorf("for each row: %w", err)
	}

	return updates, nil
}

func syncRealms(dbPool *pgxpool.Pool, schema string, lastUpdate time.Time) ([]RealmsUpdate, error) {
	fmt.Fprintf(os.Stderr, "Requst realms updates from: %s\n", lastUpdate.Format(time.RFC3339Nano))

	updates, err := queryRealmsUpdates(dbPool, schema, lastUpdate)
	if err != nil {
		return nil, fmt.Errorf("query realms updates: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Realms updates: %d\n", len(updates))

	return updates, nil
}

func queryRealmsUpdates(db *pgxpool.Pool, schema string, lastUpdate time.Time) ([]RealmsUpdate, error) {
	ctx := context.Background()

	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}

	defer tx.Rollback(ctx)

	sqlGetRealms := `
	SELECT 
		realm_id, realm_name, update_time 
	FROM 
		%s 
	WHERE 
		update_time >= $1
	`
	rows, err := tx.Query(
		ctx,
		fmt.Sprintf(
			sqlGetRealms,
			(pgx.Identifier{schema, "realms"}).Sanitize(),
		),
		lastUpdate,
	)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}

	defer rows.Close()

	var (
		updates    = []RealmsUpdate{}
		realmID    string
		realmName  string
		updateTime time.Time
	)

	_, err = pgx.ForEachRow(
		rows,
		[]any{
			&realmID,
			&realmName,
			&updateTime,
		},
		func() error {
			updates = append(
				updates,
				RealmsUpdate{
					RealmID:    realmID,
					RealmName:  realmName,
					UpdateTime: updateTime,
				},
			)

			return nil
		},
	)
	if err != nil {
		return nil, fmt.Errorf("for each row: %w", err)
	}

	return updates, nil
}

func syncIDs(dbPool *pgxpool.Pool, schema string, lastUpdate time.Time) ([]IDsUpdate, error) {
	fmt.Fprintf(os.Stderr, "Requst IDs updates from: %s\n", lastUpdate.Format(time.RFC3339Nano))

	updates, err := queryIDsUpdates(dbPool, schema, lastUpdate)
	if err != nil {
		return nil, fmt.Errorf("query IDs updates: %w", err)
	}

	fmt.Fprintf(os.Stderr, "IDs updates: %d\n", len(updates))

	return updates, nil
}

func queryIDsUpdates(db *pgxpool.Pool, schema string, lastUpdate time.Time) ([]IDsUpdate, error) {
	ctx := context.Background()

	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}

	defer tx.Rollback(ctx)

	sqlGetIDs := `
	SELECT
		brigade_id, update_time
	FROM 
		%s 
	WHERE 
		update_time >= $1
	`
	rows, err := tx.Query(
		ctx,
		fmt.Sprintf(
			sqlGetIDs,
			(pgx.Identifier{schema, "brigadiers_ids"}).Sanitize(),
		),
		lastUpdate,
	)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}

	defer rows.Close()

	var (
		updates    = []IDsUpdate{}
		brigadeID  string
		updateTime time.Time
	)

	_, err = pgx.ForEachRow(
		rows,
		[]any{
			&brigadeID,
			&updateTime,
		},
		func() error {
			updates = append(
				updates,
				IDsUpdate{
					BrigadeID:  brigadeID,
					UpdateTime: updateTime,
				},
			)

			return nil
		},
	)
	if err != nil {
		return nil, fmt.Errorf("for each row: %w", err)
	}

	return updates, nil
}

func syncStartLabels(dbPool *pgxpool.Pool, schema string, lastUpdate time.Time) ([]StartLabelsUpdate, error) {
	fmt.Fprintf(os.Stderr, "Requst start labels updates from: %s\n", lastUpdate.Format(time.RFC3339Nano))

	updates, err := queryStartLabelsUpdates(dbPool, schema, lastUpdate)
	if err != nil {
		return nil, fmt.Errorf("query start labels updates: %w", err)
	}

	fmt.Fprintf(os.Stderr, "start labels updates: %d\n", len(updates))

	return updates, nil
}

func queryStartLabelsUpdates(db *pgxpool.Pool, schema string, lastUpdate time.Time) ([]StartLabelsUpdate, error) {
	ctx := context.Background()

	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}

	defer tx.Rollback(ctx)

	sqlGetStartLabels := `
	SELECT
		brigade_id, created_at, partner_id, label_id, label, first_visit, update_time
	FROM 
		%s 
	WHERE 
		update_time >= $1
	`
	rows, err := tx.Query(
		ctx,
		fmt.Sprintf(
			sqlGetStartLabels,
			(pgx.Identifier{schema, "start_labels"}).Sanitize(),
		),
		lastUpdate,
	)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}

	defer rows.Close()

	var (
		updates    = []StartLabelsUpdate{}
		brigadeID  pgtype.UUID
		partnerID  uuid.UUID
		createdAt  pgtype.Timestamp
		labelID    uuid.UUID
		label      string
		firstVisit time.Time
		updateTime time.Time
	)

	_, err = pgx.ForEachRow(
		rows,
		[]any{
			&brigadeID,
			&createdAt,
			&partnerID,
			&labelID,
			&label,
			&firstVisit,
			&updateTime,
		},
		func() error {
			l := StartLabelsUpdate{
				LabelID:    labelID.String(),
				PartnerID:  partnerID.String(),
				Label:      label,
				FirstVisit: firstVisit,
				UpdateTime: updateTime,
			}

			if brigadeID.Valid {
				l.BrigadeID = uuid.UUID(brigadeID.Bytes).String()
			}

			if createdAt.Valid {
				l.CreatedAt = createdAt.Time
			}

			updates = append(updates, l)

			return nil
		},
	)
	if err != nil {
		return nil, fmt.Errorf("for each row: %w", err)
	}

	return updates, nil
}

func fetchLastUpdates(sshConfig *ssh.ClientConfig, addr string) (UpdateTimeResult, error) {
	result := UpdateTimeResult{}

	client, err := ssh.Dial("tcp", fmt.Sprintf("%s:22", addr), sshConfig)
	if err != nil {
		return result, fmt.Errorf("dial: %w", err)
	}

	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return result, fmt.Errorf("new session: %w", err)
	}

	defer session.Close()

	var b, e bytes.Buffer

	session.Stdout = &b
	session.Stderr = &e

	cmd := "sync_ids -ch lastupdate"
	if err := session.Run(cmd); err != nil {
		fmt.Fprintf(os.Stderr, "session errors:\n%s\n", e.String())

		return result, fmt.Errorf("run: %w", err)
	}

	resp, err := io.ReadAll(httputil.NewChunkedReader(&b))
	if err != nil {
		fmt.Fprintf(os.Stderr, "session errors:\n%s\n", e.String())

		return result, fmt.Errorf("read data: %w", err)
	}

	if e.String() != "" {
		fmt.Fprintf(os.Stderr, "<<<<<\n%s\n>>>>>\n", e.String())
	}

	if err := json.Unmarshal(resp, &result); err != nil {
		return result, fmt.Errorf("unmarshal: %w", err)
	}

	return result, nil
}

func parseArgs() (bool, string, error) {
	addr := flag.String("a", "", "address of stats server")
	dryRun := flag.Bool("n", false, "dry run")

	flag.Parse()

	return *dryRun, *addr, nil
}

func createDBPool(dburl string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(dburl)
	if err != nil {
		return nil, fmt.Errorf("conn string: %w", err)
	}

	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	return pool, nil
}

func readConfigs() (string, string, string, string, error) {
	dbURL := os.Getenv("DB_URL")
	if dbURL == "" {
		dbURL = defaultDatabaseURL
	}

	headSchema := os.Getenv("HEAD_ADMIN_SCHEMA")
	if headSchema == "" {
		headSchema = defaultHeadSchema
	}

	sshKeyFilename, err := sshVng.LookupForSSHKeyfile(os.Getenv("SSH_KEY"), sshkeyDefaultPath)
	if err != nil {
		return "", "", "", "", fmt.Errorf("lookup for ssh key: %w", err)
	}

	addr := os.Getenv("STATS_SERVER")

	return sshKeyFilename, addr, dbURL, headSchema, nil
}
