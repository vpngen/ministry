package main

import (
	"encoding/base64"
	"flag"
	"fmt"
	"os"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	defaultDatabaseURL = "postgresql:///vgdept"
)

var ErrEmptyAccessToken = fmt.Errorf("empty access token")

type initConfig struct {
	dbURL   string
	chunked bool
	name    string
	token   []byte
}

type AppConfig struct {
	DB        *pgxpool.Pool
	Chunked   bool
	Name      string
	PartnerID uuid.UUID
}

func config() (*AppConfig, error) {
	c, err := newInitConfig()
	if err != nil {
		return nil, fmt.Errorf("new init config: %w", err)
	}

	db, err := createDBPool(c.dbURL)
	if err != nil {
		return nil, fmt.Errorf("create db pool: %w", err)
	}

	partnerID, err := checkToken(db, c.token)
	if err != nil {
		return nil, fmt.Errorf("check token: %w", err)
	}

	return &AppConfig{
		DB:        db,
		Chunked:   c.chunked,
		Name:      c.name,
		PartnerID: partnerID,
	}, nil
}

func newInitConfig() (*initConfig, error) {
	c := &initConfig{}

	if err := c.readEnv(); err != nil {
		return nil, fmt.Errorf("read env: %w", err)
	}

	if err := c.parseArgs(); err != nil {
		return nil, fmt.Errorf("parse args: %w", err)
	}

	return c, nil
}

// parseArgs - parses command line arguments.
func (c *initConfig) parseArgs() error {
	chunked := flag.Bool("ch", false, "chunked output")
	tokenArg := flag.String("token", "", "token")
	name := flag.String("name", "", "name")

	flag.Parse()

	c.chunked = *chunked
	c.name = *name

	if *tokenArg == "" {
		return fmt.Errorf("token: %w", ErrEmptyAccessToken)
	}

	token := make([]byte, base64.URLEncoding.WithPadding(base64.NoPadding).DecodedLen(len(*tokenArg)))
	if _, err := base64.URLEncoding.WithPadding(base64.NoPadding).Decode(token, []byte(*tokenArg)); err != nil {
		return fmt.Errorf("decode token: %w", err)
	}

	c.token = token

	return nil
}

// readEnv - reads environment variables.
func (c *initConfig) readEnv() error {
	dbURL := os.Getenv("DB_URL")
	if dbURL == "" {
		dbURL = defaultDatabaseURL
	}

	c.dbURL = dbURL

	return nil
}
