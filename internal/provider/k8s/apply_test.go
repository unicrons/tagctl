package k8s

import (
	"context"
	"errors"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/unicrons/tagctl/internal/config"
	"github.com/unicrons/tagctl/internal/provider"
)

const (
	applyNamespace = "payments"
	applyName      = "api"
)

func labelled(namespace string) metav1.ObjectMeta {
	return metav1.ObjectMeta{Name: applyName, Namespace: namespace, Labels: map[string]string{"app": "api"}}
}

// applyFixture holds one object of every supported type, all named api.
func applyFixture(t *testing.T) (*Provider, *fake.Clientset) {
	t.Helper()
	clientset := fake.NewSimpleClientset(
		&corev1.Pod{ObjectMeta: labelled(applyNamespace)},
		&appsv1.Deployment{ObjectMeta: labelled(applyNamespace)},
		&corev1.Service{ObjectMeta: labelled(applyNamespace)},
		&corev1.ConfigMap{ObjectMeta: labelled(applyNamespace)},
		&corev1.Namespace{ObjectMeta: labelled("")},
	)
	metadataClient := fakeMetadata(t, secretMetadata(applyNamespace, applyName, map[string]string{"app": "api"}))
	return NewWithClients(clientset, metadataClient, config.KubernetesCluster{Name: "test-cluster"}), clientset
}

func typedLabels(t *testing.T, clientset *fake.Clientset, resourceType string) map[string]string {
	t.Helper()
	ctx := context.Background()
	opts := metav1.GetOptions{}

	var meta metav1.Object
	var err error
	switch resourceType {
	case ResourceTypePod:
		meta, err = clientset.CoreV1().Pods(applyNamespace).Get(ctx, applyName, opts)
	case ResourceTypeDeployment:
		meta, err = clientset.AppsV1().Deployments(applyNamespace).Get(ctx, applyName, opts)
	case ResourceTypeService:
		meta, err = clientset.CoreV1().Services(applyNamespace).Get(ctx, applyName, opts)
	case ResourceTypeConfigMap:
		meta, err = clientset.CoreV1().ConfigMaps(applyNamespace).Get(ctx, applyName, opts)
	case ResourceTypeNamespace:
		meta, err = clientset.CoreV1().Namespaces().Get(ctx, applyName, opts)
	default:
		t.Fatalf("no typed getter for %s", resourceType)
	}
	if err != nil {
		t.Fatalf("get %s: %v", resourceType, err)
	}
	return meta.GetLabels()
}

func assertProviderError(t *testing.T, err error, operation, resourceID string) {
	t.Helper()
	var providerErr *provider.ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("err = %v, want a ProviderError", err)
	}
	if providerErr.Provider != providerName || providerErr.Operation != operation || providerErr.ResourceID != resourceID {
		t.Errorf("error = %s/%s/%s, want %s/%s/%s",
			providerErr.Provider, providerErr.Operation, providerErr.ResourceID,
			providerName, operation, resourceID)
	}
}

func TestApplyTags_MergesLabelsIntoEveryTypedResource(t *testing.T) {
	tests := []struct {
		resourceType string
		resourceID   string
	}{
		{ResourceTypePod, "k8s_pod/payments/api"},
		{ResourceTypeDeployment, "k8s_deployment/payments/api"},
		{ResourceTypeService, "k8s_service/payments/api"},
		{ResourceTypeConfigMap, "k8s_configmap/payments/api"},
		{ResourceTypeNamespace, "k8s_namespace/api"},
	}

	for _, tt := range tests {
		t.Run(tt.resourceType, func(t *testing.T) {
			p, clientset := applyFixture(t)

			err := p.ApplyTags(context.Background(), tt.resourceID, map[string]string{"owner": "platform", "environment": "prod"})

			if err != nil {
				t.Fatalf("ApplyTags() error = %v", err)
			}
			labels := typedLabels(t, clientset, tt.resourceType)
			want := map[string]string{"app": "api", "owner": "platform", "environment": "prod"}
			if len(labels) != len(want) {
				t.Fatalf("labels = %v, want %v", labels, want)
			}
			for k, v := range want {
				if labels[k] != v {
					t.Errorf("labels[%s] = %q, want %q", k, labels[k], v)
				}
			}
		})
	}
}

