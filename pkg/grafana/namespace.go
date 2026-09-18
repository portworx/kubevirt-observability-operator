package grafana

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func EnsureNamespace(
	ctx context.Context,
	kube kubernetes.Interface,
	namespace string,
) error {
	if namespace == "" {
		return fmt.Errorf(
			"Grafana namespace is required",
		)
	}

	namespaces := kube.
		CoreV1().
		Namespaces()

	_, err := namespaces.Get(
		ctx,
		namespace,
		metav1.GetOptions{},
	)

	if err == nil {
		return nil
	}

	if !apierrors.IsNotFound(err) {
		return fmt.Errorf(
			"get namespace %q: %w",
			namespace,
			err,
		)
	}

	_, err = namespaces.Create(
		ctx,
		&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
				Labels: map[string]string{
					"app.kubernetes.io/managed-by": "kvoctl",
				},
			},
		},
		metav1.CreateOptions{},
	)

	if err != nil &&
		!apierrors.IsAlreadyExists(err) {
		return fmt.Errorf(
			"create namespace %q: %w",
			namespace,
			err,
		)
	}

	return nil
}
