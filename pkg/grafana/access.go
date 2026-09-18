package grafana

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	PrometheusViewClusterRole = "cluster-monitoring-view"
	GrafanaPrometheusBinding  = "kvo-grafana-cluster-monitoring-view"
)

func EnsureServiceAccountAndRBAC(
	ctx context.Context,
	kube kubernetes.Interface,
	cfg Config,
) error {
	applyDefaults(&cfg)

	serviceAccounts := kube.
		CoreV1().
		ServiceAccounts(cfg.Namespace)

	_, err := serviceAccounts.Get(
		ctx,
		GrafanaServiceAccount,
		metav1.GetOptions{},
	)

	if apierrors.IsNotFound(err) {
		_, err = serviceAccounts.Create(
			ctx,
			&corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      GrafanaServiceAccount,
					Namespace: cfg.Namespace,
					Labels: map[string]string{
						"app.kubernetes.io/managed-by": "kvoctl",
					},
				},
			},
			metav1.CreateOptions{},
		)
	}

	if err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf(
			"ensure Grafana ServiceAccount %s/%s: %w",
			cfg.Namespace,
			GrafanaServiceAccount,
			err,
		)
	}

	bindings := kube.
		RbacV1().
		ClusterRoleBindings()

	desired := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: GrafanaPrometheusBinding,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "kvoctl",
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name:     PrometheusViewClusterRole,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      GrafanaServiceAccount,
				Namespace: cfg.Namespace,
			},
		},
	}

	existing, err := bindings.Get(
		ctx,
		GrafanaPrometheusBinding,
		metav1.GetOptions{},
	)

	if err == nil {
		if existing.RoleRef != desired.RoleRef {
			return fmt.Errorf(
				"ClusterRoleBinding %q has unexpected RoleRef %s/%s",
				GrafanaPrometheusBinding,
				existing.RoleRef.Kind,
				existing.RoleRef.Name,
			)
		}

		existing.Subjects = desired.Subjects

		_, err = bindings.Update(
			ctx,
			existing,
			metav1.UpdateOptions{},
		)

		if err != nil {
			return fmt.Errorf(
				"update ClusterRoleBinding %q: %w",
				GrafanaPrometheusBinding,
				err,
			)
		}

		return nil
	}

	if !apierrors.IsNotFound(err) {
		return fmt.Errorf(
			"get ClusterRoleBinding %q: %w",
			GrafanaPrometheusBinding,
			err,
		)
	}

	_, err = bindings.Create(
		ctx,
		desired,
		metav1.CreateOptions{},
	)

	if err != nil &&
		!apierrors.IsAlreadyExists(err) {
		return fmt.Errorf(
			"create ClusterRoleBinding %q: %w",
			GrafanaPrometheusBinding,
			err,
		)
	}

	return nil
}
