package main

import "os"

// Must match the SEED_EXTRA/defaultSeedExtra used by cmd/checkbrigadier,
// cmd/restorebrigadier, cmd/createbrigade and cmd/ckvip - it's a shared
// pepper for seed generation, not a per-command secret.
const defaultSeedExtra = "даблять"

var seedExtra string

func init() {
	seedExtra = os.Getenv("SEED_EXTRA")
	if seedExtra == "" {
		seedExtra = defaultSeedExtra
	}
}
