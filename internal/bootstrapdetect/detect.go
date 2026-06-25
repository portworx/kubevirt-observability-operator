package bootstrapdetect

import kubevirtv1 "kubevirt.io/api/core/v1"

type Kind string

const (
	CloudInitNoCloud   Kind = "cloudInitNoCloud"
	CloudInitConfigDrv Kind = "cloudInitConfigDrive"
	Sysprep            Kind = "sysprep"
	None               Kind = "none"
)

type Result struct {
	Kind        Kind
	VolumeIndex int
	VolumeName  string
}

// Detect returns the bootstrap type and volume index of the given VM.
func Detect(vm *kubevirtv1.VirtualMachine) Result {
	vols := vm.Spec.Template.Spec.Volumes
	for i, v := range vols {
		switch {
		case v.CloudInitNoCloud != nil:
			return Result{Kind: CloudInitNoCloud, VolumeIndex: i, VolumeName: v.Name}
		case v.CloudInitConfigDrive != nil:
			return Result{Kind: CloudInitConfigDrv, VolumeIndex: i, VolumeName: v.Name}
		case v.Sysprep != nil:
			return Result{Kind: Sysprep, VolumeIndex: i, VolumeName: v.Name}
		}
	}
	return Result{Kind: None, VolumeIndex: -1}
}
