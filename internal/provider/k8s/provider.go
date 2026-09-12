// Package k8s provides Kubernetes resource discovery and labeling.
package k8s

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/metadata"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/unicrons/tagctl/internal/config"
	"github.com/unicrons/tagctl/internal/provider"
	"github.com/unicrons/tagctl/internal/types"
)

// providerName identifies this provider in resources and errors.
const providerName = "kubernetes"

// Supported resource types.
const (
	ResourceTypePod        = config.KubernetesPod
	ResourceTypeDeployment = config.KubernetesDeployment
	ResourceTypeService    = config.KubernetesService
	ResourceTypeNamespace  = config.KubernetesNamespace
	ResourceTypeConfigMap  = config.KubernetesConfigMap
	ResourceTypeSecret     = config.KubernetesSecret
)

// Provider implements the provider.Provider interface for Kubernetes.
type Provider struct {
	clientset     kubernetes.Interface
	metadata      metadata.Interface
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

	metadataClient, err := metadata.NewForConfig(cfg)
	if err != nil {
		return nil, provider.NewProviderError(providerName, "create_metadata_client", "", err)
	}

	return NewWithClients(clientset, metadataClient, cluster), nil
}

// NewWithClients creates a Kubernetes provider from pre-built clients.
// The metadata client serves secrets, whose data tagctl never reads.
func NewWithClients(clientset kubernetes.Interface, metadataClient metadata.Interface, cluster config.KubernetesCluster) *Provider {
	resourceTypes := cluster.ResourceTypes
	if len(resourceTypes) == 0 {
		resourceTypes = slices.Clone(config.KubernetesDefaultResourceTypes)
	}

	return &Provider{
		clientset:     clientset,
		metadata:      metadataClient,
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

type namespacedLister func(ctx context.Context, namespace string) ([]types.Resource, error)

func (p *Provider) namespacedListers() map[string]namespacedLister {
	return map[string]namespacedLister{
		ResourceTypePod:        p.listPods,
		ResourceTypeDeployment: p.listDeployments,
		ResourceTypeService:    p.listServices,
		ResourceTypeConfigMap:  p.listConfigMaps,
		ResourceTypeSecret:     p.listSecrets,
	}
}

// ListResources discovers all labeled resources across configured namespaces.
// An unsupported resource type fails before any API call.
func (p *Provider) ListResources(ctx context.Context) ([]types.Resource, error) {
	listers := p.namespacedListers()
	for _, rt := range p.resourceTypes {
		if _, ok := listers[rt]; !ok && rt != ResourceTypeNamespace {
			return nil, provider.NewProviderError(providerName, "list_resources", "",
				&UnsupportedResourceError{ResourceType: rt})
		}
	}

	namespaces, err := p.getNamespaces(ctx)
	if err != nil {
		return nil, err
	}

	var allResources []types.Resource
	var mu sync.Mutex
	var wg sync.WaitGroup
	errChan := make(chan error, len(namespaces)*len(p.resourceTypes)+1)

	for _, ns := range namespaces {
		for _, rt := range p.resourceTypes {
			list, ok := listers[rt]
			if !ok {
				continue
			}

			wg.Add(1)
			go func() {
				defer wg.Done()

				resources, err := list(ctx, ns)
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

	patch, err := buildLabelPatch(tags)
	if err != nil {
		return provider.NewProviderError(providerName, "apply_labels", resourceID, err)
	}

	switch resourceType {
	case ResourceTypePod:
		return p.patchPodLabels(ctx, namespace, name, patch)
	case ResourceTypeDeployment:
		return p.patchDeploymentLabels(ctx, namespace, name, patch)
	case ResourceTypeService:
		return p.patchServiceLabels(ctx, namespace, name, patch)
	case ResourceTypeNamespace:
		return p.patchNamespaceLabels(ctx, name, patch)
	case ResourceTypeConfigMap:
		return p.patchConfigMapLabels(ctx, namespace, name, patch)
	case ResourceTypeSecret:
		return p.patchSecretLabels(ctx, namespace, name, patch)
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

	items, err := p.allNamespaces(ctx)
	if err != nil {
		return nil, err
	}

	namespaces := make([]string, 0, len(items))
	for _, ns := range items {
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
