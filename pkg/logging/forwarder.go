package logging

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

// Config describes OpenShift log forwarding configuration.
type Config struct {
	Name                string
	Namespace           string
	ServiceAccountName  string
	LokiStackName       string
	LokiStackNamespace  string
	MaxRecordsPerSecond int64

	CollectorCPURequest    string
	CollectorMemoryRequest string
	CollectorCPULimit      string
	CollectorMemoryLimit   string
}

// Installer manages collector RBAC and ClusterLogForwarder resources.
type Installer struct {
	kube    kubernetes.Interface
	dynamic dynamic.Interface
}

// NewInstaller creates a logging installer.
func NewInstaller(
	kube kubernetes.Interface,
	dynamicClient dynamic.Interface,
) *Installer {
	return &Installer{
		kube:    kube,
		dynamic: dynamicClient,
	}
}

// EnsureCollectorServiceAccount creates the collector ServiceAccount.
func (i *Installer) EnsureCollectorServiceAccount(
	ctx context.Context,
	cfg Config,
) error {
	if cfg.Namespace == "" {
		return fmt.Errorf("collector namespace is required")
	}

	if cfg.ServiceAccountName == "" {
		return fmt.Errorf("collector ServiceAccount name is required")
	}

	serviceAccounts := i.kube.
		CoreV1().
		ServiceAccounts(cfg.Namespace)

	_, err := serviceAccounts.Get(
		ctx,
		cfg.ServiceAccountName,
		metav1.GetOptions{},
	)

	if err == nil {
		return nil
	}

	if !apierrors.IsNotFound(err) {
		return fmt.Errorf(
			"get collector ServiceAccount %s/%s: %w",
			cfg.Namespace,
			cfg.ServiceAccountName,
			err,
		)
	}

	_, err = serviceAccounts.Create(
		ctx,
		&corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cfg.ServiceAccountName,
				Namespace: cfg.Namespace,
			},
		},
		metav1.CreateOptions{},
	)

	if err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf(
			"create collector ServiceAccount %s/%s: %w",
			cfg.Namespace,
			cfg.ServiceAccountName,
			err,
		)
	}

	return nil
}

// EnsureCollectorRBAC grants the collector permission to:
//   - collect application logs
//   - write logs to LokiStack
//
// Infrastructure and audit log permissions are intentionally not granted
// because the current ClusterLogForwarder collects application logs only.
func (i *Installer) EnsureCollectorRBAC(
	ctx context.Context,
	cfg Config,
) error {
	bindings := []struct {
		Name string
		Role string
	}{
		{
			Name: "kvo-collector-application-logs",
			Role: "collect-application-logs",
		},
		{
			Name: "kvo-collector-infrastructure-logs",
			Role: "collect-infrastructure-logs",
		},
		{
			Name: "kvo-collector-audit-logs",
			Role: "collect-audit-logs",
		},
		{
			Name: "kvo-collector-logs-writer",
			Role: "logging-collector-logs-writer",
		},
	}
	for _, binding := range bindings {
		if err := i.ensureClusterRoleBinding(
			ctx,
			binding.Name,
			binding.Role,
			cfg.Namespace,
			cfg.ServiceAccountName,
		); err != nil {
			return err
		}
	}

	return nil
}

