#!/usr/bin/env bash
set -euo pipefail

NAMESPACE="${NAMESPACE:-}"
VM_NAME="${VM_NAME:-}"
SELECTOR="${SELECTOR:-}"
DRY_RUN="${DRY_RUN:-false}"

LINUX_USER="${LINUX_USER:-}"
LINUX_PASS="${LINUX_PASS:-}"
WINDOWS_USER="${WINDOWS_USER:-Administrator}"
WINDOWS_PASS="${WINDOWS_PASS:-}"

SSH_PORT="${SSH_PORT:-22}"

# Optional OCP SSH jump host.
#
# When OCP_JUMP_HOST is set:
#
#   Mac -> OCP node -> VM
#
# When it is empty:
#
#   current host -> VM
#
OCP_JUMP_HOST="${OCP_JUMP_HOST:-}"
OCP_JUMP_USER="${OCP_JUMP_USER:-core}"
OCP_JUMP_PORT="${OCP_JUMP_PORT:-22}"

# Optional private key used specifically for reaching the OCP jump host.
# Leave empty when your normal ~/.ssh config, ssh-agent, or default key works.
OCP_JUMP_KEY="${OCP_JUMP_KEY:-}"

KEY_SECRET_NS="${KEY_SECRET_NS:-kubevirt-observability-system}"
KEY_SECRET_NAME="${KEY_SECRET_NAME:-lin-vm-mon-secret}"
PUBKEY_FILE="${PUBKEY_FILE:-/tmp/kubevirt-observability-id_rsa.pub}"

SKIPPED_FILE="${SKIPPED_FILE:-skipped-existing-vms.txt}"
FAILED_FILE="${FAILED_FILE:-failed-existing-vms.txt}"

usage() {
  cat <<'USAGE'
Usage:

  Direct VM access:

    LINUX_USER=root \
    LINUX_PASS='password' \
    ./scripts/onboard-existing-vms.sh

  Access VMs through an OCP node:

    OCP_JUMP_HOST=<ocp-node-ip-or-hostname> \
    OCP_JUMP_USER=core \
    LINUX_USER=root \
    LINUX_PASS='password' \
    ./scripts/onboard-existing-vms.sh

  One VM only:

    NAMESPACE=<namespace> \
    VM_NAME=<vm-name> \
    OCP_JUMP_HOST=<ocp-node> \
    LINUX_USER=root \
    LINUX_PASS='password' \
    ./scripts/onboard-existing-vms.sh

Environment:

  NAMESPACE         Optional. Limit onboarding to one namespace.
  VM_NAME           Optional. Limit onboarding to one VM.
  SELECTOR          Optional. VM label selector.
  DRY_RUN           Optional. true/false.

Linux:
  LINUX_USER        Required for Linux VMs.
  LINUX_PASS        Required for Linux VMs.

Windows:
  WINDOWS_USER      Windows SSH user. Default: Administrator.
  WINDOWS_PASS      Required for Windows VMs.

SSH:
  SSH_PORT          VM SSH port. Default: 22.

Jump host:
  OCP_JUMP_HOST     Optional. OCP node used as SSH jump host.
  OCP_JUMP_USER     Jump host SSH user. Default: core.
  OCP_JUMP_PORT     Jump host SSH port. Default: 22.
  OCP_JUMP_KEY      Optional private key for the OCP jump host.

Monitoring key:
  KEY_SECRET_NS     Default: kubevirt-observability-system
  KEY_SECRET_NAME   Default: lin-vm-mon-secret
  PUBKEY_FILE       Temporary public key path.

Reports:
  SKIPPED_FILE      Default: skipped-existing-vms.txt
  FAILED_FILE       Default: failed-existing-vms.txt

Examples:

  # From Mac, using an OCP node as jump host:
  OCP_JUMP_HOST=<ocp-node-ip-or-hostname> \
  OCP_JUMP_USER=core \
  LINUX_USER=root \
  LINUX_PASS='password' \
  ./scripts/onboard-existing-vms.sh

  # If your jump host requires a specific SSH key:
  OCP_JUMP_HOST=<ocp-node-ip-or-hostname> \
  OCP_JUMP_USER=core \
  OCP_JUMP_KEY=/path/to/private-key \
  LINUX_USER=root \
  LINUX_PASS='password' \
  ./scripts/onboard-existing-vms.sh

  # Run directly from an OCP node:
  LINUX_USER=root \
  LINUX_PASS='password' \
  ./scripts/onboard-existing-vms.sh

USAGE
}

case "${1:-}" in
  -h|--help)
    usage
    exit 0
    ;;
  "")
    ;;
  *)
    echo "Unknown argument: $1" >&2
    echo
    usage
    exit 1
    ;;
esac

normalize_os() {
  case "$1" in
    rhel|rhel*|oel|ol|oracle|oraclelinux|rocky|rocky*|ubuntu|debian|centos|linux)
      echo "linux"
      ;;
    windows|window|win|win2022|win2025)
      echo "windows"
      ;;
    *)
      echo "unknown"
      ;;
  esac
}

