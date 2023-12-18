package main

import "os"

const defaultSeedExtra = "даблять"

var seedExtra string // extra data for seed

func init() {
	seedExtra = os.Getenv("SEED_EXTRA")
	if seedExtra == "" {
		seedExtra = defaultSeedExtra
	}
}
