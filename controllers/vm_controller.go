package controllers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"go.yaml.in/yaml/v2"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	kubevirtv1 "kubevirt.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"

	"github.com/portworx/kubevirt-observability-operator/api"
	"github.com/portworx/kubevirt-observability-operator/internal/alloyconfig"
	"github.com/portworx/kubevirt-observability-operator/internal/cloudinitmerge"
	"github.com/portworx/kubevirt-observability-operator/internal/osdetect"
	"github.com/portworx/kubevirt-observability-operator/internal/remediation"
	"github.com/portworx/kubevirt-observability-operator/internal/sysprepmerge"
	"github.com/portworx/kubevirt-observability-operator/internal/verify"
	routev1 "github.com/openshift/api/route/v1"
)

const (
	grafanaNamespace      = "grafana"
	grafanaServiceAccount = "grafana"
	loggingNamespace      = "openshift-logging"
	loggingGrafanaSA      = "grafana"
)

type VMReconciler struct {
	client.Client
	Recorder record.EventRecorder
}

// Reconcile reconciles the VM.
func (r *VMReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	vm := &kubevirtv1.VirtualMachine{}
	if err := r.Get(ctx, req.NamespacedName, vm); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if vm.Annotations[api.AnnBootstrapManaged] != "true" {
		return ctrl.Result{}, nil
	}

	osFamily := osdetect.Detect(vm)

	ann := vm.GetAnnotations()
	if ann != nil &&
		osFamily != osdetect.Unknown &&
		(ann[api.AnnBootstrapOS] == "unknown" || ann[api.AnnExporter] == "unknown") {
		base := vm.DeepCopy()
		delete(ann, api.AnnBootstrapOS)
		delete(ann, api.AnnExporter)
		delete(ann, api.AnnReason)
		delete(ann, api.AnnStatus)
		vm.SetAnnotations(ann)
		if err := r.Patch(ctx, vm, client.MergeFrom(base)); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
	}

	if osFamily == osdetect.Unknown {
		msg := "missing or unsupported VM OS label; set kubevirt-observability.io/os=linux or kubevirt-observability.io/os=windows"
		r.Recorder.Eventf(vm, corev1.EventTypeWarning, "OSDetectionSkipped", msg)
		return r.mark(ctx, vm, api.StatusSkipped, "missing-os-label")
	}

	if osFamily == osdetect.Windows {
		if err := r.ensureWindowsSSHSecret(ctx, vm.Namespace); err != nil {
			return ctrl.Result{}, err
		}
	}

	if err := r.ensureNamespaceMonitoringLabel(ctx, vm.Namespace); err != nil {
		r.Recorder.Eventf(vm, corev1.EventTypeWarning, "NamespaceLabelFailed", "Failed to label namespace for monitoring: %v", err)
		return ctrl.Result{}, err
	}

	if osFamily == osdetect.Linux || osFamily == osdetect.Windows {
		if err := r.ensureMonitoringSSHSecretInNamespace(ctx, vm.Namespace); err != nil {
			r.Recorder.Eventf(vm, corev1.EventTypeWarning, "MonitoringSecretError", "Failed to ensure monitoring secret: %v", err)
			return r.mark(ctx, vm, api.StatusPending, "monitoring-secret-error")
		}
	}
	if err := r.ensureGrafanaLokiReadRBAC(ctx, vm.Namespace); err != nil {
		r.Recorder.Eventf(vm, corev1.EventTypeWarning, "GrafanaLokiRBACFailed", "Failed to ensure Grafana Loki RBAC: %v", err)
		return ctrl.Result{}, err
	}

	if vm.Annotations[api.AnnRemediationRequired] == "true" {
		vmi, exists, err := r.getVMI(ctx, vm)
		if err != nil {
			return ctrl.Result{}, err
		}
		if !exists {
			switch osFamily {
			case osdetect.Linux:
				return r.mark(ctx, vm, api.StatusPending, "waiting-for-vmi-for-linux-remediation")
			case osdetect.Windows:
				return r.mark(ctx, vm, api.StatusPending, "waiting-for-vmi-for-windows-remediation")
			default:
				return r.mark(ctx, vm, api.StatusSkipped, "unknown-os")
			}
		}

		ip := firstIP(vmi)
		if ip == "" {
			switch osFamily {
			case osdetect.Linux:
				return r.mark(ctx, vm, api.StatusPending, "waiting-for-ip-for-linux-remediation")
			case osdetect.Windows:
				return r.mark(ctx, vm, api.StatusPending, "waiting-for-ip-for-windows-remediation")
			default:
				return r.mark(ctx, vm, api.StatusSkipped, "unknown-os")
			}
		}
		if ann["kubevirt-observability.io/bootstrap-started-at"] == "" {

			base := vm.DeepCopy()

			ann["kubevirt-observability.io/bootstrap-started-at"] =
				time.Now().UTC().Format(time.RFC3339)

			vm.SetAnnotations(ann)

			if err := r.Patch(ctx, vm, client.MergeFrom(base)); err != nil {
				return ctrl.Result{}, err
			}
		}

		switch osFamily {
		case osdetect.Linux:
			addr := ip + ":22"
			if !remediation.ProbeSSH(addr, 3*time.Second) {
				return r.mark(ctx, vm, api.StatusPending, "waiting-for-ssh-for-linux-remediation")
			}

			priv, defaultUser, err := r.getLinuxPrivateKeyAndUser(ctx)
			if err != nil {
				r.Recorder.Eventf(vm, corev1.EventTypeWarning, "LinuxRemediationSecretError", "Failed to get linux remediation private key: %v", err)
				_, markErr := r.markRemediation(ctx, vm, false, err.Error())
				if markErr != nil {
					return ctrl.Result{}, markErr
				}
				return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
			}

			user, err := r.detectLinuxSSHUser(vm, defaultUser)
			if err != nil {
				r.Recorder.Eventf(vm, corev1.EventTypeWarning, "LinuxSSHUserMissing", "%v", err)
				return r.mark(ctx, vm, api.StatusPending, "linux-ssh-user-missing")
			}

			err = remediation.RunLinuxBootstrap(remediation.LinuxSSHConfig{
				Address:    addr,
				Username:   user,
				PrivateKey: priv,
				Timeout:    10 * time.Second,
			}, remediation.LinuxNodeExporterInstallScript())
			if err != nil {
				r.Recorder.Eventf(vm, corev1.EventTypeWarning, "LinuxRemediationFailed", "Linux remediation failed over SSH: %v", err)
				_, markErr := r.markRemediation(ctx, vm, false, err.Error())
				if markErr != nil {
					return ctrl.Result{}, markErr
				}
				return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
			}

			r.Recorder.Eventf(vm, corev1.EventTypeNormal, "LinuxRemediationCompleted", "Linux remediation completed over SSH")
			if _, err := r.markRemediation(ctx, vm, true, ""); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil

		case osdetect.Windows:
			priv, _, err := r.getLinuxPrivateKeyAndUser(ctx)
			if err != nil {
				r.Recorder.Eventf(
					vm,
					corev1.EventTypeWarning,
					"WindowsRemediationSecretError",
					"Failed to get windows remediation SSH key: %v",
					err,
				)

				_, markErr := r.markRemediation(ctx, vm, false, err.Error())
				if markErr != nil {
					return ctrl.Result{}, markErr
				}

				return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
			}

			sshCfg := remediation.WindowsSSHConfig{
				Address: ip + ":22",
				User:    "Administrator",
				KeyPEM:  priv,
				Timeout: 20 * time.Second,
			}

			state, stateErr := remediation.GetWindowsBootstrapState(sshCfg)

			if stateErr != nil {
				return r.mark(ctx, vm, api.StatusPending, "waiting-for-bootstrap-state")
			}

			switch state {
			case "success":
				// bootstrap succeeded
				// remediation not required
				base := vm.DeepCopy()

				if vm.Annotations == nil {
					vm.Annotations = map[string]string{}
				}

				vm.Annotations[api.AnnRemediationRequired] = "false"
				delete(vm.Annotations, api.AnnAlloyError)

				if err := r.Patch(ctx, vm, client.MergeFrom(base)); err != nil {
					return ctrl.Result{}, err
				}

				return ctrl.Result{RequeueAfter: 5 * time.Second}, nil

			case "unknown":
				if !bootstrapTimedOut(vm, 10*time.Minute) {
					return r.mark(ctx, vm, api.StatusPending, "bootstrap-in-progress")
				}

				// timeout exceeded
				// remediation allowed

			case "failed":
				// remediation allowed immediately
			}

			err = remediation.RunWindowsSSHBootstrap(remediation.WindowsSSHConfig{
				Address: ip + ":22",
				User:    "Administrator",
				KeyPEM:  priv,
				Timeout: 20 * time.Second,
			}, remediation.WindowsExporterInstallScript())
			if err != nil {
				r.Recorder.Eventf(vm, corev1.EventTypeWarning, "WindowsRemediationFailed", "Windows remediation failed over SSH: %v", err)
				_, markErr := r.markRemediation(ctx, vm, false, err.Error())
				if markErr != nil {
					return ctrl.Result{}, markErr
				}
				return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
			}

			r.Recorder.Eventf(vm, corev1.EventTypeNormal, "WindowsRemediationCompleted", "Windows remediation completed over SSH")
			if _, err := r.markRemediation(ctx, vm, true, ""); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil

		default:
			return r.mark(ctx, vm, api.StatusSkipped, "unknown-os")
		}
	}

	if vm.Annotations[api.AnnSysprepMergeRequired] == "true" &&
		vm.Annotations[api.AnnSysprepMerged] != "true" {
		done, err := r.reconcileSysprepSource(ctx, vm)
		if err != nil {
			r.Recorder.Eventf(vm, corev1.EventTypeWarning, "SysprepMergeFailed", "Sysprep merge failed: %v", err)
			_, markErr := r.markSysprep(ctx, vm, false, err.Error())
			if markErr != nil {
				return ctrl.Result{}, markErr
			}
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}
		if done {
			r.Recorder.Eventf(vm, corev1.EventTypeNormal, "SysprepMerged", "Sysprep source merged successfully")
			if _, err := r.markSysprep(ctx, vm, true, ""); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}
	}

	if vm.Annotations[api.AnnCloudInitSecretMergeRequired] == "true" &&
		vm.Annotations[api.AnnCloudInitSecretMerged] != "true" {
		done, err := r.reconcileCloudInitSecret(ctx, vm)
		if err != nil {
			r.Recorder.Eventf(vm, corev1.EventTypeWarning, "CloudInitSecretMergeFailed", "Cloud-init Secret merge failed: %v", err)
			_, markErr := r.markCloudInitSecret(ctx, vm, false, err.Error())
			if markErr != nil {
				return ctrl.Result{}, markErr
			}
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}
		if done {
			r.Recorder.Eventf(vm, corev1.EventTypeNormal, "CloudInitSecretMerged", "Cloud-init Secret merged successfully")
			if _, err := r.markCloudInitSecret(ctx, vm, true, ""); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}
	}

	vmi, exists, err := r.getVMI(ctx, vm)
	if err != nil {
		return ctrl.Result{}, err
	}
	if !exists {
		return r.mark(ctx, vm, api.StatusPending, "waiting-for-vmi")
	}
	return r.syncMetricsScrapeResources(ctx, vm, vmi, osdetect.Detect(vm))
}