func (i *Installer) ensureClusterRoleBinding(
	ctx context.Context,
	name string,
	roleName string,
	namespace string,
	serviceAccountName string,
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
				Name:      serviceAccountName,
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
		// roleRef is immutable. If the existing binding points to a
		// different role, fail instead of silently changing security scope.
		if existing.RoleRef.Name != roleName {
			return fmt.Errorf(
				"ClusterRoleBinding %q already references role %q, expected %q",
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

// EnsureClusterLogForwarder creates or updates the ClusterLogForwarder.
func (i *Installer) EnsureClusterLogForwarder(
	ctx context.Context,
	cfg Config,
) error {
	if cfg.Name == "" {
		return fmt.Errorf("ClusterLogForwarder name is required")
	}

	if cfg.Namespace == "" {
		return fmt.Errorf("ClusterLogForwarder namespace is required")
	}

	if cfg.ServiceAccountName == "" {
		return fmt.Errorf("collector ServiceAccount name is required")
	}

	if cfg.LokiStackName == "" {
		return fmt.Errorf("LokiStack name is required")
	}

	if cfg.LokiStackNamespace == "" {
		cfg.LokiStackNamespace = cfg.Namespace
	}

	if cfg.MaxRecordsPerSecond == 0 {
		cfg.MaxRecordsPerSecond = 200
	}

	if cfg.CollectorCPURequest == "" {
		cfg.CollectorCPURequest = "1"
	}

	if cfg.CollectorMemoryRequest == "" {
		cfg.CollectorMemoryRequest = "1Gi"
	}

	if cfg.CollectorCPULimit == "" {
		cfg.CollectorCPULimit = "6"
	}

	if cfg.CollectorMemoryLimit == "" {
		cfg.CollectorMemoryLimit = "4Gi"
	}

	gvr := schema.GroupVersionResource{
		Group:    "observability.openshift.io",
		Version:  "v1",
		Resource: "clusterlogforwarders",
	}

	forwarder := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "observability.openshift.io/v1",
			"kind":       "ClusterLogForwarder",
			"metadata": map[string]interface{}{
				"name":      cfg.Name,
				"namespace": cfg.Namespace,
			},
			"spec": map[string]interface{}{
				"collector": map[string]interface{}{
					"resources": map[string]interface{}{
						"requests": map[string]interface{}{
							"cpu":    cfg.CollectorCPURequest,
							"memory": cfg.CollectorMemoryRequest,
						},
						"limits": map[string]interface{}{
							"cpu":    cfg.CollectorCPULimit,
							"memory": cfg.CollectorMemoryLimit,
						},
					},
				},
				"inputs": []interface{}{
					map[string]interface{}{
						"name":        "vm-namespaces",
						"type":        "application",
						"application": map[string]interface{}{},
					},
				},
				"managementState": "Managed",
				"outputs": []interface{}{
					map[string]interface{}{
						"name": "loki",
						"type": "lokiStack",
						"lokiStack": map[string]interface{}{
							"authentication": map[string]interface{}{
								"token": map[string]interface{}{
									"from": "serviceAccount",
								},
							},
							"target": map[string]interface{}{
								"name":      cfg.LokiStackName,
								"namespace": cfg.LokiStackNamespace,
							},
						},
						"rateLimit": map[string]interface{}{
							"maxRecordsPerSecond": cfg.MaxRecordsPerSecond,
						},
						"tls": map[string]interface{}{
							"ca": map[string]interface{}{
								"configMapName": "openshift-service-ca.crt",
								"key":           "service-ca.crt",
							},
						},
					},
				},
				"pipelines": []interface{}{
					map[string]interface{}{
						"name": "vm-only",
						"inputRefs": []interface{}{
							"vm-namespaces",
						},
						"outputRefs": []interface{}{
							"loki",
						},
					},
				},
				"serviceAccount": map[string]interface{}{
					"name": cfg.ServiceAccountName,
				},
			},
		},
	}

	resource := i.dynamic.
		Resource(gvr).
		Namespace(cfg.Namespace)

	existing, err := resource.Get(
		ctx,
		cfg.Name,
		metav1.GetOptions{},
	)

	if err == nil {
		forwarder.SetResourceVersion(
			existing.GetResourceVersion(),
		)

		_, err = resource.Update(
			ctx,
			forwarder,
			metav1.UpdateOptions{},
		)

		if err != nil {
			return fmt.Errorf(
				"update ClusterLogForwarder %s/%s: %w",
				cfg.Namespace,
				cfg.Name,
				err,
			)
		}

		return nil
	}

	if !apierrors.IsNotFound(err) {
		return fmt.Errorf(
			"get ClusterLogForwarder %s/%s: %w",
			cfg.Namespace,
			cfg.Name,
			err,
		)
	}

	_, err = resource.Create(
		ctx,
		forwarder,
		metav1.CreateOptions{},
	)

	if err != nil {
		return fmt.Errorf(
			"create ClusterLogForwarder %s/%s: %w",
			cfg.Namespace,
			cfg.Name,
			err,
		)
	}

	return nil
}

// WaitForReady waits for the ClusterLogForwarder Ready condition.
func (i *Installer) WaitForReady(
	ctx context.Context,
	namespace string,
	name string,
	timeout time.Duration,
) error {
	if timeout == 0 {
		timeout = 10 * time.Minute
	}

	gvr := schema.GroupVersionResource{
		Group:    "observability.openshift.io",
		Version:  "v1",
		Resource: "clusterlogforwarders",
	}

	waitCtx, cancel := context.WithTimeout(
		ctx,
		timeout,
	)
	defer cancel()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-waitCtx.Done():
			return fmt.Errorf(
				"timeout waiting for ClusterLogForwarder %s/%s",
				namespace,
				name,
			)

		case <-ticker.C:
			forwarder, err := i.dynamic.
				Resource(gvr).
				Namespace(namespace).
				Get(
					waitCtx,
					name,
					metav1.GetOptions{},
				)

			if err != nil {
				continue
			}

			conditions, found, _ := unstructured.NestedSlice(
				forwarder.Object,
				"status",
				"conditions",
			)

			if !found {
				continue
			}

			for _, raw := range conditions {
				condition, ok := raw.(map[string]interface{})
				if !ok {
					continue
				}

				conditionType, _, _ := unstructured.NestedString(
					condition,
					"type",
				)

				status, _, _ := unstructured.NestedString(
					condition,
					"status",
				)

				if conditionType == "Ready" &&
					status == "True" {
					return nil
				}

				if status == "False" &&
					(conditionType == "Ready" ||
						conditionType == "observability.openshift.io/Valid" ||
						conditionType == "observability.openshift.io/Authorized") {

					reason, _, _ := unstructured.NestedString(
						condition,
						"reason",
					)

					message, _, _ := unstructured.NestedString(
						condition,
						"message",
					)

					if reason != "" || message != "" {
						return fmt.Errorf(
							"ClusterLogForwarder not ready: %s: %s",
							reason,
							message,
						)
					}
				}
			}
		}
	}
}
