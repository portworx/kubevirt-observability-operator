package patch

import (
	"encoding/base64"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	kubevirtv1 "kubevirt.io/api/core/v1"

	"github.com/portworx/kubevirt-observability-operator/api"
	"github.com/portworx/kubevirt-observability-operator/internal/bootstrapdetect"
	"github.com/portworx/kubevirt-observability-operator/internal/cloudinitmerge"
	"github.com/portworx/kubevirt-observability-operator/internal/osdetect"
)

const maxInlineCloudInitBytes = 2048

// EnsureMonitoringBootstrap ensures the given VM has the monitoring bootstrap.
func EnsureMonitoringBootstrap(vm *kubevirtv1.VirtualMachine) error {
	ann := vm.GetAnnotations()
	if ann == nil {
		ann = map[string]string{}
	}

	if ann[api.AnnDisableMonitoring] == "true" {
		return nil
	}

	osFamily := osdetect.Detect(vm)
	bt := bootstrapdetect.Detect(vm)

	ann[api.AnnBootstrapManaged] = "true"
	ann[api.AnnBootstrapVersion] = api.BootstrapVersion
	ann[api.AnnBootstrapType] = string(bt.Kind)
	if _, ok := ann[api.AnnLoggingEnabled]; !ok {
		ann[api.AnnLoggingEnabled] = "true"
	}

	delete(ann, api.AnnLegacyAlloy)

	switch osFamily {
	case osdetect.Linux:
		ann[api.AnnBootstrapOS] = "linux"
		ann[api.AnnExporter] = "node_exporter"
		if err := ensureLinux(vm, bt); err != nil {
			return err
		}
	case osdetect.Windows:
		ann[api.AnnBootstrapOS] = "windows"
		ann[api.AnnExporter] = "windows_exporter"
		if err := ensureWindows(vm, bt); err != nil {
			return err
		}
	default:
		ann[api.AnnBootstrapOS] = "unknown"
		ann[api.AnnExporter] = "unknown"
		return nil
		//return fmt.Errorf("unknown OS type")
	}

	vm.SetAnnotations(ann)
	return nil
}

// ensureLinux ensures the given Linux VM has the monitoring bootstrap.
func ensureLinux(vm *kubevirtv1.VirtualMachine, bt bootstrapdetect.Result) error {
	switch bt.Kind {
	case bootstrapdetect.CloudInitNoCloud:
		return mergeLinuxNoCloud(vm, bt.VolumeIndex)
	case bootstrapdetect.CloudInitConfigDrv:
		return mergeLinuxConfigDrive(vm, bt.VolumeIndex)
	case bootstrapdetect.None:
		return injectLinuxNoCloud(vm)
	case bootstrapdetect.Sysprep:
		return fmt.Errorf("linux VM cannot use sysprep bootstrap")
	default:
		return fmt.Errorf("unsupported bootstrap type")
	}
}

// ensureWindows ensures the given Windows VM has the monitoring bootstrap.
func ensureWindows(vm *kubevirtv1.VirtualMachine, bt bootstrapdetect.Result) error {
	var err error

	switch bt.Kind {
	case bootstrapdetect.CloudInitNoCloud:
		err = mergeWindowsNoCloud(vm, bt.VolumeIndex)

	case bootstrapdetect.CloudInitConfigDrv:
		err = mergeWindowsConfigDrive(vm, bt.VolumeIndex)

	case bootstrapdetect.Sysprep:
		err = mergeWindowsSysprep(vm)

	case bootstrapdetect.None:
		err = injectWindowsBootstrap(vm)

	default:
		return fmt.Errorf("unsupported bootstrap type")
	}

	if err != nil {
		return err
	}

	return ensureWindowsSSHAccess(vm)
}

