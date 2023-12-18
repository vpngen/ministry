package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vpngen/ministry/internal/core"
	"github.com/vpngen/ministry/internal/pgsql"
	sshVng "github.com/vpngen/ministry/internal/ssh"
)

const (
	maxPostgresqlNameLen  = 63
	defaultDatabaseURL    = "postgresql:///vgdept"
	defaultBrigadesSchema = "head"
)

const (
	sshkeyRemoteUsername = "_valera_"
	sshkeyDefaultPath    = "/etc/vgdept"
	sshTimeOut           = time.Duration(80 * time.Second)
)

var errInvalidArgs = errors.New("invalid args")

func main() {
	name, mnemo, chkDel, bless, err := parseArgs()
	if err != nil {
		log.Fatalf("Can't parse args: %s\n", err)
	}

	sshKeyFilename, dbURL, schema, err := readConfigs()
	if err != nil {
		log.Fatalf("Can't read configs: %s\n", err)
	}

	sshconf, err := sshVng.CreateSSHConfig(sshKeyFilename, sshkeyRemoteUsername, sshVng.SSHDefaultTimeOut)
	if err != nil {
		log.Fatalf("%s: Can't create ssh configs: %s\n", LogTag, err)
	}

	db, err := pgsql.CreateDBPool(dbURL)
	if err != nil {
		log.Fatalf("Can't create db pool: %s\n", err)
	}

	ctx := context.Background()

	brigadeID, partnerID, person, del, delTime, delReason, err := core.CheckBrigadier(ctx, db, schema, seedExtra, name, mnemo)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			log.Fatalf("Invalid mnemonics for brigadier %q\n", name)
		}

		log.Fatalf("Can't find key: %s\n", err)
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

	vpnconf, err := core.ComposeBrigade(ctx, db, schema, sshconf, LogTag, partnerID, brigadeID, name, person)
	if err != nil {
		log.Fatalf("Can't bless brigade: %s", err)
	}

	log.Println("WGCONFIG:")

	log.Println(vpnconf.KeydeskIPv6)

	log.Println(*vpnconf.Answer.Configs.WireguardConfig.FileName)

	log.Println(*vpnconf.Answer.Configs.WireguardConfig.FileContent)
}

func readConfigs() (string, string, string, error) {
	dbURL := os.Getenv("DB_URL")
	if dbURL == "" {
		dbURL = defaultDatabaseURL
	}

	brigadesSchema := os.Getenv("BRIGADES_ADMIN_SCHEMA")
	if brigadesSchema == "" {
		brigadesSchema = defaultBrigadesSchema
	}

	sshKeyFilename, err := sshVng.LookupForSSHKeyfile(os.Getenv("SSH_KEY"), sshkeyDefaultPath)
	if err != nil {
		return "", "", "", fmt.Errorf("lookup for ssh key: %w", err)
	}

	return sshKeyFilename, dbURL, brigadesSchema, nil
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
