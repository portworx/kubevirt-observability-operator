package operators

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

const (
	operatorInstalled = "Installed"
	operatorMissing   = "Missing"
	operatorUnknown   = "Unknown"
)

// Status describes the state of an OLM-managed operator.
type Status struct {
	Name      string
	State     string
	Namespace string
	CSV       string
	Phase     string
}

// Discover checks whether the operators required by the full platform are
// already installed.
func Discover(
	ctx context.Context,
	client dynamic.Interface,
) ([]Status, error) {
	results := []Status{
		{Name: "OpenShift Logging Operator", State: operatorMissing},
		{Name: "Loki Operator", State: operatorMissing},
		{Name: "Grafana Operator", State: operatorMissing},
	}

	gvr := schema.GroupVersionResource{
		Group:    "operators.coreos.com",
		Version:  "v1alpha1",
		Resource: "clusterserviceversions",
	}

	csvList, err := client.Resource(gvr).
		Namespace(metav1.NamespaceAll).
		List(ctx, metav1.ListOptions{})
	if err != nil {
		// Some clusters may not expose OLM or the caller may not have access.
		for i := range results {
			results[i].State = operatorUnknown
		}

		return results, nil
	}

	for _, csv := range csvList.Items {
		updateOperatorStatus(results, csv)
	}

	return results, nil
}

func updateOperatorStatus(results []Status, csv unstructured.Unstructured) {
	name := strings.ToLower(csv.GetName())

	displayName, _, _ := unstructured.NestedString(
		csv.Object,
		"spec",
		"displayName",
	)

	searchValue := name + " " + strings.ToLower(displayName)

	for i := range results {
		if !matchesOperator(results[i].Name, searchValue) {
			continue
		}

		phase, _, _ := unstructured.NestedString(
			csv.Object,
			"status",
			"phase",
		)

		results[i].State = operatorInstalled
		results[i].Namespace = csv.GetNamespace()
		results[i].CSV = csv.GetName()
		results[i].Phase = phase

		return
	}
}

func matchesOperator(operatorName, value string) bool {
	switch operatorName {
	case "OpenShift Logging Operator":
		return containsAny(
			value,
			"cluster-logging",
			"openshift logging",
			"red hat openshift logging",
		)

	case "Loki Operator":
		return containsAny(
			value,
			"loki-operator",
			"loki operator",
		)

	case "Grafana Operator":
		return containsAny(
			value,
			"grafana-operator",
			"grafana operator",
		)

	default:
		return false
	}
}

func containsAny(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if strings.Contains(value, strings.ToLower(candidate)) {
			return true
		}
	}

	return false
}

// Ready returns true when an installed operator CSV is in Succeeded phase.
func (s Status) Ready() bool {
	return s.State == operatorInstalled &&
		strings.EqualFold(s.Phase, "Succeeded")
}

// String implements a useful debugging representation.
func (s Status) String() string {
	if s.State == operatorMissing {
		return fmt.Sprintf("%s: missing", s.Name)
	}

	if s.State == operatorUnknown {
		return fmt.Sprintf("%s: unknown", s.Name)
	}

	return fmt.Sprintf(
		"%s: %s (%s, %s)",
		s.Name,
		s.State,
		s.Namespace,
		s.Phase,
	)
}
