package grafana

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	KVODatasourceConfigKey = "kvo-datasources.yaml"
)

func EnsureDatasourceConfigMap(
	ctx context.Context,
	kube kubernetes.Interface,
	cfg Config,
) error {
	applyDefaults(&cfg)

	content := fmt.Sprintf(`apiVersion: 1

datasources:
  - name: KVO Prometheus
    uid: %s
    type: prometheus
    access: proxy
    orgId: 1
    url: %s
    isDefault: false
    editable: true
    jsonData:
      httpHeaderName1: Authorization
      tlsSkipVerify: true
      timeInterval: 5s
    secureJsonData:
      httpHeaderValue1: "Bearer ${PROMETHEUS_TOKEN}"

  - name: KVO Loki
    uid: %s
    type: loki
    access: proxy
    orgId: 1
    url: %s
    isDefault: false
    editable: true
    jsonData:
      httpHeaderName1: Authorization
      httpHeaderName2: X-Scope-OrgID
      tlsSkipVerify: true
    secureJsonData:
      httpHeaderValue1: "Bearer ${LOKI_TOKEN}"
      httpHeaderValue2: "application"
`,
		cfg.PrometheusUID,
		cfg.PrometheusURL,
		cfg.LokiUID,
		cfg.LokiURL,
	)

	return ensureConfigMapData(
		ctx,
		kube,
		cfg.Namespace,
		DatasourceConfigMapName,
		map[string]string{
			KVODatasourceConfigKey: content,
		},
	)
}

func ensureConfigMapData(
	ctx context.Context,
	kube kubernetes.Interface,
	namespace string,
	name string,
	desiredData map[string]string,
) error {
	configMaps := kube.
		CoreV1().
		ConfigMaps(namespace)

	existing, err := configMaps.Get(
		ctx,
		name,
		metav1.GetOptions{},
	)

	if err == nil {
		if existing.Data == nil {
			existing.Data = map[string]string{}
		}

		// Merge only KVO-owned keys.
		// Existing dashboards/datasources remain untouched.
		for key, value := range desiredData {
			existing.Data[key] = value
		}

		_, err = configMaps.Update(
			ctx,
			existing,
			metav1.UpdateOptions{},
		)

		if err != nil {
			return fmt.Errorf(
				"update ConfigMap %s/%s: %w",
				namespace,
				name,
				err,
			)
		}

		return nil
	}

	if !apierrors.IsNotFound(err) {
		return fmt.Errorf(
			"get ConfigMap %s/%s: %w",
			namespace,
			name,
			err,
		)
	}

	_, err = configMaps.Create(
		ctx,
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				Labels: map[string]string{
					"app.kubernetes.io/managed-by": "kvoctl",
				},
			},
			Data: desiredData,
		},
		metav1.CreateOptions{},
	)

	if err != nil &&
		!apierrors.IsAlreadyExists(err) {
		return fmt.Errorf(
			"create ConfigMap %s/%s: %w",
			namespace,
			name,
			err,
		)
	}

	return nil
}
