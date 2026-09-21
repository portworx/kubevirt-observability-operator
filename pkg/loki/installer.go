package loki

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

type S3Config struct {
	Bucket    string
	Endpoint  string
	Region    string
	AccessKey string
	SecretKey string
}

type Config struct {
	Name          string
	Namespace     string
	Size          string
	StorageClass  string
	RetentionDays int
	StorageSecret string
	SchemaVersion string
	SchemaDate    string
	S3            S3Config
}

type Installer struct {
	kube    kubernetes.Interface
	dynamic dynamic.Interface
}

func NewInstaller(
	kube kubernetes.Interface,
	dynamicClient dynamic.Interface,
) *Installer {
	return &Installer{
		kube:    kube,
		dynamic: dynamicClient,
	}
}

func (i *Installer) EnsureStorageSecret(
	ctx context.Context,
	cfg Config,
) error {
	if cfg.Namespace == "" {
		return fmt.Errorf("Loki namespace is required")
	}

	if cfg.StorageSecret == "" {
		return fmt.Errorf("Loki storage secret name is required")
	}

	if cfg.S3.Bucket == "" {
		return fmt.Errorf("S3 bucket is required")
	}

	if cfg.S3.AccessKey == "" {
		return fmt.Errorf("S3 access key is required")
	}

	if cfg.S3.SecretKey == "" {
		return fmt.Errorf("S3 secret key is required")
	}

	data := map[string][]byte{
		"bucketnames":       []byte(cfg.S3.Bucket),
		"access_key_id":     []byte(cfg.S3.AccessKey),
		"access_key_secret": []byte(cfg.S3.SecretKey),
	}

	if cfg.S3.Endpoint != "" {
		data["endpoint"] = []byte(cfg.S3.Endpoint)
	}

	if cfg.S3.Region != "" {
		data["region"] = []byte(cfg.S3.Region)
	}

	secrets := i.kube.
		CoreV1().
		Secrets(cfg.Namespace)

	existing, err := secrets.Get(
		ctx,
		cfg.StorageSecret,
		metav1.GetOptions{},
	)

	if err == nil {
		existing.Data = data

		_, err = secrets.Update(
			ctx,
			existing,
			metav1.UpdateOptions{},
		)

		if err != nil {
			return fmt.Errorf(
				"update Loki S3 Secret %s/%s: %w",
				cfg.Namespace,
				cfg.StorageSecret,
				err,
			)
		}

		return nil
	}

	if !apierrors.IsNotFound(err) {
		return fmt.Errorf(
			"get Loki S3 Secret %s/%s: %w",
			cfg.Namespace,
			cfg.StorageSecret,
			err,
		)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cfg.StorageSecret,
			Namespace: cfg.Namespace,
		},
		Type: corev1.SecretTypeOpaque,
		Data: data,
	}

	_, err = secrets.Create(
		ctx,
		secret,
		metav1.CreateOptions{},
	)

	if err != nil {
		return fmt.Errorf(
			"create Loki S3 Secret %s/%s: %w",
			cfg.Namespace,
			cfg.StorageSecret,
			err,
		)
	}

	return nil
}

