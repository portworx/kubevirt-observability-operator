package osdetect

import (
	"strings"

	kubevirtv1 "kubevirt.io/api/core/v1"
)

type Family string

const (
	Linux   Family = "linux"
	Windows Family = "windows"
	Unknown Family = "unknown"
)

// Detect returns the OS family of the given VM.
func Detect(vm *kubevirtv1.VirtualMachine) Family {
	labels := vm.GetLabels()
	if labels == nil {
		return Unknown
	}

	osLabel := strings.ToLower(labels["kubevirt.io/os"])

	switch {
	case strings.Contains(osLabel, "win"):
		return Windows
	case strings.Contains(osLabel, "linux"),
		strings.Contains(osLabel, "ubuntu"),
		strings.Contains(osLabel, "debian"),
		strings.Contains(osLabel, "rhel"),
		strings.Contains(osLabel, "centos"),
		strings.Contains(osLabel, "rocky"),
		strings.Contains(osLabel, "alma"),
		strings.Contains(osLabel, "oel"),
		strings.Contains(osLabel, "fedora"):
		return Linux
	default:
		return Unknown
	}
}
