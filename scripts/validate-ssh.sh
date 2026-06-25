#!/usr/bin/env bash
set -euo pipefail

NAMESPACE="${NAMESPACE:-}"
VM_NAME="${VM_NAME:-}"
SELECTOR="${SELECTOR:-}"

KEY_SECRET_NS="${KEY_SECRET_NS:-kubevirt-observability-system}"
KEY_SECRET_NAME="${KEY_SECRET_NAME:-lin-vm-mon-private}"
PRIVATE_KEY_FILE="${PRIVATE_KEY_FILE:-/tmp/kubevirt-observability-id_rsa}"

SSH_PORT="${SSH_PORT:-22}"
SKIPPED_FILE="${SKIPPED_FILE:-ssh-validation-skipped.txt}"
FAILED_FILE="${FAILED_FILE:-ssh-validation-failed.txt}"

: > "${SKIPPED_FILE}"
: > "${FAILED_FILE}"

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

oc get secret "${KEY_SECRET_NAME}" -n "${KEY_SECRET_NS}" \
  -o jsonpath='{.data.id_rsa}' | base64 -d > "${PRIVATE_KEY_FILE}"

chmod 600 "${PRIVATE_KEY_FILE}"

DEFAULT_LINUX_USER="$(
  oc get secret "${KEY_SECRET_NAME}" -n "${KEY_SECRET_NS}" \
    -o jsonpath='{.data.username}' 2>/dev/null | base64 -d || true
)"

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
  echo "No running VMIs found."
  exit 0
fi

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

  case "${os}" in
    linux)
      user="${LINUX_USER:-${DEFAULT_LINUX_USER:-root}}"
      ;;
    windows)
      user="${WINDOWS_USER:-Administrator}"
      ;;
    *)
      echo "Skipping ${ns}/${vm}: unknown os '${raw_os}'"
      echo "${ns}/${vm} unknown-os ${raw_os}" >> "${SKIPPED_FILE}"
      continue
      ;;
  esac

  echo "Validating SSH ${ns}/${vm} ${user}@${ip}"

  if ssh -i "${PRIVATE_KEY_FILE}" \
    -p "${SSH_PORT}" \
    -o BatchMode=yes \
    -o StrictHostKeyChecking=no \
    -o UserKnownHostsFile=/dev/null \
    -o ConnectTimeout=10 \
    "${user}@${ip}" \
    'echo SSH_KEY_OK' >/dev/null 2>&1; then

    echo "OK ${ns}/${vm}"

    oc annotate vm "${vm}" -n "${ns}" \
      kubevirt-observability.io/ssh-bootstrap-complete="true" \
      --overwrite >/dev/null
  else
    echo "FAILED ${ns}/${vm}"
    echo "${ns}/${vm} ${user}@${ip}" >> "${FAILED_FILE}"
  fi
done

echo
echo "Failed report: ${FAILED_FILE}"
echo "Skipped report: ${SKIPPED_FILE}"
