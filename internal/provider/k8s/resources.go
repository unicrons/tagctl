package k8s

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	k8stypes "k8s.io/apimachinery/pkg/types"

	"github.com/unicrons/tagctl/internal/provider"
	"github.com/unicrons/tagctl/internal/types"
)

// listPageSize is the Limit sent on every List call.
const listPageSize = 500

var secretsResource = corev1.SchemeGroupVersion.WithResource("secrets")

// withoutServiceAccountTokens filters out the tokens Kubernetes mints for service accounts.
var withoutServiceAccountTokens = fields.OneTermNotEqualSelector("type", string(corev1.SecretTypeServiceAccountToken)).String()

var errRepeatedContinue = errors.New("list returned the same continue token twice")

type pagedList interface {
	GetContinue() string
}

// eachPage lists with Limit/Continue and visits every page until the server
// returns no continue token.
func eachPage[L pagedList](ctx context.Context, opts metav1.ListOptions,
	list func(context.Context, metav1.ListOptions) (L, error), visit func(L),
) error {
	opts.Limit = listPageSize
	for {
		page, err := list(ctx, opts)
		if err != nil {
			return err
		}
		visit(page)

		next := page.GetContinue()
		if next == "" {
			return nil
		}
		if next == opts.Continue {
			return errRepeatedContinue
		}
		opts.Continue = next
	}
}

// newResource builds a resource from object metadata; namespace is empty for
// cluster-scoped objects.
func (p *Provider) newResource(resourceType, namespace string, meta metav1.ObjectMeta) types.Resource {
	resource := types.Resource{
		ID:       buildResourceID(resourceType, namespace, meta.Name),
		Name:     meta.Name,
		Type:     resourceType,
		Region:   namespace,
		Account:  p.cluster.Name,
		Provider: providerName,
		Tags:     copyLabels(meta.Labels),
	}

	if !meta.CreationTimestamp.IsZero() {
		t := meta.CreationTimestamp.Time
		resource.CreatedAt = &t
	}

	return resource
}

// listPods lists all pods in a namespace.
func (p *Provider) listPods(ctx context.Context, namespace string) ([]types.Resource, error) {
	var resources []types.Resource
	err := eachPage(ctx, metav1.ListOptions{}, p.clientset.CoreV1().Pods(namespace).List, func(page *corev1.PodList) {
		for _, pod := range page.Items {
			// Skip pods that are completed or failed
			if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
				continue
			}
			resources = append(resources, p.newResource(ResourceTypePod, namespace, pod.ObjectMeta))
		}
	})
	if err != nil {
		return nil, provider.NewProviderError(providerName, "list_pods", namespace, err)
	}
	return resources, nil
}

// listDeployments lists all deployments in a namespace.
func (p *Provider) listDeployments(ctx context.Context, namespace string) ([]types.Resource, error) {
	var resources []types.Resource
	err := eachPage(ctx, metav1.ListOptions{}, p.clientset.AppsV1().Deployments(namespace).List, func(page *appsv1.DeploymentList) {
		for _, deploy := range page.Items {
			resources = append(resources, p.newResource(ResourceTypeDeployment, namespace, deploy.ObjectMeta))
		}
	})
	if err != nil {
		return nil, provider.NewProviderError(providerName, "list_deployments", namespace, err)
	}
	return resources, nil
}

// listServices lists all services in a namespace.
func (p *Provider) listServices(ctx context.Context, namespace string) ([]types.Resource, error) {
	var resources []types.Resource
	err := eachPage(ctx, metav1.ListOptions{}, p.clientset.CoreV1().Services(namespace).List, func(page *corev1.ServiceList) {
		for _, svc := range page.Items {
			resources = append(resources, p.newResource(ResourceTypeService, namespace, svc.ObjectMeta))
		}
	})
	if err != nil {
		return nil, provider.NewProviderError(providerName, "list_services", namespace, err)
	}
	return resources, nil
}

// allNamespaces lists every namespace in the cluster.
func (p *Provider) allNamespaces(ctx context.Context) ([]corev1.Namespace, error) {
	var namespaces []corev1.Namespace
	err := eachPage(ctx, metav1.ListOptions{}, p.clientset.CoreV1().Namespaces().List, func(page *corev1.NamespaceList) {
		namespaces = append(namespaces, page.Items...)
	})
	if err != nil {
		return nil, provider.NewProviderError(providerName, "list_namespaces", "", err)
	}
	return namespaces, nil
}

// listNamespaces lists all namespaces.
func (p *Provider) listNamespaces(ctx context.Context) ([]types.Resource, error) {
	namespaces, err := p.allNamespaces(ctx)
	if err != nil {
		return nil, err
	}

	resources := make([]types.Resource, 0, len(namespaces))
	for _, ns := range namespaces {
		// Filter by configured namespaces if set
		if len(p.namespaces) > 0 && !contains(p.namespaces, ns.Name) {
			continue
		}
		resources = append(resources, p.newResource(ResourceTypeNamespace, "", ns.ObjectMeta))
	}

	return resources, nil
}

// listConfigMaps lists all configmaps in a namespace.
func (p *Provider) listConfigMaps(ctx context.Context, namespace string) ([]types.Resource, error) {
	var resources []types.Resource
	err := eachPage(ctx, metav1.ListOptions{}, p.clientset.CoreV1().ConfigMaps(namespace).List, func(page *corev1.ConfigMapList) {
		for _, cm := range page.Items {
			resources = append(resources, p.newResource(ResourceTypeConfigMap, namespace, cm.ObjectMeta))
		}
	})
	if err != nil {
		return nil, provider.NewProviderError(providerName, "list_configmaps", namespace, err)
	}
	return resources, nil
}

