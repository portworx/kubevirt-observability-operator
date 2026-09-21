package alerting

import (
	"context"
	"fmt"

	"k8s.io/client-go/kubernetes"
)

type Installer struct {
	client kubernetes.Interface
}

type Result struct {
	Enabled    bool
	Configured bool
	Reason     string
	SecretName string
	Namespace  string
}

func NewInstaller(
	client kubernetes.Interface,
) *Installer {
	return &Installer{
		client: client,
	}
}

func (i *Installer) Install(
	ctx context.Context,
	cfg Config,
) (Result, error) {
	result := Result{
		Enabled:    cfg.Enabled(),
		SecretName: cfg.SecretName,
		Namespace:  cfg.Namespace,
	}

	if !cfg.Enabled() {
		result.Reason =
			"KVO_SLACK_WEBHOOK_URL not provided"

		return result, nil
	}

	caps, err := discoverCapabilities(
		ctx,
		i.client,
	)
	if err != nil {
		// Alerting is optional. Do not fail platform deployment.
		result.Reason = fmt.Sprintf(
			"capability discovery failed: %v",
			err,
		)

		return result, nil
	}

	if !caps.UserWorkloadMonitoring {
		result.Reason =
			"user workload monitoring is not available"

		return result, nil
	}

	if !caps.AlertmanagerConfig {
		result.Reason =
			"AlertmanagerConfig API is not available"

		return result, nil
	}

	if err := enableUserWorkloadAlertmanager(
		ctx,
		i.client,
	); err != nil {
		// Alerting remains optional.
		result.Reason = fmt.Sprintf(
			"unable to enable user workload Alertmanager: %v",
			err,
		)

		return result, nil
	}

	if err := ensureSlackSecret(
		ctx,
		i.client,
		cfg,
	); err != nil {
		return result, fmt.Errorf(
			"configure Slack webhook secret: %w",
			err,
		)
	}

	result.Configured = true
	result.Reason = "Slack alerting enabled"

	return result, nil
}
