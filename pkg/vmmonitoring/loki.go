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

const lokiWriterTokenExpirationSeconds int64 = 365 * 24 * 60 * 60

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
	secrets := kube.
		CoreV1().
		Secrets(cfg.Namespace)

	existing, err := secrets.Get(
		ctx,
		LokiWriterTokenSecret,
		metav1.GetOptions{},
	)

	if err == nil {
		token, ok := existing.Data["token"]

		if !ok || len(token) == 0 {
			return fmt.Errorf(
				"Secret %s/%s exists but token data is missing",
				cfg.Namespace,
				LokiWriterTokenSecret,
			)
		}

		expirationText := ""

		if existing.Annotations != nil {
			expirationText =
				existing.Annotations["kvo.portworx.io/token-expiration"]
		}

		if expirationText != "" {
			expiration, parseErr := time.Parse(
				time.RFC3339,
				expirationText,
			)

			if parseErr == nil &&
				time.Now().UTC().Before(expiration) {

				metadataChanged := false

				if existing.Labels == nil {
					existing.Labels = map[string]string{}
				}

				if existing.Labels["app.kubernetes.io/managed-by"] != "kvoctl" {
					existing.Labels["app.kubernetes.io/managed-by"] = "kvoctl"
					metadataChanged = true
				}

				if existing.Annotations == nil {
					existing.Annotations = map[string]string{}
				}

				tokenServiceAccount := fmt.Sprintf(
					"%s/%s",
					cfg.LokiNamespace,
					cfg.LokiWriterSA,
				)

				if existing.Annotations["kvo.portworx.io/token-service-account"] != tokenServiceAccount {
					existing.Annotations["kvo.portworx.io/token-service-account"] =
						tokenServiceAccount
					metadataChanged = true
				}

				if metadataChanged {
					if _, err := secrets.Update(
						ctx,
						existing,
						metav1.UpdateOptions{},
					); err != nil {
						return fmt.Errorf(
							"update Loki writer token metadata: %w",
							err,
						)
					}
				}

				return nil
			}
		}
	} else if !apierrors.IsNotFound(err) {
		return fmt.Errorf(
			"get Loki writer token secret: %w",
			err,
		)
	}

	expirationSeconds := lokiWriterTokenExpirationSeconds

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

	if token.Status.Token == "" {
		return fmt.Errorf(
			"token request for ServiceAccount %s/%s returned an empty token",
			cfg.LokiNamespace,
			cfg.LokiWriterSA,
		)
	}

	if token.Status.ExpirationTimestamp.IsZero() {
		return fmt.Errorf(
			"token for ServiceAccount %s/%s has no expiration timestamp",
			cfg.LokiNamespace,
			cfg.LokiWriterSA,
		)
	}

	tokenExpiration := token.
		Status.
		ExpirationTimestamp.
		Time.
		UTC().
		Format(time.RFC3339)

	desiredData := map[string][]byte{
		"token": []byte(token.Status.Token),
	}

	if existing != nil &&
		existing.Name != "" {

		existing.Data = desiredData

		if existing.Labels == nil {
			existing.Labels = map[string]string{}
		}

		existing.Labels["app.kubernetes.io/managed-by"] = "kvoctl"

		if existing.Annotations == nil {
			existing.Annotations = map[string]string{}
		}

		existing.Annotations["kvo.portworx.io/token-expiration"] = tokenExpiration
		existing.Annotations["kvo.portworx.io/token-service-account"] =
			fmt.Sprintf(
				"%s/%s",
				cfg.LokiNamespace,
				cfg.LokiWriterSA,
			)

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

	_, err = secrets.Create(
		ctx,
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      LokiWriterTokenSecret,
				Namespace: cfg.Namespace,
				Labels: map[string]string{
					"app.kubernetes.io/managed-by": "kvoctl",
				},
				Annotations: map[string]string{
					"kvo.portworx.io/token-expiration": tokenExpiration,
					"kvo.portworx.io/token-service-account": fmt.Sprintf(
						"%s/%s",
						cfg.LokiNamespace,
						cfg.LokiWriterSA,
					),
				},
			},
			Type: corev1.SecretTypeOpaque,
			Data: desiredData,
		},
		metav1.CreateOptions{},
	)

	if err != nil &&
		!apierrors.IsAlreadyExists(err) {
		return fmt.Errorf(
			"create Loki writer token secret: %w",
			err,
		)
	}

	return nil
}
