package types

import "testing"

func TestNormalizeResult_AffectedResourcesSkipsTransitive(t *testing.T) {
	result := &NormalizeResult{
		Clusters: []ValueCluster{
			{Tag: "environment", Canonical: "staging", Variants: []ValueVariant{
				{Value: "Staging", Count: 4, Match: MatchCasing},
				{Value: "stagng", Count: 2, Match: MatchTypo},
				{Value: "stagn", Count: 1, Match: MatchTransitive},
			}},
			{Tag: "environment", Canonical: "production", Variants: []ValueVariant{
				{Value: "prod", Count: 3, Match: MatchAbbreviation},
			}},
		},
	}

	if got := result.AffectedResources(); got != 9 {
		t.Errorf("AffectedResources() = %d, want 9 (the transitive variant is not rewritten)", got)
	}
}
