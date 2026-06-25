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

KEY_SECRET_NS="${KEY_SECRET_NS:-kubevirt-observability-system}"
KEY_SECRET_NAME="${KEY_SECRET_NAME:-lin-vm-mon-secret}"
PUBKEY_FILE="${PUBKEY_FILE:-/tmp/kubevirt-observability-id_rsa.pub}"

SKIPPED_FILE="${SKIPPED_FILE:-skipped-existing-vms.txt}"

usage() {
  cat <<EOF
Usage:

  LINUX_USER=root LINUX_PASS='password' ./scripts/onboard-existing-vms.sh

  WINDOWS_USER=Administrator WINDOWS_PASS='password' ./scripts/onboard-existing-vms.sh

  NAMESPACE=<namespace> VM_NAME=<vm-name> LINUX_USER=root LINUX_PASS='password' ./scripts/onboard-existing-vms.sh

Environment:
  NAMESPACE       Optional. Limit to namespace.
  VM_NAME         Optional. Limit to VM.
  SELECTOR        Optional. VM label selector.
  LINUX_USER      Required for Linux VMs.
  LINUX_PASS      Required for Linux VMs.
  WINDOWS_USER    Required for Windows VMs. Default Administrator.
  WINDOWS_PASS    Required for Windows VMs.
  DRY_RUN         Optional. true/false.
EOF
}

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

if ! command -v sshpass >/dev/null 2>&1; then
  echo "sshpass is required."
  echo "macOS: brew install hudochenkov/sshpass/sshpass"
  exit 1
fi

: > "${SKIPPED_FILE}"

oc get secret "${KEY_SECRET_NAME}" -n "${KEY_SECRET_NS}" \
  -o jsonpath='{.data.id_rsa\.pub}' | base64 -d > "${PUBKEY_FILE}"

PUBKEY="$(cat "${PUBKEY_FILE}")"

if [ -z "${PUBKEY}" ]; then
  echo "Monitoring public key is empty."
  exit 1
fi

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

inject_linux_key() {
  local user="$1"
  local pass="$2"
  local ip="$3"

  sshpass -p "${pass}" ssh \
    -p "${SSH_PORT}" \
    -o StrictHostKeyChecking=no \
    -o UserKnownHostsFile=/dev/null \
    -o ConnectTimeout=10 \
    "${user}@${ip}" \
    "
     mkdir -p ~/.ssh && 
     chmod 700 ~/.ssh && 
     touch ~/.ssh/authorized_keys &&  
     grep -qxF '${PUBKEY}' ~/.ssh/authorized_keys || echo '${PUBKEY}' >> ~/.ssh/authorized_keys && 
     chmod 600 ~/.ssh/authorized_keys

     
     if command -v firewall-cmd >/dev/null 2>&1; then
       firewall-cmd --permanent --add-port=22/tcp || true
       firewall-cmd --permanent --add-port=9100/tcp || true
       firewall-cmd --reload || true
     fi
   "
}

inject_windows_key() {
  local user="$1"
  local pass="$2"
  local ip="$3"

  sshpass -p "${pass}" ssh \
    -p "${SSH_PORT}" \
    -o StrictHostKeyChecking=no \
    -o UserKnownHostsFile=/dev/null \
    -o ConnectTimeout=10 \
    "${user}@${ip}" \
    "powershell.exe -NoProfile -ExecutionPolicy Bypass -Command \"\$authKeys='C:\\ProgramData\\ssh\\administrators_authorized_keys'; New-Item -ItemType Directory -Force -Path 'C:\\ProgramData\\ssh' | Out-Null; if (!(Test-Path \$authKeys)) { New-Item -ItemType File -Force -Path \$authKeys | Out-Null }; \$key='${PUBKEY}'; if (-not (Select-String -Path \$authKeys -SimpleMatch \$key -Quiet)) { Add-Content -Path \$authKeys -Value \$key }; icacls \$authKeys /inheritance:r | Out-Null; icacls \$authKeys /grant 'Administrators:F' | Out-Null; icacls \$authKeys /grant 'SYSTEM:F' | Out-Null; Set-Service sshd -StartupType Automatic; Start-Service sshd\""
}

for entry in "${VMIS[@]}"; do
  ns="$(echo "${entry}" | awk '{print $1}')"
  vm="$(echo "${entry}" | awk '{print $2}')"

  if [ -n "${VM_NAME}" ] && [ "${vm}" != "${VM_NAME}" ]; then
    continue
  fi

  ip="$(oc get vmi "${vm}" -n "${ns}" -o jsonpath='{.status.interfaces[0].ipAddress}' 2>/dev/null || true)"
  raw_os="$(oc get vm "${vm}" -n "${ns}" -o jsonpath='{.metadata.labels.kubevirt\.io/os}' 2>/dev/null || true)"
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

  echo "Onboarding ${ns}/${vm} ip=${ip} os=${os}"

  if [ "${DRY_RUN}" = "true" ]; then
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
      inject_linux_key "${LINUX_USER}" "${LINUX_PASS}" "${ip}"
      ;;
    windows)
      if [ -z "${WINDOWS_USER}" ] || [ -z "${WINDOWS_PASS}" ]; then
        echo "Skipping ${ns}/${vm}: WINDOWS_USER/WINDOWS_PASS missing"
        echo "${ns}/${vm} missing-windows-credentials" >> "${SKIPPED_FILE}"
        continue
      fi
      inject_windows_key "${WINDOWS_USER}" "${WINDOWS_PASS}" "${ip}"
      ;;
  esac

  oc annotate vm "${vm}" -n "${ns}" \
    kubevirt-observability.io/bootstrap-managed="true" \
    kubevirt-observability.io/logging-enabled="true" \
    kubevirt-observability.io/remediation-required="true" \
    kubevirt-observability.io/ssh-bootstrap-complete="true" \
    kubevirt-observability.io/reconcile-at="$(date -u +%Y%m%d%H%M%S)" \
    --overwrite

  echo "Done ${ns}/${vm}"
done

echo "Skipped report: ${SKIPPED_FILE}"
