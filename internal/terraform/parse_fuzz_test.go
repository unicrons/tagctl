package terraform

import (
	"bytes"
	"testing"
)

func FuzzParse(f *testing.F) {
	for _, seed := range []string{planJSON, stateJSON, unknownPlanJSON} {
		f.Add([]byte(seed), false)
		f.Add([]byte(seed), true)
	}

	f.Fuzz(func(t *testing.T, data []byte, changedOnly bool) {
		result, err := Parse(bytes.NewReader(data), Options{ChangedOnly: changedOnly})
		if err != nil {
			return
		}
		for _, resource := range result.Resources {
			if resource.Tags == nil {
				t.Errorf("%s: returned with nil tags", resource.ID)
			}
			for _, key := range resource.UnknownTags {
				if _, present := resource.Tags[key]; !present {
					t.Errorf("%s: unknown tag %q is not among its tags", resource.ID, key)
				}
			}
		}
	})
}