// mergeLinuxNoCloud merges the given Linux VM's CloudInitNoCloud volume.
func mergeLinuxNoCloud(vm *kubevirtv1.VirtualMachine, idx int) error {
	vol := &vm.Spec.Template.Spec.Volumes[idx]
	if vol.CloudInitNoCloud == nil {
		return fmt.Errorf("volume %s has nil CloudInitNoCloud source", vol.Name)
	}

	// Secret-backed cloud-init is handled later by controller-side Secret patching.
	if vol.CloudInitNoCloud.UserDataSecretRef != nil {
		ann := vm.GetAnnotations()
		if ann == nil {
			ann = map[string]string{}
		}
		ann[api.AnnCloudInitSecretMergeRequired] = "true"
		vm.SetAnnotations(ann)
		return ensureLinuxSSHAccess(vm)
	}

	userData, isBase64, err := getNoCloudUserData(vol.CloudInitNoCloud)
	if err != nil {
		return err
	}

	merged, _, err := cloudinitmerge.Merge(userData, cloudinitmerge.OSLinux)
	if err != nil {
		return err
	}

	if !inlineCloudInitFits(merged) {
		markRemediationRequired(vm, "cloudinit-size-limit")
		return ensureLinuxSSHAccess(vm)
	}

	if err := setNoCloudUserData(vol.CloudInitNoCloud, merged, isBase64); err != nil {
		return err
	}

	return ensureLinuxSSHAccess(vm)
}

// mergeWindowsNoCloud merges the given Windows VM's CloudInitNoCloud volume.
func mergeWindowsNoCloud(vm *kubevirtv1.VirtualMachine, idx int) error {
	vol := &vm.Spec.Template.Spec.Volumes[idx]

	if vol.CloudInitNoCloud == nil {
		return fmt.Errorf("volume %s has nil CloudInitNoCloud source", vol.Name)
	}

	fmt.Printf("mergeWindowsNoCloud vm=%s/%s vol=%s userDataLen=%d userDataBase64Len=%d hasSecretRef=%t\n",
		vm.Namespace, vm.Name, vol.Name,
		len(vol.CloudInitNoCloud.UserData),
		len(vol.CloudInitNoCloud.UserDataBase64),
		vol.CloudInitNoCloud.UserDataSecretRef != nil,
	)

	if vol.CloudInitNoCloud.UserDataSecretRef != nil {
		ann := vm.GetAnnotations()
		if ann == nil {
			ann = map[string]string{}
		}
		ann[api.AnnCloudInitSecretMergeRequired] = "true"
		vm.SetAnnotations(ann)
		return nil
	}

	userData, isBase64, err := getNoCloudUserData(vol.CloudInitNoCloud)
	if err != nil {
		return err
	}

	if strings.HasPrefix(userData, "#ps1") {
		markRemediationRequired(vm, "windows-raw-powershell-userdata")
		return nil
	}

	merged, _, err := cloudinitmerge.Merge(userData, cloudinitmerge.OSWindows)
	if err != nil {
		return err
	}
	fmt.Printf("mergeWindowsNoCloud mergedLen=%d fits=%t vm=%s/%s vol=%s\n",
		len([]byte(merged)),
		inlineCloudInitFits(merged),
		vm.Namespace, vm.Name, vol.Name,
	)

	if !inlineCloudInitFits(merged) {
		secretName := vm.Name + "-cloudinit"

		vol.CloudInitNoCloud.UserData = ""
		vol.CloudInitNoCloud.UserDataBase64 = ""
		vol.CloudInitNoCloud.UserDataSecretRef = &corev1.LocalObjectReference{
			Name: secretName,
		}

		ann := vm.GetAnnotations()
		if ann == nil {
			ann = map[string]string{}
		}

		ann[api.AnnCloudInitSecretMergeRequired] = "true"
		vm.SetAnnotations(ann)

		return nil
	}

	return setNoCloudUserData(vol.CloudInitNoCloud, merged, isBase64)
}

// mergeLinuxConfigDrive merges the given Linux VM's CloudInitConfigDrive volume.
func mergeLinuxConfigDrive(vm *kubevirtv1.VirtualMachine, idx int) error {
	vol := &vm.Spec.Template.Spec.Volumes[idx]
	if err := mergeCloudInitConfigDrive(vol.CloudInitConfigDrive, cloudinitmerge.OSLinux); err != nil {
		return err
	}
	return ensureLinuxSSHAccess(vm)
}

