package deploy

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/portworx/kubevirt-observability-operator/pkg/grafana"
	"github.com/portworx/kubevirt-observability-operator/pkg/logging"
	"github.com/portworx/kubevirt-observability-operator/pkg/loki"
	"github.com/portworx/kubevirt-observability-operator/pkg/operators"
	"github.com/portworx/kubevirt-observability-operator/pkg/platform"
	"github.com/portworx/kubevirt-observability-operator/pkg/vmmonitoring"
)

const (
	redHatCatalog = "redhat-operators"

	loggingPackage   = "cluster-logging"
	loggingNamespace = "openshift-logging"

	lokiPackage   = "loki-operator"
	lokiNamespace = "openshift-operators-redhat"

	lokiStackName            = "logging-loki"
	lokiStorageSecret        = "loki-s3"
	defaultLokiSize          = "1x.medium"
	defaultSchema            = "v13"
	defaultSchemaDate        = "2024-04-02"
	defaultLokiRetentionDays = 7
	maxLokiRetentionDays     = 30
)

// Run starts deployment of the KubeVirt Observability Platform.
func Run(
	ctx context.Context,
	in io.Reader,
	out io.Writer,
) error {
	fmt.Fprintln(out, "KubeVirt Observability Platform Deployment")
	fmt.Fprintln(out)

	cfg, err := LoadConfig(in, out)
	if err != nil {
		return fmt.Errorf("load deployment configuration: %w", err)
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, "Deployment configuration:")
	fmt.Fprintf(out, "  S3 bucket                       %s\n", cfg.S3.Bucket)

	if cfg.S3.Endpoint != "" {
		fmt.Fprintf(out, "  S3 endpoint                     %s\n", cfg.S3.Endpoint)
	} else {
		fmt.Fprintln(out, "  S3 endpoint                     AWS default")
	}

	if cfg.S3.Region != "" {
		fmt.Fprintf(out, "  S3 region                       %s\n", cfg.S3.Region)
	}

	fmt.Fprintln(out, "  S3 credentials                  PROVIDED")
	fmt.Fprintln(out)

	fmt.Fprintln(out, "Running cluster preflight...")
	fmt.Fprintln(out)

	if err := RunPreflight(ctx, out); err != nil {
		return fmt.Errorf("preflight failed: %w", err)
	}

	clients, err := platform.NewClients()
	if err != nil {
		return fmt.Errorf("create Kubernetes clients: %w", err)
	}

	clusterInfo, err := platform.DiscoverCluster(ctx, clients)
	if err != nil {
		return fmt.Errorf("discover cluster: %w", err)
	}

	storageClass := cfg.StorageClass
	if storageClass == "" {
		storageClass = clusterInfo.RecommendedStorageClass
	}

	if storageClass == "" {
		return fmt.Errorf(
			"no StorageClass was selected or discovered",
		)
	}

	retentionDays := defaultLokiRetentionDays

	if raw := os.Getenv("KVO_LOKI_RETENTION_DAYS"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			return fmt.Errorf(
				"invalid KVO_LOKI_RETENTION_DAYS %q: expected an integer",
				raw,
			)
		}

		if value < 1 || value > maxLokiRetentionDays {
			return fmt.Errorf(
				"invalid KVO_LOKI_RETENTION_DAYS %d: supported range is 1-%d days",
				value,
				maxLokiRetentionDays,
			)
		}

		retentionDays = value
	}

	operatorInstaller := operators.NewInstaller(
		clients.Kube,
		clients.Dynamic,
	)

	fmt.Fprintln(out)
	fmt.Fprintln(out, "Discovering required operators...")

	loggingInfo, err := operatorInstaller.DiscoverPackage(
		ctx,
		loggingPackage,
		redHatCatalog,
	)
	if err != nil {
		return fmt.Errorf(
			"discover OpenShift Logging Operator: %w",
			err,
		)
	}

	fmt.Fprintf(
		out,
		"  %-32s %-14s %s\n",
		"OpenShift Logging Operator",
		"FOUND",
		loggingInfo.DefaultChannel,
	)

	lokiInfo, err := operatorInstaller.DiscoverPackage(
		ctx,
		lokiPackage,
		redHatCatalog,
	)
	if err != nil {
		return fmt.Errorf(
			"discover Loki Operator: %w",
			err,
		)
	}

	fmt.Fprintf(
		out,
		"  %-32s %-14s %s\n",
		"Loki Operator",
		"FOUND",
		lokiInfo.DefaultChannel,
	)

	fmt.Fprintln(out)
	fmt.Fprintln(out, "Ensuring required operators...")

	if err := operatorInstaller.EnsureInstalled(
		ctx,
		operators.InstallConfig{
			Package:           loggingPackage,
			TargetNamespace:   loggingNamespace,
			OperatorGroupName: "cluster-logging",
			CatalogSource:     loggingInfo.CatalogSource,
			CatalogNamespace:  loggingInfo.CatalogSourceNamespace,
			Channel:           loggingInfo.DefaultChannel,
		},
	); err != nil {
		return fmt.Errorf(
			"install OpenShift Logging Operator: %w",
			err,
		)
	}

	fmt.Fprintf(
		out,
		"  %-32s %-14s %s\n",
		"OpenShift Logging Operator",
		"ENSURED",
		loggingInfo.DefaultChannel,
	)

	if err := operatorInstaller.EnsureInstalled(
		ctx,
		operators.InstallConfig{
			Package:           lokiPackage,
			TargetNamespace:   lokiNamespace,
			OperatorGroupName: "loki-operator",
			CatalogSource:     lokiInfo.CatalogSource,
			CatalogNamespace:  lokiInfo.CatalogSourceNamespace,
			Channel:           lokiInfo.DefaultChannel,
		},
	); err != nil {
		return fmt.Errorf(
			"install Loki Operator: %w",
			err,
		)
	}

	fmt.Fprintf(
		out,
		"  %-32s %-14s %s\n",
		"Loki Operator",
		"ENSURED",
		lokiInfo.DefaultChannel,
	)

	fmt.Fprintln(out)
	fmt.Fprintln(out, "Waiting for operators to become ready...")

	if err := operators.WaitForReady(
		ctx,
		clients.Dynamic,
		operators.WaitConfig{
			Package:   loggingPackage,
			Namespace: loggingNamespace,
			Timeout:   15 * time.Minute,
			Interval:  5 * time.Second,
		},
	); err != nil {
		return fmt.Errorf(
			"wait for OpenShift Logging Operator: %w",
			err,
		)
	}

	fmt.Fprintf(
		out,
		"  %-32s %-14s\n",
		"OpenShift Logging Operator",
		"READY",
	)

	if err := operators.WaitForReady(
		ctx,
		clients.Dynamic,
		operators.WaitConfig{
			Package:   lokiPackage,
			Namespace: lokiNamespace,
			Timeout:   15 * time.Minute,
			Interval:  5 * time.Second,
		},
	); err != nil {
		return fmt.Errorf(
			"wait for Loki Operator: %w",
			err,
		)
	}

	fmt.Fprintf(
		out,
		"  %-32s %-14s\n",
		"Loki Operator",
		"READY",
	)

	fmt.Fprintln(out)
	fmt.Fprintln(out, "Configuring Loki storage...")

	lokiInstaller := loki.NewInstaller(
		clients.Kube,
		clients.Dynamic,
	)

	lokiConfig := loki.Config{
		Name:          lokiStackName,
		Namespace:     loggingNamespace,
		Size:          defaultLokiSize,
		StorageClass:  storageClass,
		RetentionDays: retentionDays,
		StorageSecret: lokiStorageSecret,
		SchemaVersion: defaultSchema,
		SchemaDate:    defaultSchemaDate,
		S3: loki.S3Config{
			Bucket:    cfg.S3.Bucket,
			Endpoint:  cfg.S3.Endpoint,
			Region:    cfg.S3.Region,
			AccessKey: cfg.S3.AccessKey,
			SecretKey: cfg.S3.SecretKey,
		},
	}

	if err := lokiInstaller.EnsureStorageSecret(
		ctx,
		lokiConfig,
	); err != nil {
		return fmt.Errorf(
			"configure Loki S3 Secret: %w",
			err,
		)
	}

	fmt.Fprintf(
		out,
		"  %-32s %-14s %s/%s\n",
		"Loki S3 Secret",
		"READY",
		loggingNamespace,
		lokiStorageSecret,
	)

	fmt.Fprintln(out)
	fmt.Fprintln(out, "Deploying LokiStack...")

	if err := lokiInstaller.EnsureLokiStack(
		ctx,
		lokiConfig,
	); err != nil {
		return fmt.Errorf(
			"deploy LokiStack: %w",
			err,
		)
	}

	fmt.Fprintf(
		out,
		"  %-32s %-14s %s\n",
		"LokiStack",
		"DEPLOYED",
		lokiStackName,
	)

	fmt.Fprintln(out)
	fmt.Fprintln(out, "Waiting for LokiStack to become ready...")

	if err := lokiInstaller.WaitForReady(
		ctx,
		loggingNamespace,
		lokiStackName,
		20*time.Minute,
	); err != nil {
		return fmt.Errorf(
			"wait for LokiStack: %w",
			err,
		)
	}

	fmt.Fprintf(
		out,
		"  %-32s %-14s %s\n",
		"LokiStack",
		"READY",
		lokiStackName,
	)

	fmt.Fprintln(out)
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Loki deployment completed successfully.")
	fmt.Fprintln(
		out,
		"ClusterLogForwarder deployment will run in the next stage.",
	)

	fmt.Fprintln(out)
	fmt.Fprintln(out, "Configuring log collector...")

	loggingInstaller := logging.NewInstaller(
		clients.Kube,
		clients.Dynamic,
	)

	loggingConfig := logging.Config{
		Name:                   "vm-log-forwarder",
		Namespace:              loggingNamespace,
		ServiceAccountName:     "collector",
		LokiStackName:          lokiStackName,
		LokiStackNamespace:     loggingNamespace,
		MaxRecordsPerSecond:    200,
		CollectorCPURequest:    "1",
		CollectorMemoryRequest: "1Gi",
		CollectorCPULimit:      "6",
		CollectorMemoryLimit:   "4Gi",
	}

	if err := loggingInstaller.EnsureCollectorServiceAccount(
		ctx,
		loggingConfig,
	); err != nil {
		return fmt.Errorf(
			"create collector ServiceAccount: %w",
			err,
		)
	}

	fmt.Fprintf(
		out,
		"  %-32s %-14s %s/%s\n",
		"Collector ServiceAccount",
		"READY",
		loggingNamespace,
		loggingConfig.ServiceAccountName,
	)

	if err := loggingInstaller.EnsureCollectorRBAC(
		ctx,
		loggingConfig,
	); err != nil {
		return fmt.Errorf(
			"configure collector RBAC: %w",
			err,
		)
	}

	fmt.Fprintf(
		out,
		"  %-32s %-14s\n",
		"Collector RBAC",
		"READY",
	)

	fmt.Fprintln(out)
	fmt.Fprintln(out, "Deploying ClusterLogForwarder...")

	if err := loggingInstaller.EnsureClusterLogForwarder(
		ctx,
		loggingConfig,
	); err != nil {
		return fmt.Errorf(
			"deploy ClusterLogForwarder: %w",
			err,
		)
	}

	fmt.Fprintf(
		out,
		"  %-32s %-14s %s\n",
		"ClusterLogForwarder",
		"DEPLOYED",
		loggingConfig.Name,
	)

	fmt.Fprintln(
		out,
		"Waiting for ClusterLogForwarder to become ready...",
	)

	if err := loggingInstaller.WaitForReady(
		ctx,
		loggingNamespace,
		loggingConfig.Name,
		10*time.Minute,
	); err != nil {
		return fmt.Errorf(
			"wait for ClusterLogForwarder: %w",
			err,
		)
	}

	fmt.Fprintf(
		out,
		"  %-32s %-14s %s\n",
		"ClusterLogForwarder",
		"READY",
		loggingConfig.Name,
	)

	fmt.Fprintln(out)
	fmt.Fprintln(
		out,
		"Loki and OpenShift log forwarding deployment completed successfully.",
	)

	fmt.Fprintln(out)
	fmt.Fprintln(out, "Configuring Loki query access...")

	if err := grafana.EnsureNamespace(
		ctx,
		clients.Kube,
		grafana.DefaultNamespace,
	); err != nil {
		return fmt.Errorf(
			"ensure Grafana namespace for Loki query access: %w",
			err,
		)
	}

	grafanaRBAC := grafana.NewRBACInstaller(
		clients.Kube,
	)

	if err := grafanaRBAC.EnsureReaderFoundation(
		ctx,
		grafana.DefaultNamespace,
	); err != nil {
		return fmt.Errorf(
			"configure Loki reader RBAC: %w",
			err,
		)
	}

	fmt.Fprintf(
		out,
		"  %-32s %-14s %s/%s\n",
		"Loki Reader ServiceAccount",
		"READY",
		grafana.DefaultNamespace,
		grafana.DefaultReaderServiceAccount,
	)

	fmt.Fprintf(
		out,
		"  %-32s %-14s\n",
		"Loki Application Reader",
		"READY",
	)

	fmt.Fprintln(out)
	fmt.Fprintln(out, "Configuring Grafana...")

	grafanaInstaller := grafana.NewInstaller(
		clients.Kube,
		clients.Dynamic,
	)

	grafanaConfig := grafana.DefaultConfig()

	grafanaResult, err := grafanaInstaller.Install(
		ctx,
		grafanaConfig,
	)
	if err != nil {
		return fmt.Errorf(
			"deploy Grafana observability dashboards: %w",
			err,
		)
	}

	fmt.Fprintf(
		out,
		"  %-32s %-14s %s/%s\n",
		"Grafana",
		"READY",
		grafanaResult.Namespace,
		grafanaResult.Deployment,
	)

	fmt.Fprintf(
		out,
		"  %-32s %-14s %s\n",
		"Prometheus Datasource",
		"READY",
		grafana.DefaultPrometheusUID,
	)

	fmt.Fprintf(
		out,
		"  %-32s %-14s %s\n",
		"Loki Datasource",
		"READY",
		grafana.DefaultLokiUID,
	)

	fmt.Fprintf(
		out,
		"  %-32s %-14s %s\n",
		"VM Metrics Dashboard",
		"READY",
		"vm-monitoring-production-v6.json",
	)

	fmt.Fprintf(
		out,
		"  %-32s %-14s %s\n",
		"VM Loki Dashboard",
		"READY",
		"vm-loki-failures-v6.json",
	)

	fmt.Fprintln(out)

	fmt.Fprintln(out, "Deploying KubeVirt Observability Operator...")

	vmInstaller := vmmonitoring.NewInstaller(
		clients.Kube,
	)

	vmConfig := vmmonitoring.DefaultConfig()

	if image := os.Getenv("KVO_OPERATOR_IMAGE"); image != "" {
		vmConfig.OperatorImage = image
	}

	if configDir := os.Getenv("KVO_CONFIG_DIR"); configDir != "" {
		vmConfig.ConfigDir = configDir
	}
	if err := vmInstaller.Install(
		ctx,
		vmConfig,
	); err != nil {
		return fmt.Errorf(
			"deploy KubeVirt Observability Operator: %w",
			err,
		)
	}

	fmt.Fprintf(
		out,
		"  %-32s %-14s %s\n",
		"VM Monitoring Namespace",
		"READY",
		vmConfig.Namespace,
	)

	fmt.Fprintf(
		out,
		"  %-32s %-14s %s\n",
		"Linux SSH Credentials",
		"READY",
		vmConfig.SSHUsername,
	)

	fmt.Fprintf(
		out,
		"  %-32s %-14s %s/%s\n",
		"Loki Token ServiceAccount",
		"READY",
		vmConfig.LokiNamespace,
		vmConfig.LokiWriterSA,
	)

	fmt.Fprintf(
		out,
		"  %-32s %-14s %s/%s\n",
		"Loki Writer Token",
		"READY",
		vmConfig.Namespace,
		vmmonitoring.LokiWriterTokenSecret,
	)

	fmt.Fprintf(
		out,
		"  %-32s %-14s %s\n",
		"Operator Image",
		"CONFIGURED",
		vmConfig.OperatorImage,
	)

	fmt.Fprintln(out)
	fmt.Fprintln(
		out,
		"Waiting for KubeVirt Observability Operator to become ready...",
	)

	if err := vmInstaller.WaitForReady(
		ctx,
		vmConfig,
		5*time.Minute,
	); err != nil {
		return fmt.Errorf(
			"wait for KubeVirt Observability Operator: %w",
			err,
		)
	}

	fmt.Fprintf(
		out,
		"  %-32s %-14s %s\n",
		"KubeVirt Observability Operator",
		"READY",
		vmConfig.OperatorImage,
	)

	fmt.Fprintln(out)
	fmt.Fprintln(
		out,
		"KubeVirt Observability Platform deployment completed successfully.",
	)

	return nil
}
