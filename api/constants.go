package api

// All annotations, state and service names

const (
	AnnDisableMonitoring            = "kubevirt-observability.io/disable-kubevirt-observability"
	AnnBootstrapManaged             = "kubevirt-observability.io/bootstrap-managed"
	AnnBootstrapOS                  = "kubevirt-observability.io/bootstrap-os"
	AnnBootstrapType                = "kubevirt-observability.io/bootstrap-type"
	AnnBootstrapVersion             = "kubevirt-observability.io/bootstrap-version"
	AnnStatus                       = "kubevirt-observability.io/status"
	AnnReason                       = "kubevirt-observability.io/reason"
	AnnExporter                     = "kubevirt-observability.io/exporter"
	AnnLegacyAlloy                  = "kubevirt-observability.io/alloy"
	AnnSysprepMergeRequired         = "kubevirt-observability.io/sysprep-merge-required"
	AnnSysprepMerged                = "kubevirt-observability.io/sysprep-merged"
	AnnSysprepMergeError            = "kubevirt-observability.io/sysprep-merge-error"
	AnnCloudInitSecretMergeRequired = "kubevirt-observability.io/cloudinit-secret-merge-required"
	AnnCloudInitSecretMerged        = "kubevirt-observability.io/cloudinit-secret-merged"
	AnnCloudInitSecretMergeError    = "kubevirt-observability.io/cloudinit-secret-merge-error"
	AnnBootstrapInlineSkipped       = "kubevirt-observability.io/bootstrap-inline-skipped"
	AnnRemediationRequired          = "kubevirt-observability.io/remediation-required"
	AnnRemediationReason            = "kubevirt-observability.io/remediation-reason"
	AnnRemediationCompleted         = "kubevirt-observability.io/remediation-completed"
	AnnRemediationError             = "kubevirt-observability.io/remediation-error"
	AnnLoggingEnabled               = "kubevirt-observability.io/logging-enabled"
	AnnAlloyInstalled               = "kubevirt-observability.io/alloy-installed"
	AnnAlloyConfigHash              = "kubevirt-observability.io/alloy-config-hash"
	AnnAlloyError                   = "kubevirt-observability.io/alloy-error"
	AnnLoggingStatus                = "kubevirt-observability.io/logging-status"
	AnnLinuxSSHUser                 = "kubevirt-observability.io/linux-ssh-user"
	AnnWindowsSSHEnabled            = "kubevirt-observability.io/windows-ssh-enabled"
	WindowsBootstrapDoneMarker      = `C:\kubevirt-observability\bootstrap.done`
	WindowsBootstrapFailedMarker    = `C:\kubevirt-observability\bootstrap.failed`

	StatusPending = "pending"
	StatusReady   = "ready"
	StatusFailed  = "failed"
	StatusSkipped = "skipped"

	LinuxServiceName     = "vm-linux-monitoring-service"
	WindowsScheduledTask = "KubeVirt Observability Bootstrap"
	LinuxSSHSecretName   = "lin-vm-mon-secret"

	BootstrapVersion = "v1"
)
