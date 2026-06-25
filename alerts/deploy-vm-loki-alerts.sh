#!/usr/bin/env bash
set -euo pipefail

TEMPLATE="${1:-vm-loki-failure-alerts-template.yaml}"

if [ ! -f "$TEMPLATE" ]; then
  echo "Template not found: $TEMPLATE" >&2
  exit 1
fi

namespaces="$(
  oc get vm -A \
    -o jsonpath='{range .items[?(@.metadata.annotations.vmobservability\.io/logging-enabled=="true")]}{.metadata.namespace}{"\n"}{end}' \
  | sort -u
)"

if [ -z "$namespaces" ]; then
  echo "No VM namespaces found with kubevirt-observability.io/logging-enabled=true"
  exit 0
fi

for ns in $namespaces; do
  echo "Applying Loki AlertingRule in namespace: $ns"

  sed "s/__NAMESPACE__/$ns/g" "$TEMPLATE" | oc apply -f -
done
