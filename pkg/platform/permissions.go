package platform

import (
	"context"
	"fmt"

	authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PermissionCheck represents one permission required by deployment.
type PermissionCheck struct {
	Name      string
	Verb      string
	Group     string
	Resource  string
	Namespace string
	Allowed   bool
	Reason    string
}

// CheckDeployPermissions checks permissions that kvoctl deploy will need.
// SelfSubjectAccessReview does not modify cluster resources.
func CheckDeployPermissions(
	ctx context.Context,
	clients *Clients,
) ([]PermissionCheck, error) {
	checks := []PermissionCheck{
		{
			Name:     "Create namespaces",
			Verb:     "create",
			Resource: "namespaces",
		},
		{
			Name:     "Create cluster roles",
			Verb:     "create",
			Group:    "rbac.authorization.k8s.io",
			Resource: "clusterroles",
		},
		{
			Name:     "Create cluster role bindings",
			Verb:     "create",
			Group:    "rbac.authorization.k8s.io",
			Resource: "clusterrolebindings",
		},
		{
			Name:      "Create OLM subscriptions",
			Verb:      "create",
			Group:     "operators.coreos.com",
			Resource:  "subscriptions",
			Namespace: "openshift-operators",
		},
		{
			Name:      "Create secrets",
			Verb:      "create",
			Resource:  "secrets",
			Namespace: "openshift-logging",
		},
		{
			Name:      "Create service accounts",
			Verb:      "create",
			Resource:  "serviceaccounts",
			Namespace: "openshift-logging",
		},
	}

	for i := range checks {
		attributes := authorizationv1.ResourceAttributes{
			Namespace: checks[i].Namespace,
			Verb:      checks[i].Verb,
			Group:     checks[i].Group,
			Resource:  checks[i].Resource,
		}

		review := &authorizationv1.SelfSubjectAccessReview{
			Spec: authorizationv1.SelfSubjectAccessReviewSpec{
				ResourceAttributes: &attributes,
			},
		}

		result, err := clients.Kube.
			AuthorizationV1().
			SelfSubjectAccessReviews().
			Create(ctx, review, metav1.CreateOptions{})
		if err != nil {
			return nil, fmt.Errorf(
				"check permission %q: %w",
				checks[i].Name,
				err,
			)
		}

		checks[i].Allowed = result.Status.Allowed
		checks[i].Reason = result.Status.Reason

		if result.Status.Denied && checks[i].Reason == "" {
			checks[i].Reason = "permission denied"
		}
	}

	return checks, nil
}
