# KubeVirt Observability Operator

KubeVirt Observability Operator provides monitoring, logging, remediation, dashboards and alerting for Linux and Windows Virtual Machines running on OpenShift Virtualization (KubeVirt).

---

# Features

## Metrics

* Linux node_exporter
* Windows windows_exporter
* Automatic Service creation
* Automatic Endpoints creation
* Automatic ServiceMonitor creation
* OpenShift User Workload Monitoring integration

## Logging

* Grafana Alloy
* OpenShift Loki
* Linux syslog collection
* Windows Event Log collection
* Automatic VM log labeling

## Dashboards

* VM Infrastructure Dashboard
* VM Storage Dashboard
* VM Failure Dashboard

## Alerting

### Prometheus

* Exporter Down
* High CPU
* High Memory
* Disk Space Low
* High Disk Latency
* High IOPS
* High Throughput
* High Discard Rate
* Idle VMs
* No Disk I/O

### Loki

* Windows Event 129
* Windows BSOD
* Windows WER
* Linux Kernel Panic
* Filesystem Errors
* Disk I/O Errors
* Network Errors
* High Log Volume

## Remediation

### Linux

* SSH bootstrap validation
* node_exporter remediation
* Alloy remediation

### Windows

* OpenSSH bootstrap validation
* windows_exporter remediation
* Alloy remediation

---

# Architecture

VM
→ Exporter
→ Prometheus

VM
→ Alloy
→ Loki

Prometheus + Loki
→ Grafana

---

# Prerequisites

## OpenShift Virtualization

Install:

* OpenShift Virtualization
* OpenShift Monitoring
* OpenShift User Workload Monitoring
* OpenShift Logging / LokiStack

---

# Enable User Workload Monitoring

Verify:

```bash
oc get cm cluster-monitoring-config -n openshift-monitoring
```

Namespaces monitored by the operator must have:

```yaml
openshift.io/user-monitoring: "true"
```

The operator automatically applies this label.

---

# Configure Loki

Install:

```text
OpenShift Logging Operator
Loki Operator
LokiStack
```

Verify:

```bash
oc get lokistack -A
```

Verify application tenant:

```bash
oc get route -n openshift-logging
```

Expected route:

```text
logging-loki-openshift-logging
```

---

# Create Linux SSH Secrets

Generate key:

```bash
ssh-keygen -t rsa -b 4096
```

Create public key secret:

```bash
oc create secret generic lin-vm-mon-secret \
  -n kubevirt-observability-system \
  --from-file=id_rsa.pub
```

Create private key secret:

```bash
oc create secret generic lin-vm-mon-private \
  -n kubevirt-observability-system \
  --from-file=id_rsa \
  --from-literal=username=cloud-user
```

Adjust username for your Linux image.

---
# Create grafana service account for loki

Create:

```bash
oc -n openshift-logging create token grafana
```

Create Permission:

```bash
oc adm policy add-cluster-role-to-user \
  cluster-logging-application-view \
  -z grafana \
  -n openshift-logging

oc create clusterrolebinding grafana-write-application-logs \
  --clusterrole=cluster-logging-write-application-logs \
  --serviceaccount=openshift-logging:grafana
```

# Create Loki Writer Token Secret

Obtain token with permission to push logs.

Retrieve Token:

```bash
TOKEN=$(oc -n openshift-logging create token grafana)
```

Create:

```bash
oc create secret generic vm-loki-writer-token \
  -n kubevirt-observability-system \
  --from-literal=token=$TOKEN
```

Verify:

```bash
oc get secret vm-loki-writer-token \
  -n kubevirt-observability-system
```

# Provide permission to service account where grafana is running

```bash
oc create clusterrolebinding portworx-grafana-sa-application-view \
  --clusterrole=cluster-logging-application-view \
  --serviceaccount=<grafana-name-space>:<sa>
```

---

# Build Operator Image

Build:

```bash
docker build -t kubevirt-observability-operator:latest .
```

Push:

```bash
docker tag kubevirt-observability-operator:latest \
  <registry>/kubevirt-observability-operator:latest

docker push <registry>/kubevirt-observability-operator:latest
```

---

# Create Webhook certificate secret

