package cloudinitmerge

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/portworx/kubevirt-observability-operator/api"
)

type OSType string

const (
	OSLinux   OSType = "linux"
	OSWindows OSType = "windows"
)

type cloudConfig struct {
	PackageUpdate interface{}              `yaml:"package_update,omitempty"`
	Packages      []interface{}            `yaml:"packages,omitempty"`
	WriteFiles    []map[string]interface{} `yaml:"write_files,omitempty"`
	RunCmd        []interface{}            `yaml:"runcmd,omitempty"`
	BootCmd       []interface{}            `yaml:"bootcmd,omitempty"`
	Raw           map[string]interface{}   `yaml:",inline"`
}

type SSHConfig struct {
	User      string
	PublicKey string
}

// Merge merges the given user data with the monitoring bootstrap data.
func Merge(userData string, osType OSType) (string, bool, error) {
	cfg, err := parseCloudConfig(userData)
	if err != nil {
		return "", false, err
	}

	if raw, ok := cfg.Raw["raw_userdata"].(string); ok {
		// Windows raw PowerShell userdata:
		// do not attempt YAML merge
		return raw, false, nil
	}
	changed := false

	switch osType {
	case OSLinux:
		if ensureLinuxWriteFiles(&cfg) {
			changed = true
		}
		if ensureLinuxRunCmd(&cfg) {
			changed = true
		}
	case OSWindows:
		pubKey, _ := cfg.Raw["observability_ssh_public_key"].(string)
		if ensureWindowsWriteFiles(&cfg, pubKey) {
			changed = true
		}
		if ensureWindowsRunCmd(&cfg) {
			changed = true
		}
	default:
		return "", false, fmt.Errorf("unsupported os type: %s", osType)
	}

	out, err := renderCloudConfig(cfg)
	if err != nil {
		return "", false, err
	}
	return out, changed, nil
}

// parseCloudConfig parses the given user data into a cloudConfig struct.
func parseCloudConfig(userData string) (cloudConfig, error) {
	var cfg cloudConfig

	trimmed := strings.TrimSpace(userData)
	if trimmed == "" {
		return cloudConfig{}, nil
	}

	if strings.HasPrefix(trimmed, "#cloud-config") {
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "#cloud-config"))
	}

	if trimmed == "" {
		return cloudConfig{}, nil
	}

	// Raw Windows PowerShell userdata (#ps1 or plain PowerShell)
	// should not be parsed as cloud-config YAML.
	if strings.HasPrefix(trimmed, "#ps1") ||
		strings.HasPrefix(trimmed, "powershell") ||
		strings.HasPrefix(trimmed, "net user") ||
		strings.Contains(trimmed, "winrm ") {
		return cloudConfig{
			Raw: map[string]interface{}{
				"raw_userdata": trimmed,
			},
		}, nil
	}

	if err := yaml.Unmarshal([]byte(trimmed), &cfg); err != nil {
		return cloudConfig{}, fmt.Errorf("failed to parse cloud-init userdata: %w", err)
	}
	if cfg.Raw == nil {
		cfg.Raw = map[string]interface{}{}
	}
	return cfg, nil
}

// renderCloudConfig renders the given cloudConfig struct into user data.
func renderCloudConfig(cfg cloudConfig) (string, error) {
	delete(cfg.Raw, "observability_ssh_public_key")
	delete(cfg.Raw, "observability_ssh_user")
	data, err := yaml.Marshal(&cfg)
	if err != nil {
		return "", fmt.Errorf("failed to marshal cloud-init userdata: %w", err)
	}

	return "#cloud-config\n" + string(data), nil
}

// ensureLinuxWriteFiles ensures the given cloudConfig has the Linux bootstrap files.
func ensureLinuxWriteFiles(cfg *cloudConfig) bool {

	changed := false
	if ensureLinuxSSH(cfg) {
		changed = true
	}

	if !hasWriteFile(cfg.WriteFiles, "/usr/local/bin/bootstrap-kubevirt-observability.sh") {
		cfg.WriteFiles = append(cfg.WriteFiles, map[string]interface{}{
			"path":        "/usr/local/bin/bootstrap-kubevirt-observability.sh",
			"permissions": "0755",
			"content":     linuxBootstrapScript(),
		})
		changed = true
	}

	if !hasWriteFile(cfg.WriteFiles, "/etc/systemd/system/"+api.LinuxServiceName+".service") {
		cfg.WriteFiles = append(cfg.WriteFiles, map[string]interface{}{
			"path":        "/etc/systemd/system/" + api.LinuxServiceName + ".service",
			"permissions": "0644",
			"content":     linuxSystemdUnit(),
		})
		changed = true
	}

	return changed
}

