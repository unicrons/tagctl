package k8s

import (
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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

func TestListResources_UnreachableClusterIsAnError(t *testing.T) {
	refused := errors.New("dial tcp 10.0.0.1:6443: connect: connection refused")
	clientset := fake.NewSimpleClientset()
	clientset.PrependReactor("*", "*", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, refused
	})
	p := NewWithClients(clientset, fakeMetadata(t), config.KubernetesCluster{Name: "prod"})

	resources, err := p.ListResources(context.Background())

	if !errors.Is(err, refused) {
		t.Fatalf("ListResources() error = %v, want the connection error", err)
	}
	if !strings.Contains(err.Error(), "cluster prod: ") {
		t.Errorf("ListResources() error = %v, want it to name the cluster", err)
	}
	if len(resources) != 0 {
		t.Errorf("ListResources() returned %d resources from an unreachable cluster", len(resources))
	}
}

func TestListResources_KeepsResourcesNextToAListError(t *testing.T) {
	clientset := fake.NewSimpleClientset(&corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "default"},
	})
	clientset.PrependReactor("list", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("pods are forbidden")
	})
	p := NewWithClients(clientset, fakeMetadata(t), config.KubernetesCluster{
		Name:          "prod",
		ResourceTypes: []string{ResourceTypePod, ResourceTypeService},
	})

	resources, err := p.ListResources(context.Background())

	if err == nil {
		t.Fatal("ListResources() error = nil, want the pods error")
	}
	if len(resources) != 1 || resources[0].ID != "k8s_service/default/api" {
		t.Errorf("ListResources() = %+v, want the service that could be listed", resources)
	}
}

func TestListResources_AllNamespacesListsEachTypeOnce(t *testing.T) {
	clientset := fake.NewSimpleClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "app"}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "data"}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "app"}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "data"}},
	)
	p := NewWithClients(clientset, fakeMetadata(t), config.KubernetesCluster{
		Name:          "prod",
		ResourceTypes: []string{ResourceTypePod},
	})

	resources, err := p.ListResources(context.Background())
	if err != nil {
		t.Fatalf("ListResources() error = %v", err)
	}

	identities := make([]string, 0, len(resources))
	for _, r := range resources {
		identities = append(identities, r.Identity())
	}
	slices.Sort(identities)
	want := []string{"kubernetes/prod/app/k8s_pod/app/web", "kubernetes/prod/data/k8s_pod/data/web"}
	if !slices.Equal(identities, want) {
		t.Errorf("identities = %v, want %v", identities, want)
	}
	if n := len(clientset.Actions()); n != 1 {
		t.Errorf("ListResources() made %d API calls, want one cluster-wide pod list: %v", n, clientset.Actions())
	}
}

func TestListResources_ConfiguredNamespacesNeedNoClusterWideList(t *testing.T) {
	clientset := fake.NewSimpleClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "app", Labels: map[string]string{"team": "web"}}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "kube-system"}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "app"}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "dns", Namespace: "kube-system"}},
	)
	p := NewWithClients(clientset, fakeMetadata(t), config.KubernetesCluster{
		Name:          "prod",
		Namespaces:    []string{"app"},
		ResourceTypes: []string{ResourceTypePod, ResourceTypeNamespace},
	})

	resources, err := p.ListResources(context.Background())
	if err != nil {
		t.Fatalf("ListResources() error = %v", err)
	}

	ids := make([]string, 0, len(resources))
	for _, r := range resources {
		ids = append(ids, r.ID)
	}
	slices.Sort(ids)
	if want := []string{"k8s_namespace/app", "k8s_pod/app/web"}; !slices.Equal(ids, want) {
		t.Errorf("ids = %v, want %v", ids, want)
	}
	for _, action := range clientset.Actions() {
		if action.GetNamespace() == "" && action.GetVerb() == "list" {
			t.Errorf("ListResources() made a cluster-wide list: %v", action)
		}
	}
}

func TestListResources_MissingConfiguredNamespaceIsAnError(t *testing.T) {
	p := NewWithClients(fake.NewSimpleClientset(), fakeMetadata(t), config.KubernetesCluster{
		Name:          "prod",
		Namespaces:    []string{"typo"},
		ResourceTypes: []string{ResourceTypeNamespace},
	})

	_, err := p.ListResources(context.Background())

	if !apierrors.IsNotFound(err) {
		t.Fatalf("ListResources() error = %v, want not found for the namespace", err)
	}
}

func TestAccountID_IsTheClusterName(t *testing.T) {
	p := NewWithClients(fake.NewSimpleClientset(), fakeMetadata(t), config.KubernetesCluster{Name: "prod"})

	if got := p.AccountID(); got != "prod" {
		t.Errorf("AccountID() = %q, want the cluster name", got)
	}
}

