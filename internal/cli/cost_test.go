package cli

import (
	"strings"
	"testing"

	"github.com/unicrons/tagctl/internal/config"
	"github.com/unicrons/tagctl/internal/types"
)

func costReport(coverage map[string][2]float64) *types.CostReport {
	report := &types.CostReport{
		Currency: "USD",
		Tags:     make(map[string]*types.TagCost, len(coverage)),
	}
	for tag, amounts := range coverage {
		report.Tags[tag] = &types.TagCost{
			Tag:          tag,
			Attributed:   amounts[0],
			Unattributed: amounts[1],
		}
	}
	return report
}

func TestRequiredTagNames(t *testing.T) {
	cfg := &config.Config{
		Policy: config.PolicyConfig{
			Required: []config.TagRequirement{
				{Name: "owner"},
				{Name: "environment"},
			},
			Optional: []config.TagRequirement{{Name: "tier"}},
		},
	}

	names := requiredTagNames(cfg)

	if len(names) != 2 {
		t.Fatalf("got %d names (%v), want 2: optional tags are not billed on", len(names), names)
	}
	if names[0] != "owner" || names[1] != "environment" {
		t.Errorf("names = %v, want [owner environment]", names)
	}
}

func TestRequiredTagNames_Empty(t *testing.T) {
	if names := requiredTagNames(&config.Config{}); len(names) != 0 {
		t.Errorf("got %v, want no names", names)
	}
}

func TestCostAccount_FallsBackToConfig(t *testing.T) {
	cfg := &config.Config{
		Clouds: config.CloudsConfig{
			AWS: []config.AWSAccount{
				{Profile: "first", Regions: []string{"eu-west-1", "eu-west-2"}},
				{Profile: "second"},
			},
		},
	}

	account, err := costAccount(cfg)
	if err != nil {
		t.Fatalf("costAccount() error = %v", err)
	}
	if account.Profile != "first" {
		t.Errorf("profile = %q, want the first configured account", account.Profile)
	}
	// Cost Explorer is global, so the configured regions are not needed.
	if len(account.Regions) != 1 || account.Regions[0] != "us-east-1" {
		t.Errorf("regions = %v, want just us-east-1", account.Regions)
	}
}

func TestCostAccount_NoAccountConfigured(t *testing.T) {
	if _, err := costAccount(&config.Config{}); err == nil {
		t.Error("costAccount() with no AWS account returned nil error")
	}
}

func TestSortedTagCosts_WorstFirst(t *testing.T) {
	report := costReport(map[string][2]float64{
		"owner":       {9000, 1000}, // 90%
		"cost-center": {2000, 8000}, // 20%
		"environment": {5000, 5000}, // 50%
	})

	sorted := sortedTagCosts(report)

	if len(sorted) != 3 {
		t.Fatalf("got %d tags, want 3", len(sorted))
	}
	want := []string{"cost-center", "environment", "owner"}
	for i, name := range want {
		if sorted[i].Tag != name {
			t.Errorf("position %d = %q, want %q", i, sorted[i].Tag, name)
		}
	}
}

// Ties break by name so the output does not shuffle between runs.
func TestSortedTagCosts_StableOnTies(t *testing.T) {
	report := costReport(map[string][2]float64{
		"zebra": {5000, 5000},
		"alpha": {5000, 5000},
		"mango": {5000, 5000},
	})

	for i := 0; i < 10; i++ {
		sorted := sortedTagCosts(report)
		if sorted[0].Tag != "alpha" || sorted[2].Tag != "zebra" {
			t.Fatalf("run %d: order = %s, %s, %s", i, sorted[0].Tag, sorted[1].Tag, sorted[2].Tag)
		}
	}
}

func TestCheckCostCoverage(t *testing.T) {
	report := costReport(map[string][2]float64{
		"owner":       {9000, 1000}, // 90%
		"cost-center": {2000, 8000}, // 20%
	})

	t.Run("passes when every tag clears the bar", func(t *testing.T) {
		if err := checkCostCoverage(report, 15); err != nil {
			t.Errorf("checkCostCoverage() = %v, want nil", err)
		}
	})

	t.Run("fails and names the tag below it", func(t *testing.T) {
		err := checkCostCoverage(report, 50)
		if err == nil {
			t.Fatal("checkCostCoverage() = nil, want an error")
		}
		if !strings.Contains(err.Error(), "cost-center") {
			t.Errorf("error should name the failing tag, got: %v", err)
		}
		if strings.Contains(err.Error(), "owner") {
			t.Errorf("error names a tag that passed, got: %v", err)
		}
	})

	t.Run("reports every failing tag", func(t *testing.T) {
		err := checkCostCoverage(report, 95)
		if err == nil {
			t.Fatal("checkCostCoverage() = nil, want an error")
		}
		for _, tag := range []string{"owner", "cost-center"} {
			if !strings.Contains(err.Error(), tag) {
				t.Errorf("error should name %s, got: %v", tag, err)
			}
		}
	})
}

func TestMoney(t *testing.T) {
	tests := []struct {
		amount   float64
		currency string
		want     string
	}{
		{1234.5, "USD", "1234.50 USD"},
		{0, "EUR", "0.00 EUR"},
		{99.999, "USD", "100.00 USD"},
		{42, "", "42.00 USD"}, // currency defaults rather than printing blank
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := money(tt.amount, tt.currency); got != tt.want {
				t.Errorf("money(%v, %q) = %q, want %q", tt.amount, tt.currency, got, tt.want)
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		in   string
		max  int
		want string
	}{
		{"short", 10, "short"},
		{"exactly-10", 10, "exactly-10"},
		{"this-is-far-too-long", 10, "this-is-f…"},
		{"ab", 1, "a"},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := truncate(tt.in, tt.max); got != tt.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.in, tt.max, got, tt.want)
			}
		})
	}
}