// mergeWindowsConfigDrive merges the given Windows VM's CloudInitConfigDrive volume.
func mergeWindowsConfigDrive(vm *kubevirtv1.VirtualMachine, idx int) error {
	vol := &vm.Spec.Template.Spec.Volumes[idx]
	if vol.CloudInitConfigDrive == nil {
		return fmt.Errorf("volume %s has nil CloudInitConfigDrive source", vol.Name)
	}

	userData, isBase64, err := getConfigDriveUserData(vol.CloudInitConfigDrive)
	if err != nil {
		return err
	}

	merged, _, err := cloudinitmerge.Merge(userData, cloudinitmerge.OSWindows)
	if err != nil {
		return err
	}

	if !inlineCloudInitFits(merged) {
		ann := vm.GetAnnotations()
		if ann == nil {
			ann = map[string]string{}
		}

		ann[api.AnnCloudInitSecretMergeRequired] = "true"
		vm.SetAnnotations(ann)

		return nil
	}

	return setConfigDriveUserData(vol.CloudInitConfigDrive, merged, isBase64)
}

// mergeWindowsSysprep merges the given Windows VM's Sysprep volume.
func mergeWindowsSysprep(vm *kubevirtv1.VirtualMachine) error {
	// Phase 1 behavior:
	// mark the VM for external sysprep source merge.
	// Actual Secret/ConfigMap fetch-and-update is done in the controller path.
	ann := vm.GetAnnotations()
	if ann == nil {
		ann = map[string]string{}
	}
	ann["kubevirt-observability.io/sysprep-merge-required"] = "true"
	vm.SetAnnotations(ann)
	return nil
}

// injectWindowsBootstrap injects a CloudInitNoCloud volume for the given Windows VM.
func injectWindowsBootstrap(vm *kubevirtv1.VirtualMachine) error {
	tmpl := &vm.Spec.Template.Spec

	const volName = "monitoring-cloudinit"
	const diskName = "monitoring-cloudinitdisk"

	if !hasDisk(tmpl.Domain.Devices.Disks, diskName) {
		tmpl.Domain.Devices.Disks = append(tmpl.Domain.Devices.Disks, kubevirtv1.Disk{
			Name: diskName,
			DiskDevice: kubevirtv1.DiskDevice{
				Disk: &kubevirtv1.DiskTarget{Bus: "sata"},
			},
		})
	}

	userData, _, err := cloudinitmerge.Merge("", cloudinitmerge.OSWindows)
	if err != nil {
		return err
	}

	if !hasVolume(tmpl.Volumes, volName) {
		tmpl.Volumes = append(tmpl.Volumes, kubevirtv1.Volume{
			Name: volName,
			VolumeSource: kubevirtv1.VolumeSource{
				CloudInitNoCloud: &kubevirtv1.CloudInitNoCloudSource{
					UserData: userData,
				},
			},
		})
	}

	return nil
}

// injectLinuxNoCloud injects a CloudInitNoCloud volume for the given Linux VM.
func injectLinuxNoCloud(vm *kubevirtv1.VirtualMachine) error {
	tmpl := &vm.Spec.Template.Spec

	const volName = "monitoring-cloudinit"
	const diskName = "monitoring-cloudinitdisk"

	if !hasDisk(tmpl.Domain.Devices.Disks, diskName) {
		tmpl.Domain.Devices.Disks = append(tmpl.Domain.Devices.Disks, kubevirtv1.Disk{
			Name: diskName,
			DiskDevice: kubevirtv1.DiskDevice{
				Disk: &kubevirtv1.DiskTarget{Bus: "virtio"},
			},
		})
	}

	userData, _, err := cloudinitmerge.Merge("", cloudinitmerge.OSLinux)
	if err != nil {
		return err
	}

	if !hasVolume(tmpl.Volumes, volName) {
		tmpl.Volumes = append(tmpl.Volumes, kubevirtv1.Volume{
			Name: volName,
			VolumeSource: kubevirtv1.VolumeSource{
				CloudInitNoCloud: &kubevirtv1.CloudInitNoCloudSource{
					UserData: userData,
				},
			},
		})
	}

	return ensureLinuxSSHAccess(vm)
}

