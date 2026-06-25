package remediation

import (
	"encoding/base64"
	"strings"
)

func LinuxAlloyInstallScript(configAlloy string, lokiToken string) string {
	tokenB64 := base64.StdEncoding.EncodeToString([]byte(strings.TrimSpace(lokiToken)))

	return `#!/usr/bin/env bash
set -euo pipefail

SUDO=""
if [ "$(id -u)" -ne 0 ]; then
  SUDO="sudo"
  sudo -n true || {
    echo "passwordless sudo required for Alloy remediation" >&2
    exit 1
  }
fi

$SUDO mkdir -p /etc/alloy/secrets /var/lib/alloy

if ! command -v alloy >/dev/null 2>&1; then
  if command -v dnf >/dev/null 2>&1; then
    $SUDO rpm --import https://rpm.grafana.com/gpg.key
    cat <<'REPO' | $SUDO tee /etc/yum.repos.d/grafana.repo >/dev/null
[grafana]
name=grafana
baseurl=https://rpm.grafana.com
repo_gpgcheck=1
enabled=1
gpgcheck=1
gpgkey=https://rpm.grafana.com/gpg.key
sslverify=1
sslcacert=/etc/pki/tls/certs/ca-bundle.crt
REPO
    $SUDO dnf install -y alloy
  elif command -v yum >/dev/null 2>&1; then
    $SUDO rpm --import https://rpm.grafana.com/gpg.key
    cat <<'REPO' | $SUDO tee /etc/yum.repos.d/grafana.repo >/dev/null
[grafana]
name=grafana
baseurl=https://rpm.grafana.com
repo_gpgcheck=1
enabled=1
gpgcheck=1
gpgkey=https://rpm.grafana.com/gpg.key
sslverify=1
sslcacert=/etc/pki/tls/certs/ca-bundle.crt
REPO
    $SUDO yum install -y alloy
  elif command -v apt-get >/dev/null 2>&1; then
    $SUDO apt-get update
    $SUDO apt-get install -y gpg curl
    curl -fsSL https://apt.grafana.com/gpg.key | gpg --dearmor | $SUDO tee /etc/apt/keyrings/grafana.gpg >/dev/null
    echo "deb [signed-by=/etc/apt/keyrings/grafana.gpg] https://apt.grafana.com stable main" | $SUDO tee /etc/apt/sources.list.d/grafana.list >/dev/null
    $SUDO apt-get update
    $SUDO apt-get install -y alloy
  else
    echo "unsupported Linux package manager for Alloy install" >&2
    exit 1
  fi
fi
printf '%s' '` + tokenB64 + `' | base64 -d | $SUDO tee /etc/alloy/secrets/loki.token >/dev/null

if id alloy >/dev/null 2>&1; then
  $SUDO chown -R alloy:alloy /etc/alloy/secrets
  $SUDO chmod 700 /etc/alloy/secrets
  $SUDO chmod 600 /etc/alloy/secrets/loki.token
else
  $SUDO chmod 600 /etc/alloy/secrets/loki.token
fi

cat <<'ALLOYCFG' | $SUDO tee /etc/alloy/config.alloy >/dev/null
` + configAlloy + `
ALLOYCFG

if id alloy >/dev/null 2>&1; then
  $SUDO chown alloy:alloy /etc/alloy/config.alloy
fi
$SUDO chmod 644 /etc/alloy/config.alloy
$SUDO mkdir -p /etc/systemd/system/alloy.service.d
printf '%s\n' '[Service]' 'User=root' 'Group=root' | $SUDO tee /etc/systemd/system/alloy.service.d/override.conf >/dev/null

$SUDO systemctl daemon-reload
$SUDO systemctl enable alloy
$SUDO systemctl restart alloy

for i in $(seq 1 20); do
  if $SUDO systemctl is-active --quiet alloy; then
    exit 0
  fi
  sleep 1
done

$SUDO systemctl status alloy --no-pager || true
exit 1
`
}
