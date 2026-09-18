package grafana

import (
	"context"
	"fmt"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

type Installer struct {
	kube    kubernetes.Interface
	dynamic dynamic.Interface
}

func NewInstaller(
	kube kubernetes.Interface,
	dynamicClient dynamic.Interface,
) *Installer {
	return &Installer{
		kube:    kube,
		dynamic: dynamicClient,
	}
}

func (i *Installer) Install(
	ctx context.Context,
	cfg Config,
) (DiscoveryResult, error) {
	applyDefaults(&cfg)

	/*
	   Discover whether Grafana already exists.

	   The Grafana Deployment namespace and the Loki/Prometheus
	   query identity are deliberately separate concepts.

	   Grafana may already run in namespaces such as "portworx",
	   but KubeVirt Observability uses grafana/grafana as the
	   canonical datasource query identity because the VM controller
	   grants namespace Loki access to that ServiceAccount.
	*/
	discovery, err := Discover(
		ctx,
		i.kube,
	)
	if err != nil {
		return DiscoveryResult{}, fmt.Errorf(
			"discover Grafana: %w",
			err,
		)
	}

	existingGrafana := discovery.Found

	lokiURL, err := DiscoverLokiDatasourceURL(
		ctx,
		i.dynamic,
	)
	if err != nil {
		return DiscoveryResult{}, fmt.Errorf(
			"discover Loki datasource URL: %w",
			err,
		)
	}

	cfg.LokiURL = lokiURL

	if existingGrafana {
		// Reuse the namespace where Grafana is already running.
		cfg.Namespace = discovery.Namespace
	}

	/*
	   The tokens are always issued for grafana/grafana.

	   The resulting Secrets are stored in cfg.Namespace, which may
	   be a different namespace for an existing Grafana Deployment.
	*/
	cfg.PrometheusTokenNamespace = DefaultNamespace
	cfg.PrometheusTokenServiceAccount = GrafanaServiceAccount

	cfg.LokiTokenNamespace = DefaultNamespace
	cfg.LokiTokenServiceAccount = GrafanaServiceAccount

	/*
	   Ensure the canonical Grafana authentication namespace exists.

	   This is required even when the actual Grafana Deployment
	   already exists elsewhere.
	*/
	if err := EnsureNamespace(
		ctx,
		i.kube,
		DefaultNamespace,
	); err != nil {
		return DiscoveryResult{}, fmt.Errorf(
			"ensure Grafana authentication namespace: %w",
			err,
		)
	}

	/*
	   Ensure grafana/grafana exists and has Prometheus access.

	   Use a separate config here because cfg.Namespace may point
	   to an existing Grafana Deployment such as portworx/grafana.
	*/
	authConfig := cfg
	authConfig.Namespace = DefaultNamespace

	if err := EnsureServiceAccountAndRBAC(
		ctx,
		i.kube,
		authConfig,
	); err != nil {
		return DiscoveryResult{}, fmt.Errorf(
			"ensure Grafana authentication ServiceAccount/RBAC: %w",
			err,
		)
	}

	/*
	   If kvoctl is installing Grafana itself, the deployment
	   namespace is also grafana and already exists above.

	   If Grafana already exists elsewhere, preserve that namespace.
	*/
	if err := EnsureNamespace(
		ctx,
		i.kube,
		cfg.Namespace,
	); err != nil {
		return DiscoveryResult{}, fmt.Errorf(
			"ensure Grafana deployment namespace: %w",
			err,
		)
	}

	/*
	   Merge KVO datasource provisioning into the existing
	   ConfigMap without deleting existing datasource definitions.
	*/
	if err := EnsureDatasourceConfigMap(
		ctx,
		i.kube,
		cfg,
	); err != nil {
		return DiscoveryResult{}, fmt.Errorf(
			"ensure Grafana datasource ConfigMap: %w",
			err,
		)
	}

	/*
	   Merge KVO dashboard provider and dashboards.

	   Existing Grafana dashboards remain untouched.
	*/
	if err := EnsureDashboardConfigMaps(
		ctx,
		i.kube,
		cfg,
	); err != nil {
		return DiscoveryResult{}, fmt.Errorf(
			"ensure Grafana dashboard ConfigMaps: %w",
			err,
		)
	}

	/*
	   Create one-year Prometheus and Loki tokens for grafana/grafana.

	   The Secret objects themselves are written into cfg.Namespace
	   so the actual Grafana Deployment can reference them.
	*/
	if err := EnsureDatasourceTokenSecrets(
		ctx,
		i.kube,
		cfg,
	); err != nil {
		return DiscoveryResult{}, fmt.Errorf(
			"ensure Grafana datasource token Secrets: %w",
			err,
		)
	}

	if !existingGrafana {
		if err := EnsureService(
			ctx,
			i.kube,
			cfg,
		); err != nil {
			return DiscoveryResult{}, fmt.Errorf(
				"ensure Grafana Service: %w",
				err,
			)
		}
	}

	/*
	   Existing Deployment:

	   	merge only KVO env vars, mounts and volumes.

	   New Deployment:

	   	create the full Grafana Deployment.
	*/
	if err := EnsureDeployment(
		ctx,
		i.kube,
		cfg,
	); err != nil {
		return DiscoveryResult{}, fmt.Errorf(
			"ensure Grafana Deployment: %w",
			err,
		)
	}

	/*
	   Only create networking when kvoctl owns the Grafana
	   installation. Existing Grafana networking is preserved.
	*/
	if !existingGrafana {
		if err := EnsureRoute(
			ctx,
			i.dynamic,
			cfg,
		); err != nil {
			return DiscoveryResult{}, fmt.Errorf(
				"ensure Grafana Route: %w",
				err,
			)
		}
	}

	return DiscoveryResult{
		Found:      true,
		Namespace:  cfg.Namespace,
		Deployment: GrafanaDeploymentName,
	}, nil
}
