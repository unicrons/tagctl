package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"

	"github.com/unicrons/tagctl/internal/provider"
	"github.com/unicrons/tagctl/internal/types"
)

// listPods lists all pods in a namespace.
func (p *Provider) listPods(ctx context.Context, namespace string) ([]types.Resource, error) {
	pods, err := p.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, provider.NewProviderError(providerName, "list_pods", namespace, err)
	}

	resources := make([]types.Resource, 0, len(pods.Items))
	for _, pod := range pods.Items {
		// Skip pods that are completed or failed
		if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}

		resource := types.Resource{
			ID:       buildResourceID(ResourceTypePod, namespace, pod.Name),
			Name:     pod.Name,
			Type:     ResourceTypePod,
			Region:   namespace,
			Account:  p.cluster.Name,
			Provider: providerName,
			Tags:     copyLabels(pod.Labels),
		}

		if !pod.CreationTimestamp.IsZero() {
			t := pod.CreationTimestamp.Time
			resource.CreatedAt = &t
		}

		resources = append(resources, resource)
	}

	return resources, nil
}

// listDeployments lists all deployments in a namespace.
func (p *Provider) listDeployments(ctx context.Context, namespace string) ([]types.Resource, error) {
	deployments, err := p.clientset.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, provider.NewProviderError(providerName, "list_deployments", namespace, err)
	}

	resources := make([]types.Resource, 0, len(deployments.Items))
	for _, deploy := range deployments.Items {
		resource := types.Resource{
			ID:       buildResourceID(ResourceTypeDeployment, namespace, deploy.Name),
			Name:     deploy.Name,
			Type:     ResourceTypeDeployment,
			Region:   namespace,
			Account:  p.cluster.Name,
			Provider: providerName,
			Tags:     copyLabels(deploy.Labels),
		}

		if !deploy.CreationTimestamp.IsZero() {
			t := deploy.CreationTimestamp.Time
			resource.CreatedAt = &t
		}

		resources = append(resources, resource)
	}

	return resources, nil
}

// listServices lists all services in a namespace.
func (p *Provider) listServices(ctx context.Context, namespace string) ([]types.Resource, error) {
	services, err := p.clientset.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, provider.NewProviderError(providerName, "list_services", namespace, err)
	}

	resources := make([]types.Resource, 0, len(services.Items))
	for _, svc := range services.Items {
		resource := types.Resource{
			ID:       buildResourceID(ResourceTypeService, namespace, svc.Name),
			Name:     svc.Name,
			Type:     ResourceTypeService,
			Region:   namespace,
			Account:  p.cluster.Name,
			Provider: providerName,
			Tags:     copyLabels(svc.Labels),
		}

		if !svc.CreationTimestamp.IsZero() {
			t := svc.CreationTimestamp.Time
			resource.CreatedAt = &t
		}

		resources = append(resources, resource)
	}

	return resources, nil
}

// listNamespaces lists all namespaces.
func (p *Provider) listNamespaces(ctx context.Context) ([]types.Resource, error) {
	namespaces, err := p.clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, provider.NewProviderError(providerName, "list_namespaces", "", err)
	}

	resources := make([]types.Resource, 0, len(namespaces.Items))
	for _, ns := range namespaces.Items {
		// Filter by configured namespaces if set
		if len(p.namespaces) > 0 && !contains(p.namespaces, ns.Name) {
			continue
		}

		resource := types.Resource{
			ID:       buildResourceID(ResourceTypeNamespace, "", ns.Name),
			Name:     ns.Name,
			Type:     ResourceTypeNamespace,
			Region:   "",
			Account:  p.cluster.Name,
			Provider: providerName,
			Tags:     copyLabels(ns.Labels),
		}

		if !ns.CreationTimestamp.IsZero() {
			t := ns.CreationTimestamp.Time
			resource.CreatedAt = &t
		}

		resources = append(resources, resource)
	}

	return resources, nil
}

// listConfigMaps lists all configmaps in a namespace.
func (p *Provider) listConfigMaps(ctx context.Context, namespace string) ([]types.Resource, error) {
	configMaps, err := p.clientset.CoreV1().ConfigMaps(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, provider.NewProviderError(providerName, "list_configmaps", namespace, err)
	}

	resources := make([]types.Resource, 0, len(configMaps.Items))
	for _, cm := range configMaps.Items {
		resource := types.Resource{
			ID:       buildResourceID(ResourceTypeConfigMap, namespace, cm.Name),
			Name:     cm.Name,
			Type:     ResourceTypeConfigMap,
			Region:   namespace,
			Account:  p.cluster.Name,
			Provider: providerName,
			Tags:     copyLabels(cm.Labels),
		}

		if !cm.CreationTimestamp.IsZero() {
			t := cm.CreationTimestamp.Time
			resource.CreatedAt = &t
		}

		resources = append(resources, resource)
	}

	return resources, nil
}

