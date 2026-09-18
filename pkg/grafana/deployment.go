package grafana

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
)

const (
	grafanaContainerName = "grafana"

	datasourceVolumeName = "grafana-source-config"
	providerVolumeName   = "grafana-dash-config"
	dashboardVolumeName  = "dashboard-templates"

	datasourceMountPath = "/etc/grafana/provisioning/datasources"
	providerMountPath   = "/etc/grafana/provisioning/dashboards"
	dashboardMountPath  = "/var/lib/grafana/dashboards"
)

func EnsureDeployment(
	ctx context.Context,
	kube kubernetes.Interface,
	cfg Config,
) error {
	applyDefaults(&cfg)

	deployments := kube.
		AppsV1().
		Deployments(cfg.Namespace)

	existing, err := deployments.Get(
		ctx,
		GrafanaDeploymentName,
		metav1.GetOptions{},
	)

	if err == nil {
		if err := mergeExistingGrafanaDeployment(
			existing,
			cfg,
		); err != nil {
			return err
		}

		_, err = deployments.Update(
			ctx,
			existing,
			metav1.UpdateOptions{},
		)

		if err != nil {
			return fmt.Errorf(
				"update existing Grafana Deployment %s/%s: %w",
				cfg.Namespace,
				GrafanaDeploymentName,
				err,
			)
		}

		return nil
	}

	if !apierrors.IsNotFound(err) {
		return fmt.Errorf(
			"get Grafana Deployment %s/%s: %w",
			cfg.Namespace,
			GrafanaDeploymentName,
			err,
		)
	}

	desired := newGrafanaDeployment(cfg)

	_, err = deployments.Create(
		ctx,
		desired,
		metav1.CreateOptions{},
	)

	if err != nil &&
		!apierrors.IsAlreadyExists(err) {
		return fmt.Errorf(
			"create Grafana Deployment %s/%s: %w",
			cfg.Namespace,
			GrafanaDeploymentName,
			err,
		)
	}

	return nil
}

func mergeExistingGrafanaDeployment(
	deployment *appsv1.Deployment,
	cfg Config,
) error {
	containerIndex := findGrafanaContainer(
		deployment.Spec.Template.Spec.Containers,
	)

	if containerIndex < 0 {
		return fmt.Errorf(
			"existing Grafana Deployment %s/%s has no container named %q",
			deployment.Namespace,
			deployment.Name,
			grafanaContainerName,
		)
	}

	container := &deployment.
		Spec.
		Template.
		Spec.
		Containers[containerIndex]

	if err := ensureSecretEnvVar(
		container,
		"PROMETHEUS_TOKEN",
		PrometheusTokenSecretName,
		"token",
	); err != nil {
		return fmt.Errorf(
			"configure Prometheus token environment variable: %w",
			err,
		)
	}

	if err := ensureSecretEnvVar(
		container,
		"LOKI_TOKEN",
		LokiTokenSecretName,
		"token",
	); err != nil {
		return fmt.Errorf(
			"configure Loki token environment variable: %w",
			err,
		)
	}

	if err := ensureVolumeMount(
		container,
		datasourceVolumeName,
		datasourceMountPath,
	); err != nil {
		return err
	}

	if err := ensureVolumeMount(
		container,
		providerVolumeName,
		providerMountPath,
	); err != nil {
		return err
	}

	if err := ensureVolumeMount(
		container,
		dashboardVolumeName,
		dashboardMountPath,
	); err != nil {
		return err
	}

	podSpec := &deployment.Spec.Template.Spec

	if err := ensureConfigMapVolume(
		podSpec,
		datasourceVolumeName,
		DatasourceConfigMapName,
	); err != nil {
		return err
	}

	if err := ensureConfigMapVolume(
		podSpec,
		providerVolumeName,
		DashboardProviderConfigMapName,
	); err != nil {
		return err
	}

	if err := ensureConfigMapVolume(
		podSpec,
		dashboardVolumeName,
		DashboardsConfigMapName,
	); err != nil {
		return err
	}

	return nil
}

func findGrafanaContainer(
	containers []corev1.Container,
) int {
	for index := range containers {
		if containers[index].Name == grafanaContainerName {
			return index
		}
	}

	return -1
}

func ensureSecretEnvVar(
	container *corev1.Container,
	name string,
	secretName string,
	secretKey string,
) error {
	for index := range container.Env {
		env := &container.Env[index]

		if env.Name != name {
			continue
		}

		if env.Value != "" {
			return fmt.Errorf(
				"environment variable %q already exists with a literal value; refusing to overwrite it",
				name,
			)
		}

		if env.ValueFrom == nil ||
			env.ValueFrom.SecretKeyRef == nil {
			return fmt.Errorf(
				"environment variable %q already exists but does not reference a Secret; refusing to overwrite it",
				name,
			)
		}

		ref := env.ValueFrom.SecretKeyRef

		if ref.Name != secretName ||
			ref.Key != secretKey {
			return fmt.Errorf(
				"environment variable %q references Secret %s/%s, expected %s/%s; refusing to overwrite it",
				name,
				ref.Name,
				ref.Key,
				secretName,
				secretKey,
			)
		}

		return nil
	}

	container.Env = append(
		container.Env,
		corev1.EnvVar{
			Name: name,
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: secretName,
					},
					Key: secretKey,
				},
			},
		},
	)

	return nil
}

