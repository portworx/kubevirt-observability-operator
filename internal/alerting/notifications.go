package alerting

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	CentralNamespace       = "kubevirt-observability-system"
	SlackSecretName        = "kvo-slack-webhook"
	SlackSecretKey         = "url"
	AlertmanagerConfigName = "kvo-slack-alerts"
)

func (r *Reconciler) ensureSlackSecret(
	ctx context.Context,
	namespace string,
) (bool, error) {
	source := &corev1.Secret{}

	err := r.client.Get(
		ctx,
		types.NamespacedName{
			Namespace: CentralNamespace,
			Name:      SlackSecretName,
		},
		source,
	)

	if apierrors.IsNotFound(err) {
		ctrl.LoggerFrom(ctx).Info(
			"central Slack webhook secret not found; Slack integration disabled",
			"namespace", CentralNamespace,
			"secret", SlackSecretName,
			"targetNamespace", namespace,
		)

		// Slack integration is optional.
		return false, nil
	}

	if err != nil {
		return false, fmt.Errorf(
			"get central Slack webhook secret: %w",
			err,
		)
	}

	ctrl.LoggerFrom(ctx).Info(
		"central Slack webhook secret found",
		"namespace", CentralNamespace,
		"secret", SlackSecretName,
		"targetNamespace", namespace,
	)

	webhook, found := source.Data[SlackSecretKey]

	if !found || len(webhook) == 0 {
		// Secret exists but no usable webhook has been configured.
		return false, nil
	}

	target := &corev1.Secret{}

	key := types.NamespacedName{
		Namespace: namespace,
		Name:      SlackSecretName,
	}

	err = r.client.Get(
		ctx,
		key,
		target,
	)

	if apierrors.IsNotFound(err) {
		target = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      SlackSecretName,
				Namespace: namespace,
				Labels: map[string]string{
					ManagedByLabel: ManagedByValue,
				},
			},
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{
				SlackSecretKey: append(
					[]byte(nil),
					webhook...,
				),
			},
		}

		if err := r.client.Create(
			ctx,
			target,
		); err != nil {
			if apierrors.IsAlreadyExists(err) {
				return true, nil
			}

			return false, fmt.Errorf(
				"create namespace Slack webhook secret: %w",
				err,
			)
		}

		ctrl.LoggerFrom(ctx).Info(
			"namespace Slack webhook secret created",
			"namespace", namespace,
			"secret", SlackSecretName,
		)

		return true, nil
	}

	if err != nil {
		return false, fmt.Errorf(
			"get namespace Slack webhook secret: %w",
			err,
		)
	}

	currentWebhook := target.Data[SlackSecretKey]

	if string(currentWebhook) == string(webhook) &&
		target.Labels[ManagedByLabel] == ManagedByValue {
		return true, nil
	}

	base := target.DeepCopy()

	if target.Data == nil {
		target.Data = map[string][]byte{}
	}

	if target.Labels == nil {
		target.Labels = map[string]string{}
	}

	target.Data[SlackSecretKey] = append(
		[]byte(nil),
		webhook...,
	)

	target.Labels[ManagedByLabel] = ManagedByValue

	if err := r.client.Patch(
		ctx,
		target,
		client.MergeFrom(base),
	); err != nil {
		return false, fmt.Errorf(
			"update namespace Slack webhook secret: %w",
			err,
		)
	}

	return true, nil
}

func (r *Reconciler) ensureAlertmanagerConfig(
	ctx context.Context,
	namespace string,
) error {
	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "monitoring.coreos.com/v1alpha1",
			"kind":       "AlertmanagerConfig",

			"metadata": map[string]interface{}{
				"name":      AlertmanagerConfigName,
				"namespace": namespace,

				"labels": map[string]interface{}{
					ManagedByLabel: ManagedByValue,
				},
			},

			"spec": map[string]interface{}{
				"route": map[string]interface{}{
					// Safe default: alerts without an explicit
					// kvo_notification policy do not reach Slack.
					"receiver": "kvo-null",

					"groupBy": []interface{}{
						"alertname",
						"namespace",
						"vm_name",
					},

					"routes": []interface{}{
						map[string]interface{}{
							"receiver": "kvo-slack-immediate",

							"matchers": []interface{}{
								map[string]interface{}{
									"name":      "kvo_notification",
									"value":     "immediate",
									"matchType": "=",
								},
							},

							"groupBy": []interface{}{
								"alertname",
								"namespace",
								"vm_name",
							},

							"groupWait":      "0s",
							"groupInterval":  "5m",
							"repeatInterval": "24h",
							"continue":       false,
						},

						map[string]interface{}{
							"receiver": "kvo-slack-normal",

							"matchers": []interface{}{
								map[string]interface{}{
									"name":      "kvo_notification",
									"value":     "normal",
									"matchType": "=",
								},
							},

							// Deliberately exclude device from grouping.
							// Multiple disk alerts for the same VM are
							// delivered as one Slack notification group.
							"groupBy": []interface{}{
								"alertname",
								"namespace",
								"vm_name",
							},

							"groupWait":      "1m",
							"groupInterval":  "15m",
							"repeatInterval": "24h",
							"continue":       false,
						},
					},
				},

				"receivers": []interface{}{
					// Alerts using kvo_notification=none, or alerts
					// without a policy label, terminate here.
					map[string]interface{}{
						"name": "kvo-null",
					},

					map[string]interface{}{
						"name": "kvo-slack-normal",

						"slackConfigs": []interface{}{
							map[string]interface{}{
								"apiURL": map[string]interface{}{
									"name": SlackSecretName,
									"key":  SlackSecretKey,
								},

								"sendResolved": true,

								"title": "KubeVirt Observability: {{ .CommonLabels.alertname }}",

								"text": `{{ range .Alerts }}
Namespace: {{ .Labels.namespace }}
VM: {{ .Labels.vm_name }}
Severity: {{ .Labels.severity }}
Device: {{ .Labels.device }}
Summary: {{ .Annotations.summary }}
Description: {{ .Annotations.description }}

{{ end }}`,
							},
						},
					},

					map[string]interface{}{
						"name": "kvo-slack-immediate",

						"slackConfigs": []interface{}{
							map[string]interface{}{
								"apiURL": map[string]interface{}{
									"name": SlackSecretName,
									"key":  SlackSecretKey,
								},

								"sendResolved": true,

								"title": "URGENT - KubeVirt VM: {{ .CommonLabels.alertname }}",

								"text": `{{ range .Alerts }}
Namespace: {{ .Labels.namespace }}
VM: {{ .Labels.vm_name }}
Severity: {{ .Labels.severity }}
Summary: {{ .Annotations.summary }}
Description: {{ .Annotations.description }}

Immediate investigation recommended.

{{ end }}`,
							},
						},
					},
				},
			},
		},
	}

	return r.applyUnstructured(
		ctx,
		obj,
	)
}
