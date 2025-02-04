package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"

	dcmgmt "github.com/vpngen/dc-mgmt"
)

var LogTag = setLogTag()

const defaultLogTag = "recodesnaps"

func setLogTag() string {
	executable, err := os.Executable()
	if err != nil {
		return defaultLogTag
	}

	return filepath.Base(executable)
}

func main() {
	opts, err := conf()
	if err != nil {
		log.Fatalf("%s: Can't read configs: %s\n", LogTag, err)
	}

	if err := recode(opts); err != nil {
		log.Fatalf("%s: Can't recode: %s\n", LogTag, err)
	}
}

var (
	ErrInvalidSnapshotData = errors.New("invalid snapshot data")
	ErrSnapshotErrors      = errors.New("snapshot errors")
	ErrKeysMismatch        = errors.New("keys mismatch")

	ErrNoAuthorityKeyFP = errors.New("no authority key fingerprint")
)

func recode(o *opts) error {
	snapdata, err := readSnapfile(o.snapfile)
	if err != nil {
		return fmt.Errorf("read snapfile: %w", err)
	}

	if err := checkIn(snapdata, o); err != nil {
		return fmt.Errorf("check in: %w", err)
	}

	plan, err := createMapping(snapdata, o)
	if err != nil {
		return fmt.Errorf("recode data: %w", err)
	}

	if err := json.NewEncoder(os.Stdout).Encode(plan); err != nil {
		return fmt.Errorf("encode plan: %w", err)
	}

	return nil
}

var ErrResevrTooSmall = errors.New("reservation too small")

func checkIn(data *dcmgmt.AggrSnaps, o *opts) error {
	if data.UpdateTime.IsZero() {
		return fmt.Errorf("%w: update time is zero", ErrInvalidSnapshotData)
	}

	if data.GlobalSnapAt.IsZero() {
		return fmt.Errorf("%w: global snap at is zero", ErrInvalidSnapshotData)
	}

	if data.Tag == "" {
		return fmt.Errorf("%w: empty tag", ErrInvalidSnapshotData)
	}

	if data.DatacenterID == "" {
		return fmt.Errorf("%w: empty datacenter id", ErrInvalidSnapshotData)
	}

	if data.RealmKeyFP != "" {
		return fmt.Errorf("%w: non-empty realm key fingerprint", ErrInvalidSnapshotData)
	}

	if data.AuthorityKeyFP == "" {
		return fmt.Errorf("%w: empty authority key fingerprint", ErrInvalidSnapshotData)
	}

	if data.EncryptedPreSharedSecret == "" {
		return fmt.Errorf("%w: empty encrypted pre-shared secret", ErrInvalidSnapshotData)
	}

	if len(data.Snaps) == 0 {
		return fmt.Errorf("%w: empty snaps", ErrInvalidSnapshotData)
	}

	if data.ErrorsCount > 0 {
		fmt.Fprintf(os.Stderr, "%s: errors count: %d\n", LogTag, data.ErrorsCount)
	}

	if o.authFP != data.AuthorityKeyFP {
		return fmt.Errorf("authority: %w: %s != %s", ErrKeysMismatch,
			o.authFP, data.AuthorityKeyFP)
	}

	return nil
}
