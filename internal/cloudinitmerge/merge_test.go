package cloudinitmerge

import (
	"strings"
	"testing"

	"github.com/portworx/kubevirt-observability-operator/api"
)

func normalizeCloudInitForTest(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "#cloud-config")
	return strings.TrimSpace(s)
}

func TestMergeLinuxEmpty(t *testing.T) {
	out, changed, err := Merge("", OSLinux)
	if err != nil {
		t.Fatalf("Merge returned error: %v", err)
	}
	if !changed {
		t.Fatalf("expected changed=true")
	}

	if !strings.Contains(out, "#cloud-config") {
		t.Fatalf("expected cloud-config header")
	}
	if !strings.Contains(out, "/usr/local/bin/bootstrap-kubevirt-observability.sh") {
		t.Fatalf("expected linux bootstrap script path")
	}
	if !strings.Contains(out, "/etc/systemd/system/"+api.LinuxServiceName+".service") {
		t.Fatalf("expected linux systemd service path")
	}
	if !strings.Contains(out, "systemctl enable --now "+api.LinuxServiceName) {
		t.Fatalf("expected systemctl enable command")
	}
}

func TestMergeLinuxIdempotent(t *testing.T) {
	first, changed, err := Merge("", OSLinux)
	if err != nil {
		t.Fatalf("first Merge returned error: %v", err)
	}
	if !changed {
		t.Fatalf("expected first changed=true")
	}

	second, changed, err := Merge(first, OSLinux)
	if err != nil {
		t.Fatalf("second Merge returned error: %v", err)
	}
	if changed {
		t.Fatalf("expected second changed=false")
	}
	if first != second {
		t.Fatalf("expected idempotent output")
	}
}

func TestMergeLinuxPreservesExistingRuncmd(t *testing.T) {
	in := `#cloud-config
runcmd:
  - echo hello
`
	out, _, err := Merge(in, OSLinux)
	if err != nil {
		t.Fatalf("Merge returned error: %v", err)
	}

	if !strings.Contains(out, "echo hello") {
		t.Fatalf("expected existing runcmd preserved")
	}
	if !strings.Contains(out, "systemctl daemon-reload") {
		t.Fatalf("expected monitoring runcmd added")
	}
}

func TestMergeWindowsEmpty(t *testing.T) {
	out, changed, err := Merge("", OSWindows)
	if err != nil {
		t.Fatalf("Merge returned error: %v", err)
	}
	if !changed {
		t.Fatalf("expected changed=true")
	}

	if !strings.Contains(out, `C:\ProgramData\KubeVirtObservability\bootstrap-kubevirt-observability.ps1`) {
		t.Fatalf("expected windows bootstrap script path")
	}
	if !strings.Contains(out, `powershell.exe -ExecutionPolicy Bypass -File C:\ProgramData\KubeVirtObservability\bootstrap-kubevirt-observability.ps1`) {
		t.Fatalf("expected windows bootstrap command")
	}
}

func TestMergeWindowsIdempotent(t *testing.T) {
	first, changed, err := Merge("", OSWindows)
	if err != nil {
		t.Fatalf("first Merge returned error: %v", err)
	}
	if !changed {
		t.Fatalf("expected first changed=true")
	}

	second, changed, err := Merge(first, OSWindows)
	if err != nil {
		t.Fatalf("second Merge returned error: %v", err)
	}
	if changed {
		t.Fatalf("expected second changed=false")
	}
	if normalizeCloudInitForTest(first) != normalizeCloudInitForTest(second) {
		t.Fatalf("expected idempotent output")
	}
}