// ensureLinuxSSHAccess ensures the given Linux VM has the SSH access.
func ensureLinuxSSHAccess(vm *kubevirtv1.VirtualMachine) error {
	tmpl := &vm.Spec.Template.Spec
	for _, cred := range tmpl.AccessCredentials {
		if cred.SSHPublicKey != nil &&
			cred.SSHPublicKey.Source.Secret != nil &&
			cred.SSHPublicKey.Source.Secret.SecretName == api.LinuxSSHSecretName {
			return nil
		}
	}

	tmpl.AccessCredentials = append(tmpl.AccessCredentials, kubevirtv1.AccessCredential{
		SSHPublicKey: &kubevirtv1.SSHPublicKeyAccessCredential{
			Source: kubevirtv1.SSHPublicKeyAccessCredentialSource{
				Secret: &kubevirtv1.AccessCredentialSecretSource{
					SecretName: api.LinuxSSHSecretName,
				},
			},
			PropagationMethod: kubevirtv1.SSHPublicKeyAccessCredentialPropagationMethod{
				QemuGuestAgent: &kubevirtv1.QemuGuestAgentSSHPublicKeyAccessCredentialPropagation{Users: []string{detectLinuxSSHUser(vm)}},
			},
		},
	})

	return nil
}

// hasDisk returns true if the given disk is present in the given list.
func hasDisk(disks []kubevirtv1.Disk, name string) bool {
	for _, d := range disks {
		if d.Name == name {
			return true
		}
	}
	return false
}

// hasVolume returns true if the given volume is present in the given list.
func hasVolume(vols []kubevirtv1.Volume, name string) bool {
	for _, v := range vols {
		if v.Name == name {
			return true
		}
	}
	return false
}

// mergeCloudInitNoCloud merges the given CloudInitNoCloud source.
func mergeCloudInitNoCloud(source *kubevirtv1.CloudInitNoCloudSource, osType cloudinitmerge.OSType) error {
	if source == nil {
		return fmt.Errorf("nil CloudInitNoCloud source")
	}

	fmt.Printf("mergeCloudInitNoCloud os=%s userDataLen=%d userDataBase64Len=%d hasSecretRef=%t\n",
		osType,
		len(source.UserData),
		len(source.UserDataBase64),
		source.UserDataSecretRef != nil,
	)

	if source.UserDataSecretRef != nil {
		return fmt.Errorf("cannot inline-merge secret-backed CloudInitNoCloud; use controller-side secret merge")
	}

	userData, isBase64, err := getNoCloudUserData(source)
	if err != nil {
		return err
	}

	merged, _, err := cloudinitmerge.Merge(userData, osType)
	if err != nil {
		return err
	}

	return setNoCloudUserData(source, merged, isBase64)
}

// mergeCloudInitConfigDrive merges the given CloudInitConfigDrive source.
func mergeCloudInitConfigDrive(source *kubevirtv1.CloudInitConfigDriveSource, osType cloudinitmerge.OSType) error {
	userData, isBase64, err := getConfigDriveUserData(source)
	if err != nil {
		return err
	}

	merged, _, err := cloudinitmerge.Merge(userData, osType)
	if err != nil {
		return err
	}

	return setConfigDriveUserData(source, merged, isBase64)
}

// getNoCloudUserData returns the user data from the given CloudInitNoCloud source.
func getNoCloudUserData(source *kubevirtv1.CloudInitNoCloudSource) (string, bool, error) {
	if source == nil {
		return "", false, fmt.Errorf("nil CloudInitNoCloud source")
	}

	if source.UserDataSecretRef != nil {
		return "", false, fmt.Errorf("CloudInitNoCloud uses UserDataSecretRef; inline userdata is not available")
	}

	if source.UserDataBase64 != "" {
		decoded, err := base64.StdEncoding.DecodeString(source.UserDataBase64)
		if err != nil {
			return "", true, fmt.Errorf("failed to decode UserDataBase64: %w", err)
		}
		return string(decoded), true, nil
	}

	return source.UserData, false, nil
}