// ensureLinuxRunCmd ensures the given cloudConfig has the Linux bootstrap commands.
func ensureLinuxRunCmd(cfg *cloudConfig) bool {
	cmds := []string{
		"systemctl daemon-reload",
		"systemctl enable --now " + api.LinuxServiceName,
	}

	changed := false
	for _, cmd := range cmds {
		if !hasRunCmd(cfg.RunCmd, cmd) {
			cfg.RunCmd = append(cfg.RunCmd, cmd)
			changed = true
		}
	}
	return changed
}

// ensureWindowsWriteFiles ensures the given cloudConfig has the Windows bootstrap files.
func ensureWindowsWriteFiles(cfg *cloudConfig, pubKey string) bool {
	changed := false

	if !hasWriteFile(cfg.WriteFiles, `C:\ProgramData\KubeVirtObservability\bootstrap-kubevirt-observability.ps1`) {
		cfg.WriteFiles = append(cfg.WriteFiles, map[string]interface{}{
			"path":        `C:\ProgramData\KubeVirtObservability\bootstrap-kubevirt-observability.ps1`,
			"permissions": "0644",
			"content":     windowsBootstrapScript(pubKey),
		})
		changed = true
	}

	return changed
}

// ensureWindowsRunCmd ensures the given cloudConfig has the Windows bootstrap commands.
func ensureWindowsRunCmd(cfg *cloudConfig) bool {
	cmd := windowsScheduledTaskCommand()
	if hasRunCmd(cfg.RunCmd, cmd) {
		return false
	}
	cfg.RunCmd = append(cfg.RunCmd, cmd)
	return true
}

// hasWriteFile returns true if the given path is present in the given write files.
func hasWriteFile(files []map[string]interface{}, path string) bool {
	for _, f := range files {
		v, ok := f["path"]
		if !ok {
			continue
		}
		if s, ok := v.(string); ok && strings.EqualFold(s, path) {
			return true
		}
	}
	return false
}

// hasRunCmd returns true if the given command is present in the given run commands.
func hasRunCmd(cmds []interface{}, wanted string) bool {
	normWanted := normalizeCommand(wanted)
	for _, c := range cmds {
		switch v := c.(type) {
		case string:
			if normalizeCommand(v) == normWanted {
				return true
			}
		case []interface{}:
			parts := make([]string, 0, len(v))
			for _, x := range v {
				parts = append(parts, fmt.Sprintf("%v", x))
			}
			if normalizeCommand(strings.Join(parts, " ")) == normWanted {
				return true
			}
		}
	}
	return false
}

// normalizeCommand normalizes the given command for comparison.
func normalizeCommand(s string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
}

// linuxSystemdUnit returns the Linux systemd unit for the bootstrap service.
func linuxSystemdUnit() string {
	return `[Unit]
Description=VM Linux KubeVirt Observability Bootstrap Service
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
ExecStart=/usr/local/bin/bootstrap-kubevirt-observability.sh
RemainAfterExit=yes

[Install]
WantedBy=multi-user.target
`
}

