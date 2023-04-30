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
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/ssh"
)

const (
	defaultSchema = "library"
)

const (
	sshkeyFilename       = "id_ecdsa"
	sshkeyRemoteUsername = "vgstats"
	etcDefaultPath       = "/etc/vgdept"
)

const (
	defaultDatabaseURL = "postgresql:///vgdept"
)

const sshTimeOut = time.Duration(5 * time.Second)

// UpdateTimeResultVersion - is a version of UpdateTimeResult struct.
const UpdateTimeResultVersion = 1

// UpdateTimeResult - is a struct for last update time result.
type UpdateTimeResult struct {
	Version    int       `json:"version"`
	UpdateTime time.Time `json:"update_time"`
}

// IDUpdate - is a struct of a table brigades_ids.
type IDUpdate struct {
	BrigadeID  string    `json:"brigade_id"`
	RealmID    string    `json:"realm_id"`
	PartnerID  string    `json:"partner_id"`
	Reason     string    `json:"reason,omitempty"`
	CreatedAt  time.Time `json:"created_at,omitempty"`
	DeletedAt  time.Time `json:"deleted_at,omitempty"`
	PurgedAt   time.Time `json:"purged_at,omitempty"`
	UpdateTime time.Time `json:"update_time"`
}

// IDUpdatesPackVersion - is a version of IDUpdatesPack struct.
const IDUpdatesPackVersion = 1

// IDUpdatesPack - is a update pack for brigades_ids table.
type IDUpdatesPack struct {
	Version    int        `json:"version"`
	Updates    []IDUpdate `json:"updates"`
	UpdateTime time.Time  `json:"update_time"`
}

func main() {
	var addr string

	sshKeyDir, addr1, dbURL, schema, err := readConfigs()
	if err != nil {
		log.Fatalf("Read configs: %s", err)
	}

	addr2, err := parseArgs()
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

	if err := syncIDs(sshConfig, addr, dbPool, schema); err != nil {
		log.Fatalf("Sync IDs: %s", err)
	}
}

func syncIDs(sshConfig *ssh.ClientConfig, addr string, dbPool *pgxpool.Pool, schema string) error {
	lastUpdate, err := fetchLastUpdate(sshConfig, addr)
	if err != nil {
		return fmt.Errorf("fetch last update: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Last update: %s\n", lastUpdate.Format(time.RFC3339Nano))

	updates, err := prepareUpdates(dbPool, schema, lastUpdate)
	if err != nil {
		return fmt.Errorf("prepare updates: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Updates count: %d\n", len(updates.Updates))

	if err := applyUpdates(sshConfig, addr, updates); err != nil {
		return fmt.Errorf("apply updates: %w", err)
	}

	return nil
}

func applyUpdates(sshConfig *ssh.ClientConfig, addr string, updates *IDUpdatesPack) error {
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

	stdin, err := session.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}

	if err := session.Start("/home/vgstats/syncids -ch sync"); err != nil {
		fmt.Fprintf(os.Stderr, "start:\n%s\n", err)
		fmt.Fprintf(os.Stderr, "session errors:\n%s\n", e.String())

		return fmt.Errorf("start: %w", err)
	}

	if err := json.NewEncoder(httputil.NewChunkedWriter(stdin)).Encode(updates); err != nil {
		fmt.Fprintf(os.Stderr, "session errors:\n%s\n", e.String())

		return fmt.Errorf("encode: %w", err)
	}

	if err := session.Wait(); err != nil {
		fmt.Fprintf(os.Stderr, "wait:\n%s\n", err)
		fmt.Fprintf(os.Stderr, "session errors:\n%s\n", e.String())

		return fmt.Errorf("wait: %w", err)
	}

	fmt.Fprintf(os.Stderr, "<<<<<\n%s\n>>>>>\n", e.String())

	return nil
}

func prepareUpdates(db *pgxpool.Pool, schema string, lastUpdate time.Time) (*IDUpdatesPack, error) {
	ctx := context.Background()

	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}

	defer tx.Rollback(ctx)

	sqlGetIDs := `SELECT * FROM %s WHERE update_time >= $1`
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
		updatesPack = &IDUpdatesPack{Version: IDUpdatesPackVersion, UpdateTime: lastUpdate}
		brigadeID   string
		realmID     string
		partnerID   string
		reason      string
		createdAt   time.Time
		deletedAt   pgtype.Timestamp
		purgedAt    pgtype.Timestamp
		updateTime  time.Time
	)

	_, err = pgx.ForEachRow(
		rows,
		[]any{
			&brigadeID,
			&realmID,
			&partnerID,
			&reason,
			&createdAt,
			&deletedAt,
			&purgedAt,
			&updateTime,
		},
		func() error {
			updatesPack.Updates = append(
				updatesPack.Updates,
				IDUpdate{
					BrigadeID:  brigadeID,
					RealmID:    realmID,
					PartnerID:  partnerID,
					Reason:     reason,
					CreatedAt:  createdAt,
					DeletedAt:  deletedAt.Time,
					PurgedAt:   purgedAt.Time,
					UpdateTime: updateTime,
				},
			)

			return nil
		},
	)
	if err != nil {
		return nil, fmt.Errorf("for each row: %w", err)
	}

	return updatesPack, nil
}

func fetchLastUpdate(sshConfig *ssh.ClientConfig, addr string) (time.Time, error) {
	client, err := ssh.Dial("tcp", fmt.Sprintf("%s:22", addr), sshConfig)
	if err != nil {
		return time.Time{}, fmt.Errorf("dial: %w", err)
	}

	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return time.Time{}, fmt.Errorf("new session: %w", err)
	}

	defer session.Close()

	var b, e bytes.Buffer

	session.Stdout = &b
	session.Stderr = &e

	cmd := "/home/vgstats/syncids -ch lastupdate"
	if err := session.Run(cmd); err != nil {
		fmt.Fprintf(os.Stderr, "session errors:\n%s\n", e.String())

		return time.Time{}, fmt.Errorf("run: %w", err)
	}

	resp, err := io.ReadAll(httputil.NewChunkedReader(&b))
	if err != nil {
		fmt.Fprintf(os.Stderr, "readed data:\n%s\n", err)
		fmt.Fprintf(os.Stderr, "session errors:\n%s\n", e.String())

		return time.Time{}, fmt.Errorf("read data: %w", err)
	}

	var result UpdateTimeResult
	if err := json.Unmarshal(resp, &result); err != nil {
		return time.Time{}, fmt.Errorf("unmarshal: %w", err)
	}

	fmt.Fprintf(os.Stderr, "<<<<<\n%s\n>>>>>\n", e.String())

	return result.UpdateTime, nil
}

func createSSHConfig(path string) (*ssh.ClientConfig, error) {
	// var hostKey ssh.PublicKey

	key, err := os.ReadFile(filepath.Join(path, sshkeyFilename))
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

func parseArgs() (string, error) {
	addr := flag.String("a", "", "address of stats server")
	flag.Parse()

	return *addr, nil
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
