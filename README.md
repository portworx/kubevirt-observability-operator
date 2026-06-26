# KubeVirt Observability Operator

KubeVirt Observability Operator provides end-to-end observability for Linux and Windows Virtual Machines running on **KubeVirt** and **Red Hat OpenShift Virtualization**.

It automates deployment and configuration of metrics, logs, dashboards, and alerting so platform teams can monitor virtual machines using the cloud-native observability stack.

---

## Why KubeVirt Observability Operator?

Operating virtual machines at scale requires visibility into guest operating systems, not just the Kubernetes infrastructure.

This operator automates observability for Linux and Windows virtual machines by deploying and configuring the required monitoring components, integrating with Prometheus, Loki, Grafana, and Alertmanager.

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
                 +------------------------------+
                 | KubeVirt Virtual Machines    |
                 +--------------+---------------+
                                |
              +-----------------+-----------------+
              |                                   |
         Linux VM                           Windows VM
              |                                   |
       node_exporter                    windows_exporter
              |                                   |
              +---------------+-------------------+
                              |
                        Grafana Alloy
                              |
                             Loki
                              |
                        Prometheus
                              |
               +--------------+--------------+
               |                             |
            Grafana                   Alertmanager
                                              |
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