// reconcileSysprepSource reconciles the sysprep source of the given VM.
func (r *VMReconciler) reconcileSysprepSource(ctx context.Context, vm *kubevirtv1.VirtualMachine) (bool, error) {
	vol, err := findSysprepVolume(vm)
	if err != nil {
		return false, err
	}

	switch {
	case vol.Sysprep.ConfigMap != nil:
		return r.mergeSysprepConfigMap(ctx, vm, vol.Sysprep.ConfigMap.Name)
	case vol.Sysprep.Secret != nil:
		return r.mergeSysprepSecret(ctx, vm, vol.Sysprep.Secret.Name)
	default:
		return false, fmt.Errorf("sysprep volume found but no ConfigMap or Secret source is set")
	}
}

// mergeSysprepConfigMap merges the sysprep ConfigMap of the given VM.
func (r *VMReconciler) mergeSysprepConfigMap(ctx context.Context, vm *kubevirtv1.VirtualMachine, name string) (bool, error) {
	cm := &corev1.ConfigMap{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: vm.Namespace, Name: name}, cm); err != nil {
		return false, err
	}

	mergedData, key, changed, err := sysprepmerge.MergeMapData(cm.Data)
	if err != nil {
		return false, fmt.Errorf("configmap %s: %w", name, err)
	}
	if !changed {
		return true, nil
	}

	base := cm.DeepCopy()
	cm.Data = mergedData

	if err := r.Patch(ctx, cm, client.MergeFrom(base)); err != nil {
		return false, err
	}

	r.Recorder.Eventf(vm, corev1.EventTypeNormal, "SysprepConfigMapPatched", "Patched ConfigMap %s key %s", name, key)
	return true, nil
}

