package remediation

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"net"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/masterzen/winrm"
)

type WindowsWinRMConfig struct {
	Address  string
	Username string
	Password string
	Timeout  time.Duration
}

func ProbeWinRM(address string, timeout time.Duration) bool {
	conn, err := net.DialTimeout("tcp", address, timeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func RunWindowsCommand(cfg WindowsWinRMConfig, command string) (string, string, error) {
	endpoint := winrm.NewEndpoint(
		hostFromAddress(cfg.Address),
		portFromAddress(cfg.Address),
		false,
		false,
		nil,
		nil,
		nil,
		cfg.Timeout,
	)
	params := winrm.DefaultParameters
	params.Timeout = cfg.Timeout.String()
	params.TransportDecorator = func() winrm.Transporter { return &winrm.ClientNTLM{} }

	client, err := winrm.NewClientWithParameters(endpoint, cfg.Username, cfg.Password, params)
	if err != nil {
		return "", "", fmt.Errorf("failed to create winrm client: %w", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode, err := client.Run(command, &stdout, &stderr)
	if err != nil {
		return stdout.String(), stderr.String(), fmt.Errorf("remote command failed: %w", err)
	}
	if exitCode != 0 {
		return stdout.String(), stderr.String(), fmt.Errorf("remote command returned exit code %d", exitCode)
	}

	return stdout.String(), stderr.String(), nil
}

func RunWindowsBootstrap(cfg WindowsWinRMConfig, script string) error {
	cmd := "powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -EncodedCommand " + powershellEncodedCommand(script)

	stdout, stderr, err := RunWindowsCommand(cfg, cmd)
	if err != nil {
		return fmt.Errorf("remote powershell failed: %w stdout=%s stderr=%s", err, stdout, stderr)
	}

	return nil
}

func hostFromAddress(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}

func portFromAddress(addr string) int {
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return 5985
	}
	switch portStr {
	case "5986":
		return 5986
	default:
		return 5985
	}
}

func powershellEncodedCommand(script string) string {
	normalized := strings.ReplaceAll(script, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\n", "\r\n")

	u16 := utf16.Encode([]rune(normalized))
	buf := make([]byte, len(u16)*2)
	for i, v := range u16 {
		buf[i*2] = byte(v)
		buf[i*2+1] = byte(v >> 8)
	}

	return base64.StdEncoding.EncodeToString(buf)
}