func ensureVolumeMount(
	container *corev1.Container,
	name string,
	mountPath string,
) error {
	for _, mount := range container.VolumeMounts {
		if mount.Name == name {
			if mount.MountPath != mountPath {
				return fmt.Errorf(
					"Grafana volume mount %q already exists at %q, expected %q; refusing to overwrite it",
					name,
					mount.MountPath,
					mountPath,
				)
			}

			return nil
		}

		if mount.MountPath == mountPath &&
			mount.Name != name {
			return fmt.Errorf(
				"Grafana mount path %q is already used by volume %q, expected %q; refusing to overwrite it",
				mountPath,
				mount.Name,
				name,
			)
		}
	}

	container.VolumeMounts = append(
		container.VolumeMounts,
		corev1.VolumeMount{
			Name:      name,
			MountPath: mountPath,
		},
	)

	return nil
}

func ensureConfigMapVolume(
	podSpec *corev1.PodSpec,
	volumeName string,
	configMapName string,
) error {
	for index := range podSpec.Volumes {
		volume := &podSpec.Volumes[index]

		if volume.Name != volumeName {
			continue
		}

		if volume.ConfigMap == nil {
			return fmt.Errorf(
				"Grafana volume %q already exists but is not a ConfigMap volume; refusing to overwrite it",
				volumeName,
			)
		}

		if volume.ConfigMap.Name != configMapName {
			return fmt.Errorf(
				"Grafana volume %q references ConfigMap %q, expected %q; refusing to overwrite it",
				volumeName,
				volume.ConfigMap.Name,
				configMapName,
			)
		}

		return nil
	}

	podSpec.Volumes = append(
		podSpec.Volumes,
		corev1.Volume{
			Name: volumeName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: configMapName,
					},
				},
			},
		},
	)

	return nil
}

func newGrafanaDeployment(
	cfg Config,
) *appsv1.Deployment {
	replicas := int32(1)

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GrafanaDeploymentName,
			Namespace: cfg.Namespace,
			Labels: map[string]string{
				"app":                          "grafana",
				"app.kubernetes.io/managed-by": "kvoctl",
			},
		},

		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,

			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "grafana",
				},
			},

			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "grafana",
					},
				},

				Spec: corev1.PodSpec{
					ServiceAccountName: GrafanaServiceAccount,

					Containers: []corev1.Container{
						{
							Name:            grafanaContainerName,
							Image:           cfg.GrafanaImage,
							ImagePullPolicy: corev1.PullIfNotPresent,

							Ports: []corev1.ContainerPort{
								{
									Name:          "http",
									ContainerPort: 3000,
									Protocol:      corev1.ProtocolTCP,
								},
							},

							Env: []corev1.EnvVar{
								{
									Name: "PROMETHEUS_TOKEN",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: PrometheusTokenSecretName,
											},
											Key: "token",
										},
									},
								},
								{
									Name: "LOKI_TOKEN",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: LokiTokenSecretName,
											},
											Key: "token",
										},
									},
								},
							},

							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/login",
										Port: intstr.FromInt(3000),
									},
								},
								PeriodSeconds:    10,
								TimeoutSeconds:   1,
								SuccessThreshold: 1,
								FailureThreshold: 3,
							},

							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("2"),
									corev1.ResourceMemory: resource.MustParse("4Gi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("4"),
									corev1.ResourceMemory: resource.MustParse("8Gi"),
								},
							},

							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      datasourceVolumeName,
									MountPath: datasourceMountPath,
								},
								{
									Name:      providerVolumeName,
									MountPath: providerMountPath,
								},
								{
									Name:      dashboardVolumeName,
									MountPath: dashboardMountPath,
								},
							},
						},
					},

					Volumes: []corev1.Volume{
						newConfigMapVolume(
							datasourceVolumeName,
							DatasourceConfigMapName,
						),
						newConfigMapVolume(
							providerVolumeName,
							DashboardProviderConfigMapName,
						),
						newConfigMapVolume(
							dashboardVolumeName,
							DashboardsConfigMapName,
						),
					},
				},
			},
		},
	}
}

func newConfigMapVolume(
	volumeName string,
	configMapName string,
) corev1.Volume {
	return corev1.Volume{
		Name: volumeName,
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: configMapName,
				},
			},
		},
	}
}
