#!/usr/bin/env bash
set -euo pipefail

NAMESPACE="${NAMESPACE:-}"
VM_NAME="${VM_NAME:-}"
OS="${OS:-}"
SELECTOR="${SELECTOR:-}"
DRY_RUN="${DRY_RUN:-false}"

usage() {
  cat <<EOF
Usage:

  OS=rhel NAMESPACE=<namespace> VM_NAME=<vm-name> ./scripts/label-vms.sh

  OS=windows SELECTOR='app=my-vms' ./scripts/label-vms.sh

  OS=rhel ./scripts/label-vms.sh

Environment:
  OS          Required. Example: rhel, windows, ubuntu, rocky, oel
  NAMESPACE   Optional. Limit to namespace.
  VM_NAME     Optional. Limit to one VM.
  SELECTOR    Optional. Kubernetes label selector.
  DRY_RUN     Optional. true/false. Default false.
EOF
}

if [ -z "${OS}" ]; then
  usage
  exit 1
fi

args=(-A)

if [ -n "${NAMESPACE}" ]; then
  args=(-n "${NAMESPACE}")
fi

if [ -n "${SELECTOR}" ]; then
  args+=(-l "${SELECTOR}")
fi

mapfile -t VMS < <(
  oc get vm "${args[@]}" \
    -o jsonpath='{range .items[*]}{.metadata.namespace}{" "}{.metadata.name}{"\n"}{end}'
)

if [ "${#VMS[@]}" -eq 0 ]; then
  echo "No VMs found."
  exit 0
fi

for entry in "${VMS[@]}"; do
  ns="$(echo "${entry}" | awk '{print $1}')"
  vm="$(echo "${entry}" | awk '{print $2}')"

  if [ -n "${VM_NAME}" ] && [ "${vm}" != "${VM_NAME}" ]; then
    continue
  fi

  echo "Labeling ${ns}/${vm} kubevirt.io/os=${OS}"

  if [ "${DRY_RUN}" = "true" ]; then
    echo "  oc label vm ${vm} -n ${ns} kubevirt.io/os=${OS} --overwrite"
    continue
  fi

  oc label vm "${vm}" -n "${ns}" \
    "kubevirt.io/os=${OS}" \
    --overwrite
done
