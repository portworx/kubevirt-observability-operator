package vmmonitoring

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes"
)

type Installer struct {
	kube kubernetes.Interface
}

func NewInstaller(kube kubernetes.Interface) *Installer {
	return &Installer{
		kube: kube,
	}
}

func (i *Installer) Install(
	ctx context.Context,
	cfg Config,
) error {
	applyDefaults(&cfg)

	// 1. Namespace.
	if err := i.applyFile(
		ctx,
		cfg,
		"namespace.yaml",
	); err != nil {
		return fmt.Errorf("apply namespace.yaml: %w", err)
	}

	// 2. Dynamic secrets required by the operator.
	if err := ensureSSHSecrets(
		ctx,
		i.kube,
		cfg,
	); err != nil {
		return fmt.Errorf(
			"ensure SSH credentials: %w",
			err,
		)
	}

	// Uses openshift-logging/collector token.
	if err := ensureLokiWriter(
		ctx,
		i.kube,
		cfg,
	); err != nil {
		return fmt.Errorf(
			"ensure Loki writer token: %w",
			err,
		)
	}

	// 3. Operator ServiceAccount.
	if err := i.applyFile(
		ctx,
		cfg,
		"serviceaccount.yaml",
	); err != nil {
		return fmt.Errorf(
			"apply serviceaccount.yaml: %w",
			err,
		)
	}

	// 4. Operator RBAC.
	if err := i.applyFile(
		ctx,
		cfg,
		"rbac.yaml",
	); err != nil {
		return fmt.Errorf(
			"apply rbac.yaml: %w",
			err,
		)
	}

	// 5. Webhook Service.
	if err := i.applyFile(
		ctx,
		cfg,
		"service.yaml",
	); err != nil {
		return fmt.Errorf(
			"apply service.yaml: %w",
			err,
		)
	}

	// OpenShift service-ca operator creates this Secret.
	if err := i.waitForWebhookCertificate(
		ctx,
		cfg,
		2*time.Minute,
	); err != nil {
		return err
	}

	// 6. Operator Deployment.
	// The image placeholder from deployment.yaml is replaced in applyObject().
	if err := i.applyFile(
		ctx,
		cfg,
		"deployment.yaml",
	); err != nil {
		return fmt.Errorf(
			"apply deployment.yaml: %w",
			err,
		)
	}

	// 7. Mutating webhook.
	if err := i.applyFile(
		ctx,
		cfg,
		"mutatingwebhook.yaml",
	); err != nil {
		return fmt.Errorf(
			"apply mutatingwebhook.yaml: %w",
			err,
		)
	}

	// 8. Prove that the operator actually became ready.
	if err := i.WaitForReady(
		ctx,
		cfg,
		5*time.Minute,
	); err != nil {
		return err
	}

	return nil
}

func applyDefaults(cfg *Config) {
	if cfg.Namespace == "" {
		cfg.Namespace = DefaultNamespace
	}

	if cfg.ConfigDir == "" {
		cfg.ConfigDir = DefaultConfigDir
	}

	if cfg.OperatorImage == "" {
		cfg.OperatorImage = DefaultOperatorImage
	}

	if cfg.SSHUsername == "" {
		cfg.SSHUsername = DefaultSSHUsername
	}

	if cfg.LokiNamespace == "" {
		cfg.LokiNamespace = DefaultLokiNamespace
	}

	if cfg.LokiWriterSA == "" {
		cfg.LokiWriterSA = DefaultLokiWriterSA
	}
}

func (i *Installer) applyFile(
	ctx context.Context,
	cfg Config,
	name string,
) error {
	path := filepath.Join(
		cfg.ConfigDir,
		name,
	)

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf(
			"read %s: %w",
			path,
			err,
		)
	}

	decoder := utilyaml.NewYAMLOrJSONDecoder(
		bytes.NewReader(data),
		4096,
	)

	for {
		raw := map[string]interface{}{}

		err := decoder.Decode(&raw)
		if err == io.EOF {
			break
		}

		if err != nil {
			return fmt.Errorf(
				"decode %s: %w",
				path,
				err,
			)
		}

		if len(raw) == 0 {
			continue
		}

		kind, _ := raw["kind"].(string)
		if kind == "" {
			return fmt.Errorf(
				"%s contains object without kind",
				path,
			)
		}

		jsonData, err := json.Marshal(raw)
		if err != nil {
			return fmt.Errorf(
				"marshal %s object: %w",
				kind,
				err,
			)
		}

		if err := i.applyObject(
			ctx,
			cfg,
			kind,
			jsonData,
		); err != nil {
			return fmt.Errorf(
				"apply %s from %s: %w",
				kind,
				path,
				err,
			)
		}
	}

	return nil
}

