package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	metadatafake "k8s.io/client-go/metadata/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/spf13/cobra"

	"github.com/unicrons/tagctl/internal/config"
	"github.com/unicrons/tagctl/internal/log"
	"github.com/unicrons/tagctl/internal/provider"
	"github.com/unicrons/tagctl/internal/provider/k8s"
	"github.com/unicrons/tagctl/internal/types"
)

const providerKubernetes = "kubernetes"

// useFakeClusters serves every configured cluster from the clientset registered
// under its name and returns the clusters the constructor was asked for.
func useFakeClusters(t *testing.T, clientsets map[string]*fake.Clientset) *[]config.KubernetesCluster {
	t.Helper()
	scheme := metadatafake.NewTestScheme()
	if err := metav1.AddMetaToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	var built []config.KubernetesCluster
	original := newKubernetesProvider
	newKubernetesProvider = func(_ context.Context, cluster config.KubernetesCluster) (provider.Provider, error) {
		built = append(built, cluster)
		clientset, ok := clientsets[cluster.Name]
		if !ok {
			return nil, errors.New("context \"" + cluster.Context + "\" does not exist")
		}
		return k8s.NewWithClients(clientset, metadatafake.NewSimpleMetadataClient(scheme), cluster), nil
	}
	t.Cleanup(func() { newKubernetesProvider = original })
	return &built
}

func setFlags(t *testing.T, cmd *cobra.Command, flags map[string]string) {
	t.Helper()
	for name, value := range flags {
		defValue := cmd.Flags().Lookup(name).DefValue
		if err := cmd.Flags().Set(name, value); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = cmd.Flags().Set(name, defValue) })
	}
}

func useKubernetesConfig(t *testing.T, body string) {
	t.Helper()
	if _, err := loadConfigFrom(t, body); err != nil {
		t.Fatal(err)
	}
	originalDir := OutputDir
	OutputDir = t.TempDir()
	log.SetOutput(io.Discard)
	t.Cleanup(func() {
		OutputDir = originalDir
		log.SetOutput(os.Stderr)
	})
}

func latestScan(t *testing.T) *types.ScanResult {
	t.Helper()
	scans, err := findRecentScans(OutputDir, 1)
	if err != nil || len(scans) != 1 {
		t.Fatalf("scan JSON not written: %v", err)
	}
	written, err := LoadScanFile(scans[0])
	if err != nil {
		t.Fatal(err)
	}
	return written
}

func appDeployment(name string, labels map[string]string) *appsv1.Deployment {
	return &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Namespace: "app", Name: name, Labels: labels}}
}

func TestInitProviders_BuildsOneKubernetesProviderPerCluster(t *testing.T) {
	built := useFakeClusters(t, map[string]*fake.Clientset{
		"prod":    fake.NewSimpleClientset(),
		"staging": fake.NewSimpleClientset(),
	})
	cfg := &config.Config{Clouds: config.CloudsConfig{Kubernetes: []config.KubernetesCluster{
		{Name: "prod", Kubeconfig: "/etc/kube/config", Context: "prod-eks", Namespaces: []string{"app"}},
		{Name: "staging", Context: "staging-gke"},
	}}}

	providers, err := initProviders(context.Background(), cfg, nil)
	if err != nil {
		t.Fatalf("initProviders() error = %v", err)
	}

	if len(providers) != 2 {
		t.Fatalf("initProviders() built %d providers, want one per cluster", len(providers))
	}
	for i, want := range []string{"prod", "staging"} {
		p, ok := providers[i].(*k8s.Provider)
		if !ok || p.Name() != providerKubernetes || p.AccountID() != want {
			t.Errorf("providers[%d] = %T, want the kubernetes provider of cluster %s", i, providers[i], want)
		}
	}
	if got := (*built)[0]; got.Kubeconfig != "/etc/kube/config" || got.Context != "prod-eks" || len(got.Namespaces) != 1 {
		t.Errorf("constructor got %+v, want the kubeconfig, context and namespaces from the config", got)
	}
}

func TestInitProviders_KubernetesInitFailureNamesTheCluster(t *testing.T) {
	useFakeClusters(t, nil)
	log.SetOutput(io.Discard)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	cfg := &config.Config{Clouds: config.CloudsConfig{Kubernetes: []config.KubernetesCluster{{Name: "prod", Context: "nope"}}}}

	_, err := initProviders(context.Background(), cfg, nil)

	if err == nil || !strings.Contains(err.Error(), "cluster 'prod'") || !strings.Contains(err.Error(), `context "nope" does not exist`) {
		t.Fatalf("initProviders() error = %v, want the cluster name and the cause", err)
	}
}

func TestHasConfiguredProviders_CountsKubernetesClusters(t *testing.T) {
	cfg := &config.Config{Clouds: config.CloudsConfig{Kubernetes: []config.KubernetesCluster{{Name: "prod"}}}}

	if !hasConfiguredProviders(cfg) {
		t.Error("hasConfiguredProviders() = false for a Kubernetes-only config, so scan would fall back to demo data")
	}
}

