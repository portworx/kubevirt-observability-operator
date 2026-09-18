package grafana

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type DiscoveryResult struct {
	Found      bool
	Namespace  string
	Deployment string
}

func Discover(
	ctx context.Context,
	kube kubernetes.Interface,
) (DiscoveryResult, error) {
	deployments, err := kube.
		AppsV1().
		Deployments("").
		List(
			ctx,
			metav1.ListOptions{},
		)

	if err != nil {
		return DiscoveryResult{}, fmt.Errorf(
			"list Deployments while discovering Grafana: %w",
			err,
		)
	}

	// First preference:
	// Deployment explicitly named "grafana".
	for _, deployment := range deployments.Items {
		if deployment.Name != GrafanaDeploymentName {
			continue
		}

		return DiscoveryResult{
			Found:      true,
			Namespace:  deployment.Namespace,
			Deployment: deployment.Name,
		}, nil
	}

	// Second preference:
	// Deployment carrying app=grafana.
	for _, deployment := range deployments.Items {
		if deployment.Labels["app"] != "grafana" {
			continue
		}

		return DiscoveryResult{
			Found:      true,
			Namespace:  deployment.Namespace,
			Deployment: deployment.Name,
		}, nil
	}

	return DiscoveryResult{
		Found: false,
	}, nil
}