// setNoCloudUserData sets the user data in the given CloudInitNoCloud source.
func setNoCloudUserData(source *kubevirtv1.CloudInitNoCloudSource, userData string, asBase64 bool) error {
	if source == nil {
		return fmt.Errorf("nil CloudInitNoCloud source")
	}

	if source.UserDataSecretRef != nil {
		return fmt.Errorf("refusing to set inline userdata on secret-backed CloudInitNoCloud")
	}

	if asBase64 {
		source.UserDataBase64 = base64.StdEncoding.EncodeToString([]byte(userData))
		source.UserData = ""
		return nil
	}

	source.UserData = userData
	source.UserDataBase64 = ""
	return nil
}

// getConfigDriveUserData returns the user data from the given CloudInitConfigDrive source.
func getConfigDriveUserData(source *kubevirtv1.CloudInitConfigDriveSource) (string, bool, error) {
	if source == nil {
		return "", false, fmt.Errorf("nil CloudInitConfigDrive source")
	}

	if source.UserDataBase64 != "" {
		decoded, err := base64.StdEncoding.DecodeString(source.UserDataBase64)
		if err != nil {
			return "", true, fmt.Errorf("failed to decode UserDataBase64: %w", err)
		}
		return string(decoded), true, nil
	}

	return source.UserData, false, nil
}

// setConfigDriveUserData sets the user data in the given CloudInitConfigDrive source.
func setConfigDriveUserData(source *kubevirtv1.CloudInitConfigDriveSource, userData string, asBase64 bool) error {
	if source == nil {
		return fmt.Errorf("nil CloudInitConfigDrive source")
	}

	if asBase64 {
		source.UserDataBase64 = base64.StdEncoding.EncodeToString([]byte(userData))
		source.UserData = ""
		return nil
	}

	source.UserData = userData
	source.UserDataBase64 = ""
	return nil
}

func detectLinuxSSHUser(vm *kubevirtv1.VirtualMachine) string {
	// Preferred order:
	// 1. top-level annotation "username"
	// 2. template label "username"
	// 3. default to "ubuntu"

	if vm != nil {
		if ann := vm.GetAnnotations(); ann != nil {
			if u := ann["username"]; u != "" {
				return u
			}
		}

		if vm.Spec.Template != nil {
			if labels := vm.Spec.Template.ObjectMeta.Labels; labels != nil {
				if u := labels["username"]; u != "" {
					return u
				}
			}
		}
	}

	return "ubuntu"
}

func inlineCloudInitFits(userData string) bool {
	return len([]byte(userData)) <= maxInlineCloudInitBytes
}

func markRemediationRequired(vm *kubevirtv1.VirtualMachine, reason string) {
	ann := vm.GetAnnotations()
	if ann == nil {
		ann = map[string]string{}
	}

	ann[api.AnnBootstrapInlineSkipped] = "true"
	ann[api.AnnRemediationRequired] = "true"
	ann[api.AnnRemediationReason] = reason

	delete(ann, api.AnnCloudInitSecretMergeRequired)

	vm.SetAnnotations(ann)
}

func ensureWindowsSSHAccess(vm *kubevirtv1.VirtualMachine) error {
	tmpl := &vm.Spec.Template.Spec

	for _, cred := range tmpl.AccessCredentials {
		if cred.SSHPublicKey != nil &&
			cred.SSHPublicKey.Source.Secret != nil &&
			cred.SSHPublicKey.Source.Secret.SecretName == api.LinuxSSHSecretName {
			return nil
		}
	}

	tmpl.AccessCredentials = append(tmpl.AccessCredentials, kubevirtv1.AccessCredential{
		SSHPublicKey: &kubevirtv1.SSHPublicKeyAccessCredential{
			Source: kubevirtv1.SSHPublicKeyAccessCredentialSource{
				Secret: &kubevirtv1.AccessCredentialSecretSource{
					SecretName: api.LinuxSSHSecretName,
				},
			},
			PropagationMethod: kubevirtv1.SSHPublicKeyAccessCredentialPropagationMethod{
				QemuGuestAgent: &kubevirtv1.QemuGuestAgentSSHPublicKeyAccessCredentialPropagation{
					Users: []string{"Administrator"},
				},
			},
		},
	})

	return nil
}
