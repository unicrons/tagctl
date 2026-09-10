package k8s

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/unicrons/tagctl/internal/config"
)

func TestNewWithClientset(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	cluster := config.KubernetesCluster{
		Name:       "test-cluster",
		Namespaces: []string{"default"},
	}

	p := NewWithClientset(clientset, cluster)

	if p.Name() != "kubernetes" {
		t.Errorf("Name() = %q, want %q", p.Name(), "kubernetes")
	}

	if p.ClusterName() != "test-cluster" {
		t.Errorf("ClusterName() = %q, want %q", p.ClusterName(), "test-cluster")
	}
}

func TestListPods(t *testing.T) {
	ctx := context.Background()

	// Create fake clientset with some pods
	clientset := fake.NewSimpleClientset(
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pod-1",
				Namespace: "default",
				Labels: map[string]string{
					"app":         "test",
					"environment": "dev",
				},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pod-2",
				Namespace: "default",
				Labels: map[string]string{
					"app": "test2",
				},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
			},
		},
		// This pod should be skipped (Succeeded phase)
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "completed-pod",
				Namespace: "default",
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodSucceeded,
			},
		},
	)

	cluster := config.KubernetesCluster{
		Name:       "test-cluster",
		Namespaces: []string{"default"},
	}
	p := NewWithClientset(clientset, cluster)

	pods, err := p.listPods(ctx, "default")
	if err != nil {
		t.Fatalf("listPods() error = %v", err)
	}

	// Should have 2 pods (completed pod is skipped)
	if len(pods) != 2 {
		t.Errorf("listPods() returned %d pods, want 2", len(pods))
	}

	// Check first pod
	found := false
	for _, pod := range pods {
		if pod.Name == "test-pod-1" {
			found = true
			if pod.Type != ResourceTypePod {
				t.Errorf("pod.Type = %q, want %q", pod.Type, ResourceTypePod)
			}
			if pod.Tags["app"] != "test" {
				t.Errorf("pod.Tags[app] = %q, want %q", pod.Tags["app"], "test")
			}
			if pod.Tags["environment"] != "dev" {
				t.Errorf("pod.Tags[environment] = %q, want %q", pod.Tags["environment"], "dev")
			}
		}
	}
	if !found {
		t.Error("test-pod-1 not found in results")
	}
}

func TestListDeployments(t *testing.T) {
	ctx := context.Background()

	clientset := fake.NewSimpleClientset(
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-deployment",
				Namespace: "default",
				Labels: map[string]string{
					"app":         "myapp",
					"environment": "prod",
				},
			},
		},
	)

	cluster := config.KubernetesCluster{
		Name:       "test-cluster",
		Namespaces: []string{"default"},
	}
	p := NewWithClientset(clientset, cluster)

	deployments, err := p.listDeployments(ctx, "default")
	if err != nil {
		t.Fatalf("listDeployments() error = %v", err)
	}

	if len(deployments) != 1 {
		t.Errorf("listDeployments() returned %d deployments, want 1", len(deployments))
	}

	deploy := deployments[0]
	if deploy.Name != "test-deployment" {
		t.Errorf("deploy.Name = %q, want %q", deploy.Name, "test-deployment")
	}
	if deploy.Type != ResourceTypeDeployment {
		t.Errorf("deploy.Type = %q, want %q", deploy.Type, ResourceTypeDeployment)
	}
}

func TestListServices(t *testing.T) {
	ctx := context.Background()

	clientset := fake.NewSimpleClientset(
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-service",
				Namespace: "default",
				Labels: map[string]string{
					"app": "myapp",
				},
			},
		},
	)

	cluster := config.KubernetesCluster{
		Name:       "test-cluster",
		Namespaces: []string{"default"},
	}
	p := NewWithClientset(clientset, cluster)

	services, err := p.listServices(ctx, "default")
	if err != nil {
		t.Fatalf("listServices() error = %v", err)
	}

	if len(services) != 1 {
		t.Errorf("listServices() returned %d services, want 1", len(services))
	}

	svc := services[0]
	if svc.Name != "test-service" {
		t.Errorf("svc.Name = %q, want %q", svc.Name, "test-service")
	}
	if svc.Type != ResourceTypeService {
		t.Errorf("svc.Type = %q, want %q", svc.Type, ResourceTypeService)
	}
}

