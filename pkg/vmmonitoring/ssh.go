package vmmonitoring

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"

	"golang.org/x/crypto/ssh"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func ensureSSHSecrets(
	ctx context.Context,
	kube kubernetes.Interface,
	cfg Config,
) error {
	secrets := kube.CoreV1().Secrets(cfg.Namespace)

	_, pubErr := secrets.Get(
		ctx,
		LinuxPublicKeySecret,
		metav1.GetOptions{},
	)

	_, privErr := secrets.Get(
		ctx,
		LinuxPrivateKeySecret,
		metav1.GetOptions{},
	)

	pubExists := pubErr == nil
	privExists := privErr == nil

	if pubExists && privExists {
		return nil
	}

	if pubErr != nil && !apierrors.IsNotFound(pubErr) {
		return fmt.Errorf(
			"get public SSH key secret: %w",
			pubErr,
		)
	}

	if privErr != nil && !apierrors.IsNotFound(privErr) {
		return fmt.Errorf(
			"get private SSH key secret: %w",
			privErr,
		)
	}

	// One exists but the other does not.
	if pubExists != privExists {
		return fmt.Errorf(
			"SSH key secret pair is incomplete; expected both %s and %s",
			LinuxPublicKeySecret,
			LinuxPrivateKeySecret,
		)
	}

	// Neither secret exists. Generate a new pair.
	privateKey, err := rsa.GenerateKey(
		rand.Reader,
		4096,
	)
	if err != nil {
		return fmt.Errorf(
			"generate RSA SSH key: %w",
			err,
		)
	}

	privateDER := x509.MarshalPKCS1PrivateKey(
		privateKey,
	)

	privatePEM := pem.EncodeToMemory(
		&pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: privateDER,
		},
	)

	publicKey, err := ssh.NewPublicKey(
		&privateKey.PublicKey,
	)
	if err != nil {
		return fmt.Errorf(
			"generate SSH public key: %w",
			err,
		)
	}

	publicBytes := ssh.MarshalAuthorizedKey(
		publicKey,
	)

	_, err = secrets.Create(
		ctx,
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      LinuxPublicKeySecret,
				Namespace: cfg.Namespace,
			},
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{
				"id_rsa.pub": publicBytes,
			},
		},
		metav1.CreateOptions{},
	)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf(
			"create public SSH key secret: %w",
			err,
		)
	}

	_, err = secrets.Create(
		ctx,
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      LinuxPrivateKeySecret,
				Namespace: cfg.Namespace,
			},
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{
				"id_rsa":   privatePEM,
				"username": []byte(cfg.SSHUsername),
			},
		},
		metav1.CreateOptions{},
	)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf(
			"create private SSH key secret: %w",
			err,
		)
	}

	return nil
}
