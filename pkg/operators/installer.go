package operators

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

const (
	defaultCatalogSource          = "redhat-operators"
	defaultCatalogSourceNamespace = "openshift-marketplace"
)

// PackageInfo describes an OLM package discovered from PackageManifest.
type PackageInfo struct {
	Name                   string
	CatalogSource          string
	CatalogSourceNamespace string
	DefaultChannel         string
}

// InstallConfig describes an OLM operator that kvoctl can install.
type InstallConfig struct {
	Package           string
	TargetNamespace   string
	OperatorGroupName string
	CatalogSource     string
	CatalogNamespace  string
	Channel           string
}

// Installer installs prerequisite OLM operators.
type Installer struct {
	kube    kubernetes.Interface
	dynamic dynamic.Interface
}

// NewInstaller creates an operator installer.
func NewInstaller(
	kube kubernetes.Interface,
	dynamicClient dynamic.Interface,
) *Installer {
	return &Installer{
		kube:    kube,
		dynamic: dynamicClient,
	}
}

// DiscoverPackage finds a package from the requested catalog source.
func (i *Installer) DiscoverPackage(
	ctx context.Context,
	packageName string,
	catalogSource string,
) (*PackageInfo, error) {
	if i.dynamic == nil {
		return nil, fmt.Errorf("dynamic Kubernetes client is required")
	}

	if packageName == "" {
		return nil, fmt.Errorf("package name is required")
	}

	if catalogSource == "" {
		catalogSource = defaultCatalogSource
	}

	gvr := schema.GroupVersionResource{
		Group:    "packages.operators.coreos.com",
		Version:  "v1",
		Resource: "packagemanifests",
	}

	list, err := i.dynamic.
		Resource(gvr).
		Namespace(defaultCatalogSourceNamespace).
		List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list PackageManifests: %w", err)
	}

	for _, item := range list.Items {
		if item.GetName() != packageName {
			continue
		}

		source, _, _ := unstructured.NestedString(
			item.Object,
			"status",
			"catalogSource",
		)

		if source != catalogSource {
			continue
		}

		sourceNamespace, _, _ := unstructured.NestedString(
			item.Object,
			"status",
			"catalogSourceNamespace",
		)

		defaultChannel, _, _ := unstructured.NestedString(
			item.Object,
			"status",
			"defaultChannel",
		)

		if defaultChannel == "" {
			return nil, fmt.Errorf(
				"package %q from catalog %q has no default channel",
				packageName,
				catalogSource,
			)
		}

		return &PackageInfo{
			Name:                   packageName,
			CatalogSource:          source,
			CatalogSourceNamespace: sourceNamespace,
			DefaultChannel:         defaultChannel,
		}, nil
	}

	return nil, fmt.Errorf(
		"package %q not found in catalog %q",
		packageName,
		catalogSource,
	)
}

func (i *Installer) ensureNamespace(
	ctx context.Context,
	namespace string,
) error {
	if namespace == "" {
		return fmt.Errorf("namespace is required")
	}

	existing, err := i.kube.
		CoreV1().
		Namespaces().
		Get(ctx, namespace, metav1.GetOptions{})

	if err == nil {
		if existing.Labels == nil {
			existing.Labels = map[string]string{}
		}

		if existing.Labels["openshift.io/cluster-monitoring"] != "true" {
			existing.Labels["openshift.io/cluster-monitoring"] = "true"

			if _, err := i.kube.
				CoreV1().
				Namespaces().
				Update(ctx, existing, metav1.UpdateOptions{}); err != nil {
				return fmt.Errorf(
					"label namespace %q for cluster monitoring: %w",
					namespace,
					err,
				)
			}
		}

		return nil
	}

	if !apierrors.IsNotFound(err) {
		return fmt.Errorf(
			"get namespace %q: %w",
			namespace,
			err,
		)
	}

	_, err = i.kube.
		CoreV1().
		Namespaces().
		Create(
			ctx,
			&corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: namespace,
					Labels: map[string]string{
						"openshift.io/cluster-monitoring": "true",
					},
				},
			},
			metav1.CreateOptions{},
		)

	if err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf(
			"create namespace %q: %w",
			namespace,
			err,
		)
	}

	return nil
}

