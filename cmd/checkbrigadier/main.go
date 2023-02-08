package main

import (
	"bytes"
	"context"
	"encoding/base32"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http/httputil"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofrs/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vpngen/wordsgens/namesgenerator"
	"github.com/vpngen/wordsgens/seedgenerator"
	"golang.org/x/crypto/ssh"
)

const (
	dbnameFilename     = "dbname"
	schemaNameFilename = "schema"
	etcDefaultPath     = "/etc/vgdept"
)

const (
	maxPostgresqlNameLen = 63
	postgresqlSocket     = "/var/run/postgresql"
)

const (
	sshkeyFilename       = "id_ecdsa"
	sshkeyRemoteUsername = "_valera_"
	sshTimeOut           = time.Duration(5 * time.Second)
)

const nullTime = -62135596800

const defaultSeedExtra = "даблять"

const sqlFetchBrigadier = `
SELECT
	brigadiers.brigadier,
	brigadiers.person,
	realms.control_ip
FROM
	%s,%s
WHERE
		brigadiers.brigade_id=$1
	AND
		realms.realm_id=brigadiers.realm_id
`

var errInvalidArgs = errors.New("invalid args")

var seedExtra string // extra data for seed

func init() {
	seedExtra = os.Getenv("SEED_EXTRA")
	if seedExtra == "" {
		seedExtra = defaultSeedExtra
	}
}

func main() {
	confDir := os.Getenv("CONFDIR")
	if confDir == "" {
		confDir = etcDefaultPath
	}

	name, mnemo, chkDel, bless, err := parseArgs()
	if err != nil {
		log.Fatalf("Can't parse args: %s\n", err)
	}

	dbname, schema, err := readConfigs(confDir)
	if err != nil {
		log.Fatalf("Can't read configs: %s\n", err)
	}

	db, err := createDBPool(dbname)
	if err != nil {
		log.Fatalf("Can't create db pool: %s\n", err)
	}

	salt, err := saltByName(db, schema, name)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			log.Fatalf("Brigadier %q is not found\n", name)
		}

		log.Fatalf("Can't find a brigadier: %s\n", err)
	}

	key := seedgenerator.SeedFromSaltMnemonics(mnemo, seedExtra, salt)

	ok, id, del, delTime, delReason, err := checkKey(db, schema, name, key)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			log.Fatalf("Invalid mnemonics for brigadier %q\n", name)
		}

		log.Fatalf("Can't find key: %s\n", err)
	}

	if !ok {
		log.Fatalln("FAILD")
	}

	log.Println("SUCCESS")

	if chkDel {
		switch del {
		case true:
			log.Printf("DELETED: %s: %s\n", delReason, delTime.Format(time.RFC3339))
		default:
			log.Println("ALIVE")
		}
	}

	if !bless || !del {
		return
	}

	sshconf, err := createSSHConfig(confDir)
	if err != nil {
		log.Fatalf("Can't create ssh configs: %s\n", err)
	}

	wgconf, err := blessBrigade(db, schema, sshconf, id)
	if err != nil {
		log.Fatalln("Can't bless brigade")
	}

	log.Println("WGCONFIG:")
	fmt.Println(string(wgconf))
}

func blessBrigade(db *pgxpool.Pool, schema string, sshconf *ssh.ClientConfig, id uuid.UUID) ([]byte, error) {
	ctx := context.Background()

	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}

	var (
		fullname  string
		person    namesgenerator.Person
		controlIP netip.Addr
	)

	err = tx.QueryRow(ctx,
		fmt.Sprintf(sqlFetchBrigadier,
			(pgx.Identifier{schema, "brigadiers"}.Sanitize()),
			(pgx.Identifier{schema, "realms"}.Sanitize()),
		),
		id,
	).Scan(
		&fullname,
		&person,
		&controlIP,
	)
	if err != nil {
		tx.Rollback(ctx)

		return nil, fmt.Errorf("brigade query: %w", err)
	}

	err = tx.Commit(ctx)
	if err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	fullname64 := base64.StdEncoding.WithPadding(base64.StdPadding).EncodeToString([]byte(fullname))
	person64 := base64.StdEncoding.WithPadding(base64.StdPadding).EncodeToString([]byte(person.Name))
	desc64 := base64.StdEncoding.WithPadding(base64.StdPadding).EncodeToString([]byte(person.Desc))
	url64 := base64.StdEncoding.WithPadding(base64.StdPadding).EncodeToString([]byte(person.URL))

	cmd := fmt.Sprintf("addbrigade -ch -id %s -name %s -person %s -desc %s -url %s",
		base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(id[:]),
		fullname64,
		person64,
		desc64,
		url64,
	)

	fmt.Fprintf(os.Stderr, "%s#%s:22 -> %s\n", sshkeyRemoteUsername, controlIP, cmd)

	client, err := ssh.Dial("tcp", fmt.Sprintf("%s:22", controlIP), sshconf)
	if err != nil {
		return nil, fmt.Errorf("ssh dial: %w", err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("ssh session: %w", err)
	}
	defer session.Close()

	var b, e bytes.Buffer

	session.Stdout = &b
	session.Stderr = &e

	if err := session.Run(cmd); err != nil {
		fmt.Fprintf(os.Stderr, "session errors:\n%s\n", e.String())

		return nil, fmt.Errorf("ssh run: %w", err)
	}

	wgconfx, err := io.ReadAll(httputil.NewChunkedReader(&b))
	if err != nil {
		fmt.Fprintf(os.Stderr, "readed data:\n%s\n", wgconfx)

		return nil, fmt.Errorf("chunk read: %w", err)
	}

	sqlRemoveDeleted := `DELETE FROM %s WHERE brigade_id=$1`
	_, err = tx.Exec(ctx,
		fmt.Sprintf(sqlRemoveDeleted,
			(pgx.Identifier{schema, "deleted_brigadiers"}.Sanitize()),
		),
		id,
	)
	if err != nil {
		tx.Rollback(ctx)

		return nil, fmt.Errorf("delete deleted: %w", err)
	}

	return wgconfx, nil
}

