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
	"net/http"
	"net/http/httputil"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	dcmgmt "github.com/vpngen/dc-mgmt"
	"github.com/vpngen/keydesk/keydesk"
	"github.com/vpngen/ministry"
	"github.com/vpngen/ministry/internal/core"
	"github.com/vpngen/ministry/internal/pgsql"
	sshVng "github.com/vpngen/ministry/internal/ssh"
)

const (
	sshkeyRemoteUsername = "_valera_"
	sshkeyDefaultPath    = "/etc/vgdept"
	sshTimeOut           = time.Duration(80 * time.Second)
)

const (
	defaultDatabaseURL = "postgresql:///vgdept"
)

var errInvalidArgs = errors.New("invalid args")

func main() {
	var w io.WriteCloser

	name, mnemo, dryRun, chunked, jout, err := parseArgs()
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

	sshKeyFilename, dbURL, err := readConfigs()
	if err != nil {
		fatal(w, jout, "Can't read configs: %s\n", err)
	}

	sshconf, err := sshVng.CreateSSHConfig(sshKeyFilename, sshkeyRemoteUsername, sshVng.SSHDefaultTimeOut)
	if err != nil {
		fatal(w, jout, "%s: Can't create ssh configs: %s\n", LogTag, err)
	}

	db, err := pgsql.CreateDBPool(dbURL)
	if err != nil {
		fatal(w, jout, "%s: Can't create db pool: %s\n", LogTag, err)
	}

	ctx := context.Background()

	brigadeID, partnerID, person, del, delTime, delReason, lastRestore, err := core.CheckBrigadier(
		ctx, db, seedExtra, name, mnemo,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			fmt.Fprintln(w, "NOTFOUND")

			return
		}

		fatal(w, jout, "%s: Can't find key: %s\n", LogTag, err)
	}

	var vpnconf *dcmgmt.Answer
	switch del {
	case true:
		fmt.Fprintf(os.Stderr, "%s: DELETED: %s: %s\n", LogTag, delReason, delTime.Format(time.RFC3339))

		if dryRun {
			return
		}

		if !lastRestore.IsZero() && lastRestore.After(time.Now().UTC().AddDate(0, -1, 0)) {
			tooEarly(w, jout, "Last Restore: %s\n", lastRestore.UTC().Format(time.RFC3339))

			return
		}

		vpnconf, err = core.ComposeBrigade(ctx, db, sshconf, LogTag, partnerID, brigadeID, name, person)
		if err != nil {
			fatal(w, jout, "%s: Can't bless brigade: %s\n", LogTag, err)
		}
	default:
		fmt.Fprintf(os.Stderr, "%s: ALIVE\n", LogTag)

		if dryRun {
			return
		}

		vpnconf, err = core.ReplaceBrigadier(ctx, db, LogTag, sshconf, brigadeID)
		if err != nil {
			fatal(w, jout, "%s: Can't replace brigade: %s\n", LogTag, err)
		}
	}

	// TODO: repeated code. Refactor it.
	switch jout {
	case true:
		answ := ministry.Answer{
			Answer: dcmgmt.Answer{
				Answer: keydesk.Answer{
					Code:    http.StatusCreated,
					Desc:    http.StatusText(http.StatusCreated),
					Status:  keydesk.AnswerStatusSuccess,
					Configs: vpnconf.Answer.Configs,
				},
				KeydeskIPv6: vpnconf.KeydeskIPv6,
				FreeSlots:   vpnconf.FreeSlots,
			},
		}

		payload, err := json.Marshal(answ)
		if err != nil {
			fatal(w, jout, "%s: Can't marshal answer: %s\n", LogTag, err)
		}

		if _, err := w.Write(payload); err != nil {
			fatal(w, jout, "%s: Can't write answer: %s\n", LogTag, err)
		}
	default:
		_, err := fmt.Fprintln(w, "WGCONFIG")
		if err != nil {
			log.Fatalf("%s: Can't print wgconfig: %s\n", LogTag, err)
		}

		_, err = fmt.Fprintln(w, vpnconf.FreeSlots)
		if err != nil {
			log.Fatalf("%s: Can't print free slots: %s\n", LogTag, err)
		}

		_, err = fmt.Fprintln(w, vpnconf.KeydeskIPv6)
		if err != nil {
			log.Fatalf("%s: Can't print keydesk ipv6: %s\n", LogTag, err)
		}

		_, err = fmt.Fprintln(w, *vpnconf.Answer.Configs.WireguardConfig.FileName)
		if err != nil {
			log.Fatalf("%s: Can't print wgconf filename: %s\n", LogTag, err)
		}

		_, err = fmt.Fprintln(w, *vpnconf.Answer.Configs.WireguardConfig.FileContent)
		if err != nil {
			log.Fatalf("%s: Can't print wgconf content: %s\n", LogTag, err)
		}
	}
}

func readConfigs() (string, string, error) {
	dbURL := os.Getenv("DB_URL")
	if dbURL == "" {
		dbURL = defaultDatabaseURL
	}

	sshKeyFilename, err := sshVng.LookupForSSHKeyfile(os.Getenv("SSH_KEY"), sshkeyDefaultPath)
	if err != nil {
		return "", "", fmt.Errorf("lookup for ssh key: %w", err)
	}

	return sshKeyFilename, dbURL, nil
}

func parseArgs() (string, string, bool, bool, bool, error) {
	dryRun := flag.Bool("n", false, "Dry run")
	chunked := flag.Bool("ch", false, "chunked output")
	jsonOut := flag.Bool("j", false, "json output")

	flag.Parse()

	if flag.NArg() != 2 {
		return "", "", false, false, false, fmt.Errorf("args: %w", errInvalidArgs)
	}

	// implicit base64 decoding

	name := flag.Arg(0)
	if buf, err := base64.StdEncoding.DecodeString(name); err == nil && utf8.Valid(buf) {
		name = string(buf)
	}

	words := flag.Arg(1)
	if buf, err := base64.StdEncoding.DecodeString(words); err == nil && utf8.Valid(buf) {
		words = string(buf)
	}

	return sanitizeNames(name), sanitizeNames(words), *dryRun, *chunked, *jsonOut, nil
}

func sanitizeNames(name string) string {
	return strings.Join(strings.Fields(strings.Replace(name, ",", " ", -1)), " ")
}