func TestApplyTags_SendsOneMergePatchToTheTargetOnly(t *testing.T) {
	p, clientset := applyFixture(t)

	if err := p.ApplyTags(context.Background(), "k8s_deployment/payments/api", map[string]string{"owner": "platform"}); err != nil {
		t.Fatalf("ApplyTags() error = %v", err)
	}

	actions := clientset.Actions()
	if len(actions) != 1 {
		t.Fatalf("got %d API calls, want 1 patch: %v", len(actions), actions)
	}
	patch, ok := actions[0].(k8stesting.PatchAction)
	if !ok {
		t.Fatalf("call = %T, want a patch", actions[0])
	}
	if patch.GetResource().Resource != "deployments" || patch.GetNamespace() != applyNamespace || patch.GetName() != applyName {
		t.Errorf("patched %s %s/%s, want deployments payments/api",
			patch.GetResource().Resource, patch.GetNamespace(), patch.GetName())
	}
	if patch.GetPatchType() != k8stypes.MergePatchType {
		t.Errorf("patch type = %s, want %s", patch.GetPatchType(), k8stypes.MergePatchType)
	}
	if got, want := string(patch.GetPatch()), `{"metadata":{"labels":{"owner":"platform"}}}`; got != want {
		t.Errorf("patch body = %s, want %s", got, want)
	}
	if labels := typedLabels(t, clientset, ResourceTypePod); labels["owner"] != "" {
		t.Errorf("pod with the same name was labelled too: %v", labels)
	}
}

func TestApplyTags_RejectsMalformedResourceIDWithoutCallingTheAPI(t *testing.T) {
	for _, resourceID := range []string{"", "k8s_pod", "k8s_pod/payments/api/extra"} {
		t.Run(resourceID, func(t *testing.T) {
			p, clientset := applyFixture(t)

			err := p.ApplyTags(context.Background(), resourceID, map[string]string{"owner": "platform"})

			assertProviderError(t, err, "apply_labels", resourceID)
			if n := len(clientset.Actions()); n != 0 {
				t.Errorf("made %d API calls, want 0", n)
			}
		})
	}
}

func TestApplyTags_RejectsUnsupportedTypeWithoutCallingTheAPI(t *testing.T) {
	p, clientset := applyFixture(t)

	err := p.ApplyTags(context.Background(), "k8s_ingress/payments/api", map[string]string{"owner": "platform"})

	assertProviderError(t, err, "apply_labels", "k8s_ingress/payments/api")
	var unsupported *UnsupportedResourceError
	if !errors.As(err, &unsupported) || unsupported.ResourceType != "k8s_ingress" {
		t.Errorf("err = %v, want UnsupportedResourceError for k8s_ingress", err)
	}
	if n := len(clientset.Actions()); n != 0 {
		t.Errorf("made %d API calls, want 0", n)
	}
}

func TestApplyTags_ReportsMissingResourceAsNotFound(t *testing.T) {
	tests := []struct {
		resourceID string
		operation  string
	}{
		{"k8s_pod/payments/gone", "patch_pod_labels"},
		{"k8s_deployment/payments/gone", "patch_deployment_labels"},
		{"k8s_service/payments/gone", "patch_service_labels"},
		{"k8s_configmap/payments/gone", "patch_configmap_labels"},
		{"k8s_namespace/gone", "patch_namespace_labels"},
		{"k8s_secret/payments/gone", "patch_secret_labels"},
	}

	for _, tt := range tests {
		t.Run(tt.resourceID, func(t *testing.T) {
			p, _ := applyFixture(t)

			err := p.ApplyTags(context.Background(), tt.resourceID, map[string]string{"owner": "platform"})

			assertProviderError(t, err, tt.operation, tt.resourceID)
			if !apierrors.IsNotFound(err) {
				t.Errorf("err = %v, want it to unwrap to NotFound", err)
			}
		})
	}
}

