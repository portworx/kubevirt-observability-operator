package remediation

import (
	"bytes"
	"fmt"
	"net"
	"time"

	"golang.org/x/crypto/ssh"
)

type LinuxSSHConfig struct {
	Address    string
	Username   string
	PrivateKey []byte
	Timeout    time.Duration
}

func ProbeSSH(address string, timeout time.Duration) bool {
	conn, err := net.DialTimeout("tcp", address, timeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func RunLinuxBootstrap(cfg LinuxSSHConfig, script string) error {
	signer, err := ssh.ParsePrivateKey(cfg.PrivateKey)
	if err != nil {
		return fmt.Errorf("failed to parse private key: %w", err)
	}

	clientConfig := &ssh.ClientConfig{
		User: cfg.Username,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         cfg.Timeout,
	}

	client, err := ssh.Dial("tcp", cfg.Address, clientConfig)
	if err != nil {
		return fmt.Errorf("failed to ssh dial %s: %w", cfg.Address, err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("failed to create ssh session: %w", err)
	}
	defer session.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	cmd := fmt.Sprintf("bash -s <<'EOF'\n%s\nEOF", script)
	if err := session.Run(cmd); err != nil {
		return fmt.Errorf("remote bootstrap failed: %w stdout=%s stderr=%s", err, stdout.String(), stderr.String())
	}

	return nil
}