func TestListNamespaces(t *testing.T) {
	ctx := context.Background()

	clientset := fake.NewSimpleClientset(
		&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "default",
				Labels: map[string]string{
					"kubernetes.io/metadata.name": "default",
				},
			},
		},
		&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "kube-system",
				Labels: map[string]string{
					"kubernetes.io/metadata.name": "kube-system",
				},
			},
		},
	)

	// Without namespace filter, should get all namespaces
	cluster := config.KubernetesCluster{
		Name:       "test-cluster",
		Namespaces: []string{},
	}
	p := NewWithClientset(clientset, cluster)

	namespaces, err := p.listNamespaces(ctx)
	if err != nil {
		t.Fatalf("listNamespaces() error = %v", err)
	}

	if len(namespaces) != 2 {
		t.Errorf("listNamespaces() returned %d namespaces, want 2", len(namespaces))
	}
}

func TestBuildResourceID(t *testing.T) {
	tests := []struct {
		resourceType string
		namespace    string
		name         string
		expected     string
	}{
		{ResourceTypePod, "default", "my-pod", "k8s_pod/default/my-pod"},
		{ResourceTypeDeployment, "prod", "api", "k8s_deployment/prod/api"},
		{ResourceTypeNamespace, "", "default", "k8s_namespace/default"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := buildResourceID(tt.resourceType, tt.namespace, tt.name)
			if result != tt.expected {
				t.Errorf("buildResourceID() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestParseResourceID(t *testing.T) {
	tests := []struct {
		resourceID    string
		wantType      string
		wantNamespace string
		wantName      string
		wantErr       bool
	}{
		{"k8s_pod/default/my-pod", ResourceTypePod, "default", "my-pod", false},
		{"k8s_namespace/default", ResourceTypeNamespace, "", "default", false},
		{"invalid", "", "", "", true},
		{"too/many/parts/here", "", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.resourceID, func(t *testing.T) {
			resType, ns, name, err := parseResourceID(tt.resourceID)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseResourceID() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err == nil {
				if resType != tt.wantType {
					t.Errorf("resourceType = %q, want %q", resType, tt.wantType)
				}
				if ns != tt.wantNamespace {
					t.Errorf("namespace = %q, want %q", ns, tt.wantNamespace)
				}
				if name != tt.wantName {
					t.Errorf("name = %q, want %q", name, tt.wantName)
				}
			}
		})
	}
}

func TestCopyLabels(t *testing.T) {
	// Test with nil labels
	result := copyLabels(nil)
	if result == nil {
		t.Error("copyLabels(nil) returned nil, want empty map")
	}
	if len(result) != 0 {
		t.Errorf("copyLabels(nil) returned %d labels, want 0", len(result))
	}

	// Test with actual labels
	labels := map[string]string{
		"app":         "test",
		"environment": "dev",
	}
	result = copyLabels(labels)
	if len(result) != 2 {
		t.Errorf("copyLabels() returned %d labels, want 2", len(result))
	}

	// Verify it's a copy, not the same map
	result["new-key"] = "new-value"
	if _, ok := labels["new-key"]; ok {
		t.Error("copyLabels() returned same map, not a copy")
	}
}

func TestContains(t *testing.T) {
	slice := []string{"a", "b", "c"}

	if !contains(slice, "a") {
		t.Error("contains() = false for 'a', want true")
	}
	if !contains(slice, "c") {
		t.Error("contains() = false for 'c', want true")
	}
	if contains(slice, "d") {
		t.Error("contains() = true for 'd', want false")
	}
	if contains(nil, "a") {
		t.Error("contains(nil, 'a') = true, want false")
	}
}
