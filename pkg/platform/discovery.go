package platform

import (
	"context"
	"fmt"
	"os"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	defaultStorageClassAnnotation = "storageclass.kubernetes.io/is-default-class"
	betaDefaultSCAnnotation       = "storageclass.beta.kubernetes.io/is-default-class"
)

// Clients contains Kubernetes clients used by kvoctl.
type Clients struct {
	Config         *rest.Config
	Kube           kubernetes.Interface
	Dynamic        dynamic.Interface
	Discovery      discovery.DiscoveryInterface
	CurrentContext string
}

// ClusterInfo contains information discovered from the target cluster.
type ClusterInfo struct {
	CurrentContext          string
	APIServer               string
	OpenShift               bool
	OpenShiftVersion        string
	KubeVirt                bool
	OpenShiftVirtualization bool
	DefaultStorageClass     string
	RecommendedStorageClass string
	StorageClassReason      string
	AllStorageClasses       []string
}

// NewClients creates Kubernetes clients using the normal kubeconfig loading
// behavior. This works with KUBECONFIG as well as ~/.kube/config.
func NewClients() (*Clients, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()

	if kubeconfig := os.Getenv("KUBECONFIG"); kubeconfig != "" {
		loadingRules.ExplicitPath = kubeconfig
	}

	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		loadingRules,
		&clientcmd.ConfigOverrides{},
	)

	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("load Kubernetes client configuration: %w", err)
	}

	rawConfig, err := clientConfig.RawConfig()
	if err != nil {
		return nil, fmt.Errorf("read kubeconfig: %w", err)
	}

	kubeClient, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("create Kubernetes client: %w", err)
	}

	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("create dynamic Kubernetes client: %w", err)
	}

	discoveryClient, err := discovery.NewDiscoveryClientForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("create discovery client: %w", err)
	}

	return &Clients{
		Config:         restConfig,
		Kube:           kubeClient,
		Dynamic:        dynamicClient,
		Discovery:      discoveryClient,
		CurrentContext: rawConfig.CurrentContext,
	}, nil
}

// DiscoverCluster gathers platform information needed by the installer.
func DiscoverCluster(ctx context.Context, clients *Clients) (*ClusterInfo, error) {
	info := &ClusterInfo{
		CurrentContext: clients.CurrentContext,
		APIServer:      clients.Config.Host,
	}

	if err := discoverOpenShift(ctx, clients, info); err != nil {
		return nil, err
	}

	if err := discoverVirtualization(clients, info); err != nil {
		return nil, err
	}

	if err := discoverStorageClasses(ctx, clients, info); err != nil {
		return nil, err
	}

	return info, nil
}

func discoverOpenShift(
	ctx context.Context,
	clients *Clients,
	info *ClusterInfo,
) error {
	gvr := schema.GroupVersionResource{
		Group:    "config.openshift.io",
		Version:  "v1",
		Resource: "clusterversions",
	}

	resourceList, err := clients.Discovery.ServerResourcesForGroupVersion(
		"config.openshift.io/v1",
	)
	if err != nil || resourceList == nil {
		info.OpenShift = false
		return nil
	}

	info.OpenShift = true

	clusterVersion, err := clients.Dynamic.
		Resource(gvr).
		Get(ctx, "version", metav1.GetOptions{})
	if err != nil {
		// OpenShift was detected. Lack of permission to ClusterVersion should
		// not make the entire preflight unusable.
		info.OpenShiftVersion = "unknown"
		return nil
	}

	status, ok := clusterVersion.Object["status"].(map[string]interface{})
	if !ok {
		info.OpenShiftVersion = "unknown"
		return nil
	}

	desired, ok := status["desired"].(map[string]interface{})
	if !ok {
		info.OpenShiftVersion = "unknown"
		return nil
	}

	version, _ := desired["version"].(string)
	if version == "" {
		version = "unknown"
	}

	info.OpenShiftVersion = version
	return nil
}

func discoverVirtualization(
	clients *Clients,
	info *ClusterInfo,
) error {
	groups, err := clients.Discovery.ServerGroups()
	if err != nil {
		return fmt.Errorf("discover API groups: %w", err)
	}

	for _, group := range groups.Groups {
		switch group.Name {
		case "kubevirt.io":
			info.KubeVirt = true

		case "hco.kubevirt.io":
			info.OpenShiftVirtualization = true
		}
	}

	// OpenShift Virtualization always includes KubeVirt underneath it.
	if info.OpenShiftVirtualization {
		info.KubeVirt = true
	}

	return nil
}

func discoverStorageClasses(
	ctx context.Context,
	clients *Clients,
	info *ClusterInfo,
) error {
	storageClasses, err := clients.Kube.
		StorageV1().
		StorageClasses().
		List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("list StorageClasses: %w", err)
	}

	for _, storageClass := range storageClasses.Items {
		info.AllStorageClasses = append(
			info.AllStorageClasses,
			storageClass.Name,
		)

		if isDefaultStorageClass(storageClass.Annotations) &&
			info.DefaultStorageClass == "" {
			info.DefaultStorageClass = storageClass.Name
		}
	}

	// Kubernetes default always wins.
	if info.DefaultStorageClass != "" {
		info.RecommendedStorageClass = info.DefaultStorageClass
		info.StorageClassReason = "cluster default"
		return nil
	}

	// Portworx recommendation for Loki when no Kubernetes default exists.
	for _, storageClass := range storageClasses.Items {
		if storageClass.Name == "px-csi-db" {
			info.RecommendedStorageClass = storageClass.Name
			info.StorageClassReason = "recommended Portworx storage class"
			return nil
		}
	}

	return nil
}

func isDefaultStorageClass(annotations map[string]string) bool {
	if annotations == nil {
		return false
	}

	for _, annotation := range []string{
		defaultStorageClassAnnotation,
		betaDefaultSCAnnotation,
	} {
		if strings.EqualFold(annotations[annotation], "true") {
			return true
		}
	}

	return false
}
