package vmmonitoring

const (
	DefaultNamespace = "kubevirt-observability-system"
	DefaultConfigDir = "config"

	DefaultOperatorImage = "pure-artifactory.dev.purestorage.com/px-docker-dev-virtual/vm-monitoring-operator:0.1"

	DefaultSSHUsername = "root"

	DefaultLokiNamespace = "openshift-logging"
	DefaultLokiWriterSA  = "collector"

	LinuxPublicKeySecret  = "lin-vm-mon-secret"
	LinuxPrivateKeySecret = "lin-vm-mon-private"
	LokiWriterTokenSecret = "vm-loki-writer-token"

	OperatorDeployment = "kubevirt-observability-operator"
	WebhookSecret      = "kubevirt-observability-webhook-certs"
)

type Config struct {
	Namespace string
	ConfigDir string

	OperatorImage string
	SSHUsername   string

	LokiNamespace string
	LokiWriterSA  string
}

func DefaultConfig() Config {
	return Config{
		Namespace:     DefaultNamespace,
		ConfigDir:     DefaultConfigDir,
		OperatorImage: DefaultOperatorImage,
		SSHUsername:   DefaultSSHUsername,
		LokiNamespace: DefaultLokiNamespace,
		LokiWriterSA:  DefaultLokiWriterSA,
	}
}