// mergeSysprepSecret merges the sysprep Secret of the given VM.
func (r *VMReconciler) mergeSysprepSecret(ctx context.Context, vm *kubevirtv1.VirtualMachine, name string) (bool, error) {
	secret := &corev1.Secret{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: vm.Namespace, Name: name}, secret); err != nil {
		return false, err
	}

	mergedData, key, changed, err := sysprepmerge.MergeByteData(secret.Data)
	if err != nil {
		return false, fmt.Errorf("secret %s: %w", name, err)
	}
	if !changed {
		return true, nil
	}

	base := secret.DeepCopy()
	secret.Data = mergedData

	if err := r.Patch(ctx, secret, client.MergeFrom(base)); err != nil {
		return false, err
	}

	r.Recorder.Eventf(vm, corev1.EventTypeNormal, "SysprepSecretPatched", "Patched Secret %s key %s", name, key)
	return true, nil
}

// findSysprepVolume finds the sysprep volume of the given VM.
func findSysprepVolume(vm *kubevirtv1.VirtualMachine) (*kubevirtv1.Volume, error) {
	for i := range vm.Spec.Template.Spec.Volumes {
		v := &vm.Spec.Template.Spec.Volumes[i]
		if v.Sysprep != nil {
			return v, nil
		}
	}
	return nil, fmt.Errorf("no sysprep volume found on VM")
}

// markSysprep marks the sysprep merge status of the given VM.
func (r *VMReconciler) markSysprep(ctx context.Context, vm *kubevirtv1.VirtualMachine, merged bool, errMsg string) (ctrl.Result, error) {
	base := vm.DeepCopy()
	ann := vm.GetAnnotations()
	if ann == nil {
		ann = map[string]string{}
	}

	if merged {
		ann[api.AnnSysprepMerged] = "true"
		delete(ann, api.AnnSysprepMergeError)
	} else {
		ann[api.AnnSysprepMergeError] = errMsg
	}

	vm.SetAnnotations(ann)

	if err := r.Patch(ctx, vm, client.MergeFrom(base)); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// mark marks the given VM with the given status and reason.
func (r *VMReconciler) mark(ctx context.Context, vm *kubevirtv1.VirtualMachine, status, reason string) (ctrl.Result, error) {
	ann := vm.GetAnnotations()
	if ann == nil {
		ann = map[string]string{}
	}

	// Avoid unnecessary API patches when status/reason already match.
	if ann[api.AnnStatus] == status && ann[api.AnnReason] == reason {
		if status == api.StatusReady || status == api.StatusSkipped {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	base := vm.DeepCopy()

	ann = vm.GetAnnotations()
	if ann == nil {
		ann = map[string]string{}
	}

	ann[api.AnnStatus] = status
	ann[api.AnnReason] = reason
	vm.SetAnnotations(ann)

	if err := r.Patch(ctx, vm, client.MergeFrom(base)); err != nil {
		return ctrl.Result{}, err
	}

	if status == api.StatusReady || status == api.StatusSkipped {
		return ctrl.Result{}, nil
	}

	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

// firstIP returns the first IP address of the given VMI.
func firstIP(vmi *kubevirtv1.VirtualMachineInstance) string {
	for _, iface := range vmi.Status.Interfaces {
		if iface.IP != "" {
			return iface.IP
		}
	}
	return ""
}

// SetupWithManager sets up the controller with the given manager.
func (r *VMReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kubevirtv1.VirtualMachine{}).
		Owns(&kubevirtv1.VirtualMachineInstance{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: 5,
			RateLimiter: workqueue.NewTypedItemExponentialFailureRateLimiter[ctrl.Request](
				5*time.Second,
				5*time.Minute,
			),
		}).
		Complete(r)
}

// reconcileCloudInitSecret reconciles the cloud-init secret of the given VM.
func (r *VMReconciler) reconcileCloudInitSecret(
	ctx context.Context,
	vm *kubevirtv1.VirtualMachine,
) (bool, error) {

	vol, err := findCloudInitNoCloudSecretVolume(vm)
	if err != nil {
		return false, err
	}

	secretName := vol.CloudInitNoCloud.UserDataSecretRef.Name

	secret := &corev1.Secret{}
	err = r.Get(ctx, client.ObjectKey{
		Namespace: vm.Namespace,
		Name:      secretName,
	}, secret)

	if err != nil {
		if !apierrors.IsNotFound(err) {
			return false, err
		}

		secret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: vm.Namespace,
				Labels: map[string]string{
					"app": "kubevirt-observability",
				},
			},
			Data: map[string][]byte{
				"userdata": []byte("#cloud-config\n"),
			},
		}

		if err := r.Create(ctx, secret); err != nil {
			return false, err
		}
	}

	var mergeOS cloudinitmerge.OSType

	switch osdetect.Detect(vm) {

	case osdetect.Linux:
		mergeOS = cloudinitmerge.OSLinux

		_, defaultUser, err := r.getLinuxPrivateKeyAndUser(ctx)
		if err != nil {
			return false, err
		}

		user, err := r.detectLinuxSSHUser(vm, defaultUser)
		if err != nil {
			return false, err
		}

		pubKey, err := r.getLinuxPublicKey(ctx, vm.Namespace)
		if err != nil {
			return false, err
		}

		if err := injectSSHMergeHints(secret.Data, user, string(pubKey)); err != nil {
			return false, err
		}
	case osdetect.Windows:
		mergeOS = cloudinitmerge.OSWindows

		pubKey, err := r.getWindowsPublicKey(ctx, vm.Namespace)
		if err != nil {
			return false, err
		}

		if err := injectSSHMergeHints(
			secret.Data,
			"Administrator",
			string(pubKey),
		); err != nil {
			return false, err
		}

	default:
		return false, fmt.Errorf("unknown OS for cloud-init secret merge")
	}

	mergedData, key, changed, err := cloudinitmerge.MergeSecretData(
		secret.Data,
		mergeOS,
	)
	if err != nil {
		return false, fmt.Errorf("secret %s: %w", secretName, err)
	}

	if !changed {
		return true, nil
	}

	base := secret.DeepCopy()

	secret.Data = mergedData

	if err := r.Patch(ctx, secret, client.MergeFrom(base)); err != nil {
		return false, err
	}

	r.Recorder.Eventf(
		vm,
		corev1.EventTypeNormal,
		"CloudInitSecretPatched",
		"Patched Secret %s key %s",
		secretName,
		key,
	)

	return true, nil
}

// findCloudInitNoCloudSecretVolume finds the cloud-init secret volume of the given VM.
func findCloudInitNoCloudSecretVolume(vm *kubevirtv1.VirtualMachine) (*kubevirtv1.Volume, error) {
	for i := range vm.Spec.Template.Spec.Volumes {
		v := &vm.Spec.Template.Spec.Volumes[i]
		if v.CloudInitNoCloud != nil && v.CloudInitNoCloud.UserDataSecretRef != nil {
			return v, nil
		}
	}
	return nil, fmt.Errorf("no cloudInitNoCloud secretRef volume found on VM")
}

// markCloudInitSecret marks the cloud-init secret merge status of the given VM.
func (r *VMReconciler) markCloudInitSecret(ctx context.Context, vm *kubevirtv1.VirtualMachine, merged bool, errMsg string) (ctrl.Result, error) {
	base := vm.DeepCopy()
	ann := vm.GetAnnotations()
	if ann == nil {
		ann = map[string]string{}
	}

	if merged {
		ann[api.AnnCloudInitSecretMerged] = "true"
		delete(ann, api.AnnCloudInitSecretMergeError)
	} else {
		ann[api.AnnCloudInitSecretMergeError] = errMsg
	}

	vm.SetAnnotations(ann)

	if err := r.Patch(ctx, vm, client.MergeFrom(base)); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// ensureMonitoringSSHSecretInNamespace ensures the Linux monitoring secret is present in the given namespace.
func (r *VMReconciler) ensureMonitoringSSHSecretInNamespace(ctx context.Context, namespace string) error {
	const sourceNamespace = "kubevirt-observability-system"
	const secretName = "lin-vm-mon-secret"

	src := &corev1.Secret{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: sourceNamespace, Name: secretName}, src); err != nil {
		return fmt.Errorf("failed to get source linux monitoring secret: %w", err)
	}

	pubKey, err := getLinuxPublicKeyFromSecret(src)
	if err != nil {
		return err
	}

	dst := &corev1.Secret{}
	err = r.Get(ctx, client.ObjectKey{Namespace: namespace, Name: secretName}, dst)
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to get destination linux monitoring secret: %w", err)
	}

	dataCopy := map[string][]byte{
		"id_rsa.pub": append([]byte(nil), pubKey...),
	}

	if apierrors.IsNotFound(err) {
		newSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: namespace,
				Annotations: map[string]string{
					"kubevirt-observability.io/managed": "true",
					"kubevirt-observability.io/source":  sourceNamespace + "/" + secretName,
				},
			},
			Type: corev1.SecretTypeOpaque,
			Data: dataCopy,
		}
		if err := r.Create(ctx, newSecret); err != nil {
			if apierrors.IsAlreadyExists(err) {
				return nil
			}
			return fmt.Errorf("create destination linux monitoring secret %s/%s: %w", namespace, secretName, err)
		}
		return nil
	}

	base := dst.DeepCopy()
	if dst.Annotations == nil {
		dst.Annotations = map[string]string{}
	}
	dst.Annotations["kubevirt-observability.io/managed"] = "true"
	dst.Annotations["kubevirt-observability.io/source"] = sourceNamespace + "/" + secretName
	dst.Type = corev1.SecretTypeOpaque
	dst.Data = dataCopy

	return r.Patch(ctx, dst, client.MergeFrom(base))
}

