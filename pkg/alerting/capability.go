package alerting

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type Capabilities struct {
	UserWorkloadMonitoring bool
	AlertmanagerConfig     bool
}

func discoverCapabilities(
	ctx context.Context,
	client kubernetes.Interface,
) (Capabilities, error) {
	result := Capabilities{}

	_, err := client.CoreV1().
		Namespaces().
		Get(
			ctx,
			UserWorkloadNamespace,
			metav1.GetOptions{},
		)

	switch {
	case err == nil:
		result.UserWorkloadMonitoring = true

	case apierrors.IsNotFound(err):
		return result, nil

	default:
		return result, fmt.Errorf(
			"check user workload monitoring namespace: %w",
			err,
		)
	}

	resourceList, err := client.Discovery().
		ServerResourcesForGroupVersion(
			"monitoring.coreos.com/v1beta1",
		)

	if err == nil {
		for _, resource := range resourceList.APIResources {
			if resource.Name == "alertmanagerconfigs" {
				result.AlertmanagerConfig = true
				break
			}
		}
	}

	// Some Prometheus Operator versions expose AlertmanagerConfig as v1alpha1.
	if !result.AlertmanagerConfig {
		resourceList, alphaErr := client.Discovery().
			ServerResourcesForGroupVersion(
				"monitoring.coreos.com/v1alpha1",
			)

		if alphaErr == nil {
			for _, resource := range resourceList.APIResources {
				if resource.Name == "alertmanagerconfigs" {
					result.AlertmanagerConfig = true
					break
				}
			}
		}
	}

	return result, nil
}