// linuxBootstrapScript returns the Linux bootstrap script.
func linuxBootstrapScript() string {
	return `#!/usr/bin/env bash
set -euo pipefail

MARKER="/var/lib/kubevirt-observability-bootstrap.done"
mkdir -p /var/lib

if [ -f "${MARKER}" ]; then
  exit 0
fi

ARCH="$(uname -m)"
case "${ARCH}" in
  x86_64) NODE_ARCH="amd64" ;;
  aarch64|arm64) NODE_ARCH="arm64" ;;
  *) NODE_ARCH="amd64" ;;
esac

install_node_exporter() {
  if command -v node_exporter >/dev/null 2>&1; then
    return 0
  fi

  TMP_DIR="$(mktemp -d)"
  cd "${TMP_DIR}"

  VERSION="1.10.2"
  curl -fL -o node_exporter.tar.gz \
    "https://github.com/prometheus/node_exporter/releases/download/v${VERSION}/node_exporter-${VERSION}.linux-${NODE_ARCH}.tar.gz"

  tar -xzf node_exporter.tar.gz
  install -m 0755 "node_exporter-${VERSION}.linux-${NODE_ARCH}/node_exporter" /usr/local/bin/node_exporter

  id -u node_exporter >/dev/null 2>&1 || useradd --system --no-create-home --shell /usr/sbin/nologin node_exporter || true

  cat >/etc/systemd/system/node_exporter.service <<'SERVICE'
[Unit]
Description=Prometheus Node Exporter
Wants=network-online.target
After=network-online.target

[Service]
User=node_exporter
Group=node_exporter
Type=simple
ExecStart=/usr/local/bin/node_exporter

[Install]
WantedBy=multi-user.target
SERVICE

  systemctl daemon-reload
  systemctl enable --now node_exporter
}

install_alloy() {
  if systemctl list-unit-files | grep -q '^alloy.service'; then
    systemctl enable --now alloy || true
    return 0
  fi

  if [ -f /etc/os-release ]; then
    . /etc/os-release
  fi

  if command -v apt-get >/dev/null 2>&1; then
    mkdir -p /etc/apt/keyrings
    curl -fsSL https://apt.grafana.com/gpg.key -o /etc/apt/keyrings/grafana.asc
    chmod 0644 /etc/apt/keyrings/grafana.asc
    echo "deb [signed-by=/etc/apt/keyrings/grafana.asc] https://apt.grafana.com stable main" >/etc/apt/sources.list.d/grafana.list
    apt-get update
    DEBIAN_FRONTEND=noninteractive apt-get install -y alloy
  elif command -v dnf >/dev/null 2>&1; then
    rpm --import https://rpm.grafana.com/gpg.key || true
    cat >/etc/yum.repos.d/grafana.repo <<'REPO'
[grafana]
name=grafana
baseurl=https://rpm.grafana.com
repo_gpgcheck=1
enabled=1
gpgcheck=1
gpgkey=https://rpm.grafana.com/gpg.key
sslverify=1
REPO
    dnf install -y alloy
  elif command -v yum >/dev/null 2>&1; then
    rpm --import https://rpm.grafana.com/gpg.key || true
    cat >/etc/yum.repos.d/grafana.repo <<'REPO'
[grafana]
name=grafana
baseurl=https://rpm.grafana.com
repo_gpgcheck=1
enabled=1
gpgcheck=1
gpgkey=https://rpm.grafana.com/gpg.key
sslverify=1
REPO
    yum install -y alloy
  else
    return 0
  fi

  mkdir -p /etc/alloy
  if [ ! -f /etc/alloy/config.alloy ]; then
    cat >/etc/alloy/config.alloy <<'CONFIG'
logging {
  level = "info"
}
CONFIG
  fi

  systemctl enable --now alloy || true
}

install_node_exporter
install_alloy

touch "${MARKER}"
`
}

