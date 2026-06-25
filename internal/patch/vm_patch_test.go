package patch

import (
	"encoding/base64"
	"strings"
	"testing"

	kubevirtv1 "kubevirt.io/api/core/v1"
)

func TestMergeCloudInitNoCloudUserDataBase64Linux(t *testing.T) {
	original := "#cloud-config\nruncmd:\n  - echo hello\n"

	source := &kubevirtv1.CloudInitNoCloudSource{
		UserDataBase64: base64.StdEncoding.EncodeToString([]byte(original)),
	}

	err := mergeCloudInitNoCloud(source, "linux")
	if err != nil {
		t.Fatalf("mergeCloudInitNoCloud returned error: %v", err)
	}

	if source.UserData != "" {
		t.Fatalf("expected UserData to stay empty when base64 is used")
	}
	if source.UserDataBase64 == "" {
		t.Fatalf("expected UserDataBase64 to be set")
	}

	decoded, err := base64.StdEncoding.DecodeString(source.UserDataBase64)
	if err != nil {
		t.Fatalf("failed to decode merged base64: %v", err)
	}

	out := string(decoded)
	if !strings.Contains(out, "echo hello") {
		t.Fatalf("expected original content preserved")
	}
	if !strings.Contains(out, "/usr/local/bin/bootstrap-kubevirt-observability.sh") {
		t.Fatalf("expected monitoring bootstrap added")
	}
}

func TestMergeCloudInitConfigDriveUserDataBase64Windows(t *testing.T) {
	original := "#cloud-config\nruncmd:\n  - echo hello\n"

	source := &kubevirtv1.CloudInitConfigDriveSource{
		UserDataBase64: base64.StdEncoding.EncodeToString([]byte(original)),
	}

	err := mergeCloudInitConfigDrive(source, "windows")
	if err != nil {
		t.Fatalf("mergeCloudInitConfigDrive returned error: %v", err)
	}

	if source.UserData != "" {
		t.Fatalf("expected UserData to stay empty when base64 is used")
	}
	if source.UserDataBase64 == "" {
		t.Fatalf("expected UserDataBase64 to be set")
	}

	decoded, err := base64.StdEncoding.DecodeString(source.UserDataBase64)
	if err != nil {
		t.Fatalf("failed to decode merged base64: %v", err)
	}

	out := string(decoded)
	if !strings.Contains(out, "echo hello") {
		t.Fatalf("expected original content preserved")
	}
	if !strings.Contains(out, `C:\ProgramData\KubeVirtObservability\bootstrap-kubevirt-observability.ps1`) {
		t.Fatalf("expected windows bootstrap added")
	}
}