func (i *Installer) ensureOperatorGroup(
	ctx context.Context,
	namespace string,
	name string,
) error {
	if name == "" {
		return fmt.Errorf("OperatorGroup name is required")
	}

	gvr := schema.GroupVersionResource{
		Group:    "operators.coreos.com",
		Version:  "v1",
		Resource: "operatorgroups",
	}

	resource := i.dynamic.
		Resource(gvr).
		Namespace(namespace)

	_, err := resource.Get(
		ctx,
		name,
		metav1.GetOptions{},
	)

	if err == nil {
		return nil
	}

	if !apierrors.IsNotFound(err) {
		return fmt.Errorf(
			"get OperatorGroup %s/%s: %w",
			namespace,
			name,
			err,
		)
	}

	operatorGroup := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "operators.coreos.com/v1",
			"kind":       "OperatorGroup",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": namespace,
			},
			"spec": map[string]interface{}{
				"upgradeStrategy": "Default",
			},
		},
	}

	_, err = resource.Create(
		ctx,
		operatorGroup,
		metav1.CreateOptions{},
	)

	if err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf(
			"create OperatorGroup %s/%s: %w",
			namespace,
			name,
			err,
		)
	}

	return nil
}

// EnsureInstalled creates or updates an OperatorGroup and Subscription.
func (i *Installer) EnsureInstalled(
	ctx context.Context,
	cfg InstallConfig,
) error {
	if i.kube == nil || i.dynamic == nil {
		return fmt.Errorf("Kubernetes clients are required")
	}

	if cfg.Package == "" {
		return fmt.Errorf("operator package is required")
	}

	if cfg.TargetNamespace == "" {
		return fmt.Errorf("target namespace is required")
	}

	if cfg.OperatorGroupName == "" {
		return fmt.Errorf("OperatorGroup name is required")
	}

	if cfg.CatalogSource == "" {
		cfg.CatalogSource = defaultCatalogSource
	}

	if cfg.CatalogNamespace == "" {
		cfg.CatalogNamespace = defaultCatalogSourceNamespace
	}

	if cfg.Channel == "" {
		return fmt.Errorf("operator channel is required")
	}

	if err := i.ensureNamespace(ctx, cfg.TargetNamespace); err != nil {
		return err
	}

	if err := i.ensureOperatorGroup(
		ctx,
		cfg.TargetNamespace,
		cfg.OperatorGroupName,
	); err != nil {
		return err
	}

	gvr := schema.GroupVersionResource{
		Group:    "operators.coreos.com",
		Version:  "v1alpha1",
		Resource: "subscriptions",
	}

	subscriptionName := cfg.Package

	subscription := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "operators.coreos.com/v1alpha1",
			"kind":       "Subscription",
			"metadata": map[string]interface{}{
				"name":      subscriptionName,
				"namespace": cfg.TargetNamespace,
			},
			"spec": map[string]interface{}{
				"channel":             cfg.Channel,
				"name":                cfg.Package,
				"source":              cfg.CatalogSource,
				"sourceNamespace":     cfg.CatalogNamespace,
				"installPlanApproval": "Automatic",
			},
		},
	}

	resource := i.dynamic.
		Resource(gvr).
		Namespace(cfg.TargetNamespace)

	existing, err := resource.Get(
		ctx,
		subscriptionName,
		metav1.GetOptions{},
	)

	if err == nil {
		currentChannel, _, _ := unstructured.NestedString(
			existing.Object,
			"spec",
			"channel",
		)

		currentSource, _, _ := unstructured.NestedString(
			existing.Object,
			"spec",
			"source",
		)

		if currentChannel == cfg.Channel &&
			currentSource == cfg.CatalogSource {
			return nil
		}

		if err := unstructured.SetNestedField(
			existing.Object,
			cfg.Channel,
			"spec",
			"channel",
		); err != nil {
			return fmt.Errorf("set subscription channel: %w", err)
		}

		if err := unstructured.SetNestedField(
			existing.Object,
			cfg.CatalogSource,
			"spec",
			"source",
		); err != nil {
			return fmt.Errorf("set subscription source: %w", err)
		}

		if err := unstructured.SetNestedField(
			existing.Object,
			cfg.CatalogNamespace,
			"spec",
			"sourceNamespace",
		); err != nil {
			return fmt.Errorf(
				"set subscription source namespace: %w",
				err,
			)
		}

		_, err = resource.Update(
			ctx,
			existing,
			metav1.UpdateOptions{},
		)
		if err != nil {
			return fmt.Errorf(
				"update Subscription %s/%s: %w",
				cfg.TargetNamespace,
				subscriptionName,
				err,
			)
		}

		return nil
	}

	if !apierrors.IsNotFound(err) {
		return fmt.Errorf(
			"get Subscription %s/%s: %w",
			cfg.TargetNamespace,
			subscriptionName,
			err,
		)
	}

	_, err = resource.Create(
		ctx,
		subscription,
		metav1.CreateOptions{},
	)

	if err != nil {
		return fmt.Errorf(
			"create Subscription %s/%s: %w",
			cfg.TargetNamespace,
			subscriptionName,
			err,
		)
	}

	return nil
}