// windowsBootstrapScript returns the Windows bootstrap script.
func windowsBootstrapScript(pubKey string) string {
	if pubKey == "" {
		pubKey = "ssh-rsa placeholder"
	}

	script := `$stateDir = 'C:\ProgramData\KubeVirtObservability'

New-Item -ItemType Directory -Path $stateDir -Force | Out-Null

$doneMarker = 'C:\ProgramData\KubeVirtObservability\bootstrap.done'
$failedMarker = 'C:\ProgramData\KubeVirtObservability\bootstrap.failed'

$marker = 'C:\ProgramData\KubeVirtObservability\bootstrap.done'
if (Test-Path $marker) { exit 0 }

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

# Force UTC timezone for Loki timestamp consistency
Set-TimeZone -Id "UTC"

# Ensure Windows time sync service enabled
Set-Service w32time -StartupType Automatic

# Start time sync service
Start-Service w32time

# Force immediate time synchronization
w32tm /resync

function Configure-OpenSSH {
    $authKeys = "C:\ProgramData\ssh\administrators_authorized_keys"

    New-Item -ItemType Directory -Force -Path "C:\ProgramData\ssh" | Out-Null

@'
__WINDOWS_SSH_PUBLIC_KEY__
'@ | Set-Content -Path $authKeys -Encoding ascii

    icacls $authKeys /inheritance:r | Out-Null
    icacls $authKeys /grant "Administrators:F" | Out-Null
    icacls $authKeys /grant "SYSTEM:F" | Out-Null

    Set-Service sshd -StartupType Automatic
    Start-Service sshd

    New-NetFirewallRule -DisplayName "OpenSSH Server" -Direction Inbound -Protocol TCP -LocalPort 22 -Action Allow -ErrorAction SilentlyContinue | Out-Null
}

function Enable-WinRMForObservability {
    Enable-PSRemoting -Force

    winrm set winrm/config/service/auth '@{Basic="true"}'
    winrm set winrm/config/service '@{AllowUnencrypted="true"}'

    Set-Item -Path WSMan:\localhost\Service\Auth\Basic -Value $true
    Set-Item -Path WSMan:\localhost\Service\AllowUnencrypted -Value $true

    try {
        Enable-NetFirewallRule -DisplayGroup 'Windows Remote Management' -ErrorAction Stop
    } catch {
        New-NetFirewallRule -DisplayName 'Allow WinRM HTTP 5985' -Direction Inbound -Protocol TCP -LocalPort 5985 -Action Allow -ErrorAction SilentlyContinue | Out-Null
    }

    Set-Service WinRM -StartupType Automatic
    Start-Service WinRM
}

function Install-WindowsExporter {
    if (Get-Service windows_exporter -ErrorAction SilentlyContinue) { return }

    [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

    $version = '0.31.5'
    $msi = "windows_exporter-$version-amd64.msi"
    $url = "https://github.com/prometheus-community/windows_exporter/releases/download/v$version/$msi"
    $out = "C:\ProgramData\KubeVirtObservability\$msi"

    curl.exe -L -o $out $url

    $arguments = @(
      '/i',
      $out,
	  'ENABLED_COLLECTORS=[defaults],cpu,logical_disk,memory,net,os,service,system,textfile',
      'LISTEN_PORT=9182',
      'ADDLOCAL=FirewallException',
      '/qn'
    )

    Start-Process -FilePath 'msiexec.exe' -ArgumentList $arguments -Wait -NoNewWindow

    Set-Service windows_exporter -StartupType Automatic
    
	Start-Sleep -Seconds 5

	$svc = Get-Service windows_exporter -ErrorAction SilentlyContinue

	if ($svc.Status -ne 'Running') {
    	Get-EventLog -LogName Application -Newest 50 | Out-File C:\ProgramData\KubeVirtObservability\windows-exporter-error.log
    	Write-Output "windows_exporter failed to stay running"
		return
	}

    if (-not (Get-Service windows_exporter -ErrorAction SilentlyContinue)) {
        throw "windows_exporter service missing"
    }

    curl.exe -f http://127.0.0.1:9182/metrics | Out-Null

	New-Item -ItemType File -Path C:\ProgramData\KubeVirtObservability\exporter.ready -Force | Out-Null
}

function Install-Alloy {

    [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

    if (-not (Get-Service Alloy -ErrorAction SilentlyContinue)) {

        $installer = 'C:\ProgramData\KubeVirtObservability\alloy-installer-windows-amd64.exe'
		$version = "v1.15.1"

		$url = "https://github.com/grafana/alloy/releases/download/$version/alloy-installer-windows-amd64.exe"

        curl.exe -L -o $installer $url

        Start-Process -FilePath $installer -ArgumentList "/S" -Wait -NoNewWindow
    }

    $dataDir = "C:\ProgramData\GrafanaLabs\Alloy\data"

    New-Item -ItemType Directory -Force -Path $dataDir | Out-Null

    Remove-Item "$dataDir\*" -Recurse -Force -ErrorAction SilentlyContinue | Out-Null

    New-Item -ItemType Directory -Force -Path "$dataDir\loki.source.windowsevent.application" | Out-Null

    New-Item -ItemType Directory -Force -Path "$dataDir\loki.source.windowsevent.system" | Out-Null

    icacls "$dataDir" /grant "SYSTEM:(OI)(CI)F" /T | Out-Null

    Set-Service Alloy -StartupType Automatic

    New-Item -ItemType File -Path C:\ProgramData\KubeVirtObservability\alloy.ready -Force | Out-Null
}

try {
    Configure-OpenSSH
    Enable-WinRMForObservability
    Install-WindowsExporter
    Install-Alloy

    New-Item -ItemType File -Path $doneMarker -Force | Out-Null
} catch {
    $_ | Out-File $failedMarker
    exit 1
}
`

	return strings.Replace(script, "__WINDOWS_SSH_PUBLIC_KEY__", pubKey, 1)
}

// windowsScheduledTaskCommand returns the Windows scheduled task command.
func windowsScheduledTaskCommand() string {
	return `powershell.exe -ExecutionPolicy Bypass -File C:\ProgramData\KubeVirtObservability\bootstrap-kubevirt-observability.ps1`
}

func ensureLinuxSSH(cfg *cloudConfig) bool {
	user, okUser := cfg.Raw["observability_ssh_user"].(string)
	pubKey, okKey := cfg.Raw["observability_ssh_public_key"].(string)

	if !okUser || !okKey || user == "" || pubKey == "" {
		return false
	}

	users, ok := cfg.Raw["users"].([]interface{})
	if !ok {
		users = []interface{}{}
	}

	for _, u := range users {
		m, ok := u.(map[string]interface{})
		if !ok {
			continue
		}
		if name, ok := m["name"].(string); ok && name == user {
			return false
		}
	}

	userEntry := map[string]interface{}{
		"name": user,
		"ssh_authorized_keys": []interface{}{
			pubKey,
		},
	}

	if user != "root" {
		userEntry["sudo"] = "ALL=(ALL) NOPASSWD:ALL"
		userEntry["shell"] = "/bin/bash"
	} else {
		cfg.Raw["disable_root"] = false
	}

	users = append(users, userEntry)
	cfg.Raw["users"] = users

	return true
}
