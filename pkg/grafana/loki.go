package grafana

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

const (
	LokiRouteName      = "logging-loki"
	LokiRouteNamespace = "openshift-logging"
)

var lokiRouteGVR = schema.GroupVersionResource{
	Group:    "route.openshift.io",
	Version:  "v1",
	Resource: "routes",
}

func DiscoverLokiDatasourceURL(
	ctx context.Context,
	dynamicClient dynamic.Interface,
) (string, error) {
	route, err := dynamicClient.
		Resource(lokiRouteGVR).
		Namespace(LokiRouteNamespace).
		Get(
			ctx,
			LokiRouteName,
			metav1.GetOptions{},
		)

	if err != nil {
		return "", fmt.Errorf(
			"get Loki Route %s/%s: %w",
			LokiRouteNamespace,
			LokiRouteName,
			err,
		)
	}

	host, found, err := unstructured.NestedString(
		route.Object,
		"spec",
		"host",
	)

	if err != nil {
		return "", fmt.Errorf(
			"read Loki Route host: %w",
			err,
		)
	}

	if !found || host == "" {
		return "", fmt.Errorf(
			"Loki Route %s/%s has no spec.host",
			LokiRouteNamespace,
			LokiRouteName,
		)
	}

	return fmt.Sprintf(
		"https://%s/api/logs/v1/application",
		host,
	), nil
}
