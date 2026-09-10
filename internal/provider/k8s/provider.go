// Package k8s provides Kubernetes resource discovery and labeling.
package k8s

import (
	"context"
	"os"
	"path/filepath"
	"sync"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/unicrons/tagctl/internal/config"
	"github.com/unicrons/tagctl/internal/provider"
	"github.com/unicrons/tagctl/internal/types"
)

// providerName identifies this provider in resources and errors.
const providerName = "kubernetes"

// Supported resource types
const (
	ResourceTypePod        = "k8s_pod"
	ResourceTypeDeployment = "k8s_deployment"
	ResourceTypeService    = "k8s_service"
	ResourceTypeNamespace  = "k8s_namespace"
	ResourceTypeConfigMap  = "k8s_configmap"
	ResourceTypeSecret     = "k8s_secret"
)

// DefaultResourceTypes are the resource types scanned by default.
var DefaultResourceTypes = []string{
	ResourceTypePod,
	ResourceTypeDeployment,
	ResourceTypeService,
	ResourceTypeNamespace,
	ResourceTypeConfigMap,
	ResourceTypeSecret,
}

// Provider implements the provider.Provider interface for Kubernetes.
type Provider struct {
	clientset     kubernetes.Interface
	cluster       config.KubernetesCluster
	namespaces    []string
	resourceTypes []string
}

// New creates a new Kubernetes provider with the given cluster configuration.
func New(ctx context.Context, cluster config.KubernetesCluster) (*Provider, error) {
	var cfg *rest.Config
	var err error

	if cluster.Kubeconfig == "in-cluster" {
		cfg, err = rest.InClusterConfig()
		if err != nil {
			return nil, provider.NewProviderError(providerName, "in_cluster_config", "", err)
		}
	} else {
		kubeconfig := cluster.Kubeconfig
		if kubeconfig == "" {
			// Default to ~/.kube/config
			home, _ := os.UserHomeDir()
			kubeconfig = filepath.Join(home, ".kube", "config")
		}

		// Expand ~ in path
		if len(kubeconfig) > 1 && kubeconfig[:2] == "~/" {
			home, _ := os.UserHomeDir()
			kubeconfig = filepath.Join(home, kubeconfig[2:])
		}

		loadingRules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfig}
		configOverrides := &clientcmd.ConfigOverrides{}

		if cluster.Context != "" {
			configOverrides.CurrentContext = cluster.Context
		}

		clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)
		cfg, err = clientConfig.ClientConfig()
		if err != nil {
			return nil, provider.NewProviderError(providerName, "load_kubeconfig", kubeconfig, err)
		}
	}

	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, provider.NewProviderError(providerName, "create_clientset", "", err)
	}

	resourceTypes := cluster.ResourceTypes
	if len(resourceTypes) == 0 {
		resourceTypes = DefaultResourceTypes
	}

	return &Provider{
		clientset:     clientset,
		cluster:       cluster,
		namespaces:    cluster.Namespaces,
		resourceTypes: resourceTypes,
	}, nil
}

// NewWithClientset creates a new Kubernetes provider with a pre-configured clientset.
// This is useful for testing.
func NewWithClientset(clientset kubernetes.Interface, cluster config.KubernetesCluster) *Provider {
	resourceTypes := cluster.ResourceTypes
	if len(resourceTypes) == 0 {
		resourceTypes = DefaultResourceTypes
	}

	return &Provider{
		clientset:     clientset,
		cluster:       cluster,
		namespaces:    cluster.Namespaces,
		resourceTypes: resourceTypes,
	}
}

// Name returns the provider identifier.
func (p *Provider) Name() string {
	return providerName
}

// ClusterName returns the cluster name.
func (p *Provider) ClusterName() string {
	return p.cluster.Name
}

