package alerting

import (
	"context"
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/portworx/kubevirt-observability-operator/alerts"
)

const (
	MetricsRuleName    = "vm-metrics-alerts"
	LokiNamespaceLabel = "vm-monitoring-alerts"
	LokiNamespaceValue = "enabled"

	ManagedByLabel = "app.kubernetes.io/managed-by"
	ManagedByValue = "kubevirt-observability"
)

type Reconciler struct {
	client client.Client
}

func NewReconciler(
	c client.Client,
) *Reconciler {
	return &Reconciler{
		client: c,
	}
}

func (r *Reconciler) ReconcileNamespace(
	ctx context.Context,
	namespace string,
) error {
	if namespace == "" {
		return fmt.Errorf("namespace is required")
	}

	if err := r.ensureNamespaceLabels(
		ctx,
		namespace,
	); err != nil {
		return fmt.Errorf(
			"ensure namespace alerting labels: %w",
			err,
		)
	}

	if err := r.ensurePrometheusRule(
		ctx,
		namespace,
	); err != nil {
		return fmt.Errorf(
			"ensure PrometheusRule: %w",
			err,
		)
	}

	if err := r.ensureLokiAlertingRule(
		ctx,
		namespace,
	); err != nil {
		return fmt.Errorf(
			"ensure Loki AlertingRule: %w",
			err,
		)
	}

	// Notification delivery is configured centrally by kvoctl.
	// The operator only reconciles namespace-scoped alert definitions.

	// Slack integration is optional. kvoctl stores the canonical
	// webhook in kubevirt-observability-system. AlertmanagerConfig
	// requires the referenced Secret to exist in the same namespace,
	// so mirror it into each monitored namespace.
	slackConfigured, err := r.ensureSlackSecret(
		ctx,
		namespace,
	)
	if err != nil {
		return fmt.Errorf(
			"ensure Slack webhook secret: %w",
			err,
		)
	}

	if !slackConfigured {
		return nil
	}

	if err := r.ensureAlertmanagerConfig(
		ctx,
		namespace,
	); err != nil {
		return fmt.Errorf(
			"ensure AlertmanagerConfig: %w",
			err,
		)
	}

	return nil
}

func (r *Reconciler) ensureNamespaceLabels(
	ctx context.Context,
	namespace string,
) error {
	ns := &corev1.Namespace{}

	if err := r.client.Get(
		ctx,
		types.NamespacedName{
			Name: namespace,
		},
		ns,
	); err != nil {
		return err
	}

	if ns.Labels != nil &&
		ns.Labels[LokiNamespaceLabel] == LokiNamespaceValue {
		return nil
	}

	base := ns.DeepCopy()

	if ns.Labels == nil {
		ns.Labels = map[string]string{}
	}

	ns.Labels[LokiNamespaceLabel] = LokiNamespaceValue

	return r.client.Patch(
		ctx,
		ns,
		client.MergeFrom(base),
	)
}

func (r *Reconciler) ensurePrometheusRule(
	ctx context.Context,
	namespace string,
) error {
	rendered, err := alerts.RenderVMMetrics(namespace)
	if err != nil {
		return err
	}

	obj, err := decodeUnstructured(rendered)
	if err != nil {
		return fmt.Errorf(
			"decode PrometheusRule template: %w",
			err,
		)
	}

	setManagedByLabel(obj)

	return r.applyUnstructured(
		ctx,
		obj,
	)
}

func (r *Reconciler) ensureLokiAlertingRule(
	ctx context.Context,
	namespace string,
) error {
	rendered, err := alerts.RenderVMLoki(namespace)
	if err != nil {
		return err
	}

	obj, err := decodeUnstructured(rendered)
	if err != nil {
		return fmt.Errorf(
			"decode Loki AlertingRule template: %w",
			err,
		)
	}

	setManagedByLabel(obj)

	return r.applyUnstructured(
		ctx,
		obj,
	)
}

func (r *Reconciler) applyUnstructured(
	ctx context.Context,
	desired *unstructured.Unstructured,
) error {
	current := &unstructured.Unstructured{}
	current.SetGroupVersionKind(
		desired.GroupVersionKind(),
	)

	key := client.ObjectKeyFromObject(desired)

	err := r.client.Get(
		ctx,
		key,
		current,
	)

	if meta.IsNoMatchError(err) {
		// Optional platform capability is unavailable.
		return nil
	}

	if apierrors.IsNotFound(err) {
		if err := r.client.Create(
			ctx,
			desired,
		); err != nil {
			if apierrors.IsAlreadyExists(err) {
				return nil
			}

			if meta.IsNoMatchError(err) {
				return nil
			}

			return err
		}

		return nil
	}

	if err != nil {
		return err
	}

	base := current.DeepCopy()

	desired.SetResourceVersion(
		current.GetResourceVersion(),
	)

	// Preserve API-server/controller managed metadata.
	desired.SetUID(
		current.GetUID(),
	)

	desired.SetCreationTimestamp(
		current.GetCreationTimestamp(),
	)

	desired.SetGeneration(
		current.GetGeneration(),
	)

	return r.client.Patch(
		ctx,
		desired,
		client.MergeFrom(base),
	)
}

func decodeUnstructured(
	data []byte,
) (*unstructured.Unstructured, error) {
	jsonData, err := yaml.ToJSON(data)
	if err != nil {
		return nil, err
	}

	var object map[string]interface{}

	if err := json.Unmarshal(
		jsonData,
		&object,
	); err != nil {
		return nil, err
	}

	obj := &unstructured.Unstructured{
		Object: object,
	}

	if obj.GetName() == "" ||
		obj.GetNamespace() == "" ||
		obj.GetKind() == "" {
		return nil, fmt.Errorf(
			"rendered alert resource is missing name, namespace, or kind",
		)
	}

	return obj, nil
}

func setManagedByLabel(
	obj *unstructured.Unstructured,
) {
	labels := obj.GetLabels()

	if labels == nil {
		labels = map[string]string{}
	}

	labels[ManagedByLabel] = ManagedByValue

	obj.SetLabels(labels)
}