func (i *Installer) applyObject(
	ctx context.Context,
	cfg Config,
	kind string,
	data []byte,
) error {
	switch kind {
	case "Namespace":
		var obj corev1.Namespace
		if err := json.Unmarshal(data, &obj); err != nil {
			return err
		}

		return i.applyNamespace(
			ctx,
			&obj,
		)

	case "ServiceAccount":
		var obj corev1.ServiceAccount
		if err := json.Unmarshal(data, &obj); err != nil {
			return err
		}

		return i.applyServiceAccount(
			ctx,
			&obj,
		)

	case "ClusterRole":
		var obj rbacv1.ClusterRole
		if err := json.Unmarshal(data, &obj); err != nil {
			return err
		}

		return i.applyClusterRole(
			ctx,
			&obj,
		)

	case "ClusterRoleBinding":
		var obj rbacv1.ClusterRoleBinding
		if err := json.Unmarshal(data, &obj); err != nil {
			return err
		}

		return i.applyClusterRoleBinding(
			ctx,
			&obj,
		)

	case "Service":
		var obj corev1.Service
		if err := json.Unmarshal(data, &obj); err != nil {
			return err
		}

		return i.applyService(
			ctx,
			&obj,
		)

	case "Deployment":
		var obj appsv1.Deployment
		if err := json.Unmarshal(data, &obj); err != nil {
			return err
		}

		// deployment.yaml remains a template.
		// Override only the operator image.
		foundManager := false

		for idx := range obj.Spec.Template.Spec.Containers {
			if obj.Spec.Template.Spec.Containers[idx].Name != "manager" {
				continue
			}

			obj.Spec.Template.Spec.Containers[idx].Image =
				cfg.OperatorImage

			foundManager = true
			break
		}

		if !foundManager {
			return fmt.Errorf(
				"deployment template has no manager container",
			)
		}

		return i.applyDeployment(
			ctx,
			&obj,
		)

	case "MutatingWebhookConfiguration":
		var obj admissionv1.MutatingWebhookConfiguration

		if err := json.Unmarshal(data, &obj); err != nil {
			return err
		}

		return i.applyMutatingWebhook(
			ctx,
			&obj,
		)

	default:
		return fmt.Errorf(
			"unsupported manifest kind %q",
			kind,
		)
	}
}

func (i *Installer) applyNamespace(
	ctx context.Context,
	desired *corev1.Namespace,
) error {
	namespaces := i.kube.CoreV1().Namespaces()

	_, err := namespaces.Get(
		ctx,
		desired.Name,
		metav1.GetOptions{},
	)

	if err == nil {
		return nil
	}

	if !apierrors.IsNotFound(err) {
		return err
	}

	_, err = namespaces.Create(
		ctx,
		desired,
		metav1.CreateOptions{},
	)

	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}

	return nil
}

func (i *Installer) applyServiceAccount(
	ctx context.Context,
	desired *corev1.ServiceAccount,
) error {
	serviceAccounts := i.kube.
		CoreV1().
		ServiceAccounts(desired.Namespace)

	_, err := serviceAccounts.Get(
		ctx,
		desired.Name,
		metav1.GetOptions{},
	)

	if err == nil {
		return nil
	}

	if !apierrors.IsNotFound(err) {
		return err
	}

	_, err = serviceAccounts.Create(
		ctx,
		desired,
		metav1.CreateOptions{},
	)

	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}

	return nil
}

func (i *Installer) applyClusterRole(
	ctx context.Context,
	desired *rbacv1.ClusterRole,
) error {
	roles := i.kube.
		RbacV1().
		ClusterRoles()

	existing, err := roles.Get(
		ctx,
		desired.Name,
		metav1.GetOptions{},
	)

	if err == nil {
		existing.Rules = desired.Rules
		existing.Labels = desired.Labels
		existing.Annotations = desired.Annotations

		_, err = roles.Update(
			ctx,
			existing,
			metav1.UpdateOptions{},
		)

		return err
	}

	if !apierrors.IsNotFound(err) {
		return err
	}

	_, err = roles.Create(
		ctx,
		desired,
		metav1.CreateOptions{},
	)

	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}

	return nil
}

func (i *Installer) applyClusterRoleBinding(
	ctx context.Context,
	desired *rbacv1.ClusterRoleBinding,
) error {
	bindings := i.kube.
		RbacV1().
		ClusterRoleBindings()

	existing, err := bindings.Get(
		ctx,
		desired.Name,
		metav1.GetOptions{},
	)

	if err == nil {
		// roleRef is immutable.
		if existing.RoleRef != desired.RoleRef {
			return fmt.Errorf(
				"ClusterRoleBinding %q already references %s/%s, expected %s/%s",
				desired.Name,
				existing.RoleRef.Kind,
				existing.RoleRef.Name,
				desired.RoleRef.Kind,
				desired.RoleRef.Name,
			)
		}

		existing.Subjects = desired.Subjects
		existing.Labels = desired.Labels
		existing.Annotations = desired.Annotations

		_, err = bindings.Update(
			ctx,
			existing,
			metav1.UpdateOptions{},
		)

		return err
	}

	if !apierrors.IsNotFound(err) {
		return err
	}

	_, err = bindings.Create(
		ctx,
		desired,
		metav1.CreateOptions{},
	)

	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}

	return nil
}

