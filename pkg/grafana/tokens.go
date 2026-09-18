package grafana

import (
	"context"
	"fmt"

	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const datasourceTokenExpirationSeconds int64 = 365 * 24 * 60 * 60

func ensureTokenSecret(
	ctx context.Context,
	kube kubernetes.Interface,
	targetNamespace string,
	secretName string,
	saNamespace string,
	saName string,
) error {
	// Make sure the ServiceAccount that will own the token exists.
	if _, err := kube.CoreV1().
		ServiceAccounts(saNamespace).
		Get(
			ctx,
			saName,
			metav1.GetOptions{},
		); err != nil {
		return fmt.Errorf(
			"get ServiceAccount %s/%s: %w",
			saNamespace,
			saName,
			err,
		)
	}

	secrets := kube.
		CoreV1().
		Secrets(targetNamespace)

	/*
	   Reuse an existing token Secret.

	   This is intentionally idempotent. Reissuing the token on every
	   kvoctl deploy would update the Secret while the running Grafana
	   process continued using the old environment-variable value.
	*/
	existing, err := secrets.Get(
		ctx,
		secretName,
		metav1.GetOptions{},
	)

	if err == nil {
		token, ok := existing.Data["token"]

		if !ok || len(token) == 0 {
			return fmt.Errorf(
				"Secret %s/%s exists but token data is missing",
				targetNamespace,
				secretName,
			)
		}

		return nil
	}

	if !apierrors.IsNotFound(err) {
		return fmt.Errorf(
			"get Secret %s/%s: %w",
			targetNamespace,
			secretName,
			err,
		)
	}

	/*
	   The Secret does not exist, so request a new one-year
	   ServiceAccount token.
	*/
	expiration := datasourceTokenExpirationSeconds

	tokenRequest, err := kube.CoreV1().
		ServiceAccounts(saNamespace).
		CreateToken(
			ctx,
			saName,
			&authenticationv1.TokenRequest{
				Spec: authenticationv1.TokenRequestSpec{
					ExpirationSeconds: &expiration,
				},
			},
			metav1.CreateOptions{},
		)

	if err != nil {
		return fmt.Errorf(
			"create token for ServiceAccount %s/%s: %w",
			saNamespace,
			saName,
			err,
		)
	}

	if tokenRequest.Status.Token == "" {
		return fmt.Errorf(
			"token request for ServiceAccount %s/%s returned an empty token",
			saNamespace,
			saName,
		)
	}

	if tokenRequest.Status.ExpirationTimestamp.IsZero() {
		return fmt.Errorf(
			"token for ServiceAccount %s/%s has no expiration timestamp",
			saNamespace,
			saName,
		)
	}

	_, err = secrets.Create(
		ctx,
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: targetNamespace,
				Labels: map[string]string{
					"app.kubernetes.io/managed-by": "kvoctl",
				},
				Annotations: map[string]string{
					"kvo.portworx.io/token-service-account": fmt.Sprintf(
						"%s/%s",
						saNamespace,
						saName,
					),
					"kvo.portworx.io/token-expiration": tokenRequest.
						Status.
						ExpirationTimestamp.
						Time.
						UTC().
						Format("2006-01-02T15:04:05Z"),
				},
			},
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{
				"token": []byte(tokenRequest.Status.Token),
			},
		},
		metav1.CreateOptions{},
	)

	if err != nil &&
		!apierrors.IsAlreadyExists(err) {
		return fmt.Errorf(
			"create Secret %s/%s: %w",
			targetNamespace,
			secretName,
			err,
		)
	}

	return nil
}

func EnsureDatasourceTokenSecrets(
	ctx context.Context,
	kube kubernetes.Interface,
	cfg Config,
) error {
	applyDefaults(&cfg)

	if err := ensureTokenSecret(
		ctx,
		kube,
		cfg.Namespace,
		PrometheusTokenSecretName,
		cfg.PrometheusTokenNamespace,
		cfg.PrometheusTokenServiceAccount,
	); err != nil {
		return fmt.Errorf(
			"ensure Prometheus token: %w",
			err,
		)
	}

	if err := ensureTokenSecret(
		ctx,
		kube,
		cfg.Namespace,
		LokiTokenSecretName,
		cfg.LokiTokenNamespace,
		cfg.LokiTokenServiceAccount,
	); err != nil {
		return fmt.Errorf(
			"ensure Loki token: %w",
			err,
		)
	}

	return nil
}