func (i *Installer) EnsureLokiStack(
	ctx context.Context,
	cfg Config,
) error {
	if cfg.Name == "" {
		return fmt.Errorf("LokiStack name is required")
	}

	if cfg.Namespace == "" {
		return fmt.Errorf("LokiStack namespace is required")
	}

	if cfg.Size == "" {
		cfg.Size = "1x.medium"
	}

	if cfg.StorageClass == "" {
		return fmt.Errorf("Loki StorageClass is required")
	}

	if _, err := i.kube.
		StorageV1().
		StorageClasses().
		Get(
			ctx,
			cfg.StorageClass,
			metav1.GetOptions{},
		); err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf(
				"configured Loki StorageClass %q does not exist; recreate the StorageClass or set KVO_STORAGE_CLASS to an available StorageClass",
				cfg.StorageClass,
			)
		}

		return fmt.Errorf(
			"get Loki StorageClass %q: %w",
			cfg.StorageClass,
			err,
		)
	}

	if cfg.StorageSecret == "" {
		return fmt.Errorf("Loki storage Secret is required")
	}

	if cfg.SchemaVersion == "" {
		cfg.SchemaVersion = "v13"
	}

	if cfg.SchemaDate == "" {
		cfg.SchemaDate = "2024-04-02"
	}

	if cfg.RetentionDays == 0 {
		cfg.RetentionDays = 7
	}

	if cfg.RetentionDays < 1 || cfg.RetentionDays > 30 {
		return fmt.Errorf(
			"Loki retention must be between 1 and 30 days, got %d",
			cfg.RetentionDays,
		)
	}

	gvr := schema.GroupVersionResource{
		Group:    "loki.grafana.com",
		Version:  "v1",
		Resource: "lokistacks",
	}

	stack := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "loki.grafana.com/v1",
			"kind":       "LokiStack",
			"metadata": map[string]interface{}{
				"name":      cfg.Name,
				"namespace": cfg.Namespace,
			},
			"spec": map[string]interface{}{
				"managementState": "Managed",
				"limits": map[string]interface{}{
					"global": map[string]interface{}{
						"retention": map[string]interface{}{
							"days": int64(cfg.RetentionDays),
						},
					},
				},
				"rules": map[string]interface{}{
					"enabled": true,
					"namespaceSelector": map[string]interface{}{
						"matchLabels": map[string]interface{}{
							"vm-monitoring-alerts": "enabled",
						},
					},
					"selector": map[string]interface{}{
						"matchLabels": map[string]interface{}{
							"app": "vm-monitoring",
						},
					},
				},
				"size": cfg.Size,
				"storage": map[string]interface{}{
					"schemas": []interface{}{
						map[string]interface{}{
							"effectiveDate": cfg.SchemaDate,
							"version":       cfg.SchemaVersion,
						},
					},
					"secret": map[string]interface{}{
						"name": cfg.StorageSecret,
						"type": "s3",
					},
				},
				"storageClassName": cfg.StorageClass,
				"tenants": map[string]interface{}{
					"mode": "openshift-logging",
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
		stack.SetResourceVersion(existing.GetResourceVersion())

		_, err = resource.Update(
			ctx,
			stack,
			metav1.UpdateOptions{},
		)

		if err != nil {
			return fmt.Errorf(
				"update LokiStack %s/%s: %w",
				cfg.Namespace,
				cfg.Name,
				err,
			)
		}

		return nil
	}

	if !apierrors.IsNotFound(err) {
		return fmt.Errorf(
			"get LokiStack %s/%s: %w",
			cfg.Namespace,
			cfg.Name,
			err,
		)
	}

	_, err = resource.Create(
		ctx,
		stack,
		metav1.CreateOptions{},
	)

	if err != nil {
		return fmt.Errorf(
			"create LokiStack %s/%s: %w",
			cfg.Namespace,
			cfg.Name,
			err,
		)
	}

	return nil
}

func (i *Installer) WaitForReady(
	ctx context.Context,
	namespace string,
	name string,
	timeout time.Duration,
) error {
	return i.WaitForReadyWithProgress(
		ctx,
		namespace,
		name,
		timeout,
		io.Discard,
	)
}

func (i *Installer) WaitForReadyWithProgress(
	ctx context.Context,
	namespace string,
	name string,
	timeout time.Duration,
	out io.Writer,
) error {
	if timeout == 0 {
		timeout = 15 * time.Minute
	}

	if out == nil {
		out = io.Discard
	}

	gvr := schema.GroupVersionResource{
		Group:    "loki.grafana.com",
		Version:  "v1",
		Resource: "lokistacks",
	}

	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	startedAt := time.Now()

	const reportInterval = 60 * time.Second

	nextReport := reportInterval
	lastSummary := ""

	for {
		select {
		case <-waitCtx.Done():
			summary := i.lokiPendingSummary(
				ctx,
				namespace,
				name,
			)

			if summary != "" {
				return fmt.Errorf(
					"timeout waiting for LokiStack %s/%s to become ready\n\n"+
						"Non-ready resources:\n%s\n"+
						"Suggested checks:\n"+
						"  oc get pods -n %s\n"+
						"  oc get pvc -n %s\n"+
						"  oc get lokistack %s -n %s -o yaml",
					namespace,
					name,
					summary,
					namespace,
					namespace,
					name,
					namespace,
				)
			}

			return fmt.Errorf(
				"timeout waiting for LokiStack %s/%s to become ready",
				namespace,
				name,
			)

		case <-ticker.C:
			stack, err := i.dynamic.
				Resource(gvr).
				Namespace(namespace).
				Get(
					waitCtx,
					name,
					metav1.GetOptions{},
				)

			if err == nil {
				conditions, found, _ := unstructured.NestedSlice(
					stack.Object,
					"status",
					"conditions",
				)

				if found {
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

						if conditionType == "Ready" && status == "True" {
							return nil
						}

						if conditionType == "Degraded" && status == "True" {
							reason, _, _ := unstructured.NestedString(
								condition,
								"reason",
							)

							message, _, _ := unstructured.NestedString(
								condition,
								"message",
							)

							return fmt.Errorf(
								"LokiStack is degraded: %s: %s",
								reason,
								message,
							)
						}
					}
				}
			}

			elapsed := time.Since(startedAt)

			if elapsed < nextReport {
				continue
			}

			summary := i.lokiPendingSummary(
				waitCtx,
				namespace,
				name,
			)

			// Do not repeatedly print an identical resource state.
			if summary != "" && summary != lastSummary {
				fmt.Fprintf(
					out,
					"\nStill waiting for LokiStack after %s...\n%s\n",
					elapsed.Round(time.Second),
					summary,
				)

				lastSummary = summary
			}

			for nextReport <= elapsed {
				nextReport += reportInterval
			}
		}
	}
}

