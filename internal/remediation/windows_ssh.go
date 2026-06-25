package remediation

import (
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

type WindowsSSHConfig struct {
	Address string
	User    string
	KeyPEM  []byte
	Timeout time.Duration
}

func RunWindowsSSHBootstrap(
	cfg WindowsSSHConfig,
	script string,
) error {

	signer, err := ssh.ParsePrivateKey(cfg.KeyPEM)
	if err != nil {
		return fmt.Errorf("parse private key: %w", err)
	}

	clientConfig := &ssh.ClientConfig{
		User: cfg.User,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         cfg.Timeout,
	}

	client, err := ssh.Dial("tcp", cfg.Address, clientConfig)
	if err != nil {
		return fmt.Errorf("ssh dial: %w", err)
	}
	defer client.Close()

	tmpFile := `C:\Windows\Temp\kubevirt-observability-bootstrap.ps1`

	// Step 1: upload/write script
	{
		session, err := client.NewSession()
		if err != nil {
			return fmt.Errorf("new ssh session: %w", err)
		}
		scriptB64 := base64.StdEncoding.EncodeToString([]byte(script))

		writeCmd := fmt.Sprintf(
			`[System.Text.Encoding]::UTF8.GetString([System.Convert]::FromBase64String("%s")) | Set-Content -Path '%s' -Encoding UTF8`,
			scriptB64,
			tmpFile,
		)

		cmd := fmt.Sprintf(
			`powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -Command %q`,
			writeCmd,
		)

		out, err := session.CombinedOutput(cmd)

		session.Close()

		if err != nil {
			return fmt.Errorf(
				"write bootstrap script failed: %w output=%s",
				err,
				string(out),
			)
		}
	}

	// Step 2: execute script file
	{
		session, err := client.NewSession()
		if err != nil {
			return fmt.Errorf("new ssh session: %w", err)
		}
		defer session.Close()

		cmd := fmt.Sprintf(
			`powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "%s"`,
			tmpFile,
		)

		out, err := session.CombinedOutput(cmd)

		if err != nil {
			return fmt.Errorf(
				"windows ssh remediation failed: %w output=%s",
				err,
				string(out),
			)
		}
	}

	return nil
}

func GetWindowsBootstrapState(
	cfg WindowsSSHConfig,
) (string, error) {

	script := `
if (Test-Path 'C:\ProgramData\KubeVirtObservability\bootstrap.done') {
	Write-Output success
} elseif (Test-Path 'C:\ProgramData\KubeVirtObservability\bootstrap.failed') {
	Write-Output failed
} else {
	Write-Output unknown
}
`

	out, err := RunWindowsSSHCommand(cfg, script)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(out), nil
}

func RunWindowsSSHCommand(
	cfg WindowsSSHConfig,
	script string,
) (string, error) {

	signer, err := ssh.ParsePrivateKey(cfg.KeyPEM)
	if err != nil {
		return "", fmt.Errorf("parse private key: %w", err)
	}

	clientConfig := &ssh.ClientConfig{
		User: cfg.User,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         cfg.Timeout,
	}

	client, err := ssh.Dial("tcp", cfg.Address, clientConfig)
	if err != nil {
		return "", fmt.Errorf("ssh dial: %w", err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("new ssh session: %w", err)
	}
	defer session.Close()

	cmd := fmt.Sprintf(
		`powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -Command "%s"`,
		escapePowerShellForCommand(script),
	)

	out, err := session.CombinedOutput(cmd)
	if err != nil {
		return string(out), fmt.Errorf(
			"windows ssh command failed: %w output=%s",
			err,
			string(out),
		)
	}

	return strings.TrimSpace(string(out)), nil
}

func escapePowerShellForCommand(script string) string {
	var out strings.Builder

	for _, r := range script {
		switch r {
		case '"':
			out.WriteString("`\"")
		case '\n', '\r':
			out.WriteString("; ")
		default:
			out.WriteRune(r)
		}
	}

	return strings.TrimSpace(out.String())
}
