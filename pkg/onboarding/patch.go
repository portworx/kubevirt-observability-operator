package onboarding

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/portworx/kubevirt-observability-operator/pkg/platform"
)

func markVMReadyForReconciliation(
	ctx context.Context,
	clients *platform.Clients,
	c Candidate,
) error {
	vm, err := clients.Dynamic.
		Resource(vmGVR).
		Namespace(c.Namespace).
		Get(ctx, c.Name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf(
			"get VM %s/%s before patch: %w",
			c.Namespace,
			c.Name,
			err,
		)
	}

	labels := vm.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}

	// Only assign kubevirt.io/os when kvoctl determined the OS from a
	// selector. Preserve an existing user-provided OS label unchanged.
	if c.OSSource == "selector" {
		if existing := normalizeOS(labels["kubevirt.io/os"]); existing != "" &&
			existing != c.OS {
			return fmt.Errorf(
				"refusing to overwrite conflicting kubevirt.io/os=%q",
				labels["kubevirt.io/os"],
			)
		}

		if labels["kubevirt.io/os"] == "" {
			labels["kubevirt.io/os"] = c.OS
		}
	}

	annotations := vm.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}

	annotations[annBootstrapManaged] = "true"
	annotations["kubevirt-observability.io/logging-enabled"] = "true"
	annotations["kubevirt-observability.io/remediation-required"] = "true"
	annotations["kubevirt-observability.io/ssh-bootstrap-complete"] = "true"
	annotations["kubevirt-observability.io/reconcile-at"] =
		time.Now().UTC().Format("20060102150405")

	// Remove stale state left when the webhook originally skipped this VM
	// because kubevirt.io/os was unavailable.
	if annotations["kubevirt-observability.io/reason"] == "missing-os-label" {
		delete(annotations, "kubevirt-observability.io/reason")
		delete(annotations, "kubevirt-observability.io/status")
		delete(annotations, "kubevirt-observability.io/bootstrap-os")
		delete(annotations, "kubevirt-observability.io/exporter")
	}

	patch := map[string]interface{}{
		"metadata": map[string]interface{}{
			"labels":      labels,
			"annotations": annotations,
		},
	}

	data, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("marshal VM patch: %w", err)
	}

	_, err = clients.Dynamic.
		Resource(vmGVR).
		Namespace(c.Namespace).
		Patch(
			ctx,
			c.Name,
			types.MergePatchType,
			data,
			metav1.PatchOptions{},
		)
	if err != nil {
		return fmt.Errorf(
			"patch VM %s/%s: %w",
			c.Namespace,
			c.Name,
			err,
		)
	}

	return nil
}
