package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime/debug"

	admissionv1 "k8s.io/api/admission/v1"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/portworx/kubevirt-observability-operator/internal/patch"
)

type VMMutator struct {
	Decoder admission.Decoder
}

// Handle handles the given admission request.
func (m *VMMutator) Handle(ctx context.Context, req admission.Request) (resp admission.Response) {
	defer func() {
		if r := recover(); r != nil {
			stack := debug.Stack()
			fmt.Printf("PANIC in VMMutator.Handle: %v\n%s\n", r, string(stack))
			resp = admission.Denied(fmt.Sprintf("panic: %v\n%s", r, string(stack)))
		}
	}()

	if req.Operation != admissionv1.Create {
		return admission.Allowed("only create requests are mutated")
	}

	if m.Decoder == nil {
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("webhook decoder is nil"))
	}

	vm := &kubevirtv1.VirtualMachine{}
	if err := m.Decoder.Decode(req, vm); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	ann := vm.GetAnnotations()
	if ann != nil && ann["kubevirt-observability.io/disable-kubevirt-observability"] == "true" {
		return admission.Allowed("kubevirt observability disabled")
	}

	original, err := json.Marshal(vm)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	if err := patch.EnsureMonitoringBootstrap(vm); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	for i := range vm.Spec.Template.Spec.Volumes {
		v := &vm.Spec.Template.Spec.Volumes[i]
		if v.CloudInitNoCloud != nil {
			secretName := ""
			if v.CloudInitNoCloud.UserDataSecretRef != nil {
				secretName = v.CloudInitNoCloud.UserDataSecretRef.Name
			}
			fmt.Printf("POST-MUTATION vm=%s/%s vol=%s userDataLen=%d userDataBase64Len=%d hasSecretRef=%t secret=%s\n",
				vm.Namespace, vm.Name, v.Name,
				len(v.CloudInitNoCloud.UserData),
				len(v.CloudInitNoCloud.UserDataBase64),
				v.CloudInitNoCloud.UserDataSecretRef != nil,
				secretName,
			)
		}
	}

	mutated, err := json.Marshal(vm)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	ann = vm.GetAnnotations()
	if ann == nil {
		ann = map[string]string{}
	}
	ann["kubevirt-observability.io/debug-webhook-version"] = "windows-inline-skip-v1"
	vm.SetAnnotations(ann)

	return admission.PatchResponseFromRaw(original, mutated)
}

// InjectDecoder injects the given decoder.
func (m *VMMutator) InjectDecoder(d admission.Decoder) error {
	m.Decoder = d
	return nil
}
