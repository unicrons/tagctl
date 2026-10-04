package k8s

import (
	"context"
	"errors"
	"maps"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/unicrons/tagctl/internal/config"
	"github.com/unicrons/tagctl/internal/provider"
	"github.com/unicrons/tagctl/internal/types"
)

var _ provider.TagRemover = (*Provider)(nil)

func TestRemoveTags_DeletesOnlyTheGivenLabels(t *testing.T) {
	ctx := context.Background()
	clientset := fake.NewSimpleClientset(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name: "web", Namespace: "default",
		Labels: map[string]string{"Env": "prod", "temp": "1", "owner": "platform"},
	}})
	p := NewWithClients(clientset, fakeMetadata(t), config.KubernetesCluster{Name: "test-cluster"})

	if err := p.RemoveTags(ctx, types.Resource{ID: "k8s_pod/default/web"}, []string{"Env", "temp", "absent"}); err != nil {
		t.Fatalf("RemoveTags() error = %v", err)
	}

	pod, err := clientset.CoreV1().Pods("default").Get(ctx, "web", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if want := map[string]string{"owner": "platform"}; !maps.Equal(pod.Labels, want) {
		t.Errorf("labels = %v, want %v", pod.Labels, want)
	}
}

func TestRemoveTags_PatchesSecretThroughMetadataClient(t *testing.T) {
	ctx := context.Background()
	clientset := fake.NewSimpleClientset()
	metadataClient := fakeMetadata(t, secretMetadata("payments", "api-token", map[string]string{"Env": "prod", "owner": "platform"}))
	p := NewWithClients(clientset, metadataClient, config.KubernetesCluster{Name: "test-cluster"})

	if err := p.RemoveTags(ctx, types.Resource{ID: "k8s_secret/payments/api-token"}, []string{"Env"}); err != nil {
		t.Fatalf("RemoveTags() error = %v", err)
	}

	got, err := metadataClient.Resource(secretsResource).Namespace("payments").Get(ctx, "api-token", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if want := map[string]string{"owner": "platform"}; !maps.Equal(got.Labels, want) {
		t.Errorf("secret labels = %v, want %v", got.Labels, want)
	}
	if n := len(clientset.Actions()); n != 0 {
		t.Errorf("RemoveTags() made %d typed client calls, want 0", n)
	}
}

func TestRemoveTags_Errors(t *testing.T) {
	ctx := context.Background()
	clientset := fake.NewSimpleClientset()
	p := NewWithClients(clientset, fakeMetadata(t), config.KubernetesCluster{Name: "test-cluster"})

	if err := p.RemoveTags(ctx, types.Resource{ID: "k8s_pod/default/web"}, nil); err != nil || len(clientset.Actions()) != 0 {
		t.Errorf("RemoveTags() with no keys = %v after %d calls, want nil and no call", err, len(clientset.Actions()))
	}
	if err := p.RemoveTags(ctx, types.Resource{ID: "web"}, []string{"Env"}); err == nil {
		t.Error("RemoveTags() with a malformed ID = nil, want an error")
	}
	var unsupported *UnsupportedResourceError
	if err := p.RemoveTags(ctx, types.Resource{ID: "k8s_job/default/web"}, []string{"Env"}); !errors.As(err, &unsupported) {
		t.Errorf("RemoveTags() on an unsupported type = %v, want UnsupportedResourceError", err)
	}
	if err := p.RemoveTags(ctx, types.Resource{ID: "k8s_pod/default/missing"}, []string{"Env"}); err == nil {
		t.Error("RemoveTags() on a missing pod = nil, want the API error")
	}
}