// ListResources discovers all labeled resources across configured namespaces.
func (p *Provider) ListResources(ctx context.Context) ([]types.Resource, error) {
	namespaces, err := p.getNamespaces(ctx)
	if err != nil {
		return nil, err
	}

	var allResources []types.Resource
	var mu sync.Mutex
	var wg sync.WaitGroup
	errChan := make(chan error, len(namespaces)*len(p.resourceTypes))

	for _, ns := range namespaces {
		ns := ns

		for _, rt := range p.resourceTypes {
			rt := rt

			wg.Add(1)
			go func() {
				defer wg.Done()

				var resources []types.Resource
				var err error

				switch rt {
				case ResourceTypePod:
					resources, err = p.listPods(ctx, ns)
				case ResourceTypeDeployment:
					resources, err = p.listDeployments(ctx, ns)
				case ResourceTypeService:
					resources, err = p.listServices(ctx, ns)
				case ResourceTypeConfigMap:
					resources, err = p.listConfigMaps(ctx, ns)
				case ResourceTypeSecret:
					resources, err = p.listSecrets(ctx, ns)
				}

				if err != nil {
					errChan <- err
					return
				}

				mu.Lock()
				allResources = append(allResources, resources...)
				mu.Unlock()
			}()
		}
	}

	// List namespaces separately (cluster-scoped)
	if p.shouldListResourceType(ResourceTypeNamespace) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resources, err := p.listNamespaces(ctx)
			if err != nil {
				errChan <- err
				return
			}
			mu.Lock()
			allResources = append(allResources, resources...)
			mu.Unlock()
		}()
	}

	wg.Wait()
	close(errChan)

	// Collect errors
	// errChan is closed after every writer finished, so len is the exact count.
	errs := make([]error, 0, len(errChan))
	for err := range errChan {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return allResources, errs[0]
	}

	return allResources, nil
}

// ApplyTags applies labels to a Kubernetes resource.
func (p *Provider) ApplyTags(ctx context.Context, resourceID string, tags map[string]string) error {
	// Parse resource ID: type/namespace/name or type/name for cluster-scoped
	resourceType, namespace, name, err := parseResourceID(resourceID)
	if err != nil {
		return provider.NewProviderError(providerName, "apply_labels", resourceID, err)
	}

	switch resourceType {
	case ResourceTypePod:
		return p.patchPodLabels(ctx, namespace, name, tags)
	case ResourceTypeDeployment:
		return p.patchDeploymentLabels(ctx, namespace, name, tags)
	case ResourceTypeService:
		return p.patchServiceLabels(ctx, namespace, name, tags)
	case ResourceTypeNamespace:
		return p.patchNamespaceLabels(ctx, name, tags)
	case ResourceTypeConfigMap:
		return p.patchConfigMapLabels(ctx, namespace, name, tags)
	case ResourceTypeSecret:
		return p.patchSecretLabels(ctx, namespace, name, tags)
	default:
		return provider.NewProviderError(providerName, "apply_labels", resourceID,
			&UnsupportedResourceError{ResourceType: resourceType})
	}
}

// getNamespaces returns the list of namespaces to scan.
func (p *Provider) getNamespaces(ctx context.Context) ([]string, error) {
	if len(p.namespaces) > 0 {
		return p.namespaces, nil
	}

	// List all namespaces
	nsList, err := p.clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, provider.NewProviderError(providerName, "list_namespaces", "", err)
	}

	namespaces := make([]string, 0, len(nsList.Items))
	for _, ns := range nsList.Items {
		namespaces = append(namespaces, ns.Name)
	}

	return namespaces, nil
}

// shouldListResourceType checks if a resource type should be listed.
func (p *Provider) shouldListResourceType(rt string) bool {
	for _, t := range p.resourceTypes {
		if t == rt {
			return true
		}
	}
	return false
}

// UnsupportedResourceError is returned when trying to operate on an unsupported resource type.
type UnsupportedResourceError struct {
	ResourceType string
}

func (e *UnsupportedResourceError) Error() string {
	return "unsupported resource type: " + e.ResourceType
}