func checkKey(db *pgxpool.Pool, schema, name string, key []byte) (bool, uuid.UUID, bool, time.Time, string, error) {
	ctx := context.Background()
	emptyTime := time.Time{}

	tx, err := db.Begin(ctx)
	if err != nil {
		return false, uuid.Nil, false, emptyTime, "", fmt.Errorf("begin: %w", err)
	}

	var (
		id      pgtype.UUID
		getname string
		dt      pgtype.Timestamp
		dr      string
	)

	sqlSaltByName := `SELECT
		brigadiers.brigade_id,
		brigadiers.brigadier,
		deleted_at,
		reason
	FROM %s, %s
	LEFT JOIN %s ON 
		brigadiers.brigade_id=deleted_brigadiers.brigade_id
	WHERE
		brigadiers.brigadier=$1
	AND
		brigadier_keys.key=$2
	AND
		brigadiers.brigade_id=brigadier_keys.brigade_id
	`

	err = tx.QueryRow(ctx,
		fmt.Sprintf(sqlSaltByName,
			(pgx.Identifier{schema, "brigadier_keys"}.Sanitize()),
			(pgx.Identifier{schema, "brigadiers"}.Sanitize()),
			(pgx.Identifier{schema, "deleted_brigadiers"}.Sanitize()),
		),
		name,
		key,
	).Scan(
		&id,
		&getname,
		&dt,
		&dr,
	)
	if err != nil {
		tx.Rollback(ctx)

		return false, uuid.Nil, false, emptyTime, "", fmt.Errorf("key query: %w", err)
	}

	err = tx.Commit(ctx)
	if err != nil {
		return false, uuid.Nil, false, emptyTime, "", fmt.Errorf("commit: %w", err)
	}

	return true, uuid.UUID(id.Bytes), dt.Time.Unix() != nullTime, dt.Time, dr, nil
}

func saltByName(db *pgxpool.Pool, schema, name string) ([]byte, error) {
	ctx := context.Background()

	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}

	var (
		salt []byte
	)

	sqlSaltByName := `SELECT
		brigadier_salts.salt
	FROM %s, %s
	WHERE
		brigadiers.brigadier=$1
	AND
		brigadiers.brigade_id=brigadier_salts.brigade_id
	`

	err = tx.QueryRow(ctx, fmt.Sprintf(sqlSaltByName, (pgx.Identifier{schema, "brigadier_salts"}.Sanitize()), (pgx.Identifier{schema, "brigadiers"}.Sanitize())), name).Scan(&salt)
	if err != nil {
		tx.Rollback(ctx)

		return nil, fmt.Errorf("salt query: %w", err)
	}

	err = tx.Commit(ctx)
	if err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return salt, nil
}

func createDBPool(dbname string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(fmt.Sprintf("host=%s dbname=%s", postgresqlSocket, dbname))
	if err != nil {
		return nil, fmt.Errorf("conn string: %w", err)
	}

	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	return pool, nil
}

func readConfigs(path string) (string, string, error) {
	f, err := os.Open(filepath.Join(path, dbnameFilename))
	if err != nil {
		return "", "", fmt.Errorf("can't open: %s: %w", dbnameFilename, err)
	}

	dbname, err := io.ReadAll(io.LimitReader(f, maxPostgresqlNameLen))
	if err != nil {
		return "", "", fmt.Errorf("can't read: %s: %w", dbnameFilename, err)
	}

	f, err = os.Open(filepath.Join(path, schemaNameFilename))
	if err != nil {
		return "", "", fmt.Errorf("can't open: %s: %w", schemaNameFilename, err)
	}

	schema, err := io.ReadAll(io.LimitReader(f, maxPostgresqlNameLen))
	if err != nil {
		return "", "", fmt.Errorf("can't read: %s: %w", schemaNameFilename, err)
	}

	return string(dbname), string(schema), nil
}

func parseArgs() (string, string, bool, bool, error) {
	checkDel := flag.Bool("chkdel", false, "Check deletion status")
	recreate := flag.Bool("bless", false, "Recreate brigade")

	flag.Parse()

	if flag.NArg() != 2 {
		return "", "", false, false, fmt.Errorf("args: %w", errInvalidArgs)
	}

	return strings.Join(strings.Fields(flag.Arg(0)), " "), strings.Join(strings.Fields(flag.Arg(1)), " "), *checkDel, *recreate, nil
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
