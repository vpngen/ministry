package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"os"
	"time"

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
	maxPostgresqlNameLen  = 63
	defaultDatabaseURL    = "postgresql:///vgdept"
	defaultBrigadesSchema = "head"
)

func main() {
	var w io.WriteCloser

	chunked, jout, token, err := parseArgs()
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

	sshKeyFilename, dbURL, schema, err := readConfigs()
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

	partnerID, ok, err := checkToken(ctx, db, schema, token)
	if err != nil || !ok {
		if err != nil {
			fatal(w, jout, "%s: Can't check token: %s\n", LogTag, err)
		}

		fatal(w, jout, "%s: Access denied\n", LogTag)
	}

	brigadeID, mnemo, fullname, person, err := createBrigade(ctx, db, schema, partnerID, brigadeCreationType)
	if err != nil {
		fatal(w, jout, "%s: Can't create brigade: %s\n", LogTag, err)
	}

	vpnconf, err := core.ComposeBrigade(ctx, db, schema, sshconf, LogTag, partnerID, brigadeID, fullname, person)
	if err != nil {
		fatal(w, jout, "%s: Can't request brigade: %s\n", LogTag, err)
	}

	// TODO: Repeated code
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
			Mnemo:  mnemo,
			Name:   fullname,
			Person: *person,
		}

		payload, err := json.Marshal(answ)
		if err != nil {
			fatal(w, jout, "%s: Can't marshal answer: %s\n", LogTag, err)
		}

		if _, err := w.Write(payload); err != nil {
			fatal(w, jout, "%s: Can't write answer: %s\n", LogTag, err)
		}
	default:
		_, err = fmt.Fprintln(w, fullname)
		if err != nil {
			log.Fatalf("%s: Can't print fullname: %s\n", LogTag, err)
		}
		_, err = fmt.Fprintln(w, person.Name)
		if err != nil {
			log.Fatalf("%s: Can't print person: %s\n", LogTag, err)
		}
		_, err = fmt.Fprintln(w,
			base64.StdEncoding.WithPadding(base64.StdPadding).EncodeToString([]byte(person.Desc)),
		)
		if err != nil {
			log.Fatalf("%s: Can't print desc: %s\n", LogTag, err)
		}
		_, err = fmt.Fprintln(w,
			base64.StdEncoding.WithPadding(base64.StdPadding).EncodeToString([]byte(person.URL)),
		)
		if err != nil {
			log.Fatalf("%s: Can't print url: %s\n", LogTag, err)
		}
		_, err = fmt.Fprintln(w, mnemo)
		if err != nil {
			log.Fatalf("%s: Can't print memo: %s\n", LogTag, err)
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

func parseArgs() (bool, bool, []byte, error) {
	chunked := flag.Bool("ch", false, "chunked output")
	jout := flag.Bool("j", false, "json output")

	flag.Parse()

	a := flag.Args()
	if len(a) < 1 {
		return false, false, nil, fmt.Errorf("access token: %w", errEmptyAccessToken)
	}

	token := make([]byte, base64.URLEncoding.WithPadding(base64.NoPadding).DecodedLen(len(a[0])))
	_, err := base64.URLEncoding.WithPadding(base64.NoPadding).Decode(token, []byte(a[0]))
	if err != nil {
		return false, false, nil, fmt.Errorf("access token: %w", err)
	}

	return *chunked, *jout, token, nil
}