if ! command -v oc >/dev/null 2>&1; then
  echo "ERROR: oc command is required."
  exit 1
fi

if ! command -v sshpass >/dev/null 2>&1; then
  echo "ERROR: sshpass is required."
  echo "macOS:"
  echo "  brew install hudochenkov/sshpass/sshpass"
  exit 1
fi

: > "${SKIPPED_FILE}"
: > "${FAILED_FILE}"

###############################################################################
# SSH options
###############################################################################

SSH_OPTIONS=(
  -p "${SSH_PORT}"
  -o StrictHostKeyChecking=no
  -o UserKnownHostsFile=/dev/null
  -o ConnectTimeout=10
  -o ServerAliveInterval=5
  -o ServerAliveCountMax=2
)

if [ -n "${OCP_JUMP_HOST}" ]; then
  JUMP_TARGET="${OCP_JUMP_USER}@${OCP_JUMP_HOST}"

  echo "SSH topology:"
  echo "  Local host"
  echo "      -> ${JUMP_TARGET}:${OCP_JUMP_PORT}"
  echo "      -> VM:<${SSH_PORT}>"
  echo

  if [ -n "${OCP_JUMP_KEY}" ]; then
    if [ ! -f "${OCP_JUMP_KEY}" ]; then
      echo "ERROR: OCP jump host key does not exist:"
      echo "  ${OCP_JUMP_KEY}"
      exit 1
    fi

    SSH_OPTIONS+=(
      -o "ProxyCommand=ssh -i '${OCP_JUMP_KEY}' -p ${OCP_JUMP_PORT} -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=10 -W %h:%p ${JUMP_TARGET}"
    )
  else
    SSH_OPTIONS+=(
      -o "ProxyJump=${JUMP_TARGET}:${OCP_JUMP_PORT}"
    )
  fi
else
  echo "SSH topology:"
  echo "  Direct connection to VM IPs"
  echo
fi

###############################################################################
# Fetch monitoring public key
###############################################################################

echo "Reading monitoring public key:"
echo "  ${KEY_SECRET_NS}/${KEY_SECRET_NAME}"

if ! oc get secret "${KEY_SECRET_NAME}" \
  -n "${KEY_SECRET_NS}" \
  -o jsonpath='{.data.id_rsa\.pub}' |
  base64 -d > "${PUBKEY_FILE}"; then

  echo "ERROR: unable to retrieve monitoring public key."
  exit 1
fi

PUBKEY="$(cat "${PUBKEY_FILE}")"

if [ -z "${PUBKEY}" ]; then
  echo "ERROR: monitoring public key is empty."
  exit 1
fi

###############################################################################
# Discover running VMIs
###############################################################################

args=(-A)

if [ -n "${NAMESPACE}" ]; then
  args=(-n "${NAMESPACE}")
fi

if [ -n "${SELECTOR}" ]; then
  args+=(-l "${SELECTOR}")
fi

mapfile -t VMIS < <(
  oc get vmi "${args[@]}" \
    -o jsonpath='{range .items[*]}{.metadata.namespace}{" "}{.metadata.name}{"\n"}{end}'
)

if [ "${#VMIS[@]}" -eq 0 ]; then
  echo "No running VMIs found. Stopped VMs are skipped."
  exit 0
fi

###############################################################################
# Linux bootstrap
###############################################################################

inject_linux_key() {
  local user="$1"
  local pass="$2"
  local ip="$3"

  sshpass -p "${pass}" ssh \
    "${SSH_OPTIONS[@]}" \
    "${user}@${ip}" \
    "
      set -e

      mkdir -p ~/.ssh
      chmod 700 ~/.ssh

      touch ~/.ssh/authorized_keys

      grep -qxF '${PUBKEY}' ~/.ssh/authorized_keys ||
        echo '${PUBKEY}' >> ~/.ssh/authorized_keys

      chmod 600 ~/.ssh/authorized_keys

      if command -v firewall-cmd >/dev/null 2>&1; then
        firewall-cmd --permanent --add-port=22/tcp || true
        firewall-cmd --permanent --add-port=9100/tcp || true
        firewall-cmd --reload || true
      fi
    "
}

###############################################################################
# Windows bootstrap
###############################################################################

inject_windows_key() {
  local user="$1"
  local pass="$2"
  local ip="$3"

  sshpass -p "${pass}" ssh \
    "${SSH_OPTIONS[@]}" \
    "${user}@${ip}" \
    "powershell.exe -NoProfile -ExecutionPolicy Bypass -Command \"\
\$authKeys='C:\\ProgramData\\ssh\\administrators_authorized_keys'; \
New-Item -ItemType Directory -Force -Path 'C:\\ProgramData\\ssh' | Out-Null; \
if (!(Test-Path \$authKeys)) { \
  New-Item -ItemType File -Force -Path \$authKeys | Out-Null \
}; \
\$key='${PUBKEY}'; \
if (-not (Select-String -Path \$authKeys -SimpleMatch \$key -Quiet)) { \
  Add-Content -Path \$authKeys -Value \$key \
}; \
icacls \$authKeys /inheritance:r | Out-Null; \
icacls \$authKeys /grant 'Administrators:F' | Out-Null; \
icacls \$authKeys /grant 'SYSTEM:F' | Out-Null; \
Set-Service sshd -StartupType Automatic; \
Start-Service sshd\""
}

