package sysprepmerge

import (
	"strings"
	"testing"

	"github.com/portworx/kubevirt-observability-operator/api"
)

func TestMergeEmptySysprep(t *testing.T) {
	in := `<?xml version="1.0" encoding="utf-8"?>
<unattend xmlns="urn:schemas-microsoft-com:unattend"></unattend>`

	out, changed, err := Merge(in)
	if err != nil {
		t.Fatalf("Merge returned error: %v", err)
	}
	if !changed {
		t.Fatalf("expected changed=true")
	}

	if !strings.Contains(out, "Microsoft-Windows-Deployment") {
		t.Fatalf("expected deployment component")
	}
	if !strings.Contains(out, "KubeVirt Observability Bootstrap") {
		t.Fatalf("expected managed description")
	}
	if !strings.Contains(out, api.WindowsScheduledTask) {
		t.Fatalf("expected scheduled task name")
	}
}

func TestMergeSysprepIdempotent(t *testing.T) {
	in := `<?xml version="1.0" encoding="utf-8"?>
<unattend xmlns="urn:schemas-microsoft-com:unattend"></unattend>`

	first, changed, err := Merge(in)
	if err != nil {
		t.Fatalf("first Merge returned error: %v", err)
	}
	if !changed {
		t.Fatalf("expected first changed=true")
	}

	second, changed, err := Merge(first)
	if err != nil {
		t.Fatalf("second Merge returned error: %v", err)
	}
	if changed {
		t.Fatalf("expected second changed=false")
	}

	count := strings.Count(second, "<Description>KubeVirt Observability Bootstrap</Description>")
	if count != 1 {
		t.Fatalf("expected managed Description exactly once, got %d", count)
	}
}

func TestMergeMapDataConfigMap(t *testing.T) {
	data := map[string]string{
		"Autounattend.xml": `<?xml version="1.0" encoding="utf-8"?>
<unattend xmlns="urn:schemas-microsoft-com:unattend"></unattend>`,
	}

	out, key, changed, err := MergeMapData(data)
	if err != nil {
		t.Fatalf("MergeMapData returned error: %v", err)
	}
	if !changed {
		t.Fatalf("expected changed=true")
	}
	if key != "Autounattend.xml" {
		t.Fatalf("unexpected key: %s", key)
	}
	if !strings.Contains(out[key], "KubeVirt Observability Bootstrap") {
		t.Fatalf("expected managed command in merged data")
	}
}

func TestMergeByteDataSecret(t *testing.T) {
	data := map[string][]byte{
		"Unattend.xml": []byte(`<?xml version="1.0" encoding="utf-8"?>
<unattend xmlns="urn:schemas-microsoft-com:unattend"></unattend>`),
	}

	out, key, changed, err := MergeByteData(data)
	if err != nil {
		t.Fatalf("MergeByteData returned error: %v", err)
	}
	if !changed {
		t.Fatalf("expected changed=true")
	}
	if key != "Unattend.xml" {
		t.Fatalf("unexpected key: %s", key)
	}
	if !strings.Contains(string(out[key]), "KubeVirt Observability Bootstrap") {
		t.Fatalf("expected managed command in merged data")
	}
}
