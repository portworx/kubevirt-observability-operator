package grafana

import (
	"context"
	"fmt"

	routev1 "github.com/openshift/api/route/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/dynamic"
)

const (
	GrafanaRouteName = "grafana"
)

var routeGVR = schema.GroupVersionResource{
	Group:    "route.openshift.io",
	Version:  "v1",
	Resource: "routes",
}

func EnsureRoute(
	ctx context.Context,
	dynamicClient dynamic.Interface,
	cfg Config,
) error {
	applyDefaults(&cfg)

	routes := dynamicClient.
		Resource(routeGVR).
		Namespace(cfg.Namespace)

	_, err := routes.Get(
		ctx,
		GrafanaRouteName,
		metav1.GetOptions{},
	)

	if err == nil {
		return nil
	}

	if !apierrors.IsNotFound(err) {
		return fmt.Errorf(
			"get Grafana Route %s/%s: %w",
			cfg.Namespace,
			GrafanaRouteName,
			err,
		)
	}

	route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GrafanaRouteName,
			Namespace: cfg.Namespace,
			Labels: map[string]string{
				"app":                          "grafana",
				"app.kubernetes.io/managed-by": "kvoctl",
			},
		},

		Spec: routev1.RouteSpec{
			To: routev1.RouteTargetReference{
				Kind: "Service",
				Name: GrafanaServiceName,
			},

			Port: &routev1.RoutePort{
				TargetPort: intstr.FromString("http"),
			},

			TLS: &routev1.TLSConfig{
				Termination: routev1.TLSTerminationEdge,
			},
		},
	}

	object, err := runtime.DefaultUnstructuredConverter.
		ToUnstructured(route)

	if err != nil {
		return fmt.Errorf(
			"convert Grafana Route to unstructured: %w",
			err,
		)
	}

	_, err = routes.Create(
		ctx,
		&unstructured.Unstructured{
			Object: object,
		},
		metav1.CreateOptions{},
	)

	if err != nil &&
		!apierrors.IsAlreadyExists(err) {
		return fmt.Errorf(
			"create Grafana Route %s/%s: %w",
			cfg.Namespace,
			GrafanaRouteName,
			err,
		)
	}

	return nil
}
