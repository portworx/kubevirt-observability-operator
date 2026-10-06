package onboarding

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/portworx/kubevirt-observability-operator/pkg/platform"
)

var (
	vmGVR = schema.GroupVersionResource{
		Group:    "kubevirt.io",
		Version:  "v1",
		Resource: "virtualmachines",
	}

	vmiGVR = schema.GroupVersionResource{
		Group:    "kubevirt.io",
		Version:  "v1",
		Resource: "virtualmachineinstances",
	}
)

const (
	annDisableMonitoring = "kubevirt-observability.io/disable-kubevirt-observability"
	annBootstrapManaged  = "kubevirt-observability.io/bootstrap-managed"
	annRemediationDone   = "kubevirt-observability.io/remediation-completed"
)

func Run(
	ctx context.Context,
	out io.Writer,
	cfg Config,
) error {
	applyDefaults(&cfg)

	clients, err := platform.NewClients()
	if err != nil {
		return fmt.Errorf("create Kubernetes clients: %w", err)
	}

	candidates, err := Discover(ctx, clients, cfg)
	if err != nil {
		return err
	}

	fmt.Fprintln(out, "KubeVirt Observability Existing VM Onboarding")
	fmt.Fprintln(out)

	if cfg.DryRun {
		fmt.Fprintln(out, "DRY RUN - no changes will be made")
		fmt.Fprintln(out)
	}

	if len(candidates) == 0 {
		fmt.Fprintln(out, "No running VMs found.")
		return nil
	}

	fmt.Fprintf(
		out,
		"%-45s %-30s %-10s %-10s %s\n",
		"NAMESPACE",
		"VM",
		"OS",
		"SOURCE",
		"STATUS",
	)

	for _, c := range candidates {
		status := string(c.State)
		if c.Reason != "" {
			status += " - " + c.Reason
		}

		fmt.Fprintf(
			out,
			"%-45s %-30s %-10s %-10s %s\n",
			c.Namespace,
			c.Name,
			c.OS,
			c.OSSource,
			status,
		)
	}

	var ready, managed, progress, skipped, conflicts int

	for _, c := range candidates {
		switch c.State {
		case StateReady:
			ready++
		case StateAlreadyManaged:
			managed++
		case StateInProgress:
			progress++
		case StateConflict:
			conflicts++
		default:
			skipped++
		}
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, "Summary:")
	fmt.Fprintf(out, "  Running VMs:       %d\n", len(candidates))
	fmt.Fprintf(out, "  Ready to onboard:  %d\n", ready)
	fmt.Fprintf(out, "  Already managed:   %d\n", managed)
	fmt.Fprintf(out, "  In progress:       %d\n", progress)
	fmt.Fprintf(out, "  Conflicts:         %d\n", conflicts)
	fmt.Fprintf(out, "  Skipped:           %d\n", skipped)

	if cfg.DryRun || ready == 0 {
		return nil
	}

	publicKey, err := monitoringPublicKey(
		ctx,
		clients,
		cfg,
	)
	if err != nil {
		return err
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, "Executing onboarding...")
	fmt.Fprintln(out)

	var onboarded int
	var failed int

	for idx := range candidates {
		c := &candidates[idx]

		if c.State != StateReady {
			continue
		}

		fmt.Fprintf(
			out,
			"Onboarding %s/%s (%s, %s)...\n",
			c.Namespace,
			c.Name,
			c.OS,
			c.IP,
		)

		if c.IP == "" || c.IP == "10.0.2.2" {
			c.State = StateFailed
			c.Reason = "invalid VM IP"
			failed++
			fmt.Fprintf(out, "  FAILED: %s\n", c.Reason)
			continue
		}

		if err := bootstrapGuest(
			ctx,
			cfg,
			*c,
			publicKey,
		); err != nil {
			c.State = StateFailed
			c.Reason = err.Error()
			failed++
			fmt.Fprintf(out, "  FAILED: %v\n", err)
			continue
		}

		if err := markVMReadyForReconciliation(
			ctx,
			clients,
			*c,
		); err != nil {
			c.State = StateFailed
			c.Reason = err.Error()
			failed++
			fmt.Fprintf(out, "  FAILED: %v\n", err)
			continue
		}

		c.State = StateOnboarded
		c.Reason = ""
		onboarded++
		fmt.Fprintln(out, "  ONBOARDED")
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, "Execution summary:")
	fmt.Fprintf(out, "  Onboarded: %d\n", onboarded)
	fmt.Fprintf(out, "  Failed:    %d\n", failed)

	if failed > 0 {
		return fmt.Errorf(
			"%d VM(s) failed onboarding",
			failed,
		)
	}

	return nil
}

