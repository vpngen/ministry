package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type labelLine struct {
	firstVisit time.Time
	label      string
	labelID    uuid.UUID
}

func main() {
	cfg, err := config()
	if err != nil {
		log.Fatalf("Read configs: %s\n", err)
	}

	if err := readAndSync(cfg); err != nil {
		log.Fatalf("Read and sync: %s\n", err)
	}
}

func readAndSync(cfg *AppConfig) error {
	ctx := context.Background()

	tx, err := cfg.DB.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	defer tx.Rollback(ctx)

	fileScanner := bufio.NewScanner(os.Stdin)

	for fileScanner.Scan() {
		line := fileScanner.Text()
		line = strings.TrimSpace(line)

		if line == "" {
			continue
		}

		ll, err := parseLine(line)
		if err != nil {
			return fmt.Errorf("parse line: %w", err)
		}

		if err := syncLabel(ctx, tx, ll); err != nil {
			return fmt.Errorf("sync label: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}

	return nil
}

var (
	ErrZeroTime  = errors.New("zero time")
	ErrZeroUUID  = errors.New("zero uuid")
	ErrEmtyLabel = errors.New("empty label")
)

func parseLine(line string) (*labelLine, error) {
	var (
		fv    int64
		label string
		id    string
	)

	if _, err := fmt.Sscanf(line, "%d|%s|%s", &fv, &id, &label); err != nil {
		return nil, fmt.Errorf("scanf: %w", err)
	}

	fvTime := time.Unix(fv, 0)
	if fvTime.IsZero() {
		return nil, ErrZeroTime
	}

	lid, err := uuid.Parse(id)
	if err != nil {
		return nil, fmt.Errorf("parse uuid: %w", err)
	}

	if lid == uuid.Nil {
		return nil, ErrZeroUUID
	}

	if label == "" {
		return nil, ErrEmtyLabel
	}

	return &labelLine{
		firstVisit: fvTime,
		label:      label,
		labelID:    lid,
	}, nil
}

// syncLabel - syncs IDs to database.
func syncLabel(ctx context.Context, tx pgx.Tx, ll *labelLine) error {
	// assume that brigade creation make a full update.
	// this is only for the labels without a brigade creation.
	sqlInsertLabel := `
INSERT INTO 
		%s 
	(label, label_id, first_visit, update_time) 
VALUES 
	($1, $2, $3, NOW() AT TIME ZONE 'UTC')
ON CONFLICT (label_id) DO NOTHING
`

	if _, err := tx.Exec(ctx,
		fmt.Sprintf(
			sqlInsertLabel,
			(pgx.Identifier{"head", "start_labels"}).Sanitize(),
		),
		ll.label, ll.labelID, ll.firstVisit,
	); err != nil {
		return fmt.Errorf("insert brigade_id: %w", err)
	}

	return nil
}
