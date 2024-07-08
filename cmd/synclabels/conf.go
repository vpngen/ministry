package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	defaultDatabaseURL = "postgresql:///vgstats"
)

type initConfig struct {
	dbURL   string
	chunked bool
	name    string
}

type AppConfig struct {
	DB      *pgxpool.Pool
	Chunked bool
	Name    string
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

	return &AppConfig{
		DB:      db,
		Chunked: c.chunked,
		Name:    c.name,
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

	flag.Parse()

	c.chunked = *chunked

	name := "unknown"
	if len(flag.Args()) > 0 {
		name = flag.Arg(0)
	}

	c.name = name

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