// getLinuxPublicKeyFromSecret returns the public key from the given Linux monitoring secret.
func getLinuxPublicKeyFromSecret(src *corev1.Secret) ([]byte, error) {
	for _, k := range []string{"key", "id_rsa.pub"} {
		if v, ok := src.Data[k]; ok && len(v) > 0 {
			return v, nil
		}
	}
	return nil, fmt.Errorf("source linux monitoring secret missing required public key (tried: key, id_rsa.pub)")
}

// getLinuxPrivateKeyAndUser returns the private key and username from the Linux monitoring secret.
func (r *VMReconciler) getLinuxPrivateKeyAndUser(ctx context.Context) ([]byte, string, error) {
	const sourceNamespace = "kubevirt-observability-system"
	const secretName = "lin-vm-mon-private"

	secret := &corev1.Secret{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: sourceNamespace, Name: secretName}, secret); err != nil {
		return nil, "", fmt.Errorf("failed to get linux private key secret: %w", err)
	}

	priv, ok := secret.Data["id_rsa"]
	if !ok || len(priv) == 0 {
		return nil, "", fmt.Errorf("linux private key secret missing id_rsa")
	}

	username := ""
	if v, ok := secret.Data["username"]; ok && len(v) > 0 {
		username = string(v)
	}

	return priv, username, nil
}

// detectLinuxSSHUser detects the Linux SSH user for the given VM.
func (r *VMReconciler) detectLinuxSSHUser(vm *kubevirtv1.VirtualMachine, defaultUser string) (string, error) {
	if vm != nil {
		if ann := vm.GetAnnotations(); ann != nil {
			if u := ann[api.AnnLinuxSSHUser]; u != "" {
				return u, nil
			}
		}
	}

	if defaultUser != "" {
		return defaultUser, nil
	}

	return "", fmt.Errorf("linux ssh user is required; set %s annotation or username key in lin-vm-mon-private secret", api.AnnLinuxSSHUser)
}

