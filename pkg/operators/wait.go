package operators

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

// WaitConfig controls readiness polling for an installed operator.
type WaitConfig struct {
	Package   string
	Namespace string
	Timeout   time.Duration
	Interval  time.Duration
}

// WaitForReady waits for an OLM Subscription to resolve to a CSV in
// Succeeded phase.
func WaitForReady(
	ctx context.Context,
	client dynamic.Interface,
	cfg WaitConfig,
) error {
	if client == nil {
		return fmt.Errorf("dynamic Kubernetes client is required")
	}

	if cfg.Package == "" {
		return fmt.Errorf("operator package is required")
	}

	if cfg.Namespace == "" {
		return fmt.Errorf("operator namespace is required")
	}

	if cfg.Timeout == 0 {
		cfg.Timeout = 10 * time.Minute
	}

	if cfg.Interval == 0 {
		cfg.Interval = 5 * time.Second
	}

	subscriptionGVR := schema.GroupVersionResource{
		Group:    "operators.coreos.com",
		Version:  "v1alpha1",
		Resource: "subscriptions",
	}

	csvGVR := schema.GroupVersionResource{
		Group:    "operators.coreos.com",
		Version:  "v1alpha1",
		Resource: "clusterserviceversions",
	}

	deadlineCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-deadlineCtx.Done():
			return fmt.Errorf(
				"timeout waiting for operator %q in namespace %q",
				cfg.Package,
				cfg.Namespace,
			)

		case <-ticker.C:
			subscription, err := client.
				Resource(subscriptionGVR).
				Namespace(cfg.Namespace).
				Get(
					deadlineCtx,
					cfg.Package,
					metav1.GetOptions{},
				)
			if err != nil {
				continue
			}

			csvName, _, _ := unstructured.NestedString(
				subscription.Object,
				"status",
				"installedCSV",
			)

			if csvName == "" {
				continue
			}

			csv, err := client.
				Resource(csvGVR).
				Namespace(cfg.Namespace).
				Get(
					deadlineCtx,
					csvName,
					metav1.GetOptions{},
				)
			if err != nil {
				continue
			}

			phase, _, _ := unstructured.NestedString(
				csv.Object,
				"status",
				"phase",
			)

			switch phase {
			case "Succeeded":
				return nil

			case "Failed":
				reason, _, _ := unstructured.NestedString(
					csv.Object,
					"status",
					"reason",
				)

				message, _, _ := unstructured.NestedString(
					csv.Object,
					"status",
					"message",
				)

				return fmt.Errorf(
					"operator %q installation failed: %s: %s",
					cfg.Package,
					reason,
					message,
				)
			}
		}
	}
}
