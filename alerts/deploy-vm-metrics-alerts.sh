#!/usr/bin/env bash
set -euo pipefail

TEMPLATE="${1:-alerts/vm-metrics-alerts-template.yaml}"

if [ ! -f "$TEMPLATE" ]; then
  echo "Template not found: $TEMPLATE"
  exit 1
fi

for ns in $(oc get vm -A \
  -o jsonpath='{range .items[?(@.metadata.annotations.vmobservability\.io/bootstrap-managed=="true")]}{.metadata.namespace}{"\n"}{end}' \
  | sort -u); do

  echo "Applying PrometheusRule in namespace: $ns"

  oc label namespace "$ns" \
    openshift.io/user-monitoring=true \
    --overwrite >/dev/null

  sed "s/__NAMESPACE__/${ns}/g" "$TEMPLATE" | oc apply -n "$ns" -f -
done