func (i *Installer) applyService(
	ctx context.Context,
	desired *corev1.Service,
) error {
	services := i.kube.
		CoreV1().
		Services(desired.Namespace)

	existing, err := services.Get(
		ctx,
		desired.Name,
		metav1.GetOptions{},
	)

	if err == nil {
		// Preserve Kubernetes-assigned immutable/network fields.
		desired.ResourceVersion =
			existing.ResourceVersion

		desired.Spec.ClusterIP =
			existing.Spec.ClusterIP

		desired.Spec.ClusterIPs =
			existing.Spec.ClusterIPs

		desired.Spec.IPFamilies =
			existing.Spec.IPFamilies

		desired.Spec.IPFamilyPolicy =
			existing.Spec.IPFamilyPolicy

		desired.Spec.HealthCheckNodePort =
			existing.Spec.HealthCheckNodePort

		_, err = services.Update(
			ctx,
			desired,
			metav1.UpdateOptions{},
		)

		return err
	}

	if !apierrors.IsNotFound(err) {
		return err
	}

	_, err = services.Create(
		ctx,
		desired,
		metav1.CreateOptions{},
	)

	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}

	return nil
}

func (i *Installer) applyDeployment(
	ctx context.Context,
	desired *appsv1.Deployment,
) error {
	deployments := i.kube.
		AppsV1().
		Deployments(desired.Namespace)

	existing, err := deployments.Get(
		ctx,
		desired.Name,
		metav1.GetOptions{},
	)

	if err == nil {
		desired.ResourceVersion =
			existing.ResourceVersion

		_, err = deployments.Update(
			ctx,
			desired,
			metav1.UpdateOptions{},
		)

		return err
	}

	if !apierrors.IsNotFound(err) {
		return err
	}

	_, err = deployments.Create(
		ctx,
		desired,
		metav1.CreateOptions{},
	)

	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}

	return nil
}

func (i *Installer) applyMutatingWebhook(
	ctx context.Context,
	desired *admissionv1.MutatingWebhookConfiguration,
) error {
	webhooks := i.kube.
		AdmissionregistrationV1().
		MutatingWebhookConfigurations()

	existing, err := webhooks.Get(
		ctx,
		desired.Name,
		metav1.GetOptions{},
	)

	if err == nil {
		desired.ResourceVersion =
			existing.ResourceVersion

		_, err = webhooks.Update(
			ctx,
			desired,
			metav1.UpdateOptions{},
		)

		return err
	}

	if !apierrors.IsNotFound(err) {
		return err
	}

	_, err = webhooks.Create(
		ctx,
		desired,
		metav1.CreateOptions{},
	)

	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}

	return nil
}

func (i *Installer) waitForWebhookCertificate(
	ctx context.Context,
	cfg Config,
	timeout time.Duration,
) error {
	waitCtx, cancel := context.WithTimeout(
		ctx,
		timeout,
	)
	defer cancel()

	ticker := time.NewTicker(
		2 * time.Second,
	)
	defer ticker.Stop()

	for {
		select {
		case <-waitCtx.Done():
			return fmt.Errorf(
				"timeout waiting for webhook certificate Secret %s/%s",
				cfg.Namespace,
				WebhookSecret,
			)

		case <-ticker.C:
			secret, err := i.kube.
				CoreV1().
				Secrets(cfg.Namespace).
				Get(
					waitCtx,
					WebhookSecret,
					metav1.GetOptions{},
				)

			if err != nil {
				if apierrors.IsNotFound(err) {
					continue
				}

				return fmt.Errorf(
					"get webhook certificate Secret: %w",
					err,
				)
			}

			if len(secret.Data["tls.crt"]) > 0 &&
				len(secret.Data["tls.key"]) > 0 {
				return nil
			}
		}
	}
}

func (i *Installer) WaitForReady(
	ctx context.Context,
	cfg Config,
	timeout time.Duration,
) error {
	applyDefaults(&cfg)

	waitCtx, cancel := context.WithTimeout(
		ctx,
		timeout,
	)
	defer cancel()

	ticker := time.NewTicker(
		5 * time.Second,
	)
	defer ticker.Stop()

	for {
		select {
		case <-waitCtx.Done():
			return fmt.Errorf(
				"timeout waiting for Deployment %s/%s",
				cfg.Namespace,
				OperatorDeployment,
			)

		case <-ticker.C:
			deployment, err := i.kube.
				AppsV1().
				Deployments(cfg.Namespace).
				Get(
					waitCtx,
					OperatorDeployment,
					metav1.GetOptions{},
				)

			if err != nil {
				if apierrors.IsNotFound(err) {
					continue
				}

				return err
			}

			if deployment.Status.ObservedGeneration <
				deployment.Generation {
				continue
			}

			if deployment.Status.ReadyReplicas >= 1 &&
				deployment.Status.AvailableReplicas >= 1 {
				return nil
			}
		}
	}
}