// markRemediation marks the remediation status of the given VM.
func (r *VMReconciler) markRemediation(ctx context.Context, vm *kubevirtv1.VirtualMachine, completed bool, errMsg string) (ctrl.Result, error) {
	base := vm.DeepCopy()
	ann := vm.GetAnnotations()
	if ann == nil {
		ann = map[string]string{}
	}

	if completed {
		ann[api.AnnRemediationCompleted] = "true"
		delete(ann, api.AnnRemediationError)
		delete(ann, api.AnnRemediationRequired)
	} else {
		ann[api.AnnRemediationError] = errMsg
	}

	vm.SetAnnotations(ann)

	if err := r.Patch(ctx, vm, client.MergeFrom(base)); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// exporterPortForOS returns the exporter port for the given OS family.
func exporterPortForOS(osFamily osdetect.Family) (int, bool) {
	switch osFamily {
	case osdetect.Linux:
		return 9100, true
	case osdetect.Windows:
		return 9182, true
	default:
		return 0, false
	}
}

// exporterNameForOS returns the exporter name for the given OS family.
func exporterNameForOS(osFamily osdetect.Family) string {
	switch osFamily {
	case osdetect.Windows:
		return "windows_exporter"
	case osdetect.Linux:
		return "node_exporter"
	default:
		return "unknown"
	}
}

// ensureVMServiceAndEndpoints ensures the VM Service and Endpoints are present.
func (r *VMReconciler) ensureVMServiceAndEndpoints(ctx context.Context, vm *kubevirtv1.VirtualMachine, ip string, port int) error {
	name := vm.Name + "-metrics"
	osFamily := osdetect.Detect(vm)

	labels := map[string]string{
		"app":      "vm-metrics",
		"vm_name":  vm.Name,
		"os":       string(osFamily),
		"exporter": exporterNameForOS(osFamily),

		"kubevirt-observability.io/vm-name":  vm.Name,
		"kubevirt-observability.io/vm-os":    string(osFamily),
		"kubevirt-observability.io/exporter": exporterNameForOS(osFamily),
	}

	svc := &corev1.Service{}
	err := r.Get(ctx, client.ObjectKey{Name: name, Namespace: vm.Namespace}, svc)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("get service %s: %w", name, err)
		}

		newSvc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: vm.Namespace,
				Labels:    labels,
			},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{
					{
						Name:       "metrics",
						Port:       int32(port),
						Protocol:   corev1.ProtocolTCP,
						TargetPort: intstr.FromInt(port),
					},
				},
			},
		}
		if err := r.Create(ctx, newSvc); err != nil {
			if apierrors.IsAlreadyExists(err) {
				return nil
			}
			return fmt.Errorf("create service %s: %w", name, err)
		}
	} else {
		base := svc.DeepCopy()

		if svc.Labels == nil {
			svc.Labels = map[string]string{}
		}
		for k, v := range labels {
			svc.Labels[k] = v
		}

		svc.Spec.Ports = []corev1.ServicePort{
			{
				Name:       "metrics",
				Port:       int32(port),
				Protocol:   corev1.ProtocolTCP,
				TargetPort: intstr.FromInt(port),
			},
		}

		if err := r.Patch(ctx, svc, client.MergeFrom(base)); err != nil {
			return fmt.Errorf("patch service %s: %w", name, err)
		}
	}

	eps := &corev1.Endpoints{}
	err = r.Get(ctx, client.ObjectKey{Name: name, Namespace: vm.Namespace}, eps)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("get endpoints %s: %w", name, err)
		}

		newEps := &corev1.Endpoints{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: vm.Namespace,
				Labels:    labels,
			},
			Subsets: []corev1.EndpointSubset{
				{
					Addresses: []corev1.EndpointAddress{
						{IP: ip},
					},
					Ports: []corev1.EndpointPort{
						{
							Name:     "metrics",
							Port:     int32(port),
							Protocol: corev1.ProtocolTCP,
						},
					},
				},
			},
		}

		if err := r.Create(ctx, newEps); err != nil {
			if apierrors.IsAlreadyExists(err) {
				return nil
			}
			return fmt.Errorf("create endpoints %s: %w", name, err)
		}
		return nil
	}

	base := eps.DeepCopy()

	if eps.Labels == nil {
		eps.Labels = map[string]string{}
	}
	for k, v := range labels {
		eps.Labels[k] = v
	}

	expected := []corev1.EndpointSubset{
		{
			Addresses: []corev1.EndpointAddress{
				{IP: ip},
			},
			Ports: []corev1.EndpointPort{
				{
					Name:     "metrics",
					Port:     int32(port),
					Protocol: corev1.ProtocolTCP,
				},
			},
		},
	}

	if !endpointSubsetsEqual(eps.Subsets, expected) || !mapsEqual(base.Labels, eps.Labels) {
		eps.Subsets = expected
		if err := r.Patch(ctx, eps, client.MergeFrom(base)); err != nil {
			return fmt.Errorf("patch endpoints %s: %w", name, err)
		}
	}

	return nil
}

// mapsEqual returns true if the given maps are equal.
func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func endpointSubsetsEqual(a, b []corev1.EndpointSubset) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if len(a[i].Addresses) != len(b[i].Addresses) || len(a[i].Ports) != len(b[i].Ports) {
			return false
		}
		for j := range a[i].Addresses {
			if a[i].Addresses[j].IP != b[i].Addresses[j].IP {
				return false
			}
		}
		for j := range a[i].Ports {
			if a[i].Ports[j].Name != b[i].Ports[j].Name ||
				a[i].Ports[j].Port != b[i].Ports[j].Port ||
				a[i].Ports[j].Protocol != b[i].Ports[j].Protocol {
				return false
			}
		}
	}
	return true
}

// ensureNamespaceServiceMonitor ensures the namespace ServiceMonitor is present.
func (r *VMReconciler) ensureNamespaceServiceMonitor(ctx context.Context, namespace string) error {
	const name = "vm-metrics"

	sm := &monitoringv1.ServiceMonitor{}
	err := r.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, sm)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("get servicemonitor %s: %w", name, err)
		}

		newSM := &monitoringv1.ServiceMonitor{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				Labels: map[string]string{
					"app": "vm-metrics",
				},
			},
			Spec: monitoringv1.ServiceMonitorSpec{
				Selector: metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "vm-metrics",
					},
				},
				TargetLabels: []string{
					"vm_name",
					"os",
					"exporter",
				},
				Endpoints: []monitoringv1.Endpoint{
					{
						Port:     "metrics",
						Path:     "/metrics",
						Interval: monitoringv1.Duration("30s"),
					},
				},
			},
		}

		if err := r.Create(ctx, newSM); err != nil {
			if apierrors.IsAlreadyExists(err) {
				return nil
			}
			return fmt.Errorf("create servicemonitor %s: %w", name, err)
		}
		return nil
	}

	base := sm.DeepCopy()

	if sm.Labels == nil {
		sm.Labels = map[string]string{}
	}
	sm.Labels["app"] = "vm-metrics"

	sm.Spec.Selector = metav1.LabelSelector{
		MatchLabels: map[string]string{
			"app": "vm-metrics",
		},
	}
	sm.Spec.TargetLabels = []string{
		"vm_name",
		"os",
		"exporter",
	}

	sm.Spec.Endpoints = []monitoringv1.Endpoint{
		{
			Port:     "metrics",
			Path:     "/metrics",
			Interval: monitoringv1.Duration("30s"),
		},
	}

	if err := r.Patch(ctx, sm, client.MergeFrom(base)); err != nil {
		return fmt.Errorf("patch servicemonitor %s: %w", name, err)
	}

	return nil
}

