package alerting

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/yaml"
)

func enableUserWorkloadAlertmanager(
	ctx context.Context,
	client kubernetes.Interface,
) error {
	configMaps := client.CoreV1().
		ConfigMaps(UserWorkloadNamespace)

	cm, err := configMaps.Get(
		ctx,
		UserWorkloadConfigMap,
		metav1.GetOptions{},
	)

	if apierrors.IsNotFound(err) {
		return fmt.Errorf(
			"user workload monitoring ConfigMap %s/%s not found",
			UserWorkloadNamespace,
			UserWorkloadConfigMap,
		)
	}

	if err != nil {
		return fmt.Errorf(
			"get user workload monitoring ConfigMap: %w",
			err,
		)
	}

	if cm.Data == nil {
		cm.Data = map[string]string{}
	}

	current := cm.Data["config.yaml"]

	config := map[string]interface{}{}

	if current != "" {
		if err := yaml.Unmarshal(
			[]byte(current),
			&config,
		); err != nil {
			return fmt.Errorf(
				"parse existing user workload monitoring config: %w",
				err,
			)
		}
	}

	alertmanager, ok := config["alertmanager"].(map[string]interface{})
	if !ok || alertmanager == nil {
		alertmanager = map[string]interface{}{}
	}

	alertmanager["enabled"] = true
	alertmanager["enableAlertmanagerConfig"] = true

	config["alertmanager"] = alertmanager

	updated, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf(
			"marshal user workload monitoring config: %w",
			err,
		)
	}

	cm.Data["config.yaml"] = string(updated)

	_, err = configMaps.Update(
		ctx,
		cm,
		metav1.UpdateOptions{},
	)
	if err != nil {
		return fmt.Errorf(
			"update user workload monitoring ConfigMap: %w",
			err,
		)
	}

	return nil
}
