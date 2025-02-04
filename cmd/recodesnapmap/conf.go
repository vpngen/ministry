package main

import (
	"crypto/rsa"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	snapsCrypto "github.com/vpngen/keydesk-snap/core/crypto"
	"golang.org/x/crypto/ssh"
)

const (
	defaultAuthorityKeyFilename = "authority_priv.pem"
	defaultMasterKeyFilename    = "shuffler_priv.json"
)

type cfg struct {
	authPrivKeyfile string
	relmsKeysfile   string

	snapfile string

	home string
}

type opts struct {
	authPrivKey *rsa.PrivateKey

	authFP string

	snapfile string
}

var (
	ErrEmptySnapfile = errors.New("empty snapshot file")
	ErrNoRealmKeyFP  = errors.New("no realm key fingerprint")

	ErrNoAuthorityPrivKey = errors.New("no authority private key")
	ErrNoRealmsKeysFile   = errors.New("no realms keys file")
)

func conf() (*opts, error) {
	c := &cfg{}

	if err := readEnv(c); err != nil {
		return nil, fmt.Errorf("can't read env: %w", err)
	}

	if err := parseArgs(c); err != nil {
		return nil, fmt.Errorf("can't parse args: %w", err)
	}

	if err := ckconfdefs(c); err != nil {
		return nil, fmt.Errorf("config check failed: %w", err)
	}

	authPrivKey, err := snapsCrypto.ReadPrivateSSHKeyFile(c.authPrivKeyfile)
	if err != nil {
		return nil, fmt.Errorf("can't read authority private key: %w", err)
	}

	apub, err := ssh.NewPublicKey(authPrivKey.Public())
	if err != nil {
		return nil, fmt.Errorf("new ssh public key: %w", err)
	}

	authFP := ssh.FingerprintSHA256(apub)

	return &opts{
		authPrivKey: authPrivKey,
		authFP:      authFP,

		snapfile: c.snapfile,
	}, nil
}

func ckconfdefs(c *cfg) error {
	if c.snapfile == "" {
		return ErrEmptySnapfile
	}

	if c.authPrivKeyfile == "" {
		return ErrNoAuthorityPrivKey
	}

	if c.relmsKeysfile == "" {
		return ErrNoRealmsKeysFile
	}

	return nil
}

func parseArgs(c *cfg) error {
	akey := flag.String("akey", filepath.Join(c.home, ".secret", defaultAuthorityKeyFilename), "authority private RSA key.")
	rkeys := flag.String("rkeys", filepath.Join("/etc/vgdept", snapsCrypto.DefaultRealmsKeysFileName), "realms keys file.")
	insnap := flag.String("in", "", "input snapshot file. Default: none")

	flag.Parse()

	c.authPrivKeyfile = *akey
	c.relmsKeysfile = *rkeys
	c.snapfile = *insnap

	return nil
}

func readEnv(c *cfg) error {
	akey := os.Getenv("AUTHORITY_PRIV_KEY_FILE")
	if akey != "" {
		c.authPrivKeyfile = akey
	}

	rkeys := os.Getenv("REALMS_KEYS_FILE")
	if rkeys != "" {
		c.relmsKeysfile = rkeys
	}

	var err error

	home := os.Getenv("HOME")
	if home == "" {
		home, err = filepath.Abs(".")
		if err != nil {
			return fmt.Errorf("can't get home dir: %w", err)
		}
	}

	c.home = home

	return nil
}
