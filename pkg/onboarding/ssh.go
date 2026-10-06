package onboarding

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/portworx/kubevirt-observability-operator/pkg/platform"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func monitoringPublicKey(
	ctx context.Context,
	clients *platform.Clients,
	cfg Config,
) (string, error) {
	secret, err := clients.Kube.CoreV1().
		Secrets(cfg.KeySecretNamespace).
		Get(ctx, cfg.KeySecretName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf(
			"read monitoring SSH secret %s/%s: %w",
			cfg.KeySecretNamespace,
			cfg.KeySecretName,
			err,
		)
	}

	key := strings.TrimSpace(string(secret.Data["id_rsa.pub"]))
	if key == "" {
		return "", fmt.Errorf(
			"monitoring SSH secret %s/%s has no id_rsa.pub",
			cfg.KeySecretNamespace,
			cfg.KeySecretName,
		)
	}

	return key, nil
}

func bootstrapGuest(
	ctx context.Context,
	cfg Config,
	c Candidate,
	publicKey string,
) error {
	switch c.OS {
	case "linux":
		if cfg.LinuxUser == "" || cfg.LinuxPassword == "" {
			return fmt.Errorf(
				"KVO_LINUX_USER/KVO_LINUX_PASSWORD not configured",
			)
		}

		return injectLinuxKey(
			ctx,
			cfg,
			c.IP,
			cfg.LinuxUser,
			cfg.LinuxPassword,
			publicKey,
		)

	case "windows":
		if cfg.WindowsUser == "" || cfg.WindowsPassword == "" {
			return fmt.Errorf(
				"KVO_WINDOWS_USER/KVO_WINDOWS_PASSWORD not configured",
			)
		}

		return injectWindowsKey(
			ctx,
			cfg,
			c.IP,
			cfg.WindowsUser,
			cfg.WindowsPassword,
			publicKey,
		)

	default:
		return fmt.Errorf("unsupported OS %q", c.OS)
	}
}

func baseSSHArgs(cfg Config) ([]string, error) {
	port := cfg.SSHPort
	if port == 0 {
		port = 22
	}

	args := []string{
		"-p", strconv.Itoa(port),
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "ConnectTimeout=10",
		"-o", "ServerAliveInterval=5",
		"-o", "ServerAliveCountMax=2",
	}

	if cfg.JumpHost == "" {
		return args, nil
	}

	jumpUser := cfg.JumpUser
	if jumpUser == "" {
		jumpUser = "core"
	}

	jumpPort := cfg.JumpPort
	if jumpPort == 0 {
		jumpPort = 22
	}

	jumpTarget := jumpUser + "@" + cfg.JumpHost

	if cfg.JumpKey != "" {
		if _, err := os.Stat(cfg.JumpKey); err != nil {
			return nil, fmt.Errorf(
				"OCP jump host key %q: %w",
				cfg.JumpKey,
				err,
			)
		}

		proxy := fmt.Sprintf(
			"ssh -i %s -p %d -o StrictHostKeyChecking=no "+
				"-o UserKnownHostsFile=/dev/null "+
				"-o ConnectTimeout=10 -W %%h:%%p %s",
			cfg.JumpKey,
			jumpPort,
			jumpTarget,
		)

		args = append(
			args,
			"-o",
			"ProxyCommand="+proxy,
		)

		return args, nil
	}

	args = append(
		args,
		"-o",
		fmt.Sprintf(
			"ProxyJump=%s:%d",
			jumpTarget,
			jumpPort,
		),
	)

	return args, nil
}

func runPasswordSSH(
	ctx context.Context,
	cfg Config,
	user string,
	password string,
	ip string,
	remoteCommand string,
) error {
	if _, err := exec.LookPath("sshpass"); err != nil {
		return fmt.Errorf(
			"sshpass is required for existing VM onboarding",
		)
	}

	args, err := baseSSHArgs(cfg)
	if err != nil {
		return err
	}

	args = append(
		args,
		user+"@"+ip,
		remoteCommand,
	)

	cmdArgs := append(
		[]string{"-p", password, "ssh"},
		args...,
	)

	cmd := exec.CommandContext(
		ctx,
		"sshpass",
		cmdArgs...,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf(
			"SSH bootstrap failed: %w: %s",
			err,
			strings.TrimSpace(string(output)),
		)
	}

	return nil
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(
		value,
		"'",
		`'"'"'`,
	) + "'"
}

func injectLinuxKey(
	ctx context.Context,
	cfg Config,
	ip string,
	user string,
	password string,
	publicKey string,
) error {
	key := shellSingleQuote(publicKey)

	command := fmt.Sprintf(`
set -e
mkdir -p ~/.ssh
chmod 700 ~/.ssh
touch ~/.ssh/authorized_keys
grep -qxF %s ~/.ssh/authorized_keys ||
  echo %s >> ~/.ssh/authorized_keys
chmod 600 ~/.ssh/authorized_keys

if command -v firewall-cmd >/dev/null 2>&1; then
  firewall-cmd --permanent --add-port=22/tcp || true
  firewall-cmd --permanent --add-port=9100/tcp || true
  firewall-cmd --reload || true
fi
`, key, key)

	return runPasswordSSH(
		ctx,
		cfg,
		user,
		password,
		ip,
		command,
	)
}

func injectWindowsKey(
	ctx context.Context,
	cfg Config,
	ip string,
	user string,
	password string,
	publicKey string,
) error {
	escapedKey := strings.ReplaceAll(
		publicKey,
		"'",
		"''",
	)

	command := fmt.Sprintf(
		`powershell.exe -NoProfile -ExecutionPolicy Bypass -Command "`+
			`$authKeys='C:\ProgramData\ssh\administrators_authorized_keys'; `+
			`New-Item -ItemType Directory -Force -Path 'C:\ProgramData\ssh' | Out-Null; `+
			`if (!(Test-Path $authKeys)) { New-Item -ItemType File -Force -Path $authKeys | Out-Null }; `+
			`$key='%s'; `+
			`if (-not (Select-String -Path $authKeys -SimpleMatch $key -Quiet)) { Add-Content -Path $authKeys -Value $key }; `+
			`icacls $authKeys /inheritance:r | Out-Null; `+
			`icacls $authKeys /grant 'Administrators:F' | Out-Null; `+
			`icacls $authKeys /grant 'SYSTEM:F' | Out-Null; `+
			`Set-Service sshd -StartupType Automatic; `+
			`Start-Service sshd"`,
		escapedKey,
	)

	return runPasswordSSH(
		ctx,
		cfg,
		user,
		password,
		ip,
		command,
	)
}
