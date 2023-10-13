package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	dcmgmt "github.com/vpngen/dc-mgmt"
	"github.com/vpngen/keydesk/keydesk"
	"github.com/vpngen/ministry"
	"github.com/vpngen/ministry/internal/kdlib"
	"github.com/vpngen/wordsgens/namesgenerator"
	"github.com/vpngen/wordsgens/seedgenerator"
	"golang.org/x/crypto/ssh"
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

const maxCollisionAttemts = 1000

const (
	sqlCheckToken = `
	SELECT
		t.token
	FROM
		%s AS t
		JOIN %s AS p ON p.partner_id=t.partner_id
	WHERE
		p.is_active=true
		AND t.token=$1
	LIMIT 1
	`

	sqlCreateBrigadier = `
	INSERT INTO
		%s
		(
			brigade_id,
			brigadier,
			person,
			realm_id,
			partner_id
		)
			SELECT 
				$1, $2, $3,
				pr.realm_id, pr.partner_id 
			FROM 
				%s AS t 					-- partners_tokens
				JOIN  %s AS p ON p.partner_id=t.partner_id      -- partners
				JOIN %s AS pr ON pr.partner_id=p.partner_id     -- partners_realms
				JOIN %s AS r ON r.realm_id=pr.realm_id          -- realms
			WHERE
				p.is_active=true
				AND r.is_active=true
				AND r.free_slots>0
				AND t.token=$4
			ORDER BY RANDOM() LIMIT 1
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

	sqlCreateBrigadeEvent = `
	INSERT INTO
		%s
		(
			brigade_id,
			event_name,
			event_info,
			event_time
		)
	VALUES
		(
			$1,
			'create_brigade',
			$2,
			NOW() AT TIME ZONE 'UTC'
		)
	`

	sqlFetchBrigadier = `
	SELECT
		brigadiers.brigadier,
		brigadiers.person,
		realms.control_ip,
		realms.realm_id
	FROM
		%s,%s
	WHERE
			brigadiers.brigade_id=$1
		AND
			realms.realm_id=brigadiers.realm_id
	`
)

var (
	errEmptyAccessToken = errors.New("token not specified")
	errAccessDenied     = errors.New("access denied")
)

const brigadeCreationType = "ssh_api"

const defaultSeedExtra = "даблять"

var seedExtra string // extra data for seed

var LogTag = setLogTag()

const defaultLogTag = "createbrigade"

func setLogTag() string {
	executable, err := os.Executable()
	if err != nil {
		return defaultLogTag
	}

	return filepath.Base(executable)
}

func init() {
	seedExtra = os.Getenv("SEED_EXTRA")
	if seedExtra == "" {
		seedExtra = defaultSeedExtra
	}
}

func main() {
	var w io.WriteCloser

	// fmt.Printf("token: %s\n", token)

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

	sshconf, err := kdlib.CreateSSHConfig(sshKeyFilename, sshkeyRemoteUsername, kdlib.SSHDefaultTimeOut)
	if err != nil {
		fatal(w, jout, "%s: Can't create ssh configs: %s\n", LogTag, err)
	}

	db, err := createDBPool(dbURL)
	if err != nil {
		fatal(w, jout, "%s: Can't create db pool: %s\n", LogTag, err)
	}

	id, mnemo, err := createBrigade(db, schema, token, brigadeCreationType)
	if err != nil {
		fatal(w, jout, "%s: Can't create brigade: %s\n", LogTag, err)
	}

	// wgconfx = wgconf + keydesk IP
	wgconfx, person, fullname, personName, desc64, url64, err := requestBrigade(db, schema, sshconf, id)
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
					Configs: wgconfx.Answer.Configs,
				},
				KeydeskIPv6: wgconfx.KeydeskIPv6,
				FreeSlots:   wgconfx.FreeSlots,
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
		_, err = fmt.Fprintln(w, personName)
		if err != nil {
			log.Fatalf("%s: Can't print person: %s\n", LogTag, err)
		}
		_, err = fmt.Fprintln(w, desc64)
		if err != nil {
			log.Fatalf("%s: Can't print desc: %s\n", LogTag, err)
		}
		_, err = fmt.Fprintln(w, url64)
		if err != nil {
			log.Fatalf("%s: Can't print url: %s\n", LogTag, err)
		}
		_, err = fmt.Fprintln(w, mnemo)
		if err != nil {
			log.Fatalf("%s: Can't print memo: %s\n", LogTag, err)
		}

		_, err = fmt.Fprintln(w, wgconfx.FreeSlots)
		if err != nil {
			log.Fatalf("%s: Can't print free slots: %s\n", LogTag, err)
		}

		_, err = fmt.Fprintln(w, wgconfx.KeydeskIPv6)
		if err != nil {
			log.Fatalf("%s: Can't print keydesk ipv6: %s\n", LogTag, err)
		}

		_, err = fmt.Fprintln(w, *wgconfx.Answer.Configs.WireguardConfig.FileName)
		if err != nil {
			log.Fatalf("%s: Can't print wgconf filename: %s\n", LogTag, err)
		}

		_, err = fmt.Fprintln(w, *wgconfx.Answer.Configs.WireguardConfig.FileContent)
		if err != nil {
			log.Fatalf("%s: Can't print wgconf content: %s\n", LogTag, err)
		}
	}
}

const fatalString = `{
	"code" : 500,
	"desc" : "Internal Server Error",
	"status" : "error",
	"message" : "%s"
}`

func fatal(w io.Writer, jout bool, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)

	switch jout {
	case true:
		fmt.Fprintf(w, fatalString, msg)
	default:
		fmt.Fprint(w, msg)
	}

	log.Fatal(msg)
}

func requestBrigade(db *pgxpool.Pool, schema string, sshconf *ssh.ClientConfig, id uuid.UUID) (*dcmgmt.Answer, *namesgenerator.Person, string, string, string, string, error) {
	ctx := context.Background()

	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, nil, "", "", "", "", fmt.Errorf("begin: %w", err)
	}

	var (
		fullname   string
		person     namesgenerator.Person
		control_ip netip.Addr
		realm_id   uuid.UUID
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
		&realm_id,
	)
	if err != nil {
		tx.Rollback(ctx)

		return nil, nil, "", "", "", "", fmt.Errorf("brigade query: %w", err)
	}

	if err = tx.Commit(ctx); err != nil {
		return nil, nil, "", "", "", "", fmt.Errorf("commit: %w", err)
	}

	fullname64 := base64.StdEncoding.WithPadding(base64.StdPadding).EncodeToString([]byte(fullname))
	person64 := base64.StdEncoding.WithPadding(base64.StdPadding).EncodeToString([]byte(person.Name))
	desc64 := base64.StdEncoding.WithPadding(base64.StdPadding).EncodeToString([]byte(person.Desc))
	url64 := base64.StdEncoding.WithPadding(base64.StdPadding).EncodeToString([]byte(person.URL))

	cmd := fmt.Sprintf("addbrigade -ch -j -id %s -name %s -person %s -desc %s -url %s",
		base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(id[:]),
		fullname64,
		person64,
		desc64,
		url64,
	)

	fmt.Fprintf(os.Stderr, "%s: %s#%s:22 -> %s\n", LogTag, sshkeyRemoteUsername, control_ip, cmd)

	client, err := ssh.Dial("tcp", fmt.Sprintf("%s:22", control_ip), sshconf)
	if err != nil {
		return nil, nil, "", "", "", "", fmt.Errorf("ssh dial: %w", err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return nil, nil, "", "", "", "", fmt.Errorf("ssh session: %w", err)
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

	if err := session.Run(cmd); err != nil {
		return nil, nil, "", "", "", "", fmt.Errorf("ssh run: %w", err)
	}

	r := bufio.NewReader(httputil.NewChunkedReader(&b))

	payload, err := io.ReadAll(r)
	if err != nil {
		return nil, nil, "", "", "", "", fmt.Errorf("chunk read: %w", err)
	}

	wgconf := &dcmgmt.Answer{}
	if err := json.Unmarshal(payload, &wgconf); err != nil {
		return nil, nil, "", "", "", "", fmt.Errorf("json unmarshal: %w", err)
	}

	/*
		_, err = r.ReadString('\n')
			if err != nil {
				return nil, nil, "", "", "", "", fmt.Errorf("num read: %w", err)
			}

			freeSlots, err := strconv.Atoi(strings.TrimSpace(num))
			if err != nil {
				return nil, "", "", "", "", fmt.Errorf("num parse: %w", err)
			}

			tx2, err := db.Begin(ctx)
			if err != nil {
				return nil, "", "", "", "", fmt.Errorf("begin: %w", err)
			}

			sqlUpdateRealmFreeSlots := "UPDATE %s SET free_slots = $1 WHERE realm_id = $2"
			if _, err := tx2.Exec(ctx, fmt.Sprintf(sqlUpdateRealmFreeSlots, pgx.Identifier{schema, "realms"}.Sanitize()), freeSlots, realm_id); err != nil {
				tx2.Rollback(ctx)

				return nil, "", "", "", "", fmt.Errorf("update realm free slots: %w", err)
			}

			if err := tx2.Commit(ctx); err != nil {
				return nil, "", "", "", "", fmt.Errorf("commit: %w", err)
			}
	*/

	return wgconf, &person, fullname, person.Name, desc64, url64, nil
}

func createBrigade(db *pgxpool.Pool, schema string, token []byte, creationInfo string) (uuid.UUID, string, error) {
	id := uuid.New()

	mnemo, seed, salt, err := seedgenerator.Seed(seedgenerator.ENT64, seedExtra)
	if err != nil {
		return id, "", fmt.Errorf("gen seed6: %w", err)
	}

	ctx := context.Background()

	tx, err := db.Begin(ctx)
	if err != nil {
		return id, "", fmt.Errorf("begin: %w", err)
	}

	t := make([]byte, 32) // just to check if token exists.
	err = tx.QueryRow(ctx, fmt.Sprintf(sqlCheckToken,
		pgx.Identifier{schema, "partners_tokens"}.Sanitize(),
		pgx.Identifier{schema, "partners"}.Sanitize()),
		token,
	).Scan(&t)
	if err != nil {
		tx.Rollback(ctx)

		return id, "", errAccessDenied
	}

	bcnt := 0
	for {
		fullname, person, err := namesgenerator.PhysicsAwardeeShort()
		if err != nil {
			tx.Rollback(ctx)

			return id, "", fmt.Errorf("physics generate: %s", err)
		}

		ntx, err := tx.Begin(ctx)
		if err != nil {
			tx.Rollback(ctx)

			return id, "", fmt.Errorf("begin: %w", err)
		}

		_, err = ntx.Exec(ctx,
			fmt.Sprintf(
				sqlCreateBrigadier,
				pgx.Identifier{schema, "brigadiers"}.Sanitize(),
				pgx.Identifier{schema, "partners_tokens"}.Sanitize(),
				pgx.Identifier{schema, "partners"}.Sanitize(),
				pgx.Identifier{schema, "partners_realms"}.Sanitize(),
				pgx.Identifier{schema, "realms"}.Sanitize(),
			),
			id,
			fullname,
			person,
			token,
		)

		if err == nil {
			err := ntx.Commit(ctx)
			if err != nil {
				tx.Rollback(ctx)

				return id, "", fmt.Errorf("nested commit: %w", err)
			}

			break
		}

		ntx.Rollback(ctx)

		if bcnt++; bcnt > maxCollisionAttemts {
			tx.Rollback(ctx)

			return id, "", fmt.Errorf("create brigadier: %w: attempts: %d", err, bcnt)
		}

		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			switch pgErr.ConstraintName {
			case "brigadiers_brigade_id_key":
				id = uuid.New()
				continue
			case "brigadiers_brigadier_key":
				continue
			default:
				tx.Rollback(ctx)

				return id, "", fmt.Errorf("create brigadier: %w", pgErr)
			}
		}
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

	_, err = tx.Exec(ctx,
		fmt.Sprintf(sqlCreateBrigadeEvent, (pgx.Identifier{schema, "brigades_actions"}.Sanitize())),
		id,
		creationInfo,
	)
	if err != nil {
		tx.Rollback(ctx)

		return id, "", fmt.Errorf("create brigade event: %w", err)
	}

	err = tx.Commit(ctx)
	if err != nil {
		return id, "", fmt.Errorf("commit: %w", err)
	}

	return id, mnemo, nil
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

func readConfigs() (string, string, string, error) {
	dbURL := os.Getenv("DB_URL")
	if dbURL == "" {
		dbURL = defaultDatabaseURL
	}

	brigadesSchema := os.Getenv("BRIGADES_ADMIN_SCHEMA")
	if brigadesSchema == "" {
		brigadesSchema = defaultBrigadesSchema
	}

	sshKeyFilename, err := kdlib.LookupForSSHKeyfile(os.Getenv("SSH_KEY"), sshkeyDefaultPath)
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