```bash
NAMESPACE=kubevirt-observability-system
SERVICE=kubevirt-observability-webhook
SECRET=kubevirt-observability-webhook-certs
TMPDIR=$(mktemp -d)

cat > "${TMPDIR}/csr.conf" <<EOF
[req]
req_extensions = v3_req
distinguished_name = req_distinguished_name
prompt = no

[req_distinguished_name]
CN = ${SERVICE}.${NAMESPACE}.svc

[v3_req]
keyUsage = keyEncipherment, dataEncipherment
extendedKeyUsage = serverAuth
subjectAltName = @alt_names

[alt_names]
DNS.1 = ${SERVICE}
DNS.2 = ${SERVICE}.${NAMESPACE}
DNS.3 = ${SERVICE}.${NAMESPACE}.svc
DNS.4 = ${SERVICE}.${NAMESPACE}.svc.cluster.local
EOF

openssl req -x509 -nodes -newkey rsa:2048 \
  -keyout "${TMPDIR}/tls.key" \
  -out "${TMPDIR}/tls.crt" \
  -days 365 \
  -config "${TMPDIR}/csr.conf" \
  -extensions v3_req

oc delete secret "${SECRET}" -n "${NAMESPACE}" --ignore-not-found

oc create secret tls "${SECRET}" \
  -n "${NAMESPACE}" \
  --cert="${TMPDIR}/tls.crt" \
  --key="${TMPDIR}/tls.key"

oc rollout restart deployment/kubevirt-observability-operator -n "${NAMESPACE}"
```

---

# Deploy Operator

Create namespace:

```bash
oc apply -f config/namespace.yaml
```

Create service account:

```bash
oc apply -f config/serviceaccount.yaml
```

Create RBAC:

```bash
oc apply -f config/rbac.yaml
```

Create Service:

```bash
oc apply -f config/service.yaml
```

Create Mutating Webhook:

```bash
oc apply -f config/mutatingwebhook.yaml
```

Update Image: 

in config/deployment.yaml


Deploy:

```bash
oc apply -f config/deployment.yaml
```

Verify:

```bash
oc get pods -n kubevirt-observability-system
```

---

# Deploy Webhook

```bash
oc apply -f config/mutatingwebhook.yaml
```

Verify:

```bash
oc get mutatingwebhookconfiguration
```

---

# Test VMs

Deploy sample VMs:

```bash
oc apply -f test/rhel-vm.yaml
oc apply -f test/windows-vm.yaml
```

---

# Onboarding Existing Virtual Machines

The KubeVirt Observability Operator automatically configures monitoring for newly created VMs through cloud-init (Linux) and sysprep (Windows).

Existing VMs created before the operator was installed require a one-time onboarding process.

---

## Why Existing VMs Require Onboarding

New VMs receive the monitoring SSH public key during provisioning.

Existing VMs were already provisioned before the operator existed and therefore do not contain the monitoring SSH key required for remediation and Alloy installation.

Because cloud-init and sysprep normally execute only during initial provisioning, simply restarting an existing VM is usually not sufficient.

A one-time SSH key injection step is required.

---

# Step 1 - Label Existing VMs

Assign the correct operating system label.

Linux Example:

```bash
OS=rhel \
NAMESPACE=<namespace> \
VM_NAME=<vm-name> \
./scripts/label-vms.sh
```

Windows Example:

```bash
OS=windows \
NAMESPACE=<namespace> \
VM_NAME=<vm-name> \
./scripts/label-vms.sh
```

Examples:

```bash
OS=rhel ./scripts/label-vms.sh

OS=windows SELECTOR='app=my-vms' ./scripts/label-vms.sh
```

Verify:

```bash
oc get vm -A -L kubevirt.io/os
```

---

# Step 2 - Onboard Existing VMs

The onboarding script:

* Injects the monitoring SSH public key
* Enables monitoring annotations
* Marks the VM for remediation
* Triggers operator reconciliation

Linux Example:

```bash
LINUX_USER=root \
LINUX_PASS='<password>' \
NAMESPACE=<namespace> \
VM_NAME=<vm-name> \
./scripts/onboard-existing-vms.sh
```

Windows Example:

```bash
WINDOWS_USER=Administrator \
WINDOWS_PASS='<password>' \
NAMESPACE=<namespace> \
VM_NAME=<vm-name> \
./scripts/onboard-existing-vms.sh
```

Cluster-wide Example:

```bash
LINUX_USER=root \
LINUX_PASS='<password>' \
WINDOWS_USER=Administrator \
WINDOWS_PASS='<password>' \
./scripts/onboard-existing-vms.sh
```

The script automatically:

```text
Injects monitoring SSH public key
Adds monitoring annotations
Marks VM for remediation
Triggers operator reconciliation
```

---

# Step 3 - Validate SSH Access

Validate that the monitoring SSH key was successfully installed.

Linux Example:

