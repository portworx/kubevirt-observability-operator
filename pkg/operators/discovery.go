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
	Package   string
	State     string
	Namespace string
	CSV       string
	Phase     string
}

// Discover checks whether the operators required by the platform are
// installed.
//
// The Subscription is used as the source of truth for the installation
// namespace. This avoids using copied CSVs that can appear in many namespaces
// for operators with broad install scope.
func Discover(
	ctx context.Context,
	client dynamic.Interface,
) ([]Status, error) {
	results := []Status{
		{
			Name:    "OpenShift Logging Operator",
			Package: "cluster-logging",
			State:   operatorMissing,
		},
		{
			Name:    "Loki Operator",
			Package: "loki-operator",
			State:   operatorMissing,
		},
		{
			Name:    "Grafana Operator",
			Package: "grafana-operator",
			State:   operatorMissing,
		},
	}

	subscriptionGVR := schema.GroupVersionResource{
		Group:    "operators.coreos.com",
		Version:  "v1alpha1",
		Resource: "subscriptions",
	}

	csvGVR := schema.GroupVersionResource{
		Group:    "operators.coreos.com",
		Version:  "v1alpha1",
		Resource: "clusterserviceversions",
	}

	subscriptions, err := client.
		Resource(subscriptionGVR).
		Namespace(metav1.NamespaceAll).
		List(ctx, metav1.ListOptions{})
	if err != nil {
		for i := range results {
			results[i].State = operatorUnknown
		}

		return results, nil
	}

	for i := range results {
		subscription := findSubscription(
			subscriptions.Items,
			results[i].Package,
		)

		if subscription == nil {
			continue
		}

		results[i].State = operatorInstalled
		results[i].Namespace = subscription.GetNamespace()

		installedCSV, _, _ := unstructured.NestedString(
			subscription.Object,
			"status",
			"installedCSV",
		)

		if installedCSV == "" {
			// Subscription exists, but OLM has not yet resolved an installed CSV.
			results[i].Phase = "Installing"
			continue
		}

		results[i].CSV = installedCSV

		csv, err := client.
			Resource(csvGVR).
			Namespace(subscription.GetNamespace()).
			Get(
				ctx,
				installedCSV,
				metav1.GetOptions{},
			)
		if err != nil {
			results[i].Phase = "Unknown"
			continue
		}

		phase, _, _ := unstructured.NestedString(
			csv.Object,
			"status",
			"phase",
		)

		if phase == "" {
			phase = "Unknown"
		}

		results[i].Phase = phase
	}

	return results, nil
}

func findSubscription(
	subscriptions []unstructured.Unstructured,
	packageName string,
) *unstructured.Unstructured {
	for i := range subscriptions {
		name, _, _ := unstructured.NestedString(
			subscriptions[i].Object,
			"spec",
			"name",
		)

		if name == packageName {
			return &subscriptions[i]
		}
	}

	return nil
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