// ensureNamespaceMonitoringLabel ensures the namespace is labeled for monitoring.
func (r *VMReconciler) ensureNamespaceMonitoringLabel(ctx context.Context, namespace string) error {
	ns := &corev1.Namespace{}
	if err := r.Get(ctx, client.ObjectKey{Name: namespace}, ns); err != nil {
		return fmt.Errorf("get namespace %s: %w", namespace, err)
	}

	base := ns.DeepCopy()

	if ns.Labels == nil {
		ns.Labels = map[string]string{}
	}

	if ns.Labels["openshift.io/user-monitoring"] == "true" {
		return nil
	}

	ns.Labels["openshift.io/user-monitoring"] = "true"

	if err := r.Patch(ctx, ns, client.MergeFrom(base)); err != nil {
		return fmt.Errorf("patch namespace %s: %w", namespace, err)
	}

	return nil
}

func (r *VMReconciler) syncMetricsScrapeResources(
	ctx context.Context,
	vm *kubevirtv1.VirtualMachine,
	vmi *kubevirtv1.VirtualMachineInstance,
	osFamily osdetect.Family,
) (ctrl.Result, error) {

	ip := firstIP(vmi)
	if ip == "" {
		return r.mark(ctx, vm, api.StatusPending, "waiting-for-ip")
	}

	port, ok := exporterPortForOS(osFamily)
	if !ok {
		return r.mark(ctx, vm, api.StatusSkipped, "unknown-os")
	}

	if err := r.ensureVMServiceAndEndpoints(ctx, vm, ip, port); err != nil {
		r.Recorder.Eventf(
			vm,
			corev1.EventTypeWarning,
			"MetricsEndpointSyncFailed",
			"Failed to ensure metrics Service/Endpoints: %v",
			err,
		)
		return ctrl.Result{}, err
	}

	if err := r.ensureNamespaceServiceMonitor(ctx, vm.Namespace); err != nil {
		r.Recorder.Eventf(
			vm,
			corev1.EventTypeWarning,
			"ServiceMonitorSyncFailed",
			"Failed to ensure namespace ServiceMonitor: %v",
			err,
		)
		return ctrl.Result{}, err
	}

	bootstrapState := "unknown"

	if osFamily == osdetect.Windows {

		priv, _, err := r.getLinuxPrivateKeyAndUser(ctx)
		if err == nil {

			state, stateErr := remediation.GetWindowsBootstrapState(
				remediation.WindowsSSHConfig{
					Address: ip + ":22",
					User:    "Administrator",
					KeyPEM:  priv,
					Timeout: 10 * time.Second,
				},
			)

			if stateErr == nil {
				bootstrapState = state
			}
		}
	}

	if !verify.TCP(ip, port, 2*time.Second) {

		switch bootstrapState {

		case "success":
			r.Recorder.Eventf(
				vm,
				corev1.EventTypeNormal,
				"BootstrapCompleted",
				"Cloud-init bootstrap completed successfully",
			)

			// continue verification flow
			// exporter may still be stabilizing

		case "failed":
			// remediation allowed
			break

		case "unknown":
			if !bootstrapTimedOut(vm, 10*time.Minute) {
				return r.mark(ctx, vm, api.StatusPending, "bootstrap-in-progress")
			}

			// timeout exceeded
			// remediation allowed
			break

		default:
			return r.mark(ctx, vm, api.StatusPending, "exporter-not-ready")
		}
	}

	// Windows bootstrap already succeeded.
	// Clear stale remediation state before Alloy reconciliation.

	if osFamily == osdetect.Windows && bootstrapState == "success" {

		base := vm.DeepCopy()

		if vm.Annotations == nil {
			vm.Annotations = map[string]string{}
		}

		firstReady := vm.Annotations["kubevirt-observability.io/bootstrap-ready"] != "true"

		delete(vm.Annotations, api.AnnAlloyError)
		delete(vm.Annotations, api.AnnReason)

		vm.Annotations[api.AnnRemediationRequired] = "false"
		vm.Annotations["kubevirt-observability.io/bootstrap-ready"] = "true"

		if err := r.Patch(ctx, vm, client.MergeFrom(base)); err != nil {
			return ctrl.Result{}, err
		}

		if firstReady {
			r.Recorder.Eventf(
				vm,
				corev1.EventTypeNormal,
				"AlloyBootstrapReady",
				"Alloy already configured by cloud-init bootstrap",
			)
		}

		bootstrapState = "success"
	}

	updated, err := r.reconcileAlloyIfEnabled(ctx, vm, ip, osFamily)
	if err != nil {

		r.Recorder.Eventf(
			vm,
			corev1.EventTypeWarning,
			"AlloyInstallFailed",
			"Alloy install/config failed: %v",
			err,
		)

		base := vm.DeepCopy()

		if vm.Annotations == nil {
			vm.Annotations = map[string]string{}
		}

		vm.Annotations[api.AnnAlloyError] = err.Error()

		if patchErr := r.Patch(ctx, vm, client.MergeFrom(base)); patchErr != nil {
			return ctrl.Result{}, patchErr
		}

		return r.mark(ctx, vm, api.StatusPending, "alloy-install-failed")
	}

	if updated {
		r.Recorder.Eventf(
			vm,
			corev1.EventTypeNormal,
			"AlloyConfigured",
			"Alloy installed/configured",
		)
	}

	r.Recorder.Eventf(
		vm,
		corev1.EventTypeNormal,
		"MonitoringReady",
		"Monitoring verified",
	)

	base := vm.DeepCopy()

	if vm.Annotations == nil {
		vm.Annotations = map[string]string{}
	}

	delete(vm.Annotations, api.AnnAlloyError)
	delete(vm.Annotations, api.AnnReason)

	vm.Annotations[api.AnnRemediationRequired] = "false"

	if err := r.Patch(ctx, vm, client.MergeFrom(base)); err != nil {
		return ctrl.Result{}, err
	}

	return r.mark(ctx, vm, api.StatusReady, "verified")
}

func (r *VMReconciler) getVMI(ctx context.Context, vm *kubevirtv1.VirtualMachine) (*kubevirtv1.VirtualMachineInstance, bool, error) {
	vmi := &kubevirtv1.VirtualMachineInstance{}
	err := r.Get(ctx, client.ObjectKey{Name: vm.Name, Namespace: vm.Namespace}, vmi)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return vmi, true, nil
}

