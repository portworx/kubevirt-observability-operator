# kvoctl

`kvoctl` is the deployment CLI for the KubeVirt Observability Platform.

It validates cluster prerequisites and deploys the components required for
KubeVirt VM metrics, logs, dashboards, and alerting.

## Prerequisites

Before running `kvoctl`, ensure:

- Access to the target OpenShift cluster.
- The current kubeconfig context points to the correct cluster.
- KubeVirt / OpenShift Virtualization is available.
- A usable StorageClass is available.
- S3-compatible object storage is available for Loki.
- The current user has sufficient deployment permissions.

Run the preflight check before deployment:

    kvoctl preflight

The preflight check validates:

- Cluster connectivity.
- Current kubeconfig context.
- OpenShift availability and version.
- KubeVirt availability.
- OpenShift Virtualization availability.
- StorageClass discovery.
- Platform operator status.
- Existing Grafana availability.
- Deployment permissions.

OpenShift Logging and Loki operators do not need to be installed manually.
`kvoctl deploy` can install the required operators when they are missing.

If an existing Grafana deployment is discovered, `kvoctl` reuses it.

## Environment Variables

### Required for non-interactive deployment

    export LOKI_S3_BUCKET=<bucket>
    export LOKI_S3_ACCESS_KEY=<access-key>
    export LOKI_S3_SECRET_KEY=<secret-key>

### S3 configuration

    export LOKI_S3_ENDPOINT=<endpoint>
    export LOKI_S3_REGION=<region>

`LOKI_S3_REGION` may be left empty when the object-storage provider does not
require a region.

### Optional platform configuration

    export KVO_STORAGE_CLASS=<storage-class>
    export KVO_OPERATOR_IMAGE=<operator-image>
    export KVO_CONFIG_DIR=<config-directory>
    export KVO_LOKI_RETENTION_DAYS=<1-30>

`KVO_STORAGE_CLASS` overrides the StorageClass selected during cluster
discovery.

`KVO_OPERATOR_IMAGE` overrides the KubeVirt Observability Operator image.

`KVO_CONFIG_DIR` overrides the operator configuration directory.

`KVO_LOKI_RETENTION_DAYS` configures Loki retention. The supported range is
1 through 30 days.

### Optional Slack integration

    export KVO_SLACK_WEBHOOK_URL=<slack-webhook-url>

When `KVO_SLACK_WEBHOOK_URL` is not provided, deployment continues without
Slack notification configuration.

## Commands

### Preflight

    kvoctl preflight

Checks whether the target cluster is ready for deployment.

### Deploy

    kvoctl deploy

The deploy command runs preflight automatically before starting deployment.

### Version

    kvoctl version

Displays the kvoctl version, Git commit, and build date.

## Installing kvoctl

Prebuilt `kvoctl` binaries are published with GitHub releases for the
following platforms:

| Operating system | Architecture | Binary |
| --- | --- | --- |
| Linux | amd64 | `kvoctl-linux-amd64` |
| Linux | arm64 | `kvoctl-linux-arm64` |
| macOS | Intel (amd64) | `kvoctl-darwin-amd64` |
| macOS | Apple Silicon (arm64) | `kvoctl-darwin-arm64` |

Download the appropriate binary from the project releases page:

https://github.com/portworx/kubevirt-observability-operator/releases

### macOS Apple Silicon

Set the release version to install:

    VERSION=<release-version>

For example:

    VERSION=v0.2.0

Download the binary:

    curl -fL       -o kvoctl       "https://github.com/portworx/kubevirt-observability-operator/releases/download/${VERSION}/kvoctl-darwin-arm64"

Install it:

    chmod +x kvoctl
    sudo mv kvoctl /usr/local/bin/kvoctl

### macOS Intel

    VERSION=<release-version>

    curl -fL       -o kvoctl       "https://github.com/portworx/kubevirt-observability-operator/releases/download/${VERSION}/kvoctl-darwin-amd64"

    chmod +x kvoctl
    sudo mv kvoctl /usr/local/bin/kvoctl

### Linux amd64

    VERSION=<release-version>

    curl -fL       -o kvoctl       "https://github.com/portworx/kubevirt-observability-operator/releases/download/${VERSION}/kvoctl-linux-amd64"

    chmod +x kvoctl
    sudo mv kvoctl /usr/local/bin/kvoctl

### Linux arm64

    VERSION=<release-version>

    curl -fL       -o kvoctl       "https://github.com/portworx/kubevirt-observability-operator/releases/download/${VERSION}/kvoctl-linux-arm64"

    chmod +x kvoctl
    sudo mv kvoctl /usr/local/bin/kvoctl

### Verify the installation

Run:

    kvoctl version

Then validate the target cluster:

    kvoctl preflight

### Verify the checksum

Each release includes `SHA256SUMS`.

Download it from the same release and verify the downloaded binary before
installation.

On Linux:

    sha256sum -c SHA256SUMS --ignore-missing

On macOS:

    shasum -a 256 kvoctl

Compare the result with the corresponding entry in `SHA256SUMS`.

## Building kvoctl

Build binaries for all supported platforms:

    ./scripts/build-kvoctl.sh

The build creates:

    dist/
    ├── kvoctl-linux-amd64
    ├── kvoctl-linux-arm64
    ├── kvoctl-darwin-amd64
    ├── kvoctl-darwin-arm64
    └── SHA256SUMS

## Example Deployment

    export LOKI_S3_BUCKET=kvo-loki
    export LOKI_S3_ENDPOINT=https://s3.example.com
    export LOKI_S3_REGION=us-west-2
    export LOKI_S3_ACCESS_KEY='<access-key>'
    export LOKI_S3_SECRET_KEY='<secret-key>'

    export KVO_STORAGE_CLASS=px-csi-db

    # Optional
    export KVO_LOKI_RETENTION_DAYS=7
    export KVO_SLACK_WEBHOOK_URL='https://hooks.slack.com/services/...'

    kvoctl preflight
    kvoctl deploy

## Service Account Tokens

`kvoctl` requests one-year ServiceAccount tokens for observability access.

Managed token Secrets contain metadata including:

    app.kubernetes.io/managed-by=kvoctl
    kvo.portworx.io/token-expiration=<expiration>
    kvo.portworx.io/token-service-account=<namespace/service-account>

Repeated `kvoctl deploy` operations are idempotent:

- A valid, unexpired token is reused.
- Missing management metadata is repaired without rotating a valid token.
- An expired token is replaced with a new token.
- Missing or invalid expiration metadata causes a new token to be generated.

## Validation

Check the KubeVirt Observability Operator:

    oc get pods -n kubevirt-observability-system

Check Loki:

    oc get lokistack -n openshift-logging

Check VM alerting resources:

    oc get prometheusrule -A
    oc get alertingrule.loki.grafana.com -A
    oc get alertmanagerconfig -A

## Security

S3 credentials and Slack webhook URLs are sensitive.

Do not commit credentials or webhook URLs to source control.