###############################################################################
# Onboard VMs
###############################################################################

for entry in "${VMIS[@]}"; do
  ns="$(echo "${entry}" | awk '{print $1}')"
  vm="$(echo "${entry}" | awk '{print $2}')"

  if [ -n "${VM_NAME}" ] && [ "${vm}" != "${VM_NAME}" ]; then
    continue
  fi

  ip="$(
    oc get vmi "${vm}" \
      -n "${ns}" \
      -o jsonpath='{.status.interfaces[0].ipAddress}' \
      2>/dev/null || true
  )"

  raw_os="$(
    oc get vm "${vm}" \
      -n "${ns}" \
      -o jsonpath='{.metadata.labels.kubevirt\.io/os}' \
      2>/dev/null || true
  )"

  os="$(normalize_os "${raw_os}")"

  if [ -z "${ip}" ] || [ "${ip}" = "10.0.2.2" ]; then
    echo "Skipping ${ns}/${vm}: invalid IP '${ip}'"
    echo "${ns}/${vm} invalid-ip ${ip}" >> "${SKIPPED_FILE}"
    continue
  fi

  if [ "${os}" = "unknown" ]; then
    echo "Skipping ${ns}/${vm}: unknown kubevirt.io/os='${raw_os}'"
    echo "${ns}/${vm} unknown-os ${raw_os}" >> "${SKIPPED_FILE}"
    continue
  fi

  echo
  echo "================================================================"
  echo "Onboarding ${ns}/${vm}"
  echo "  IP: ${ip}"
  echo "  OS: ${os}"

  if [ -n "${OCP_JUMP_HOST}" ]; then
    echo "  Jump host: ${OCP_JUMP_USER}@${OCP_JUMP_HOST}"
  else
    echo "  Jump host: none"
  fi

  echo "================================================================"

  if [ "${DRY_RUN}" = "true" ]; then
    echo "DRY RUN:"
    echo "  would inject SSH key"
    echo "  would add monitoring annotations"
    continue
  fi

  case "${os}" in
    linux)
      if [ -z "${LINUX_USER}" ] || [ -z "${LINUX_PASS}" ]; then
        echo "Skipping ${ns}/${vm}: LINUX_USER/LINUX_PASS missing"
        echo "${ns}/${vm} missing-linux-credentials" >> "${SKIPPED_FILE}"
        continue
      fi

      echo "Injecting monitoring SSH key into Linux VM..."

      if ! inject_linux_key \
        "${LINUX_USER}" \
        "${LINUX_PASS}" \
        "${ip}"; then

        echo "FAILED ${ns}/${vm}: Linux SSH bootstrap failed"
        echo "${ns}/${vm} linux-ssh-bootstrap-failed ${ip}" >> "${FAILED_FILE}"
        continue
      fi
      ;;

    windows)
      if [ -z "${WINDOWS_USER}" ] || [ -z "${WINDOWS_PASS}" ]; then
        echo "Skipping ${ns}/${vm}: WINDOWS_USER/WINDOWS_PASS missing"
        echo "${ns}/${vm} missing-windows-credentials" >> "${SKIPPED_FILE}"
        continue
      fi

      echo "Injecting monitoring SSH key into Windows VM..."

      if ! inject_windows_key \
        "${WINDOWS_USER}" \
        "${WINDOWS_PASS}" \
        "${ip}"; then

        echo "FAILED ${ns}/${vm}: Windows SSH bootstrap failed"
        echo "${ns}/${vm} windows-ssh-bootstrap-failed ${ip}" >> "${FAILED_FILE}"
        continue
      fi
      ;;
  esac

  echo "Applying KubeVirt Observability annotations..."

  if ! oc annotate vm "${vm}" -n "${ns}" \
    kubevirt-observability.io/bootstrap-managed="true" \
    kubevirt-observability.io/logging-enabled="true" \
    kubevirt-observability.io/remediation-required="true" \
    kubevirt-observability.io/ssh-bootstrap-complete="true" \
    kubevirt-observability.io/reconcile-at="$(date -u +%Y%m%d%H%M%S)" \
    --overwrite; then

    echo "FAILED ${ns}/${vm}: unable to annotate VM"
    echo "${ns}/${vm} annotation-failed" >> "${FAILED_FILE}"
    continue
  fi

  echo "Done ${ns}/${vm}"
done

echo
echo "============================================================"
echo "Onboarding completed"
echo "============================================================"
echo "Skipped report: ${SKIPPED_FILE}"
echo "Failed report:  ${FAILED_FILE}"