func (r *VMReconciler) getLokiWriterToken(ctx context.Context) (string, error) {
	const namespace = "kubevirt-observability-system"
	const name = "vm-loki-writer-token"

	secret := &corev1.Secret{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, secret); err != nil {
		return "", fmt.Errorf("get loki writer token secret %s/%s: %w", namespace, name, err)
	}

	token, ok := secret.Data["token"]
	if !ok || len(token) == 0 {
		return "", fmt.Errorf("secret %s/%s missing token key", namespace, name)
	}

	return strings.TrimSpace(string(token)), nil
}

func (r *VMReconciler) getLokiPushURL(ctx context.Context) (string, error) {
	route := &routev1.Route{}

	if err := r.Get(ctx, client.ObjectKey{
		Namespace: "openshift-logging",
		Name:      "logging-loki",
	}, route); err != nil {
		return "", fmt.Errorf("get Loki route openshift-logging/logging-loki: %w", err)
	}

	if strings.TrimSpace(route.Spec.Host) == "" {
		return "", fmt.Errorf("Loki route openshift-logging/logging-loki has empty host")
	}

	return fmt.Sprintf(
		"https://%s/api/logs/v1/application/loki/api/v1/push",
		route.Spec.Host,
	), nil
}

func (r *VMReconciler) reconcileLinuxAlloy(
	ctx context.Context,
	vm *kubevirtv1.VirtualMachine,
	ip string,
	username string,
	privateKey []byte,
) (bool, error) {
	token, err := r.getLokiWriterToken(ctx)
	if err != nil {
		return false, err
	}
	token = strings.TrimSpace(token)

	lokiURL, err := r.getLokiPushURL(ctx)
	if err != nil {
		return false, err
	}

	cfg := alloyconfig.RenderLinux(alloyconfig.VMLogConfig{
		Namespace: vm.Namespace,
		VMName:    vm.Name,
		LokiURL:   lokiURL,
	})

	desiredHash := hashStrings(cfg, token)

	ann := vm.GetAnnotations()
	if ann != nil && ann[api.AnnAlloyConfigHash] == desiredHash && ann[api.AnnAlloyInstalled] == "true" {
		return false, nil
	}

	addr := ip + ":22"
	if !remediation.ProbeSSH(addr, 3*time.Second) {
		return false, fmt.Errorf("ssh not reachable at %s", addr)
	}

	script := remediation.LinuxAlloyInstallScript(cfg, token)

	if err := remediation.RunLinuxBootstrap(remediation.LinuxSSHConfig{
		Address:    addr,
		Username:   username,
		PrivateKey: privateKey,
		Timeout:    60 * time.Second,
	}, script); err != nil {
		return false, err
	}

	base := vm.DeepCopy()
	if vm.Annotations == nil {
		vm.Annotations = map[string]string{}
	}
	vm.Annotations[api.AnnAlloyInstalled] = "true"
	vm.Annotations[api.AnnAlloyConfigHash] = desiredHash
	delete(vm.Annotations, api.AnnAlloyError)

	if err := r.Patch(ctx, vm, client.MergeFrom(base)); err != nil {
		return false, err
	}

	return true, nil
}