func Discover(
	ctx context.Context,
	clients *platform.Clients,
	cfg Config,
) ([]Candidate, error) {
	linuxSelector, err := parseSelector(cfg.LinuxSelector)
	if err != nil {
		return nil, fmt.Errorf("invalid --linux-selector: %w", err)
	}

	windowsSelector, err := parseSelector(cfg.WindowsSelector)
	if err != nil {
		return nil, fmt.Errorf("invalid --windows-selector: %w", err)
	}

	namespace := cfg.Namespace
	if namespace == "" {
		namespace = metav1.NamespaceAll
	}

	vmis, err := clients.Dynamic.
		Resource(vmiGVR).
		Namespace(namespace).
		List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list VirtualMachineInstances: %w", err)
	}

	result := make([]Candidate, 0, len(vmis.Items))

	for i := range vmis.Items {
		vmi := &vmis.Items[i]

		phase, _, _ := unstructured.NestedString(
			vmi.Object,
			"status",
			"phase",
		)

		if phase != "Running" {
			continue
		}

		name := vmi.GetName()
		ns := vmi.GetNamespace()

		if cfg.VMName != "" && name != cfg.VMName {
			continue
		}

		vm, err := clients.Dynamic.
			Resource(vmGVR).
			Namespace(ns).
			Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf(
				"get VirtualMachine %s/%s: %w",
				ns,
				name,
				err,
			)
		}

		c := classifyVM(
			vm,
			vmi,
			linuxSelector,
			windowsSelector,
			cfg,
		)

		result = append(result, c)
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].Namespace == result[j].Namespace {
			return result[i].Name < result[j].Name
		}
		return result[i].Namespace < result[j].Namespace
	})

	return result, nil
}

func parseSelector(raw string) (labels.Selector, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}

	return labels.Parse(raw)
}

func classifyVM(
	vm *unstructured.Unstructured,
	vmi *unstructured.Unstructured,
	linuxSelector labels.Selector,
	windowsSelector labels.Selector,
	cfg Config,
) Candidate {
	c := Candidate{
		Namespace: vm.GetNamespace(),
		Name:      vm.GetName(),
		OS:        "unknown",
		OSSource:  "-",
		State:     StateReady,
	}

	ifaces, _, _ := unstructured.NestedSlice(
		vmi.Object,
		"status",
		"interfaces",
	)

	if len(ifaces) > 0 {
		if first, ok := ifaces[0].(map[string]interface{}); ok {
			if ip, ok := first["ipAddress"].(string); ok {
				c.IP = ip
			}
		}
	}

	annotations := vm.GetAnnotations()

	if annotations[annDisableMonitoring] == "true" {
		c.State = StateSkipped
		c.Reason = "monitoring disabled"
		return c
	}

	// Resolve OS before evaluating managed state. A VM may already be
	// bootstrap-managed by the CREATE webhook but have been skipped because
	// its OS label was missing. kvoctl onboard must be able to recover it.
	vmLabels := labels.Set(vm.GetLabels())
	vmiLabels := labels.Set(vmi.GetLabels())

	linuxMatch := selectorMatches(
		linuxSelector,
		vmLabels,
		vmiLabels,
	)

	windowsMatch := selectorMatches(
		windowsSelector,
		vmLabels,
		vmiLabels,
	)

	if linuxMatch && windowsMatch {
		c.State = StateConflict
		c.Reason = "matches both Linux and Windows selectors"
		return c
	}

	existingOS := normalizeOS(vm.GetLabels()["kubevirt.io/os"])

	if existingOS != "" {
		c.OS = existingOS
		c.OSSource = "label"

		if linuxMatch && existingOS != "linux" {
			c.State = StateConflict
			c.Reason = "existing OS label conflicts with Linux selector"
			return c
		}

		if windowsMatch && existingOS != "windows" {
			c.State = StateConflict
			c.Reason = "existing OS label conflicts with Windows selector"
			return c
		}
	} else if linuxMatch {
		c.OS = "linux"
		c.OSSource = "selector"
	} else if windowsMatch {
		c.OS = "windows"
		c.OSSource = "selector"
	}

	if annotations[annBootstrapManaged] == "true" {
		// Fully completed remediation is idempotent: never trigger it again.
		if annotations[annRemediationDone] == "true" {
			c.State = StateAlreadyManaged
			c.Reason = "onboarding completed"
			return c
		}

		// The CREATE webhook may already own the VM but the controller can
		// have skipped it because no OS label existed at creation time.
		// A selector supplied to kvoctl can recover this state.
		if annotations["kubevirt-observability.io/status"] == "skipped" &&
			annotations["kubevirt-observability.io/reason"] == "missing-os-label" {

			if c.OS == "unknown" {
				c.State = StateSkipped
				c.Reason = "OS could not be determined"
			} else {
				c.State = StateReady
				c.Reason = "recover skipped VM"
			}
			return c
		}

		c.State = StateInProgress
		c.Reason = "already managed"
		return c
	}

	if c.OS != "unknown" {
		return c
	}

	c.State = StateSkipped
	c.Reason = "OS could not be determined"
	return c
}

func resolveExistingOS(c *Candidate, vmLabels map[string]string) {
	if os := normalizeOS(vmLabels["kubevirt.io/os"]); os != "" {
		c.OS = os
		c.OSSource = "label"
	}
}

func selectorMatches(
	selector labels.Selector,
	vmLabels labels.Set,
	vmiLabels labels.Set,
) bool {
	if selector == nil {
		return false
	}

	return selector.Matches(vmLabels) ||
		selector.Matches(vmiLabels)
}

func normalizeOS(raw string) string {
	v := strings.ToLower(strings.TrimSpace(raw))

	switch {
	case strings.Contains(v, "win"):
		return "windows"

	case strings.Contains(v, "linux"),
		strings.Contains(v, "ubuntu"),
		strings.Contains(v, "debian"),
		strings.Contains(v, "rhel"),
		strings.Contains(v, "centos"),
		strings.Contains(v, "rocky"),
		strings.Contains(v, "alma"),
		strings.Contains(v, "oel"),
		v == "ol",
		strings.Contains(v, "oracle"),
		strings.Contains(v, "fedora"):
		return "linux"

	default:
		return ""
	}
}
