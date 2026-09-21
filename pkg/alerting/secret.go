package alerting

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func ensureNamespace(
	ctx context.Context,
	client kubernetes.Interface,
	namespace string,
) error {
	namespaces := client.CoreV1().Namespaces()

	_, err := namespaces.Get(
		ctx,
		namespace,
		metav1.GetOptions{},
	)
	if err == nil {
		return nil
	}

	if !apierrors.IsNotFound(err) {
		return fmt.Errorf("get namespace %s: %w", namespace, err)
	}

	_, err = namespaces.Create(
		ctx,
		&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
		},
		metav1.CreateOptions{},
	)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create namespace %s: %w", namespace, err)
	}

	return nil
}

func ensureSlackSecret(
	ctx context.Context,
	client kubernetes.Interface,
	cfg Config,
) error {
	if err := ensureNamespace(
		ctx,
		client,
		cfg.Namespace,
	); err != nil {
		return err
	}

	secrets := client.CoreV1().Secrets(cfg.Namespace)

	existing, err := secrets.Get(
		ctx,
		cfg.SecretName,
		metav1.GetOptions{},
	)

	if apierrors.IsNotFound(err) {
		_, err = secrets.Create(
			ctx,
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cfg.SecretName,
					Namespace: cfg.Namespace,
					Labels: map[string]string{
						"app.kubernetes.io/name":      "kubevirt-observability",
						"app.kubernetes.io/component": "alerting",
					},
				},
				Type: corev1.SecretTypeOpaque,
				StringData: map[string]string{
					SlackSecretKey: cfg.SlackWebhookURL,
				},
			},
			metav1.CreateOptions{},
		)
		if err != nil {
			return fmt.Errorf("create Slack webhook secret: %w", err)
		}

		return nil
	}

	if err != nil {
		return fmt.Errorf("get Slack webhook secret: %w", err)
	}

	if existing.Data == nil {
		existing.Data = map[string][]byte{}
	}

	existing.Data[SlackSecretKey] = []byte(cfg.SlackWebhookURL)

	if existing.Labels == nil {
		existing.Labels = map[string]string{}
	}

	existing.Labels["app.kubernetes.io/name"] = "kubevirt-observability"
	existing.Labels["app.kubernetes.io/component"] = "alerting"

	_, err = secrets.Update(
		ctx,
		existing,
		metav1.UpdateOptions{},
	)
	if err != nil {
		return fmt.Errorf("update Slack webhook secret: %w", err)
	}

	return nil
}
