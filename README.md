# KubeVirt Virtual Machine Observability Platform

**KubeVirt Observability Operator** is the first component of the **KubeVirt Virtual Machine Observability Platform**. It provides end-to-end observability for Linux and Windows virtual machines running on **KubeVirt** and **Red Hat OpenShift Virtualization**.

The operator automates deployment and lifecycle management of observability components, enabling platform teams to collect metrics, logs, dashboards, and alerts using the cloud-native observability stack, including **Prometheus**, **Grafana**, **Grafana Alloy**, **Loki**, and **Alertmanager**.

---

## Why KubeVirt Observability Operator?

Modern virtual machine platforms require observability that extends beyond Kubernetes infrastructure metrics. Operators need visibility into guest operating systems to troubleshoot performance issues, storage bottlenecks, Windows Event Logs, Linux system logs, and application health.

KubeVirt Observability Operator automatically deploys and configures the required observability components inside Linux and Windows virtual machines while integrating with the Kubernetes observability ecosystem. It provides a consistent, automated approach to collecting metrics, logs, dashboards, and alerts across virtual machine workloads.

## Features

### Metrics

* Linux monitoring using **node_exporter**
* Windows monitoring using **windows_exporter**
* Automatic Prometheus integration
* VM-specific Prometheus alert rules
* Unified dashboards for Linux and Windows

### Logging

* Grafana Alloy deployment and configuration
* OpenShift Loki integration
* Linux system logs
* Windows Event Logs
* VM-specific Loki alert rules

### Dashboards

* Unified Grafana dashboards
* Prometheus metrics visualization
* Loki log exploration
* Linux and Windows support

### Alerting

* Prometheus alerts
* Loki alerts
* Alertmanager integration
* Slack notifications

### VM Bootstrap

* Linux Cloud-Init support
* Windows Sysprep support
* Existing VM onboarding
* Automatic observability bootstrap

---

# Architecture

```text
                     KubeVirt Cluster
                            │
          ┌─────────────────┴─────────────────┐
          │                                   │
      Linux VM                          Windows VM
          │                                   │
   node_exporter                     windows_exporter
          │                                   │
          └──────────────┬────────────────────┘
                         │
                   Grafana Alloy
                         │
               ┌─────────┴─────────┐
               │                   │
          Prometheus             Loki
               │                   │
               └─────────┬─────────┘
                         │
                     Grafana
                         │
                   Alertmanager
                         │
                       Slack
```

---

# Supported Components

| Component                | Status |
| ------------------------ | ------ |
| Linux VMs                | ✅      |
| Windows VMs              | ✅      |
| KubeVirt                 | ✅      |
| OpenShift Virtualization | ✅      |
| Prometheus               | ✅      |
| Grafana Alloy            | ✅      |
| Loki                     | ✅      |
| Grafana                  | ✅      |
| Alertmanager             | ✅      |
| Slack Notifications      | ✅      |

---

# Current Capabilities

* Automatic VM observability bootstrap
* Metrics collection
* Log collection
* Unified dashboards
* Prometheus alerting
* Loki alerting
* Existing VM onboarding
* Linux and Windows support

---

# Roadmap

## v0.2

* Observability Profiles

  * Metrics Only
  * Logs Only
  * Full Observability

* Filtered Windows Event Log collection

* Filtered Linux log collection

* Namespace-local secrets

* QEMU Guest Agent (QGA) transport

* SSH as optional fallback

## Future

* AI-assisted troubleshooting
* MCP integration
* Capacity planning
* VM rightsizing recommendations
* Performance analytics

---

# Quick Start

Clone the repository:

```bash
git clone https://github.com/portworx/kubevirt-observability-operator.git
cd kubevirt-observability-operator
```

Deploy the operator:

```bash
kubectl apply -f config/
```

Additional installation and configuration guides will be added in future releases.

---

# Documentation

Documentation is being expanded and will include:

* Installation Guide
* Existing VM Onboarding
* Linux Configuration
* Windows Configuration
* Dashboards
* Alerting
* Troubleshooting
* Architecture

---

# Contributing

Contributions are welcome.

Please read the project's `CONTRIBUTING.md` before submitting pull requests.

---

# Security

Please report security vulnerabilities privately to the project maintainers.

See `SECURITY.md` for reporting guidance.

---

# License

This project is licensed under the Apache License 2.0.