// listSecrets lists all secrets in a namespace.
func (p *Provider) listSecrets(ctx context.Context, namespace string) ([]types.Resource, error) {
	secrets, err := p.clientset.CoreV1().Secrets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, provider.NewProviderError(providerName, "list_secrets", namespace, err)
	}

	resources := make([]types.Resource, 0, len(secrets.Items))
	for _, secret := range secrets.Items {
		// Skip service account tokens and other system secrets
		if secret.Type == corev1.SecretTypeServiceAccountToken {
			continue
		}

		resource := types.Resource{
			ID:       buildResourceID(ResourceTypeSecret, namespace, secret.Name),
			Name:     secret.Name,
			Type:     ResourceTypeSecret,
			Region:   namespace,
			Account:  p.cluster.Name,
			Provider: providerName,
			Tags:     copyLabels(secret.Labels),
		}

		if !secret.CreationTimestamp.IsZero() {
			t := secret.CreationTimestamp.Time
			resource.CreatedAt = &t
		}

		resources = append(resources, resource)
	}

	return resources, nil
}

// patchPodLabels patches labels on a pod.
func (p *Provider) patchPodLabels(ctx context.Context, namespace, name string, labels map[string]string) error {
	patch := buildLabelPatch(labels)
	_, err := p.clientset.CoreV1().Pods(namespace).Patch(ctx, name, k8stypes.MergePatchType, patch, metav1.PatchOptions{})
	if err != nil {
		return provider.NewProviderError(providerName, "patch_pod_labels", buildResourceID(ResourceTypePod, namespace, name), err)
	}
	return nil
}

// patchDeploymentLabels patches labels on a deployment.
func (p *Provider) patchDeploymentLabels(ctx context.Context, namespace, name string, labels map[string]string) error {
	patch := buildLabelPatch(labels)
	_, err := p.clientset.AppsV1().Deployments(namespace).Patch(ctx, name, k8stypes.MergePatchType, patch, metav1.PatchOptions{})
	if err != nil {
		return provider.NewProviderError(providerName, "patch_deployment_labels", buildResourceID(ResourceTypeDeployment, namespace, name), err)
	}
	return nil
}

// patchServiceLabels patches labels on a service.
func (p *Provider) patchServiceLabels(ctx context.Context, namespace, name string, labels map[string]string) error {
	patch := buildLabelPatch(labels)
	_, err := p.clientset.CoreV1().Services(namespace).Patch(ctx, name, k8stypes.MergePatchType, patch, metav1.PatchOptions{})
	if err != nil {
		return provider.NewProviderError(providerName, "patch_service_labels", buildResourceID(ResourceTypeService, namespace, name), err)
	}
	return nil
}

// patchNamespaceLabels patches labels on a namespace.
func (p *Provider) patchNamespaceLabels(ctx context.Context, name string, labels map[string]string) error {
	patch := buildLabelPatch(labels)
	_, err := p.clientset.CoreV1().Namespaces().Patch(ctx, name, k8stypes.MergePatchType, patch, metav1.PatchOptions{})
	if err != nil {
		return provider.NewProviderError(providerName, "patch_namespace_labels", buildResourceID(ResourceTypeNamespace, "", name), err)
	}
	return nil
}

// patchConfigMapLabels patches labels on a configmap.
func (p *Provider) patchConfigMapLabels(ctx context.Context, namespace, name string, labels map[string]string) error {
	patch := buildLabelPatch(labels)
	_, err := p.clientset.CoreV1().ConfigMaps(namespace).Patch(ctx, name, k8stypes.MergePatchType, patch, metav1.PatchOptions{})
	if err != nil {
		return provider.NewProviderError(providerName, "patch_configmap_labels", buildResourceID(ResourceTypeConfigMap, namespace, name), err)
	}
	return nil
}

// patchSecretLabels patches labels on a secret.
func (p *Provider) patchSecretLabels(ctx context.Context, namespace, name string, labels map[string]string) error {
	patch := buildLabelPatch(labels)
	_, err := p.clientset.CoreV1().Secrets(namespace).Patch(ctx, name, k8stypes.MergePatchType, patch, metav1.PatchOptions{})
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
func buildLabelPatch(labels map[string]string) []byte {
	patch := map[string]interface{}{
		"metadata": map[string]interface{}{
			"labels": labels,
		},
	}
	data, _ := json.Marshal(patch)
	return data
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
