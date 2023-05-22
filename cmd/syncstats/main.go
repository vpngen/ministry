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

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/ssh"
)

const (
	defaultSchema = "library"
)

const (
	sshkeyED25519Filename = "id_ed25519"
	sshkeyRemoteUsername  = "vgstats"
	etcDefaultPath        = "/etc/vgdept"
)

const (
	defaultDatabaseURL = "postgresql:///vgdept"
)

const sshTimeOut = time.Duration(5 * time.Second)

// UpdateTimeResultVersion - is a version of UpdateTimeResult struct.
const UpdateTimeResultVersion = 1

// UpdateTimeResult - is a struct for last update time result.
type UpdateTimeResult struct {
	Version            int       `json:"version"`
	UpdateTimeIDs      time.Time `json:"ids_update_time"`
	UpdateTimePartners time.Time `json:"partners_update_time"`
	UpdateTimeRealms   time.Time `json:"realms_update_time"`
	UpdateTimeActions  time.Time `json:"actions_update_time"`
}

// IDsUpdate - is a struct of a table brigades_ids.
type IDsUpdate struct {
	BrigadeID  string    `json:"brigade_id"`
	RealmID    string    `json:"realm_id"`
	PartnerID  string    `json:"partner_id"`
	UpdateTime time.Time `json:"update_time"`
}

// RealmsUpdate - is a struct of a table brigades_realms.
type PartnersUpdate struct {
	PartnerID   string    `json:"partner_id"`
	PartnerName string    `json:"partner_name"`
	UpdateTime  time.Time `json:"update_time"`
}