```bash
NAMESPACE=<namespace> \
VM_NAME=<vm-name> \
./scripts/validate-ssh.sh
```

Cluster-wide Example:

```bash
./scripts/validate-ssh.sh
```

Successful output:

```text
OK namespace/vm-name
```

Failed VMs are written to:

```text
ssh-validation-failed.txt
```

Skipped VMs are written to:

```text
ssh-validation-skipped.txt
```

---

# Required Annotations

The onboarding script automatically applies:

```yaml
kubevirt-observability.io/bootstrap-managed: "true"

kubevirt-observability.io/logging-enabled: "true"

kubevirt-observability.io/remediation-required: "true"

kubevirt-observability.io/ssh-bootstrap-complete: "true"
```

No manual annotation management is required.

---

# Verify Monitoring Status

After remediation completes:

```bash
oc get vm -A \
  -o custom-columns='NAMESPACE:.metadata.namespace,VM:.metadata.name,STATUS:.metadata.annotations.vmobservability\.io/status,REASON:.metadata.annotations.vmobservability\.io/reason'
```

Expected:

```text
STATUS   REASON
ready    verified
```

---

# Notes

* Running VMs can be onboarded immediately.
* Stopped VMs are skipped because they do not have a VMI or IP address.
* Re-run onboarding after starting previously stopped VMs.
* New VMs created after the operator is installed do not require this onboarding process.
* Existing VMs only need onboarding once.

---

# Deploy Dashboards

Import dashboard JSON files into Grafana.

Required datasources:

* Prometheus
* Loki

---

# Deploy Prometheus Alerts

```bash
oc label ns kubevirt-observability-system \
  openshift.io/user-monitoring=true \
  --overwrite
```

```bash
chmod +x alerts/deploy-vm-metrics-alerts.sh

./alerts/deploy-vm-metrics-alerts.sh \
  alerts/vm-metrics-alerts-template.yaml
```

---

# Deploy Loki Alerts

```bash
chmod +x alerts/deploy-vm-loki-alerts.sh

./alerts/deploy-vm-loki-alerts.sh \
  alerts/vm-loki-failure-alerts-template.yaml
```

---

# Supported VM Labels

Linux:

```yaml
kubevirt.io/os: linux
```

Windows:

```yaml
kubevirt.io/os: window
```

Enable logging:

```yaml
kubevirt-observability.io/logging-enabled: "true"
```

---

# Repository Layout

```text
alerts/
api/
build/
cmd/
config/
controllers/
internal/
webhook/
```

---

# Troubleshooting

## Alloy

Linux:

```bash
systemctl status alloy
```

Windows:

```powershell
Get-Service Alloy
```

## Windows Event Logs

```powershell
Get-EventLog -LogName Application -Newest 50
```

## Loki Query

```logql
{kubernetes_namespace_name="<namespace>"}
```

## Exporters

Linux:

```bash
curl localhost:9100/metrics
```

Windows:

```powershell
curl.exe http://127.0.0.1:9182/metrics
```

---

# Disable Monitoring for a VM

The operator supports disabling monitoring for individual virtual machines.

When the following annotation is present:

```yaml
kubevirt-observability.io/disabled: "true"
```

the operator skips all monitoring automation for that VM.

This includes:

* Cloud-init merge
* Sysprep merge
* SSH key injection
* Exporter remediation
* Alloy remediation
* Service creation
* Endpoints creation
* ServiceMonitor creation
* Loki configuration
* Monitoring status updates

## Disable Monitoring

```bash
oc annotate vm <vm-name> \
  -n <namespace> \
  kubevirt-observability.io/disabled="true" \
  --overwrite
```

Example:

```bash
oc annotate vm win-mssql-vm \
  -n win-cloud-init-3 \
  kubevirt-observability.io/disabled="true" \
  --overwrite
```

## Re-enable Monitoring

Remove the annotation:

```bash
oc annotate vm <vm-name> \
  -n <namespace> \
  kubevirt-observability.io/disabled- \
  --overwrite
```

Example:

```bash
oc annotate vm win-mssql-vm \
  -n win-cloud-init-3 \
  kubevirt-observability.io/disabled- \
  --overwrite
```

## Verify

```bash
oc get vm <vm-name> \
  -n <namespace> \
  -o yaml | grep kubevirt-observability.io/disabled
```

If no output is returned, monitoring is enabled.

## Use Cases

Typical scenarios:

* Test VMs that should not be monitored
* Short-lived benchmark VMs
* VMs managed by another monitoring solution
* Temporary troubleshooting
* Preventing remediation during maintenance windows

---

# License

Apache 2.0