func hashStrings(values ...string) string {
	h := sha256.New()
	for _, v := range values {
		_, _ = h.Write([]byte(v))
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (r *VMReconciler) reconcileAlloyIfEnabled(
	ctx context.Context,
	vm *kubevirtv1.VirtualMachine,
	ip string,
	osFamily osdetect.Family,
) (bool, error) {
	if vm.Annotations[api.AnnLoggingEnabled] != "true" {
		return false, nil
	}

	switch osFamily {
	case osdetect.Linux:
		priv, defaultUser, err := r.getLinuxPrivateKeyAndUser(ctx)
		if err != nil {
			return false, fmt.Errorf("get linux key for Alloy install: %w", err)
		}

		user, err := r.detectLinuxSSHUser(vm, defaultUser)
		if err != nil {
			return false, err
		}
		return r.reconcileLinuxAlloy(ctx, vm, ip, user, priv)

	case osdetect.Windows:
		return r.reconcileWindowsAlloy(ctx, vm, ip)

	default:
		return false, fmt.Errorf("unsupported OS for Alloy reconciliation: %s", osFamily)
	}
}

func (r *VMReconciler) reconcileWindowsAlloy(
	ctx context.Context,
	vm *kubevirtv1.VirtualMachine,
	ip string,
) (bool, error) {

	token, err := r.getLokiWriterToken(ctx)
	if err != nil {
		return false, err
	}

	lokiURL, err := r.getLokiPushURL(ctx)
	if err != nil {
		return false, err
	}

	cfg := alloyconfig.RenderWindows(alloyconfig.VMLogConfig{
		Namespace: vm.Namespace,
		VMName:    vm.Name,
		LokiURL:   lokiURL,
	})

	desiredHash := hashStrings(cfg, token)

	ann := vm.GetAnnotations()

	if ann != nil &&
		ann[api.AnnAlloyConfigHash] == desiredHash &&
		ann[api.AnnAlloyInstalled] == "true" {

		return false, nil
	}

	priv, _, err := r.getLinuxPrivateKeyAndUser(ctx)
	if err != nil {
		return false, err
	}

	sshCfg := remediation.WindowsSSHConfig{
		Address: ip + ":22",
		User:    "Administrator",
		KeyPEM:  priv,
		Timeout: 60 * time.Second,
	}

	state, err := remediation.GetWindowsBootstrapState(sshCfg)
	if err != nil {
		return false, err
	}

	// Bootstrap already configured Alloy.
	// Only update config/token drift if needed.
	if state == "success" {

		script := remediation.WindowsAlloyUpdateScript(cfg, token)

		if err := remediation.RunWindowsSSHBootstrap(
			sshCfg,
			script,
		); err != nil {
			return false, err
		}

		base := vm.DeepCopy()

		if vm.Annotations == nil {
			vm.Annotations = map[string]string{}
		}

		vm.Annotations[api.AnnAlloyInstalled] = "true"
		vm.Annotations[api.AnnAlloyConfigHash] = desiredHash

		delete(vm.Annotations, api.AnnAlloyError)

		if err := r.Patch(ctx, vm, client.MergeFrom(base)); err != nil {
			return false, err
		}

		return true, nil
	}

	return false, fmt.Errorf(
		"windows bootstrap not completed yet",
	)
}

func (r *VMReconciler) getLinuxPublicKey(ctx context.Context, namespace string) ([]byte, error) {
	const secretName = "lin-vm-mon-secret"

	secret := &corev1.Secret{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: namespace, Name: secretName}, secret); err != nil {
		return nil, fmt.Errorf("failed to get linux public key secret %s/%s: %w", namespace, secretName, err)
	}

	return getLinuxPublicKeyFromSecret(secret)
}

// injectSSHMergeHints injects the given SSH user and public key into the given cloud-init secret data.
func injectSSHMergeHints(data map[string][]byte, user, pubKey string) error {
	if strings.TrimSpace(user) == "" {
		return fmt.Errorf("linux ssh user is required")
	}
	if strings.TrimSpace(pubKey) == "" {
		return fmt.Errorf("linux ssh public key is required")
	}

	key := "userData"
	if _, ok := data[key]; !ok {
		key = "userdata"
	}
	if _, ok := data[key]; !ok {
		return fmt.Errorf("cloud-init secret missing userData/userdata")
	}

	cfgMap := map[string]interface{}{}

	raw := strings.TrimSpace(string(data[key]))
	raw = strings.TrimPrefix(raw, "#cloud-config")
	raw = strings.TrimSpace(raw)

	if raw != "" {
		if err := yaml.Unmarshal([]byte(raw), &cfgMap); err != nil {
			return fmt.Errorf("parse cloud-init userdata for ssh injection: %w", err)
		}
	}

	cfgMap["observability_ssh_user"] = user
	cfgMap["observability_ssh_public_key"] = pubKey

	newData, err := yaml.Marshal(cfgMap)
	if err != nil {
		return fmt.Errorf("marshal cloud-init userdata for ssh injection: %w", err)
	}

	data[key] = []byte("#cloud-config\n" + string(newData))
	return nil
}

func (r *VMReconciler) ensureGrafanaLokiReadRBAC(ctx context.Context, namespace string) error {
	bindings := []struct {
		name     string
		saNS     string
		saName   string
		roleName string
		roleKind string
		apiGroup string
	}{
		{
			name:     "kubevirt-observability-grafana-loki-reader",
			saNS:     grafanaNamespace,
			saName:   grafanaServiceAccount,
			roleName: "kubevirt-observability-loki-reader",
			roleKind: "ClusterRole",
			apiGroup: "rbac.authorization.k8s.io",
		},
		{
			name:     "kubevirt-observability-openshift-logging-grafana-loki-reader",
			saNS:     loggingNamespace,
			saName:   loggingGrafanaSA,
			roleName: "kubevirt-observability-loki-reader",
			roleKind: "ClusterRole",
			apiGroup: "rbac.authorization.k8s.io",
		},
	}

	for _, b := range bindings {
		rb := &rbacv1.RoleBinding{}
		err := r.Get(ctx, client.ObjectKey{Name: b.name, Namespace: namespace}, rb)
		if err != nil {
			if !apierrors.IsNotFound(err) {
				return fmt.Errorf("get rolebinding %s/%s: %w", namespace, b.name, err)
			}

			newRB := &rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      b.name,
					Namespace: namespace,
					Labels: map[string]string{
						"app": "kubevirt-observability",
					},
				},
				Subjects: []rbacv1.Subject{
					{
						Kind:      "ServiceAccount",
						Name:      b.saName,
						Namespace: b.saNS,
					},
				},
				RoleRef: rbacv1.RoleRef{
					APIGroup: b.apiGroup,
					Kind:     b.roleKind,
					Name:     b.roleName,
				},
			}

			if err := r.Create(ctx, newRB); err != nil {
				if apierrors.IsAlreadyExists(err) {
					continue
				}
				return fmt.Errorf("create rolebinding %s/%s: %w", namespace, b.name, err)
			}
			continue
		}

		base := rb.DeepCopy()
		rb.Subjects = []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      b.saName,
				Namespace: b.saNS,
			},
		}
		rb.RoleRef = rbacv1.RoleRef{
			APIGroup: b.apiGroup,
			Kind:     b.roleKind,
			Name:     b.roleName,
		}

		if rb.Labels == nil {
			rb.Labels = map[string]string{}
		}
		rb.Labels["app"] = "kubevirt-observability"

		if err := r.Patch(ctx, rb, client.MergeFrom(base)); err != nil {
			return fmt.Errorf("patch rolebinding %s/%s: %w", namespace, b.name, err)
		}
	}

	return nil
}

func (r *VMReconciler) ensureWindowsSSHSecret(
	ctx context.Context,
	namespace string,
) error {

	source := &corev1.Secret{}

	err := r.Get(ctx, client.ObjectKey{
		Namespace: "kubevirt-observability-system",
		Name:      api.LinuxSSHSecretName,
	}, source)

	if err != nil {
		return fmt.Errorf(
			"get source windows ssh secret: %w",
			err,
		)
	}

	target := &corev1.Secret{}

	err = r.Get(ctx, client.ObjectKey{
		Namespace: namespace,
		Name:      api.LinuxSSHSecretName,
	}, target)

	if err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}

		target = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      api.LinuxSSHSecretName,
				Namespace: namespace,
				Labels: map[string]string{
					"app": "kubevirt-observability",
				},
			},
			Type: source.Type,
			Data: source.Data,
		}

		return r.Create(ctx, target)
	}

	base := target.DeepCopy()

	target.Type = source.Type
	target.Data = source.Data

	return r.Patch(ctx, target, client.MergeFrom(base))
}

func (r *VMReconciler) getWindowsPublicKey(
	ctx context.Context,
	namespace string,
) ([]byte, error) {

	secret := &corev1.Secret{}

	err := r.Get(ctx, client.ObjectKey{
		Namespace: namespace,
		Name:      api.LinuxSSHSecretName,
	}, secret)

	if err != nil {
		return nil, fmt.Errorf(
			"failed to get windows public key secret %s/%s: %w",
			namespace,
			api.LinuxSSHSecretName,
			err,
		)
	}

	candidateKeys := []string{
		"id_rsa.pub",
		"ssh-publickey",
		"id_ed25519.pub",
		"authorized_keys",
	}

	for _, k := range candidateKeys {
		if v, ok := secret.Data[k]; ok && len(v) > 0 {
			return v, nil
		}
	}

	return nil, fmt.Errorf(
		"windows ssh secret missing public key data",
	)
}

func bootstrapTimedOut(
	vm *kubevirtv1.VirtualMachine,
	timeout time.Duration,
) bool {

	ann := vm.GetAnnotations()
	if ann == nil {
		return false
	}

	started := ann["kubevirt-observability.io/bootstrap-started-at"]
	if started == "" {
		return false
	}

	t, err := time.Parse(time.RFC3339, started)
	if err != nil {
		return false
	}

	return time.Since(t) > timeout
}