// RealmsUpdate - is a struct of a table brigades_realms.
type RealmsUpdate struct {
	RealmID    string    `json:"realm_id"`
	RealmName  string    `json:"realm_name"`
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

// UpdatesPackVersion - is a version of IDUpdatesPack struct.
const UpdatesPackVersion = 1

// UpdatesPack - is a update pack for brigades_ids table.
type UpdatesPack struct {
	Version         int              `json:"version"`
	RealmsUpdates   []RealmsUpdate   `json:"updates_realms"`
	PartnersUpdates []PartnersUpdate `json:"updates_partners"`
	IDsUpdates      []IDsUpdate      `json:"updates_ids"`
	ActionsUpdates  []ActionsUpdate  `json:"updates_actions"`
	UpdatesFrom     UpdateTimeResult `json:"updates_from"`
	UpdateTime      time.Time        `json:"update_time"`
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

	sshKeyDir, addr1, dbURL, schema, err := readConfigs()
	if err != nil {
		log.Fatalf("Read configs: %s", err)
	}

	dryRun, addr2, err := parseArgs()
	if err != nil {
		log.Fatalf("Parse args: %s", err)
	}

	switch {
	case addr1 != "":
		addr = addr1
	case addr2 != "":
		addr = addr2
	default:
		log.Fatalf("Stats server address is not set")
	}

	sshConfig, err := createSSHConfig(sshKeyDir)
	if err != nil {
		log.Fatalf("Create SSH config: %s", err)
	}

	dbPool, err := createDBPool(dbURL)
	if err != nil {
		log.Fatalf("Create DB pool: %s", err)
	}

	fmt.Fprintf(os.Stderr, "Fetching last updates from %s\n", addr)

	lastUpdates, err := fetchLastUpdates(sshConfig, addr)
	if err != nil {
		log.Fatalf("Can't fetch last updates: %s", err)
	}

	realms, err := syncRealms(sshConfig, addr, dbPool, schema, lastUpdates.UpdateTimeRealms)
	if err != nil {
		log.Fatalf("Sync realms: %s", err)
	}

	log.Println(realms)

	partners, err := syncPartners(sshConfig, addr, dbPool, schema, lastUpdates.UpdateTimePartners)
	if err != nil {
		log.Fatalf("Sync partners: %s", err)
	}

	log.Println(partners)

	ids, err := syncIDs(sshConfig, addr, dbPool, schema, lastUpdates.UpdateTimeIDs)
	if err != nil {
		log.Fatalf("Sync IDs: %s", err)
	}

	log.Println(ids)

	actions, err := syncActions(sshConfig, addr, dbPool, schema, lastUpdates.UpdateTimeActions)
	if err != nil {
		log.Fatalf("Sync actions: %s", err)
	}

	log.Println(actions)

	pack := &UpdatesPack{
		Version:         UpdatesPackVersion,
		RealmsUpdates:   realms,
		PartnersUpdates: partners,
		IDsUpdates:      ids,
		ActionsUpdates:  actions,
		UpdatesFrom:     lastUpdates,
		UpdateTime:      time.Now().UTC(),
	}

	if dryRun {
		buf, err := json.MarshalIndent(pack, "", "  ")
		if err != nil {
			log.Fatalf("Marshal updates pack: %s", err)
		}

		fmt.Println(string(buf))

		return
	}

	if err := applyUpdates(sshConfig, addr, pack); err != nil {
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

	if err := session.Start("/home/vgstats/syncids -ch sync"); err != nil {
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

func syncActions(sshConfig *ssh.ClientConfig, addr string, dbPool *pgxpool.Pool, schema string, lastUpdate time.Time) ([]ActionsUpdate, error) {
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

	sqlGetActions := `SELECT brigade_id, event_name, event_info, event_time,update_time FROM %s WHERE update_time >= $1`
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

func syncPartners(sshConfig *ssh.ClientConfig, addr string, dbPool *pgxpool.Pool, schema string, lastUpdate time.Time) ([]PartnersUpdate, error) {
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

	sqlGetPartners := `SELECT partner_id,partner,update_time FROM %s WHERE update_time >= $1`
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

func syncRealms(sshConfig *ssh.ClientConfig, addr string, dbPool *pgxpool.Pool, schema string, lastUpdate time.Time) ([]RealmsUpdate, error) {
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

	sqlGetRealms := `SELECT realm_id,realm_name,update_time FROM %s WHERE update_time >= $1`
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

func syncIDs(sshConfig *ssh.ClientConfig, addr string, dbPool *pgxpool.Pool, schema string, lastUpdate time.Time) ([]IDsUpdate, error) {
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

	sqlGetIDs := `SELECT brigade_id,realm_id,partner_id,update_time FROM %s WHERE update_time >= $1`
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
		realmID    string
		partnerID  string
		updateTime time.Time
	)

	_, err = pgx.ForEachRow(
		rows,
		[]any{
			&brigadeID,
			&realmID,
			&partnerID,
			&updateTime,
		},
		func() error {
			updates = append(
				updates,
				IDsUpdate{
					BrigadeID:  brigadeID,
					RealmID:    realmID,
					PartnerID:  partnerID,
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

	cmd := "/home/vgstats/syncids -ch lastupdate"
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

func createSSHConfig(path string) (*ssh.ClientConfig, error) {
	// var hostKey ssh.PublicKey

	key, err := os.ReadFile(filepath.Join(path, sshkeyED25519Filename))
	if err != nil {
		return nil, fmt.Errorf("read private key: %w", err)
	}

	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}

	config := &ssh.ClientConfig{
		User: sshkeyRemoteUsername,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		// HostKeyCallback: ssh.FixedHostKey(hostKey),
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         sshTimeOut,
	}

	return config, nil
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

func parseArgs() (bool, string, error) {
	addr := flag.String("a", "", "address of stats server")
	dryRun := flag.Bool("n", false, "dry run")

	flag.Parse()

	return *dryRun, *addr, nil
}

func readConfigs() (string, string, string, string, error) {
	dbURL := os.Getenv("DB_URL")
	if dbURL == "" {
		dbURL = defaultDatabaseURL
	}

	schema := os.Getenv("SCHEMA")
	if schema == "" {
		schema = defaultSchema
	}

	addr := os.Getenv("STATS_SERVER")

	sshKeyDir := os.Getenv("CONFDIR")
	if sshKeyDir == "" {
		sshKeyDir = etcDefaultPath
	}

	if fstat, err := os.Stat(sshKeyDir); err != nil || !fstat.IsDir() {
		sshKeyDir = etcDefaultPath
	}

	return sshKeyDir, addr, dbURL, schema, nil
}