// listSecrets lists secret metadata in a namespace; secret data never reaches tagctl.
func (p *Provider) listSecrets(ctx context.Context, namespace string) ([]types.Resource, error) {
	var resources []types.Resource
	opts := metav1.ListOptions{FieldSelector: withoutServiceAccountTokens}
	err := eachPage(ctx, opts, p.metadata.Resource(secretsResource).Namespace(namespace).List, func(page *metav1.PartialObjectMetadataList) {
		for _, secret := range page.Items {
			resources = append(resources, p.newResource(ResourceTypeSecret, namespace, secret.ObjectMeta))
		}
	})
	if err != nil {
		return nil, provider.NewProviderError(providerName, "list_secrets", namespace, err)
	}
	return resources, nil
}

// patchPodLabels patches labels on a pod.
func (p *Provider) patchPodLabels(ctx context.Context, namespace, name string, patch []byte) error {
	_, err := p.clientset.CoreV1().Pods(namespace).Patch(ctx, name, k8stypes.MergePatchType, patch, metav1.PatchOptions{})
	if err != nil {
		return provider.NewProviderError(providerName, "patch_pod_labels", buildResourceID(ResourceTypePod, namespace, name), err)
	}
	return nil
}

// patchDeploymentLabels patches labels on a deployment.
func (p *Provider) patchDeploymentLabels(ctx context.Context, namespace, name string, patch []byte) error {
	_, err := p.clientset.AppsV1().Deployments(namespace).Patch(ctx, name, k8stypes.MergePatchType, patch, metav1.PatchOptions{})
	if err != nil {
		return provider.NewProviderError(providerName, "patch_deployment_labels", buildResourceID(ResourceTypeDeployment, namespace, name), err)
	}
	return nil
}

// patchServiceLabels patches labels on a service.
func (p *Provider) patchServiceLabels(ctx context.Context, namespace, name string, patch []byte) error {
	_, err := p.clientset.CoreV1().Services(namespace).Patch(ctx, name, k8stypes.MergePatchType, patch, metav1.PatchOptions{})
	if err != nil {
		return provider.NewProviderError(providerName, "patch_service_labels", buildResourceID(ResourceTypeService, namespace, name), err)
	}
	return nil
}

// patchNamespaceLabels patches labels on a namespace.
func (p *Provider) patchNamespaceLabels(ctx context.Context, name string, patch []byte) error {
	_, err := p.clientset.CoreV1().Namespaces().Patch(ctx, name, k8stypes.MergePatchType, patch, metav1.PatchOptions{})
	if err != nil {
		return provider.NewProviderError(providerName, "patch_namespace_labels", buildResourceID(ResourceTypeNamespace, "", name), err)
	}
	return nil
}

// patchConfigMapLabels patches labels on a configmap.
func (p *Provider) patchConfigMapLabels(ctx context.Context, namespace, name string, patch []byte) error {
	_, err := p.clientset.CoreV1().ConfigMaps(namespace).Patch(ctx, name, k8stypes.MergePatchType, patch, metav1.PatchOptions{})
	if err != nil {
		return provider.NewProviderError(providerName, "patch_configmap_labels", buildResourceID(ResourceTypeConfigMap, namespace, name), err)
	}
	return nil
}

// patchSecretLabels patches labels on a secret through the metadata client,
// so the response carries metadata and not the secret data.
func (p *Provider) patchSecretLabels(ctx context.Context, namespace, name string, patch []byte) error {
	_, err := p.metadata.Resource(secretsResource).Namespace(namespace).Patch(ctx, name, k8stypes.MergePatchType, patch, metav1.PatchOptions{})
	if err != nil {
		return provider.NewProviderError(providerName, "patch_secret_labels", buildResourceID(ResourceTypeSecret, namespace, name), err)
	}
	return nil
}

// buildResourceID builds a resource ID from type, namespace, and name.
func buildResourceID(resourceType, namespace, name string) string {
	if namespace == "" {
		return resourceType + "/" + name
	}
	return resourceType + "/" + namespace + "/" + name
}

// parseResourceID parses a resource ID into type, namespace, and name.
func parseResourceID(resourceID string) (resourceType, namespace, name string, err error) {
	parts := strings.Split(resourceID, "/")
	switch len(parts) {
	case 2:
		// Cluster-scoped resource: type/name
		return parts[0], "", parts[1], nil
	case 3:
		// Namespace-scoped resource: type/namespace/name
		return parts[0], parts[1], parts[2], nil
	default:
		return "", "", "", fmt.Errorf("invalid resource ID format: %s", resourceID)
	}
}

// buildLabelPatch builds a JSON merge patch for labels.
func buildLabelPatch(labels map[string]string) ([]byte, error) {
	patch := map[string]any{
		"metadata": map[string]any{
			"labels": labels,
		},
	}
	data, err := json.Marshal(patch)
	if err != nil {
		return nil, fmt.Errorf("marshal label patch: %w", err)
	}
	return data, nil
}

// copyLabels makes a copy of a labels map.
func copyLabels(labels map[string]string) map[string]string {
	if labels == nil {
		return make(map[string]string)
	}
	result := make(map[string]string, len(labels))
	for k, v := range labels {
		result[k] = v
	}
	return result
}

// contains checks if a slice contains a string.
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
