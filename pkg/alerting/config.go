package alerting

import (
	"os"
	"strings"
)

const (
	DefaultNamespace  = "kubevirt-observability-system"
	DefaultSecretName = "kvo-slack-webhook"
	SlackSecretKey    = "url"

	UserWorkloadNamespace = "openshift-user-workload-monitoring"
	UserWorkloadConfigMap = "user-workload-monitoring-config"
)

type Config struct {
	Namespace       string
	SecretName      string
	SlackWebhookURL string
}

func DefaultConfig() Config {
	return Config{
		Namespace:       DefaultNamespace,
		SecretName:      DefaultSecretName,
		SlackWebhookURL: strings.TrimSpace(os.Getenv("KVO_SLACK_WEBHOOK_URL")),
	}
}

func (c Config) Enabled() bool {
	return strings.TrimSpace(c.SlackWebhookURL) != ""
}
