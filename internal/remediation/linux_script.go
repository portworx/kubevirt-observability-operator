package remediation

func LinuxNodeExporterInstallScript() string {
	return `#!/usr/bin/env bash
set -euo pipefail

MARKER="/var/lib/kubevirt-observability-remediation.done"

SUDO=""
if [ "$(id -u)" -ne 0 ]; then
  SUDO="sudo"
  sudo -n true || {
    echo "passwordless sudo required for remediation" >&2
    exit 1
  }
fi

$SUDO mkdir -p /var/lib

if [ -f "${MARKER}" ]; then
  exit 0
fi

ARCH="$(uname -m)"
case "${ARCH}" in
  x86_64) NODE_ARCH="amd64" ;;
  aarch64|arm64) NODE_ARCH="arm64" ;;
  *) NODE_ARCH="amd64" ;;
esac

if ! command -v node_exporter >/dev/null 2>&1; then
  TMP_DIR="$(mktemp -d)"
  cd "${TMP_DIR}"

  VERSION="1.10.2"
  curl -fL -o node_exporter.tar.gz \
    "https://github.com/prometheus/node_exporter/releases/download/v${VERSION}/node_exporter-${VERSION}.linux-${NODE_ARCH}.tar.gz"

  tar -xzf node_exporter.tar.gz

  $SUDO install -m 0755 "node_exporter-${VERSION}.linux-${NODE_ARCH}/node_exporter" /usr/local/bin/node_exporter

  id -u node_exporter >/dev/null 2>&1 || $SUDO useradd --system --no-create-home --shell /usr/sbin/nologin node_exporter || true

  cat <<'SERVICE' | $SUDO tee /etc/systemd/system/node_exporter.service >/dev/null
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

  $SUDO systemctl daemon-reload
  $SUDO systemctl enable --now node_exporter
fi

$SUDO touch "${MARKER}"
`
}
