package config

import (
	"slices"
	"strings"
	"testing"
)

func TestValidate_KubernetesResourceTypes(t *testing.T) {
	cases := []struct {
		name    string
		types   []string
		wantErr string
	}{
		{"empty means defaults", nil, ""},
		{"every supported type", KubernetesResourceTypes, ""},
		{"secrets listed explicitly", []string{KubernetesSecret}, ""},
		{"typo", []string{KubernetesPod, "k8s_pods"}, `unknown resource type "k8s_pods"`},
		{"other provider type", []string{"aws_instance"}, `unknown resource type "aws_instance"`},
		{"listed twice", []string{KubernetesService, KubernetesService}, `resource type "k8s_service" listed twice`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{Clouds: CloudsConfig{Kubernetes: []KubernetesCluster{{Name: "prod", ResourceTypes: tc.types}}}}
			err := cfg.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Validate() = %v, want error containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestKubernetesDefaultResourceTypes_LeaveSecretsOut(t *testing.T) {
	if slices.Contains(KubernetesDefaultResourceTypes, KubernetesSecret) {
		t.Errorf("KubernetesDefaultResourceTypes = %v, secrets must be opt-in", KubernetesDefaultResourceTypes)
	}
	for _, rt := range KubernetesDefaultResourceTypes {
		if !slices.Contains(KubernetesResourceTypes, rt) {
			t.Errorf("default type %q is not in KubernetesResourceTypes", rt)
		}
	}
}
