// Package k8s provides Kubernetes resource discovery and labeling.
package k8s

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

// inClusterKubeconfig is the kubeconfig value that selects the pod's service account.
const inClusterKubeconfig = "in-cluster"

// Client-side limits: client-go defaults to 5 requests per second, and a
// request to an unresponsive API server would otherwise never return.
const (
	clientQPS     = 50
	clientBurst   = 100
	clientTimeout = 60 * time.Second
)

// New creates a new Kubernetes provider with the given cluster configuration.
func New(_ context.Context, cluster config.KubernetesCluster) (*Provider, error) {
	cfg, err := restConfig(cluster)
	if err != nil {
		return nil, err
	}
	cfg.QPS = clientQPS
	cfg.Burst = clientBurst
	cfg.Timeout = clientTimeout
	cfg.UserAgent = "tagctl"

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

// restConfig resolves the API server and credentials for a cluster: the
// in-cluster service account, an explicit kubeconfig file, or the kubectl
// loading rules (KUBECONFIG, then ~/.kube/config, then in-cluster).
func restConfig(cluster config.KubernetesCluster) (*rest.Config, error) {
	if cluster.Kubeconfig == inClusterKubeconfig {
		cfg, err := rest.InClusterConfig()
		if err != nil {
			return nil, provider.NewProviderError(providerName, "in_cluster_config", "", err)
		}
		return cfg, nil
	}

	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if cluster.Kubeconfig != "" {
		loadingRules.ExplicitPath = expandHome(cluster.Kubeconfig)
	}

	overrides := &clientcmd.ConfigOverrides{CurrentContext: cluster.Context}
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides).ClientConfig()
	if err != nil {
		return nil, provider.NewProviderError(providerName, "load_kubeconfig", cluster.Kubeconfig, err)
	}
	return cfg, nil
}

// expandHome replaces a leading ~/ with the user's home directory.
func expandHome(path string) string {
	rest, ok := strings.CutPrefix(path, "~/")
	if !ok {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, rest)
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

// AccountID returns the cluster name, the account its resources carry, so a
// plan entry is applied to the cluster it was scanned from.
func (p *Provider) AccountID() string {
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

// ListResources discovers the configured resource types. Without configured
// namespaces each type is listed once across the whole cluster. Errors name
// the cluster and are returned joined, next to whatever was listed.
func (p *Provider) ListResources(ctx context.Context) ([]types.Resource, error) {
	listers := p.namespacedListers()
	for _, rt := range p.resourceTypes {
		if _, ok := listers[rt]; !ok && rt != ResourceTypeNamespace {
			return nil, p.clusterError(provider.NewProviderError(providerName, "list_resources", "",
				&UnsupportedResourceError{ResourceType: rt}))
		}
	}

	namespaces := p.namespaces
	if len(namespaces) == 0 {
		namespaces = []string{metav1.NamespaceAll}
	}

	var calls []func() ([]types.Resource, error)
	for _, rt := range p.resourceTypes {
		if rt == ResourceTypeNamespace {
			calls = append(calls, func() ([]types.Resource, error) { return p.listNamespaces(ctx) })
			continue
		}
		for _, ns := range namespaces {
			calls = append(calls, func() ([]types.Resource, error) { return listers[rt](ctx, ns) })
		}
	}

	results := make([][]types.Resource, len(calls))
	errs := make([]error, len(calls))
	var wg sync.WaitGroup
	for i, call := range calls {
		wg.Go(func() {
			results[i], errs[i] = call()
			if errs[i] != nil {
				errs[i] = p.clusterError(errs[i])
			}
		})
	}
	wg.Wait()

	return slices.Concat(results...), errors.Join(errs...)
}

func (p *Provider) clusterError(err error) error {
	return fmt.Errorf("cluster %s: %w", p.cluster.Name, err)
}

// ApplyTags applies labels to a Kubernetes resource.
func (p *Provider) ApplyTags(ctx context.Context, resourceID string, tags map[string]string) error {
	// Parse resource ID: type/namespace/name or type/name for cluster-scoped
	resourceType, namespace, name, err := parseResourceID(resourceID)
	if err != nil {
		return provider.NewProviderError(providerName, "apply_labels", resourceID, err)
	}

	if err = validateLabels(tags); err != nil {
		return provider.NewProviderError(providerName, "apply_labels", resourceID, err)
	}

	patch, err := buildLabelPatch(tags)
	if err != nil {
		return provider.NewProviderError(providerName, "apply_labels", resourceID, err)
	}
	return p.patchLabels(ctx, "apply_labels", resourceID, resourceType, namespace, name, patch)
}

// RemoveTags deletes labels from a Kubernetes resource.
func (p *Provider) RemoveTags(ctx context.Context, resource types.Resource, keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	resourceType, namespace, name, err := parseResourceID(resource.ID)
	if err != nil {
		return provider.NewProviderError(providerName, "remove_labels", resource.ID, err)
	}

	// A null value in a JSON merge patch deletes the key.
	labels := make(map[string]any, len(keys))
	for _, key := range keys {
		labels[key] = nil
	}
	patch, err := buildLabelPatch(labels)
	if err != nil {
		return provider.NewProviderError(providerName, "remove_labels", resource.ID, err)
	}
	return p.patchLabels(ctx, "remove_labels", resource.ID, resourceType, namespace, name, patch)
}

// patchLabels sends a label merge patch to the resource a parsed ID names.
func (p *Provider) patchLabels(ctx context.Context, operation, resourceID, resourceType, namespace, name string, patch []byte) error {
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
		return provider.NewProviderError(providerName, operation, resourceID,
			&UnsupportedResourceError{ResourceType: resourceType})
	}
}

// UnsupportedResourceError is returned when trying to operate on an unsupported resource type.
type UnsupportedResourceError struct {
	ResourceType string
}

func (e *UnsupportedResourceError) Error() string {
	return "unsupported resource type: " + e.ResourceType
}