const kubernetesConfig = `clouds:
  kubernetes:
    - name: prod
      context: prod
      resource_types: [k8s_deployment]
    - name: staging
      context: staging
      namespaces: [app]
      resource_types: [k8s_deployment]
policy:
  required:
    - name: environment
      values: [dev, staging, prod]
rules:
  defaults:
    - resource: "k8s_*"
      when:
        "tag:environment": absent
      set:
        environment: %s
`

func TestScanPlanApply_Kubernetes(t *testing.T) {
	prod := fake.NewSimpleClientset(
		appDeployment("api", map[string]string{"environment": "prod"}),
		appDeployment("worker", nil),
	)
	staging := fake.NewSimpleClientset(appDeployment("api", nil))
	useFakeClusters(t, map[string]*fake.Clientset{"prod": prod, "staging": staging})
	useKubernetesConfig(t, strings.Replace(kubernetesConfig, "%s", "dev", 1))

	if err := runScan(scanCmd, nil); err != nil {
		t.Fatalf("runScan() error = %v", err)
	}

	scan := latestScan(t)
	if scan.Partial || scan.TotalResources != 3 || scan.CompliantCount != 1 {
		t.Fatalf("scan partial = %v, total = %d, compliant = %d; want 3 deployments, 1 compliant",
			scan.Partial, scan.TotalResources, scan.CompliantCount)
	}
	failed := map[string]bool{}
	for _, f := range scan.FailedFindings() {
		failed[f.Resource.Identity()] = true
	}
	for _, want := range []string{"kubernetes/prod/app/k8s_deployment/app/worker", "kubernetes/staging/app/k8s_deployment/app/api"} {
		if !failed[want] {
			t.Errorf("failed findings = %v, want %s", failed, want)
		}
	}

	if err := runPlan(planCmd, nil); err != nil {
		t.Fatalf("runPlan() error = %v", err)
	}
	setFlags(t, applyCmd, map[string]string{"auto-approve": "true"})
	if err := runApply(applyCmd, nil); err != nil {
		t.Fatalf("runApply() error = %v", err)
	}

	ctx := context.Background()
	for name, tc := range map[string]struct {
		clientset *fake.Clientset
		object    string
		want      string
	}{
		"prod worker is labelled":      {prod, "worker", "dev"},
		"prod api keeps its label":     {prod, "api", "prod"},
		"staging api is labelled once": {staging, "api", "dev"},
	} {
		got, err := tc.clientset.AppsV1().Deployments("app").Get(ctx, tc.object, metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if got.Labels["environment"] != tc.want {
			t.Errorf("%s: environment = %q, want %q", name, got.Labels["environment"], tc.want)
		}
	}
}

func TestApply_KubernetesRejectsInvalidLabelValue(t *testing.T) {
	prod := fake.NewSimpleClientset(appDeployment("worker", nil))
	useFakeClusters(t, map[string]*fake.Clientset{"prod": prod, "staging": fake.NewSimpleClientset()})
	useKubernetesConfig(t, strings.Replace(kubernetesConfig, "%s", "platform@company.com", 1))

	if err := runScan(scanCmd, nil); err != nil {
		t.Fatalf("runScan() error = %v", err)
	}
	if err := runPlan(planCmd, nil); err != nil {
		t.Fatalf("runPlan() error = %v", err)
	}
	prod.ClearActions()
	setFlags(t, applyCmd, map[string]string{"auto-approve": "true"})

	err := runApply(applyCmd, nil)

	if err == nil || !strings.Contains(err.Error(), "1 of 1 changes failed") {
		t.Fatalf("runApply() error = %v, want the change to fail", err)
	}
	if n := len(prod.Actions()); n != 0 {
		t.Errorf("apply made %d API calls with an invalid label value: %v", n, prod.Actions())
	}
}

func TestRunScan_UnreachableKubernetesClusterIsPartial(t *testing.T) {
	refused := errors.New("dial tcp 10.0.0.1:6443: connect: connection refused")
	prod := fake.NewSimpleClientset()
	prod.PrependReactor("*", "*", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, refused
	})
	staging := fake.NewSimpleClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "app"}},
		appDeployment("api", nil),
	)
	useFakeClusters(t, map[string]*fake.Clientset{"prod": prod, "staging": staging})
	useKubernetesConfig(t, strings.Replace(kubernetesConfig, "%s", "dev", 1))

	err := runScan(scanCmd, nil)

	if err == nil || !strings.Contains(err.Error(), "--allow-partial") || !strings.Contains(err.Error(), "cluster prod") {
		t.Fatalf("runScan() error = %v, want a partial scan naming the cluster", err)
	}
	if code := ExitCode(err); code != exitError {
		t.Errorf("ExitCode() = %d, want %d", code, exitError)
	}
	scan := latestScan(t)
	if !scan.Partial || len(scan.Errors) != 1 || scan.TotalResources != 1 {
		t.Errorf("scan partial = %v, errors = %q, total = %d; want a partial scan keeping the reachable cluster",
			scan.Partial, scan.Errors, scan.TotalResources)
	}
}
