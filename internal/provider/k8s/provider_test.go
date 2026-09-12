package k8s

import (
	"context"
	"errors"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	metadatafake "k8s.io/client-go/metadata/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/unicrons/tagctl/internal/config"
)

func fakeMetadata(t *testing.T, objects ...runtime.Object) *metadatafake.FakeMetadataClient {
	t.Helper()
	scheme := metadatafake.NewTestScheme()
	if err := metav1.AddMetaToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return metadatafake.NewSimpleMetadataClient(scheme, objects...)
}

func secretMetadata(namespace, name string, labels map[string]string) *metav1.PartialObjectMetadata {
	return &metav1.PartialObjectMetadata{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: labels},
	}
}

func secretActions(actions []k8stesting.Action) []k8stesting.Action {
	var matched []k8stesting.Action
	for _, a := range actions {
		if a.GetResource().Resource == "secrets" {
			matched = append(matched, a)
		}
	}
	return matched
}

func TestNewWithClients(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	cluster := config.KubernetesCluster{
		Name:       "test-cluster",
		Namespaces: []string{"default"},
	}

	p := NewWithClients(clientset, fakeMetadata(t), cluster)

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
	p := NewWithClients(clientset, fakeMetadata(t), cluster)

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
	p := NewWithClients(clientset, fakeMetadata(t), cluster)

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
	p := NewWithClients(clientset, fakeMetadata(t), cluster)

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
	p := NewWithClients(clientset, fakeMetadata(t), cluster)

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

func TestListResources_RejectsUnknownTypeBeforeListing(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	metadataClient := fakeMetadata(t)
	p := NewWithClients(clientset, metadataClient, config.KubernetesCluster{
		Name:          "test-cluster",
		ResourceTypes: []string{ResourceTypePod, "k8s_pods"},
	})

	resources, err := p.ListResources(context.Background())

	var unsupported *UnsupportedResourceError
	if !errors.As(err, &unsupported) || unsupported.ResourceType != "k8s_pods" {
		t.Fatalf("ListResources() error = %v, want UnsupportedResourceError for k8s_pods", err)
	}
	if len(resources) != 0 {
		t.Errorf("ListResources() returned %d resources, want 0", len(resources))
	}
	if n := len(clientset.Actions()) + len(metadataClient.Actions()); n != 0 {
		t.Errorf("ListResources() made %d API calls before rejecting the type", n)
	}
}

func TestListResources_NamespaceListErrorWithNoNamespaces(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	calls := 0
	clientset.PrependReactor("list", "namespaces", func(k8stesting.Action) (bool, runtime.Object, error) {
		calls++
		if calls == 1 {
			return true, &corev1.NamespaceList{}, nil
		}
		return true, nil, errors.New("forbidden")
	})
	p := NewWithClients(clientset, fakeMetadata(t), config.KubernetesCluster{
		Name:          "test-cluster",
		ResourceTypes: []string{ResourceTypeNamespace},
	})

	done := make(chan error, 1)
	go func() {
		_, err := p.ListResources(context.Background())
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("ListResources() error = nil, want the namespace list error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ListResources() blocked sending the namespace list error")
	}
}

func TestListResources_SecretsOnlyWhenListed(t *testing.T) {
	cases := []struct {
		name        string
		types       []string
		wantSecrets int
	}{
		{"defaults leave secrets out", nil, 0},
		{"listed explicitly", []string{ResourceTypeSecret}, 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clientset := fake.NewSimpleClientset(&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "db-credentials", Namespace: "default"},
			})
			metadataClient := fakeMetadata(t, secretMetadata("default", "db-credentials", nil))
			p := NewWithClients(clientset, metadataClient, config.KubernetesCluster{
				Name:          "test-cluster",
				Namespaces:    []string{"default"},
				ResourceTypes: tc.types,
			})

			resources, err := p.ListResources(context.Background())
			if err != nil {
				t.Fatalf("ListResources() error = %v", err)
			}

			secrets := 0
			for _, r := range resources {
				if r.Type == ResourceTypeSecret {
					secrets++
				}
			}
			if secrets != tc.wantSecrets {
				t.Errorf("ListResources() returned %d secrets, want %d", secrets, tc.wantSecrets)
			}
			if tc.wantSecrets == 0 {
				if n := len(secretActions(clientset.Actions())) + len(secretActions(metadataClient.Actions())); n != 0 {
					t.Errorf("ListResources() made %d secrets calls, want 0", n)
				}
			}
		})
	}
}

func TestListSecrets_ReadsMetadataOnly(t *testing.T) {
	clientset := fake.NewSimpleClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "db-credentials", Namespace: "default"},
		Data:       map[string][]byte{"token": []byte("redacted")},
	})
	metadataClient := fakeMetadata(t, secretMetadata("default", "db-credentials", map[string]string{"app": "db"}))
	p := NewWithClients(clientset, metadataClient, config.KubernetesCluster{Name: "test-cluster"})

	secrets, err := p.listSecrets(context.Background(), "default")
	if err != nil {
		t.Fatalf("listSecrets() error = %v", err)
	}

	if len(secrets) != 1 || secrets[0].ID != "k8s_secret/default/db-credentials" || secrets[0].Tags["app"] != "db" {
		t.Fatalf("listSecrets() = %+v, want db-credentials labelled app=db", secrets)
	}
	if typed := secretActions(clientset.Actions()); len(typed) != 0 {
		t.Errorf("listSecrets() used the typed client, which returns secret data: %v", typed)
	}

	lists := secretActions(metadataClient.Actions())
	if len(lists) != 1 {
		t.Fatalf("metadata client got %d secrets calls, want 1 list", len(lists))
	}
	list, ok := lists[0].(k8stesting.ListAction)
	if !ok {
		t.Fatalf("metadata client call = %T, want a list", lists[0])
	}
	if got := list.GetListRestrictions().Fields.String(); got != "type!=kubernetes.io/service-account-token" {
		t.Errorf("secrets field selector = %q, want service account tokens excluded", got)
	}
}

func TestApplyTags_PatchesSecretThroughMetadataClient(t *testing.T) {
	ctx := context.Background()
	clientset := fake.NewSimpleClientset()
	metadataClient := fakeMetadata(t, secretMetadata("default", "db-credentials", nil))
	p := NewWithClients(clientset, metadataClient, config.KubernetesCluster{Name: "test-cluster"})

	if err := p.ApplyTags(ctx, "k8s_secret/default/db-credentials", map[string]string{"owner": "platform"}); err != nil {
		t.Fatalf("ApplyTags() error = %v", err)
	}

	got, err := metadataClient.Resource(secretsResource).Namespace("default").Get(ctx, "db-credentials", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Labels["owner"] != "platform" {
		t.Errorf("secret labels = %v, want owner=platform", got.Labels)
	}
	if n := len(clientset.Actions()); n != 0 {
		t.Errorf("ApplyTags() made %d typed client calls, want 0", n)
	}
}
