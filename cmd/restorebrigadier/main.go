package main

import (
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
	"unicode/utf8"

	"github.com/gofrs/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vpngen/keydesk/keydesk"
	"github.com/vpngen/ministry"
	"github.com/vpngen/ministry/internal/kdlib"
	realmadmin "github.com/vpngen/realm-admin"
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

var LogTag = setLogTag()

const defaultLogTag = "restorebrigadier"

func setLogTag() string {
	executable, err := os.Executable()
	if err != nil {
		return defaultLogTag
	}

	return filepath.Base(executable)
}

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

	salt, err := saltByName(db, schema, name)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			fmt.Fprintln(w, "NOTFOUND")

			return
		}

		fatal(w, jout, "%s: Can't find a brigadier: %s\n", LogTag, err)
	}

	key := seedgenerator.SeedFromSaltMnemonics(mnemo, seedExtra, salt)

	id, del, delTime, delReason, addr, err := checkKey(db, schema, name, key)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			fmt.Fprintln(w, "NOTFOUND")

			return
		}

		fatal(w, jout, "%s: Can't find key: %s\n", LogTag, err)
	}

	var wgconfx *realmadmin.Answer
	switch del {
	case true:
		fmt.Fprintf(os.Stderr, "%s: DELETED: %s: %s\n", LogTag, delReason, delTime.Format(time.RFC3339))

		if dryRun {
			return
		}

		wgconfx, err = blessBrigade(db, schema, sshconf, id)
		if err != nil {
			fatal(w, jout, "%s: Can't bless brigade: %s", LogTag, err)
		}
	default:
		fmt.Fprintf(os.Stderr, "%s: ALIVE", LogTag)

		if dryRun {
			return
		}

		wgconfx, err = replaceBrigadier(db, schema, sshconf, addr, id)
		if err != nil {
			fatal(w, jout, "%s: Can't replace brigade: %s", LogTag, err)
		}
	}

	// TODO: repeated code. Refactor it.
	switch jout {
	case true:
		answ := ministry.Answer{
			Answer: realmadmin.Answer{
				Answer: keydesk.Answer{
					Code:    http.StatusCreated,
					Desc:    http.StatusText(http.StatusCreated),
					Status:  keydesk.AnswerStatusSuccess,
					Configs: wgconfx.Answer.Configs,
				},
				KeydeskIPv6: wgconfx.KeydeskIPv6,
				FreeSlots:   wgconfx.FreeSlots,
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

func replaceBrigadier(db *pgxpool.Pool, schema string, sshconf *ssh.ClientConfig, controlIP netip.Addr, id uuid.UUID) (*realmadmin.Answer, error) {
	cmd := fmt.Sprintf("replacebrigadier -ch -j -id %s",
		base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(id[:]),
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
		return nil, fmt.Errorf("ssh run: %w", err)
	}

	payload, err := io.ReadAll(httputil.NewChunkedReader(&b))
	if err != nil {
		return nil, fmt.Errorf("chunk read: %w", err)
	}

	wgconf := &realmadmin.Answer{}
	if err := json.Unmarshal(payload, &wgconf); err != nil {
		return nil, fmt.Errorf("json unmarshal: %w", err)
	}

	return wgconf, nil
}

func blessBrigade(db *pgxpool.Pool, schema string, sshconf *ssh.ClientConfig, id uuid.UUID) (*realmadmin.Answer, error) {
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

	cmd := fmt.Sprintf("addbrigade -ch -j -id %s -name %s -person %s -desc %s -url %s",
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
		return nil, fmt.Errorf("ssh run: %w", err)
	}

	payload, err := io.ReadAll(httputil.NewChunkedReader(&b))
	if err != nil {
		return nil, fmt.Errorf("chunk read: %w", err)
	}

	wgconf := &realmadmin.Answer{}
	if err := json.Unmarshal(payload, &wgconf); err != nil {
		return nil, fmt.Errorf("json unmarshal: %w", err)
	}

	tx, err = db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin2: %w", err)
	}

	defer tx.Rollback(ctx)

	sqlRemoveDeleted := `DELETE FROM %s WHERE brigade_id=$1`
	_, err = tx.Exec(ctx,
		fmt.Sprintf(sqlRemoveDeleted,
			(pgx.Identifier{schema, "deleted_brigadiers"}.Sanitize()),
		),
		id,
	)
	if err != nil {
		return nil, fmt.Errorf("delete deleted: %w", err)
	}

	sqlRestoreEventAdd := `
INSERT INTO 
	%s 
	(brigade_id, event_name, event_time, event_info) 
VALUES 
	($1, 'restore_brigade', NOW(), 'ssh_api')
`
	_, err = tx.Exec(ctx,
		fmt.Sprintf(sqlRestoreEventAdd,
			(pgx.Identifier{schema, "brigades_actions"}.Sanitize()),
		),
		id,
	)
	if err != nil {
		return nil, fmt.Errorf("insert restore: %w", err)
	}

	err = tx.Commit(ctx)
	if err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return wgconf, nil
}

func checkKey(db *pgxpool.Pool, schema, name string, key []byte) (uuid.UUID, bool, time.Time, string, netip.Addr, error) {
	ctx := context.Background()
	emptyTime := time.Time{}
	emptyAddr := netip.Addr{}

	tx, err := db.Begin(ctx)
	if err != nil {
		return uuid.Nil, false, emptyTime, "", emptyAddr, fmt.Errorf("begin: %w", err)
	}

	var (
		id             pgtype.UUID
		getName        string
		controlIP      netip.Addr
		deletedTime    pgtype.Timestamp
		deletionReason pgtype.Text
	)

	sqlSaltByName := `SELECT
		brigadiers.brigade_id,
		brigadiers.brigadier,
		realms.control_ip,
		deleted_brigadiers.deleted_at,
		deleted_brigadiers.reason
	FROM %s, %s, %s
	LEFT JOIN %s ON 
		brigadiers.brigade_id=deleted_brigadiers.brigade_id
	WHERE
		brigadiers.brigadier=$1
	AND
		brigadier_keys.key=$2
	AND
		brigadiers.brigade_id=brigadier_keys.brigade_id
	AND
		brigadiers.realm_id=realms.realm_id
	`

	err = tx.QueryRow(ctx,
		fmt.Sprintf(sqlSaltByName,
			(pgx.Identifier{schema, "brigadier_keys"}.Sanitize()),
			(pgx.Identifier{schema, "realms"}.Sanitize()),
			(pgx.Identifier{schema, "brigadiers"}.Sanitize()),
			(pgx.Identifier{schema, "deleted_brigadiers"}.Sanitize()),
		),
		name,
		key,
	).Scan(
		&id,
		&getName,
		&controlIP,
		&deletedTime,
		&deletionReason,
	)
	if err != nil {
		tx.Rollback(ctx)

		return uuid.Nil, false, emptyTime, "", emptyAddr, fmt.Errorf("key query: %w", err)
	}

	err = tx.Commit(ctx)
	if err != nil {
		return uuid.Nil, false, emptyTime, "", emptyAddr, fmt.Errorf("commit: %w", err)
	}

	return uuid.UUID(id.Bytes), deletedTime.Valid, deletedTime.Time, deletionReason.String, controlIP, nil
}

func saltByName(db *pgxpool.Pool, schema, name string) ([]byte, error) {
	ctx := context.Background()

	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}

	var salt []byte

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