func (i *Installer) lokiPendingSummary(
	ctx context.Context,
	namespace string,
	stackName string,
) string {
	var out strings.Builder

	pods, err := i.kube.
		CoreV1().
		Pods(namespace).
		List(
			ctx,
			metav1.ListOptions{},
		)

	if err == nil {
		for _, pod := range pods.Items {
			if !matchesLokiStackResource(
				pod.Name,
				pod.Labels,
				stackName,
			) {
				continue
			}

			if isPodReady(&pod) {
				continue
			}

			fmt.Fprintf(
				&out,
				"  %-32s %s\n",
				"Namespace",
				namespace,
			)

			fmt.Fprintf(
				&out,
				"  %-32s %s\n",
				"Pod",
				pod.Name,
			)

			fmt.Fprintf(
				&out,
				"  %-32s %s\n",
				"Phase",
				pod.Status.Phase,
			)

			for _, status := range pod.Status.InitContainerStatuses {
				if status.Ready {
					continue
				}

				fmt.Fprintf(
					&out,
					"  %-32s %s\n",
					"InitContainer "+status.Name,
					containerState(status),
				)
			}

			for _, status := range pod.Status.ContainerStatuses {
				if status.Ready {
					continue
				}

				fmt.Fprintf(
					&out,
					"  %-32s %s\n",
					"Container "+status.Name,
					containerState(status),
				)
			}

			fmt.Fprintln(&out)
		}
	}

	pvcs, err := i.kube.
		CoreV1().
		PersistentVolumeClaims(namespace).
		List(
			ctx,
			metav1.ListOptions{},
		)

	if err == nil {
		for _, pvc := range pvcs.Items {
			if !matchesLokiStackResource(
				pvc.Name,
				pvc.Labels,
				stackName,
			) {
				continue
			}

			if pvc.Status.Phase == corev1.ClaimBound {
				continue
			}

			fmt.Fprintf(
				&out,
				"  %-32s %s\n",
				"Namespace",
				namespace,
			)

			fmt.Fprintf(
				&out,
				"  %-32s %s\n",
				"PVC",
				pvc.Name,
			)

			fmt.Fprintf(
				&out,
				"  %-32s %s\n",
				"Status",
				pvc.Status.Phase,
			)

			if pvc.Spec.StorageClassName != nil {
				fmt.Fprintf(
					&out,
					"  %-32s %s\n",
					"StorageClass",
					*pvc.Spec.StorageClassName,
				)
			}

			fmt.Fprintln(&out)
		}
	}

	return out.String()
}

func matchesLokiStackResource(
	resourceName string,
	labels map[string]string,
	stackName string,
) bool {
	if strings.Contains(resourceName, stackName) {
		return true
	}

	for key, value := range labels {
		if strings.Contains(
			strings.ToLower(key),
			"loki",
		) && value == stackName {
			return true
		}
	}

	return false
}

func isPodReady(
	pod *corev1.Pod,
) bool {
	if pod.Status.Phase != corev1.PodRunning {
		return false
	}

	for _, status := range pod.Status.InitContainerStatuses {
		if !status.Ready {
			return false
		}
	}

	for _, status := range pod.Status.ContainerStatuses {
		if !status.Ready {
			return false
		}
	}

	return true
}

func containerState(
	status corev1.ContainerStatus,
) string {
	if status.State.Waiting != nil {
		if status.State.Waiting.Reason != "" {
			return status.State.Waiting.Reason
		}

		return "Waiting"
	}

	if status.State.Terminated != nil {
		if status.State.Terminated.Reason != "" {
			return "Terminated: " +
				status.State.Terminated.Reason
		}

		return "Terminated"
	}

	if status.State.Running != nil {
		return "Running / NotReady"
	}

	return "NotReady"
}