func TestApplyTags_RejectsInvalidLabelsBeforeCallingTheAPI(t *testing.T) {
	tests := []struct {
		name string
		tags map[string]string
		want string
	}{
		{"e-mail value", map[string]string{"owner": "platform@company.com"}, `value "platform@company.com" of label "owner" is not a valid Kubernetes label value`},
		{"value with a space", map[string]string{"team": "data platform"}, `value "data platform" of label "team"`},
		{"value over 63 characters", map[string]string{"team": strings.Repeat("a", 64)}, `of label "team" is not a valid Kubernetes label value`},
		{"key with a colon", map[string]string{"aws:owner": "platform"}, `label key "aws:owner" is not valid`},
		{"one bad value among good ones", map[string]string{"environment": "prod", "owner": "a@b"}, `value "a@b" of label "owner"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clientset := fake.NewSimpleClientset(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "default"}})
			clientset.ClearActions()
			p := NewWithClients(clientset, fakeMetadata(t), config.KubernetesCluster{Name: "prod"})

			err := p.ApplyTags(context.Background(), "k8s_pod/default/web", tt.tags)

			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ApplyTags() error = %v, want one containing %q", err, tt.want)
			}
			if !strings.Contains(err.Error(), "k8s_pod/default/web") {
				t.Errorf("ApplyTags() error = %v, want it to name the resource", err)
			}
			if n := len(clientset.Actions()); n != 0 {
				t.Errorf("ApplyTags() made %d API calls with an invalid label", n)
			}
		})
	}
}

func TestApplyTags_PatchesValidLabels(t *testing.T) {
	ctx := context.Background()
	clientset := fake.NewSimpleClientset(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "default", Labels: map[string]string{"app": "api"}},
	})
	p := NewWithClients(clientset, fakeMetadata(t), config.KubernetesCluster{Name: "prod"})

	tags := map[string]string{"environment": "prod", "example.com/cost-center": "TECH-001", "note": ""}
	if err := p.ApplyTags(ctx, "k8s_deployment/default/api", tags); err != nil {
		t.Fatalf("ApplyTags() error = %v", err)
	}

	got, err := clientset.AppsV1().Deployments("default").Get(ctx, "api", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"app": "api", "environment": "prod", "example.com/cost-center": "TECH-001", "note": ""}
	if !maps.Equal(got.Labels, want) {
		t.Errorf("labels = %v, want %v", got.Labels, want)
	}
}

const testKubeconfig = `apiVersion: v1
kind: Config
current-context: dev
clusters:
  - name: dev
    cluster: {server: "https://dev.example.com"}
  - name: prod
    cluster: {server: "https://prod.example.com"}
contexts:
  - name: dev
    context: {cluster: dev, user: tester}
  - name: prod
    context: {cluster: prod, user: tester}
users:
  - name: tester
    user: {token: test-token}
`

func writeKubeconfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte(testKubeconfig), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRestConfig(t *testing.T) {
	explicit := writeKubeconfig(t)
	fromEnv := writeKubeconfig(t)
	t.Setenv("KUBECONFIG", fromEnv)

	tests := []struct {
		name     string
		cluster  config.KubernetesCluster
		wantHost string
		wantErr  string
	}{
		{"explicit file uses its current context", config.KubernetesCluster{Kubeconfig: explicit}, "https://dev.example.com", ""},
		{"context selects the cluster", config.KubernetesCluster{Kubeconfig: explicit, Context: "prod"}, "https://prod.example.com", ""},
		{"no kubeconfig falls back to KUBECONFIG", config.KubernetesCluster{Context: "prod"}, "https://prod.example.com", ""},
		{"unknown context", config.KubernetesCluster{Kubeconfig: explicit, Context: "nope"}, "", `context "nope" does not exist`},
		{"missing file", config.KubernetesCluster{Kubeconfig: filepath.Join(t.TempDir(), "absent")}, "", "load_kubeconfig failed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := restConfig(tt.cluster)

			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("restConfig() error = %v, want one containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("restConfig() error = %v", err)
			}
			if cfg.Host != tt.wantHost {
				t.Errorf("Host = %q, want %q", cfg.Host, tt.wantHost)
			}
		})
	}
}

func TestRestConfig_InClusterOutsideAClusterFails(t *testing.T) {
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	t.Setenv("KUBERNETES_SERVICE_PORT", "")

	_, err := restConfig(config.KubernetesCluster{Kubeconfig: "in-cluster"})

	if err == nil || !strings.Contains(err.Error(), "in_cluster_config failed") {
		t.Fatalf("restConfig() error = %v, want the in-cluster failure", err)
	}
}

func TestNew_BuildsAProviderWithoutContactingTheCluster(t *testing.T) {
	p, err := New(context.Background(), config.KubernetesCluster{Name: "prod", Kubeconfig: writeKubeconfig(t), Context: "prod"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if p.AccountID() != "prod" {
		t.Errorf("AccountID() = %q, want prod", p.AccountID())
	}
}

func TestListResources_ReportsEveryListError(t *testing.T) {
	podsErr := errors.New("pods are forbidden")
	servicesErr := errors.New("services are forbidden")
	clientset := fake.NewSimpleClientset()
	clientset.PrependReactor("list", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, podsErr
	})
	clientset.PrependReactor("list", "services", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, servicesErr
	})
	p := NewWithClients(clientset, fakeMetadata(t), config.KubernetesCluster{
		Name:          "test-cluster",
		Namespaces:    []string{"default"},
		ResourceTypes: []string{ResourceTypePod, ResourceTypeService},
	})

	_, err := p.ListResources(context.Background())

	if !errors.Is(err, podsErr) || !errors.Is(err, servicesErr) {
		t.Errorf("err = %v, want both list errors", err)
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
			clientset := fake.NewSimpleClientset(
				&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
				&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "db-credentials", Namespace: "default"}},
			)
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
