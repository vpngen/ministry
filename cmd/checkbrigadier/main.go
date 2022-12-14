package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vpngen/wordsgens/seedgenerator"
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

const defaultSeedExtra = "даблять"

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

	name, mnemo, err := parseArgs()
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

	ok, err := checkKey(db, schema, name, key)
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
}

func checkKey(db *pgxpool.Pool, schema, name string, key []byte) (bool, error) {
	ctx := context.Background()

	tx, err := db.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin: %w", err)
	}

	var (
		getname string
	)

	sqlSaltByName := `SELECT
		brigadiers.brigadier
	FROM %s, %s
	WHERE
		brigadiers.brigadier=$1
	AND
		brigadier_keys.key=$2
	AND
		brigadiers.brigade_id=brigadier_keys.brigade_id
	`

	err = tx.QueryRow(ctx, fmt.Sprintf(sqlSaltByName, (pgx.Identifier{schema, "brigadier_keys"}.Sanitize()), (pgx.Identifier{schema, "brigadiers"}.Sanitize())), name, key).Scan(&getname)
	if err != nil {
		tx.Rollback(ctx)

		return false, fmt.Errorf("key query: %w", err)
	}

	err = tx.Commit(ctx)
	if err != nil {
		return false, fmt.Errorf("commit: %w", err)
	}

	return true, nil
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

func parseArgs() (string, string, error) {
	flag.Parse()

	if flag.NArg() != 2 {
		return "", "", fmt.Errorf("args: %w", errInvalidArgs)
	}

	return strings.Join(strings.Fields(flag.Arg(0)), " "), strings.Join(strings.Fields(flag.Arg(1)), " "), nil
}
