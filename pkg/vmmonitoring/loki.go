package vmmonitoring

import (
	"context"
	"fmt"
	"time"

	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func ensureLokiWriter(
	ctx context.Context,
	kube kubernetes.Interface,
	cfg Config,
) error {
	// The collector ServiceAccount is created earlier by the
	// ClusterLogForwarder deployment stage and already has
	// logging-collector-logs-writer permission.
	_, err := kube.
		CoreV1().
		ServiceAccounts(cfg.LokiNamespace).
		Get(
			ctx,
			cfg.LokiWriterSA,
			metav1.GetOptions{},
		)

	if err != nil {
		return fmt.Errorf(
			"get Loki token ServiceAccount %s/%s: %w",
			cfg.LokiNamespace,
			cfg.LokiWriterSA,
			err,
		)
	}

	return ensureLokiWriterTokenSecret(
		ctx,
		kube,
		cfg,
	)
}

func ensureLokiWriterTokenSecret(
	ctx context.Context,
	kube kubernetes.Interface,
	cfg Config,
) error {
	expirationSeconds := int64(
		24 * time.Hour / time.Second,
	)

	tokenRequest := &authenticationv1.TokenRequest{
		Spec: authenticationv1.TokenRequestSpec{
			ExpirationSeconds: &expirationSeconds,
		},
	}

	token, err := kube.
		CoreV1().
		ServiceAccounts(cfg.LokiNamespace).
		CreateToken(
			ctx,
			cfg.LokiWriterSA,
			tokenRequest,
			metav1.CreateOptions{},
		)

	if err != nil {
		return fmt.Errorf(
			"create token for ServiceAccount %s/%s: %w",
			cfg.LokiNamespace,
			cfg.LokiWriterSA,
			err,
		)
	}

	secrets := kube.
		CoreV1().
		Secrets(cfg.Namespace)

	desiredData := map[string][]byte{
		"token": []byte(token.Status.Token),
	}

	existing, err := secrets.Get(
		ctx,
		LokiWriterTokenSecret,
		metav1.GetOptions{},
	)

	if err == nil {
		existing.Data = desiredData

		_, err = secrets.Update(
			ctx,
			existing,
			metav1.UpdateOptions{},
		)

		if err != nil {
			return fmt.Errorf(
				"update Loki writer token secret: %w",
				err,
			)
		}

		return nil
	}

	if !apierrors.IsNotFound(err) {
		return fmt.Errorf(
			"get Loki writer token secret: %w",
			err,
		)
	}

	_, err = secrets.Create(
		ctx,
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      LokiWriterTokenSecret,
				Namespace: cfg.Namespace,
			},
			Type: corev1.SecretTypeOpaque,
			Data: desiredData,
		},
		metav1.CreateOptions{},
	)

	if err != nil {
		return fmt.Errorf(
			"create Loki writer token secret: %w",
			err,
		)
	}

	return nil
}
