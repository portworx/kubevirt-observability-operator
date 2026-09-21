package deploy

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/portworx/kubevirt-observability-operator/pkg/grafana"
	"github.com/portworx/kubevirt-observability-operator/pkg/operators"
	"github.com/portworx/kubevirt-observability-operator/pkg/platform"
)

// RunPreflight checks the target cluster and prints a deployment readiness
// summary. It does not modify the cluster.
func RunPreflight(ctx context.Context, out io.Writer) error {
	fmt.Fprintln(out, "KubeVirt Observability Platform Preflight")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Checking cluster...")

	clients, err := platform.NewClients()
	if err != nil {
		printStatus(out, "Cluster connectivity", "ERROR", "")
		return err
	}

	info, err := platform.DiscoverCluster(ctx, clients)
	if err != nil {
		printStatus(out, "Cluster discovery", "ERROR", "")
		return err
	}

	printStatus(out, "Cluster connectivity", "READY", info.APIServer)

	contextName := info.CurrentContext
	if contextName == "" {
		contextName = "unknown"
	}
	printStatus(out, "Current context", "READY", contextName)

	if info.OpenShift {
		version := info.OpenShiftVersion
		if version == "" {
			version = "unknown"
		}

		printStatus(out, "OpenShift", "READY", version)
	} else {
		printStatus(out, "OpenShift", "MISSING", "")
	}

	if info.KubeVirt {
		printStatus(out, "KubeVirt", "READY", "")
	} else {
		printStatus(out, "KubeVirt", "MISSING", "")
	}

	if info.OpenShiftVirtualization {
		printStatus(out, "OpenShift Virtualization", "READY", "")
	} else {
		printStatus(out, "OpenShift Virtualization", "MISSING", "")
	}

	if info.RecommendedStorageClass != "" {
		detail := info.RecommendedStorageClass

		if info.StorageClassReason != "" {
			detail += " (" + info.StorageClassReason + ")"
		}

		printStatus(
			out,
			"StorageClass",
			"READY",
			detail,
		)
	} else {
		detail := "no default or recommended StorageClass found"

		if len(info.AllStorageClasses) > 0 {
			detail += "; available: " +
				strings.Join(info.AllStorageClasses, ", ")
		}

		printStatus(
			out,
			"StorageClass",
			"NEEDS INPUT",
			detail,
		)
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, "Checking platform operators...")

	operatorStatuses, err := operators.Discover(ctx, clients.Dynamic)
	if err != nil {
		return fmt.Errorf("discover operators: %w", err)
	}

	for _, operator := range operatorStatuses {
		switch {
		case operator.Ready():
			printStatus(
				out,
				operator.Name,
				"READY",
				operator.Namespace,
			)

		case operator.State == "Installed":
			detail := operator.Phase
			if operator.Namespace != "" {
				detail = operator.Namespace + " / " + operator.Phase
			}

			printStatus(
				out,
				operator.Name,
				"NOT READY",
				detail,
			)

		case operator.State == "Unknown":
			printStatus(
				out,
				operator.Name,
				"UNKNOWN",
				"unable to query OLM CSVs",
			)

		default:
			if operator.Name == "Grafana Operator" {
				discovery, discoverErr := grafana.Discover(
					ctx,
					clients.Kube,
				)

				switch {
				case discoverErr != nil:
					printStatus(
						out,
						operator.Name,
						"NOT INSTALLED",
						"existing Grafana discovery failed",
					)

				case discovery.Found:
					printStatus(
						out,
						operator.Name,
						"NOT INSTALLED",
						"not required; existing Grafana will be reused",
					)

					printStatus(
						out,
						"Grafana",
						"REUSING",
						discovery.Namespace+"/"+discovery.Deployment,
					)

				default:
					printStatus(
						out,
						operator.Name,
						"NOT INSTALLED",
						"not required; kvoctl will deploy Grafana",
					)
				}

				continue
			}

			printStatus(
				out,
				operator.Name,
				"NOT INSTALLED",
				"will be installed by kvoctl deploy",
			)
		}
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Checking deployment permissions...")

	permissionChecks, err := platform.CheckDeployPermissions(ctx, clients)
	if err != nil {
		return fmt.Errorf("check deployment permissions: %w", err)
	}

	allPermissionsAllowed := true

	for _, check := range permissionChecks {
		if check.Allowed {
			printStatus(
				out,
				check.Name,
				"ALLOWED",
				"",
			)
			continue
		}

		allPermissionsAllowed = false

		detail := check.Reason
		if detail == "" {
			detail = "permission denied"
		}

		printStatus(
			out,
			check.Name,
			"DENIED",
			detail,
		)
	}

	fmt.Fprintln(out)

	if allPermissionsAllowed {
		fmt.Fprintln(
			out,
			"Preflight completed. Cluster is ready for deployment.",
		)
	} else {
		fmt.Fprintln(
			out,
			"Preflight completed with permission issues.",
		)
	}

	return nil
}

func printStatus(
	out io.Writer,
	name string,
	status string,
	detail string,
) {
	const nameWidth = 32
	const statusWidth = 14

	if detail == "" {
		fmt.Fprintf(
			out,
			"  %-*s %-*s\n",
			nameWidth,
			name,
			statusWidth,
			status,
		)
		return
	}

	fmt.Fprintf(
		out,
		"  %-*s %-*s %s\n",
		nameWidth,
		name,
		statusWidth,
		status,
		detail,
	)
}
