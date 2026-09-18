package grafana

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
)

const (
	GrafanaServiceName = "grafana"
)

func EnsureService(
	ctx context.Context,
	kube kubernetes.Interface,
	cfg Config,
) error {
	applyDefaults(&cfg)

	services := kube.
		CoreV1().
		Services(cfg.Namespace)

	_, err := services.Get(
		ctx,
		GrafanaServiceName,
		metav1.GetOptions{},
	)

	if err == nil {
		// Preserve an existing Grafana Service.
		return nil
	}

	if !apierrors.IsNotFound(err) {
		return fmt.Errorf(
			"get Grafana Service %s/%s: %w",
			cfg.Namespace,
			GrafanaServiceName,
			err,
		)
	}

	_, err = services.Create(
		ctx,
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      GrafanaServiceName,
				Namespace: cfg.Namespace,
				Labels: map[string]string{
					"app":                          "grafana",
					"app.kubernetes.io/managed-by": "kvoctl",
				},
			},
			Spec: corev1.ServiceSpec{
				Selector: map[string]string{
					"app": "grafana",
				},
				Ports: []corev1.ServicePort{
					{
						Name:       "http",
						Port:       3000,
						TargetPort: intstr.FromInt(3000),
						Protocol:   corev1.ProtocolTCP,
					},
				},
			},
		},
		metav1.CreateOptions{},
	)

	if err != nil &&
		!apierrors.IsAlreadyExists(err) {
		return fmt.Errorf(
			"create Grafana Service %s/%s: %w",
			cfg.Namespace,
			GrafanaServiceName,
			err,
		)
	}

	return nil
}
