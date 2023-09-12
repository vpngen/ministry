package kdlib

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"time"

	"golang.org/x/crypto/ssh"
)

const (
	SSHKeyED25519Filename = "id_ed25519"
	SSHDefaultFilename    = SSHKeyED25519Filename
	SSHDefaultTimeOut     = time.Duration(15 * time.Second)
)

var ErrNoSSHKeyFile = errors.New("no ssh key file")

// CreateSSHConfig - creates ssh client config.
func CreateSSHConfig(filename, username string, timeout time.Duration) (*ssh.ClientConfig, error) {
	// var hostKey ssh.PublicKey

	key, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("read private key: %w", err)
	}

	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}

	config := &ssh.ClientConfig{
		User: username,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		// HostKeyCallback: ssh.FixedHostKey(hostKey),
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         timeout,
	}

	return config, nil
}

func LookupForSSHKeyfile(keyFilename, path string) (string, error) {
	if keyFilename != "" {
		return keyFilename, nil
	}

	sysUser, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("user: %w", err)
	}

	sshKeyDirs := []string{filepath.Join(sysUser.HomeDir, ".ssh"), path}
	for _, dir := range sshKeyDirs {
		if fstat, err := os.Stat(dir); err != nil || !fstat.IsDir() {
			continue
		}

		keyFilename := filepath.Join(dir, SSHKeyED25519Filename)
		if _, err := os.Stat(keyFilename); err == nil {
			return keyFilename, nil
		}
	}

	return "", ErrNoSSHKeyFile
}
