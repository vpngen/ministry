package main

import (
	"encoding/json"
	"fmt"
	"os"

	dcmgmt "github.com/vpngen/dc-mgmt"
)

func readSnapfile(path string) (*dcmgmt.AggrSnaps, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}

	defer f.Close()

	snapdata := &dcmgmt.AggrSnaps{}

	if err := json.NewDecoder(f).Decode(snapdata); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	return snapdata, nil
}
