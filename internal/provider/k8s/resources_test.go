package k8s

import (
	"context"
	"errors"
	"slices"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/unicrons/tagctl/internal/config"
)

func podPage(next string, names ...string) *corev1.PodList {
	page := &corev1.PodList{ListMeta: metav1.ListMeta{Continue: next}}
	for _, name := range names {
		page.Items = append(page.Items, corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"}})
	}
	return page
}

func podNames(names *[]string) func(*corev1.PodList) {
	return func(page *corev1.PodList) {
		for _, pod := range page.Items {
			*names = append(*names, pod.Name)
		}
	}
}

func TestEachPage_FollowsContinueTokens(t *testing.T) {
	pages := map[string]*corev1.PodList{
		"":       podPage("page-2", "a", "b"),
		"page-2": podPage("page-3", "c"),
		"page-3": podPage("", "d"),
	}
	var calls []metav1.ListOptions
	list := func(_ context.Context, opts metav1.ListOptions) (*corev1.PodList, error) {
		calls = append(calls, opts)
		return pages[opts.Continue], nil
	}

	var names []string
	err := eachPage(context.Background(), metav1.ListOptions{LabelSelector: "app=web"}, list, podNames(&names))
	if err != nil {
		t.Fatalf("eachPage() error = %v", err)
	}

	if want := []string{"a", "b", "c", "d"}; !slices.Equal(names, want) {
		t.Errorf("visited %v, want %v", names, want)
	}
	if len(calls) != 3 {
		t.Fatalf("list called %d times, want 3", len(calls))
	}
	for i, opts := range calls {
		if opts.Limit != listPageSize || opts.LabelSelector != "app=web" {
			t.Errorf("call %d options = %+v, want Limit %d and the caller's selector", i, opts, listPageSize)
		}
	}
}

func TestEachPage_Errors(t *testing.T) {
	errList := errors.New("forbidden")
	cases := []struct {
		name string
		list func(context.Context, metav1.ListOptions) (*corev1.PodList, error)
		want error
	}{
		{
			name: "list error",
			list: func(context.Context, metav1.ListOptions) (*corev1.PodList, error) { return nil, errList },
			want: errList,
		},
		{
			name: "same continue token twice",
			list: func(context.Context, metav1.ListOptions) (*corev1.PodList, error) { return podPage("stuck", "a"), nil },
			want: errRepeatedContinue,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var names []string
			err := eachPage(context.Background(), metav1.ListOptions{}, tc.list, podNames(&names))
			if !errors.Is(err, tc.want) {
				t.Errorf("eachPage() error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestListPods_ReadsEveryPage(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	var limits []int64
	clientset.PrependReactor("list", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		opts := action.(k8stesting.ListActionImpl).ListOptions
		limits = append(limits, opts.Limit)
		if opts.Continue == "" {
			return true, podPage("page-2", "web-1"), nil
		}
		return true, podPage("", "web-2"), nil
	})
	p := NewWithClients(clientset, fakeMetadata(t), config.KubernetesCluster{Name: "test-cluster"})

	pods, err := p.listPods(context.Background(), "default")
	if err != nil {
		t.Fatalf("listPods() error = %v", err)
	}

	if len(pods) != 2 {
		t.Errorf("listPods() returned %d pods, want 2", len(pods))
	}
	if want := []int64{listPageSize, listPageSize}; !slices.Equal(limits, want) {
		t.Errorf("list limits = %v, want %v", limits, want)
	}
}

func TestBuildLabelPatch(t *testing.T) {
	patch, err := buildLabelPatch(map[string]string{"owner": "platform"})
	if err != nil {
		t.Fatalf("buildLabelPatch() error = %v", err)
	}
	if want := `{"metadata":{"labels":{"owner":"platform"}}}`; string(patch) != want {
		t.Errorf("buildLabelPatch() = %s, want %s", patch, want)
	}
}
