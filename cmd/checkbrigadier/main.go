package main

import (
	"context"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http/httputil"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
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
	var w io.WriteCloser

	name, mnemo, chunked, jout, chkDel, bless, err := parseArgs()
	if err != nil {
		log.Fatalf("Can't parse args: %s\n", err)
	}

	switch chunked {
	case true:
		w = httputil.NewChunkedWriter(os.Stdout)
		defer w.Close()
	default:
		w = os.Stdout
	}

	sshKeyFilename, dbURL, _, obfsUUID, err := readConfigs()
	if err != nil {
		fatal(w, jout, "Can't read configs: %s\n", err)
	}

	db, err := pgsql.CreateDBPool(dbURL)
	if err != nil {
		fatal(w, jout, "Can't create db pool: %s\n", err)
	}

	ctx := context.Background()

	brigadeID, person, del, delTime, delReason, _, _, _, err := core.CheckBrigadier(ctx, db, seedExtra, name, mnemo, false)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			notFound(w, jout, "Invalid name or words for brigadier %q\n", name)

			return
		}

		fatal(w, jout, "Can't find key: %s\n", err)
	}

	// -bless is a manual admin-only operation (recreate a deleted brigade in
	// place); it's never used by the read-only -j lookup path, so -j always
	// returns as soon as the brigade is identified, before any bless attempt.
	if jout {
		var outUUID uuid.UUID
		for i := range 16 {
			outUUID[i] = brigadeID[i] ^ obfsUUID[i]
		}

		success(w, jout, outUUID, del)

		return
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

	sshconf, err := sshVng.CreateSSHConfig(sshKeyFilename, sshkeyRemoteUsername, sshVng.SSHDefaultTimeOut)
	if err != nil {
		fatal(w, jout, "%s: Can't create ssh configs: %s\n", LogTag, err)
	}

	vpnconf, err := core.ComposeBrigade(ctx, db, sshconf, LogTag, false, false, brigadeID, name, person)
	if err != nil {
		fatal(w, jout, "Can't bless brigade: %s", err)
	}

	log.Println("WGCONFIG:")

	log.Println(vpnconf.KeydeskIPv6)

	log.Println(*vpnconf.Answer.Configs.WireguardConfig.FileName)

	log.Println(*vpnconf.Answer.Configs.WireguardConfig.FileContent)
}

func readConfigs() (string, string, string, uuid.UUID, error) {
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
		return "", "", "", uuid.Nil, fmt.Errorf("lookup for ssh key: %w", err)
	}

	obfsUUID, err := uuid.Parse(os.Getenv("OBFS_UUID"))
	if err != nil {
		return "", "", "", uuid.Nil, fmt.Errorf("parse obfs uuid: %w", err)
	}

	obfsUUID[6] &= 0x0F
	obfsUUID[8] &= 0x3F

	return sshKeyFilename, dbURL, brigadesSchema, obfsUUID, nil
}

func parseArgs() (string, string, bool, bool, bool, bool, error) {
	chunked := flag.Bool("ch", false, "chunked output")
	jsonOut := flag.Bool("j", false, "json output")
	checkDel := flag.Bool("chkdel", false, "Check deletion status")
	recreate := flag.Bool("bless", false, "Recreate brigade")

	flag.Parse()

	if flag.NArg() != 2 {
		return "", "", false, false, false, false, fmt.Errorf("args: %w", errInvalidArgs)
	}

	// implicit base64 decoding, matching restorebrigadier's arg convention

	name := flag.Arg(0)
	if buf, err := base64.StdEncoding.DecodeString(name); err == nil && utf8.Valid(buf) {
		name = string(buf)
	}

	mnemo := flag.Arg(1)
	if buf, err := base64.StdEncoding.DecodeString(mnemo); err == nil && utf8.Valid(buf) {
		mnemo = string(buf)
	}

	name = strings.Join(strings.Fields(strings.Replace(name, ",", " ", -1)), " ")
	mnemo = strings.Join(strings.Fields(strings.Replace(mnemo, ",", " ", -1)), " ")

	return name, mnemo, *chunked, *jsonOut, *checkDel, *recreate, nil
}
