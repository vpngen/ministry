package main

import (
	"crypto/rsa"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	snapsCrypto "github.com/vpngen/keydesk-snap/core/crypto"
	"github.com/vpngen/vpngine/naclkey"
	"golang.org/x/crypto/ssh"
)

const (
	defaultAuthorityKeyFilename = "authority_priv.pem"
	defaultMasterKeyFilename    = "shuffler_priv.json"
)

type cfg struct {
	authPrivKeyfile   string
	masterPrivKeyfile string
	relmsKeysfile     string

	targetRealmFP string

	snapfile     string
	reservConfig string
	planfile     string

	home string

	force  bool
	mirror bool
}

type opts struct {
	authPrivKey   *rsa.PrivateKey
	masterPrivKey naclkey.NaclBoxKeypair
	targetPubKey  *rsa.PublicKey

	authFP        string
	targetRealmFP string

	snapfile   string
	reservfile string
	planfile   string

	force  bool
	mirror bool
}

var (
	ErrEmptySnapfile   = errors.New("empty snapshot file")
	ErrEmptyReservConf = errors.New("empty reservation config")
	ErrEmptyPlanfile   = errors.New("empty plan file")
	ErrNoRealmKeyFP    = errors.New("no realm key fingerprint")

	ErrNoAuthorityPrivKey = errors.New("no authority private key")
	ErrNoMasterPrivKey    = errors.New("no master private key")
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

	masterPrivKey, err := naclkey.ReadKeypairFile(c.masterPrivKeyfile)
	if err != nil {
		return nil, fmt.Errorf("can't read master private key: %w", err)
	}

	targetPubKey, err := snapsCrypto.FindPubKeyInFile(c.relmsKeysfile, c.targetRealmFP)
	if err != nil {
		return nil, fmt.Errorf("can't find realm public key: %w", err)
	}

	return &opts{
		authPrivKey:   authPrivKey,
		targetPubKey:  targetPubKey,
		masterPrivKey: masterPrivKey,

		authFP:        authFP,
		targetRealmFP: c.targetRealmFP,

		snapfile:   c.snapfile,
		reservfile: c.reservConfig,
		planfile:   c.planfile,

		force:  c.force,
		mirror: c.mirror,
	}, nil
}

func ckconfdefs(c *cfg) error {
	if c.snapfile == "" {
		return ErrEmptySnapfile
	}

	if c.reservConfig == "" {
		return ErrEmptyReservConf
	}

	if c.planfile == "" {
		return ErrEmptyPlanfile
	}

	if c.targetRealmFP == "" {
		return ErrNoRealmKeyFP
	}

	if c.authPrivKeyfile == "" {
		return ErrNoAuthorityPrivKey
	}

	if c.masterPrivKeyfile == "" {
		return ErrNoMasterPrivKey
	}

	if c.relmsKeysfile == "" {
		return ErrNoRealmsKeysFile
	}

	return nil
}

func parseArgs(c *cfg) error {
	akey := flag.String("akey", filepath.Join(c.home, ".secret", defaultAuthorityKeyFilename), "authority private RSA key.")
	mkey := flag.String("mkey", filepath.Join(c.home, ".secret", defaultMasterKeyFilename), "master private nacl key.")
	rkeys := flag.String("rkeys", filepath.Join("/etc/vgdept", snapsCrypto.DefaultRealmsKeysFileName), "realms keys file.")
	insnap := flag.String("in", "", "input snapshot file. Default: none")
	reservConf := flag.String("c", "", "reservation config file. Default: none")
	outplan := flag.String("out", "", "output plan file. Default: none")
	fp := flag.String("tfp", "", "target realm key fingerprint")
	force := flag.Bool("force", false, "force ignore snapshot errors")
	mirror := flag.Bool("mirror", false, "mirror mode. Only same IP addresses on both sides are allowed")

	flag.Parse()

	if fp == nil || *fp == "" {
		return ErrNoRealmKeyFP
	}

	c.authPrivKeyfile = *akey
	c.masterPrivKeyfile = *mkey
	c.relmsKeysfile = *rkeys
	c.snapfile = *insnap
	c.reservConfig = *reservConf
	c.planfile = *outplan
	c.targetRealmFP = *fp
	c.force = *force
	c.mirror = *mirror

	return nil
}

func readEnv(c *cfg) error {
	akey := os.Getenv("AUTHORITY_PRIV_KEY_FILE")
	if akey != "" {
		c.authPrivKeyfile = akey
	}

	mkey := os.Getenv("MASTER_PRIV_KEY_FILE")
	if mkey != "" {
		c.masterPrivKeyfile = mkey
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