func TestPatchLabels_EachFunctionPatchesItsOwnResource(t *testing.T) {
	patch, err := buildLabelPatch(map[string]string{"owner": "platform"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	tests := []struct {
		resourceType string
		resource     string
		run          func(*Provider) error
	}{
		{ResourceTypePod, "pods", func(p *Provider) error { return p.patchPodLabels(ctx, applyNamespace, applyName, patch) }},
		{ResourceTypeDeployment, "deployments", func(p *Provider) error { return p.patchDeploymentLabels(ctx, applyNamespace, applyName, patch) }},
		{ResourceTypeService, "services", func(p *Provider) error { return p.patchServiceLabels(ctx, applyNamespace, applyName, patch) }},
		{ResourceTypeConfigMap, "configmaps", func(p *Provider) error { return p.patchConfigMapLabels(ctx, applyNamespace, applyName, patch) }},
		{ResourceTypeNamespace, "namespaces", func(p *Provider) error { return p.patchNamespaceLabels(ctx, applyName, patch) }},
	}

	for _, tt := range tests {
		t.Run(tt.resourceType, func(t *testing.T) {
			p, clientset := applyFixture(t)

			if err := tt.run(p); err != nil {
				t.Fatalf("patch error = %v", err)
			}

			actions := clientset.Actions()
			if len(actions) != 1 || actions[0].GetVerb() != "patch" || actions[0].GetResource().Resource != tt.resource {
				t.Fatalf("API calls = %v, want one patch on %s", actions, tt.resource)
			}
			if labels := typedLabels(t, clientset, tt.resourceType); labels["owner"] != "platform" || labels["app"] != "api" {
				t.Errorf("labels = %v, want owner added and app kept", labels)
			}
		})
	}
}

func TestPatchSecretLabels_KeepsExistingLabels(t *testing.T) {
	ctx := context.Background()
	clientset := fake.NewSimpleClientset()
	metadataClient := fakeMetadata(t, secretMetadata(applyNamespace, applyName, map[string]string{"app": "api"}))
	p := NewWithClients(clientset, metadataClient, config.KubernetesCluster{Name: "test-cluster"})
	patch, err := buildLabelPatch(map[string]string{"owner": "platform"})
	if err != nil {
		t.Fatal(err)
	}

	if err = p.patchSecretLabels(ctx, applyNamespace, applyName, patch); err != nil {
		t.Fatalf("patchSecretLabels() error = %v", err)
	}

	got, err := metadataClient.Resource(secretsResource).Namespace(applyNamespace).Get(ctx, applyName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Labels["owner"] != "platform" || got.Labels["app"] != "api" {
		t.Errorf("labels = %v, want owner added and app kept", got.Labels)
	}
	if n := len(clientset.Actions()); n != 0 {
		t.Errorf("made %d typed client calls, want 0", n)
	}
}

func TestPatchLabels_WrapAPIErrorsWithOperationAndResourceID(t *testing.T) {
	ctx := context.Background()
	denied := errors.New("forbidden: cannot patch")

	tests := []struct {
		operation  string
		resourceID string
		run        func(*Provider) error
	}{
		{"patch_pod_labels", "k8s_pod/payments/api", func(p *Provider) error { return p.patchPodLabels(ctx, applyNamespace, applyName, nil) }},
		{"patch_deployment_labels", "k8s_deployment/payments/api", func(p *Provider) error { return p.patchDeploymentLabels(ctx, applyNamespace, applyName, nil) }},
		{"patch_service_labels", "k8s_service/payments/api", func(p *Provider) error { return p.patchServiceLabels(ctx, applyNamespace, applyName, nil) }},
		{"patch_configmap_labels", "k8s_configmap/payments/api", func(p *Provider) error { return p.patchConfigMapLabels(ctx, applyNamespace, applyName, nil) }},
		{"patch_namespace_labels", "k8s_namespace/api", func(p *Provider) error { return p.patchNamespaceLabels(ctx, applyName, nil) }},
		{"patch_secret_labels", "k8s_secret/payments/api", func(p *Provider) error { return p.patchSecretLabels(ctx, applyNamespace, applyName, nil) }},
	}

	for _, tt := range tests {
		t.Run(tt.operation, func(t *testing.T) {
			clientset := fake.NewSimpleClientset()
			metadataClient := fakeMetadata(t)
			fail := func(k8stesting.Action) (bool, runtime.Object, error) { return true, nil, denied }
			clientset.PrependReactor("patch", "*", fail)
			metadataClient.PrependReactor("patch", "*", fail)
			p := NewWithClients(clientset, metadataClient, config.KubernetesCluster{Name: "test-cluster"})

			err := tt.run(p)

			assertProviderError(t, err, tt.operation, tt.resourceID)
			if !errors.Is(err, denied) {
				t.Errorf("err = %v, want it to wrap the API error", err)
			}
		})
	}
}
