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
	DefaultReaderServiceAccount = "grafana"
	LokiApplicationReaderRole   = "grafana-loki-application-reader"
)

type RBACInstaller struct {
	kube kubernetes.Interface
}

func NewRBACInstaller(
	kube kubernetes.Interface,
) *RBACInstaller {
	return &RBACInstaller{
		kube: kube,
	}
}

func (i *RBACInstaller) EnsureReaderFoundation(
	ctx context.Context,
	namespace string,
) error {
	if namespace == "" {
		return fmt.Errorf("Grafana reader namespace is required")
	}

	if err := i.ensureServiceAccount(
		ctx,
		namespace,
		DefaultReaderServiceAccount,
	); err != nil {
		return err
	}

	if err := i.ensureLokiApplicationReaderRole(ctx); err != nil {
		return err
	}

	if err := i.ensureClusterRoleBinding(
		ctx,
		LokiApplicationReaderRole,
		LokiApplicationReaderRole,
		namespace,
		DefaultReaderServiceAccount,
	); err != nil {
		return err
	}

	return nil
}

func (i *RBACInstaller) ensureServiceAccount(
	ctx context.Context,
	namespace string,
	name string,
) error {
	serviceAccounts := i.kube.
		CoreV1().
		ServiceAccounts(namespace)

	_, err := serviceAccounts.Get(
		ctx,
		name,
		metav1.GetOptions{},
	)

	if err == nil {
		return nil
	}

	if !apierrors.IsNotFound(err) {
		return fmt.Errorf(
			"get ServiceAccount %s/%s: %w",
			namespace,
			name,
			err,
		)
	}

	_, err = serviceAccounts.Create(
		ctx,
		&corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
		},
		metav1.CreateOptions{},
	)

	if err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf(
			"create ServiceAccount %s/%s: %w",
			namespace,
			name,
			err,
		)
	}

	return nil
}

func (i *RBACInstaller) ensureLokiApplicationReaderRole(
	ctx context.Context,
) error {
	desired := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: LokiApplicationReaderRole,
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{
					"loki.grafana.com",
				},
				Resources: []string{
					"application",
				},
				ResourceNames: []string{
					"logs",
				},
				Verbs: []string{
					"get",
				},
			},
		},
	}

	roles := i.kube.RbacV1().ClusterRoles()

	existing, err := roles.Get(
		ctx,
		desired.Name,
		metav1.GetOptions{},
	)

	if err == nil {
		existing.Rules = desired.Rules

		_, err = roles.Update(
			ctx,
			existing,
			metav1.UpdateOptions{},
		)

		if err != nil {
			return fmt.Errorf(
				"update ClusterRole %q: %w",
				desired.Name,
				err,
			)
		}

		return nil
	}

	if !apierrors.IsNotFound(err) {
		return fmt.Errorf(
			"get ClusterRole %q: %w",
			desired.Name,
			err,
		)
	}

	_, err = roles.Create(
		ctx,
		desired,
		metav1.CreateOptions{},
	)

	if err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf(
			"create ClusterRole %q: %w",
			desired.Name,
			err,
		)
	}

	return nil
}

func (i *RBACInstaller) ensureClusterRoleBinding(
	ctx context.Context,
	name string,
	roleName string,
	namespace string,
	serviceAccount string,
) error {
	bindings := i.kube.
		RbacV1().
		ClusterRoleBindings()

	desired := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name:     roleName,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      serviceAccount,
				Namespace: namespace,
			},
		},
	}

	existing, err := bindings.Get(
		ctx,
		name,
		metav1.GetOptions{},
	)

	if err == nil {
		if existing.RoleRef.Name != roleName {
			return fmt.Errorf(
				"ClusterRoleBinding %q references %q, expected %q",
				name,
				existing.RoleRef.Name,
				roleName,
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
				name,
				err,
			)
		}

		return nil
	}

	if !apierrors.IsNotFound(err) {
		return fmt.Errorf(
			"get ClusterRoleBinding %q: %w",
			name,
			err,
		)
	}

	_, err = bindings.Create(
		ctx,
		desired,
		metav1.CreateOptions{},
	)

	if err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf(
			"create ClusterRoleBinding %q: %w",
			name,
			err,
		)
	}

	return nil
}
