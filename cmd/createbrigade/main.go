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
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vpngen/wordsgens/namesgenerator"
	"github.com/vpngen/wordsgens/seedgenerator"
	"golang.org/x/crypto/ssh"
)

const (
	dbnameFilename       = "dbname"
	schemaNameFilename   = "schema"
	sshkeyFilename       = "id_ecdsa"
	sshkeyRemoteUsername = "_valera_"
	etcDefaultPath       = "/etc/vgdept"
)

const (
	maxPostgresqlNameLen = 63
	postgresqlSocket     = "/var/run/postgresql"
)

const sshTimeOut = time.Duration(5 * time.Second)

const (
	sqlPickRealm = `
	SELECT realm_id, control_ip FROM %s LIMIT 1
	`
	sqlCreateBrigadier = `
	INSERT INTO
		%s
		(
			brigade_id,
			realm_id,
			brigadier,
			person
		)
	VALUES
		(
			$1, $2, $3, $4
		)
	`
	sqlCreateBrigadierSalt = `
	INSERT INTO
		%s
		(
			brigade_id,
			salt
		)
	VALUES
		(
			$1, $2
		)
	`

	sqlCreateBrigadierKey = `
	INSERT INTO
		%s
		(
			brigade_id,
			key
		)
	VALUES
		(
			$1, $2
		)
	`

	sqlFetchBrigadier = `
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
)

const seedPrefix = "даблять"

var errEmptyAccessToken = errors.New("token not specified")

func main() {
	var w io.WriteCloser

	confDir := os.Getenv("CONFDIR")
	if confDir == "" {
		confDir = etcDefaultPath
	}

	chunked, _, err := parseArgs()
	if err != nil {
		log.Fatalf("Can't parse args: %s\n", err)
	}

	dbname, schema, err := readConfigs(confDir)
	if err != nil {
		log.Fatalf("Can't read configs: %s\n", err)
	}

	sshconf, err := createSSHConfig(confDir)
	if err != nil {
		log.Fatalf("Can't create ssh configs: %s\n", err)
	}

	db, err := createDBPool(dbname)
	if err != nil {
		log.Fatalf("Can't create db pool: %s\n", err)
	}

	id, mnemo, err := createBrigade(db, schema)
	if err != nil {
		log.Fatalf("Can't create brigade: %s\n", err)
	}

	// wgconfx = wgconf + keydesk IP
	wgconfx, err := requestBrigade(db, schema, sshconf, id)
	if err != nil {
		log.Fatalf("Can't request brigade: %s\n", err)
	}

	switch chunked {
	case true:
		w = httputil.NewChunkedWriter(os.Stdout)
		defer w.Close()
	default:
		w = os.Stdout
	}

	_, err = fmt.Fprintln(w, mnemo)
	if err != nil {
		log.Fatalf("Can't print memo: %s\n", err)
	}

	_, err = w.Write(wgconfx)
	if err != nil {
		log.Fatalf("Can't print wgconfx: %s\n", err)
	}
}

func requestBrigade(db *pgxpool.Pool, schema string, sshconf *ssh.ClientConfig, id uuid.UUID) ([]byte, error) {
	ctx := context.Background()

	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}

	var (
		fullname   string
		person     namesgenerator.Person
		control_ip netip.Addr
	)

	err = tx.QueryRow(ctx,
		fmt.Sprintf(sqlFetchBrigadier,
			(pgx.Identifier{schema, "brigadiers"}.Sanitize()),
			(pgx.Identifier{schema, "realms"}.Sanitize()),
		),
		id.String(),
	).Scan(
		&fullname,
		&person,
		&control_ip,
	)
	if err != nil {
		tx.Rollback(ctx)

		return nil, fmt.Errorf("brigade query: %w", err)
	}

	err = tx.Commit(ctx)
	if err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	cmd := fmt.Sprintf("-ch -id %s -name %s -person %s -desc %s -url %s",
		base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(id[:]),
		base64.StdEncoding.WithPadding(base64.StdPadding).EncodeToString([]byte(fullname)),
		base64.StdEncoding.WithPadding(base64.StdPadding).EncodeToString([]byte(person.Name)),
		base64.StdEncoding.WithPadding(base64.StdPadding).EncodeToString([]byte(person.Desc)),
		base64.StdEncoding.WithPadding(base64.StdPadding).EncodeToString([]byte(person.URL)),
	)

	fmt.Fprintf(os.Stderr, "%s#%s:22 -> %s\n", sshkeyRemoteUsername, control_ip, cmd)

	client, err := ssh.Dial("tcp", fmt.Sprintf("%s:22", control_ip), sshconf)
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
		fmt.Fprintf(os.Stderr, "Session errors:\n%s\n", e.String())

		return nil, fmt.Errorf("ssh run: %w", err)
	}

	wgconfx, err := io.ReadAll(httputil.NewChunkedReader(&b))
	if err != nil {
                fmt.Fprintf(os.Stderr, "Data:\n%s\n", wgconfx)

		return nil, fmt.Errorf("chunk read: %w", err)
	}

	return wgconfx, nil
}

func createBrigade(db *pgxpool.Pool, schema string) (uuid.UUID, string, error) {
	id := uuid.New()

	fullname, person, err := namesgenerator.PhysicsAwardee()
	if err != nil {
		return id, "", fmt.Errorf("physics generate: %s", err)
	}

	mnemo, seed, salt, err := seedgenerator.Seed(seedgenerator.ENT128, seedPrefix)
	if err != nil {
		return id, "", fmt.Errorf("gen seed12: %w", err)
	}

	ctx := context.Background()

	tx, err := db.Begin(ctx)
	if err != nil {
		return id, "", fmt.Errorf("begin: %w", err)
	}

	var (
		realm_id   string
		control_ip netip.Addr
	)

	err = tx.QueryRow(ctx, fmt.Sprintf(sqlPickRealm, (pgx.Identifier{schema, "realms"}.Sanitize()))).Scan(&realm_id, &control_ip)
	if err != nil {
		tx.Rollback(ctx)

		return id, "", fmt.Errorf("pair query: %w", err)
	}

	_, err = tx.Exec(ctx,
		fmt.Sprintf(sqlCreateBrigadier, (pgx.Identifier{schema, "brigadiers"}.Sanitize())),
		id,
		realm_id,
		fullname,
		person,
	)
	if err != nil {
		tx.Rollback(ctx)

		return id, "", fmt.Errorf("create brigadier: %w", err)
	}

	_, err = tx.Exec(ctx,
		fmt.Sprintf(sqlCreateBrigadierSalt, (pgx.Identifier{schema, "brigadier_salts"}.Sanitize())),
		id,
		salt,
	)
	if err != nil {
		tx.Rollback(ctx)

		return id, "", fmt.Errorf("create brigadier salt: %w", err)
	}

	_, err = tx.Exec(ctx,
		fmt.Sprintf(sqlCreateBrigadierKey, (pgx.Identifier{schema, "brigadier_keys"}.Sanitize())),
		id,
		seed,
	)
	if err != nil {
		tx.Rollback(ctx)

		return id, "", fmt.Errorf("create brigadier key: %w", err)
	}

	err = tx.Commit(ctx)
	if err != nil {
		return id, "", fmt.Errorf("commit: %w", err)
	}

	return id, mnemo, nil
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

func parseArgs() (bool, []byte, error) {
	chunked := flag.Bool("ch", false, "chunked output")

	flag.Parse()

	a := flag.Args()
	if len(a) < 1 {
		return false, nil, fmt.Errorf("access token: %w", errEmptyAccessToken)
	}

	token, err := base64.StdEncoding.WithPadding(base64.NoPadding).DecodeString(a[0])
	if err != nil {
		return false, nil, fmt.Errorf("access token: %w", err)
	}

	return *chunked, token, nil
}
