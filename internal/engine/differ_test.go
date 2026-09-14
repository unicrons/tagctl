package engine

import (
	"testing"
	"time"

	"github.com/unicrons/tagctl/internal/types"
)

// testAccount is the account used by findings that do not exercise
// multi-account behaviour.
const testAccount = "111"

// tagOwner is the tag most of these tests check.
const tagOwner = "owner"

// tagEnvironment is the second tag these tests use.
const tagEnvironment = "environment"

// valueProduction and valueProd are the spellings the normalizer tests compare.
const (
	valueProduction = "Production"
	valueProd       = "prod"
)

// failing builds a FAILED finding for a resource/tag pair.
func failing(account, resourceID, tag string) types.Finding {
	return types.Finding{
		Resource: types.Resource{
			ID:       resourceID,
			Type:     "aws_instance",
			Account:  account,
			Provider: "aws",
		},
		Tag:    tag,
		Status: types.StatusFailed,
		Reason: types.ReasonMissing,
	}
}

// passing builds a PASS finding for the owner tag in the default test account.
func passing(resourceID string) types.Finding {
	f := failing(testAccount, resourceID, tagOwner)
	f.Status = types.StatusPass
	f.Reason = types.ReasonCompliant
	return f
}

func scanWith(findings ...types.Finding) *types.ScanResult {
	return &types.ScanResult{
		ScannedAt: time.Now(),
		Findings:  findings,
		ByTag:     map[string]*types.TagStats{},
		ByAccount: map[string]*types.AccountStats{},
	}
}

func TestDiff_DetectsRegressions(t *testing.T) {
	baseline := scanWith(
		passing("i-1"),
		failing(testAccount, "i-2", tagOwner),
	)
	current := scanWith(
		failing(testAccount, "i-1", tagOwner), // regressed
		failing(testAccount, "i-2", tagOwner), // unchanged
	)

	diff := Diff(baseline, current)

	if len(diff.Regressions) != 1 {
		t.Fatalf("got %d regressions, want 1", len(diff.Regressions))
	}
	if diff.Regressions[0].Resource.ID != "i-1" {
		t.Errorf("regression is %q, want i-1", diff.Regressions[0].Resource.ID)
	}
	if diff.Unchanged != 1 {
		t.Errorf("Unchanged = %d, want 1", diff.Unchanged)
	}
	if len(diff.Resolved) != 0 {
		t.Errorf("got %d resolved, want 0", len(diff.Resolved))
	}
	if !diff.HasRegressions() {
		t.Error("HasRegressions() = false, want true")
	}
}

func TestDiff_DetectsResolved(t *testing.T) {
	baseline := scanWith(
		failing(testAccount, "i-1", tagOwner),
		failing(testAccount, "i-1", tagEnvironment),
	)
	current := scanWith(
		passing("i-1"),
		failing(testAccount, "i-1", tagEnvironment),
	)

	diff := Diff(baseline, current)

	if len(diff.Resolved) != 1 {
		t.Fatalf("got %d resolved, want 1", len(diff.Resolved))
	}
	if diff.Resolved[0].Tag != tagOwner {
		t.Errorf("resolved tag = %q, want owner", diff.Resolved[0].Tag)
	}
	if len(diff.Regressions) != 0 {
		t.Errorf("got %d regressions, want 0", len(diff.Regressions))
	}
	if diff.HasRegressions() {
		t.Error("HasRegressions() = true, want false")
	}
}

// The same resource failing two tags must produce two independent findings.
func TestDiff_TracksEachTagSeparately(t *testing.T) {
	baseline := scanWith(failing(testAccount, "i-1", tagOwner))
	current := scanWith(
		failing(testAccount, "i-1", tagOwner),
		failing(testAccount, "i-1", tagEnvironment),
	)

	diff := Diff(baseline, current)

	if len(diff.Regressions) != 1 {
		t.Fatalf("got %d regressions, want 1", len(diff.Regressions))
	}
	if diff.Regressions[0].Tag != tagEnvironment {
		t.Errorf("regression tag = %q, want environment", diff.Regressions[0].Tag)
	}
	if diff.Unchanged != 1 {
		t.Errorf("Unchanged = %d, want 1", diff.Unchanged)
	}
}

// The same resource ID in two different accounts is two different resources.
func TestDiff_SeparatesAccounts(t *testing.T) {
	baseline := scanWith(failing(testAccount, "i-1", tagOwner))
	current := scanWith(
		failing(testAccount, "i-1", tagOwner),
		failing("222", "i-1", tagOwner),
	)

	diff := Diff(baseline, current)

	if len(diff.Regressions) != 1 {
		t.Fatalf("got %d regressions, want 1", len(diff.Regressions))
	}
	if diff.Regressions[0].Resource.Account != "222" {
		t.Errorf("regression account = %q, want 222", diff.Regressions[0].Resource.Account)
	}
}

func TestDiff_NewAndRemovedResources(t *testing.T) {
	baseline := scanWith(
		passing("i-old"),
		passing("i-kept"),
	)
	current := scanWith(
		passing("i-kept"),
		passing("i-new"),
	)

	diff := Diff(baseline, current)

	if len(diff.NewResources) != 1 || diff.NewResources[0].ID != "i-new" {
		t.Errorf("NewResources = %+v, want just i-new", diff.NewResources)
	}
	if len(diff.RemovedResources) != 1 || diff.RemovedResources[0].ID != "i-old" {
		t.Errorf("RemovedResources = %+v, want just i-old", diff.RemovedResources)
	}
}

// An empty policy produces no findings; the inventory still names the resources.
func TestDiff_NewAndRemovedResourcesWithoutFindings(t *testing.T) {
	inventory := func(ids ...string) *types.ScanResult {
		scan := scanWith()
		for _, id := range ids {
			resource := failing(testAccount, id, tagOwner).Resource
			scan.Resources = append(scan.Resources, resource.Ref())
		}
		scan.TotalResources = len(ids)
		return scan
	}

	diff := Diff(inventory("i-old", "i-kept"), inventory("i-kept", "i-new"))

	if len(diff.NewResources) != 1 || diff.NewResources[0].ID != "i-new" {
		t.Errorf("NewResources = %+v, want just i-new", diff.NewResources)
	}
	if len(diff.RemovedResources) != 1 || diff.RemovedResources[0].ID != "i-old" {
		t.Errorf("RemovedResources = %+v, want just i-old", diff.RemovedResources)
	}
	if diff.IsClean() {
		t.Error("IsClean() = true, want false when resources moved")
	}
}

// A baseline written before scans kept an inventory still names its resources
// through findings, so they must not show up as new.
func TestDiff_InventoryMatchesResourcesFromFindings(t *testing.T) {
	baseline := scanWith(failing(testAccount, "i-1", tagOwner))
	current := scanWith(failing(testAccount, "i-1", tagOwner))
	for _, id := range []string{"i-1", "i-2"} {
		resource := failing(testAccount, id, tagOwner).Resource
		current.Resources = append(current.Resources, resource.Ref())
	}

	diff := Diff(baseline, current)

	if len(diff.NewResources) != 1 || diff.NewResources[0].ID != "i-2" {
		t.Errorf("NewResources = %+v, want just i-2", diff.NewResources)
	}
	if len(diff.RemovedResources) != 0 {
		t.Errorf("RemovedResources = %+v, want none", diff.RemovedResources)
	}
}

// The same ID in two regions is two resources, listed in a stable order.
func TestDiff_NewResourcesAreSortedAcrossRegions(t *testing.T) {
	current := scanWith()
	for _, region := range []string{"us-east-1", "eu-west-1", "ap-south-1"} {
		resource := failing(testAccount, "/aws/lambda/fn", tagOwner).Resource
		resource.Region = region
		current.Resources = append(current.Resources, resource.Ref())
	}

	for i := 0; i < 5; i++ {
		diff := Diff(scanWith(), current)
		got := make([]string, 0, len(diff.NewResources))
		for _, r := range diff.NewResources {
			got = append(got, r.Region)
		}
		want := []string{"ap-south-1", "eu-west-1", "us-east-1"}
		if len(got) != len(want) {
			t.Fatalf("run %d: new resource regions = %v, want %v", i, got, want)
		}
		for j := range want {
			if got[j] != want[j] {
				t.Fatalf("run %d: new resource regions = %v, want %v", i, got, want)
			}
		}
	}
}

func TestDiff_CompliancePctDelta(t *testing.T) {
	baseline := scanWith()
	baseline.CompliancePct = 88.0
	current := scanWith()
	current.CompliancePct = 62.5

	diff := Diff(baseline, current)

	if got := diff.CompliancePctDelta(); got != -25.5 {
		t.Errorf("CompliancePctDelta() = %v, want -25.5", got)
	}
}

func TestDiff_IdenticalScansAreClean(t *testing.T) {
	findings := []types.Finding{
		failing(testAccount, "i-1", tagOwner),
		passing("i-2"),
	}
	diff := Diff(scanWith(findings...), scanWith(findings...))

	if !diff.IsClean() {
		t.Errorf("IsClean() = false for identical scans; regressions=%d resolved=%d new=%d removed=%d",
			len(diff.Regressions), len(diff.Resolved), len(diff.NewResources), len(diff.RemovedResources))
	}
	if diff.Unchanged != 1 {
		t.Errorf("Unchanged = %d, want 1", diff.Unchanged)
	}
}

func TestDiff_TagDeltas(t *testing.T) {
	baseline := scanWith()
	baseline.ByTag = map[string]*types.TagStats{
		tagOwner:       {Tag: tagOwner, CompliancePct: 90, Missing: 5, Invalid: 1},
		"legacy-tag":   {Tag: "legacy-tag", CompliancePct: 50, Missing: 10},
		tagEnvironment: {Tag: tagEnvironment, CompliancePct: 70, Missing: 3},
	}
	current := scanWith()
	current.ByTag = map[string]*types.TagStats{
		tagOwner:       {Tag: tagOwner, CompliancePct: 95, Missing: 2, Invalid: 1},
		tagEnvironment: {Tag: tagEnvironment, CompliancePct: 60, Missing: 8},
		"new-tag":      {Tag: "new-tag", CompliancePct: 20, Missing: 40},
	}

	diff := Diff(baseline, current)

	owner := diff.ByTag[tagOwner]
	if owner == nil || owner.Delta() != 5 {
		t.Errorf("owner delta = %+v, want +5", owner)
	}
	if owner.FailedBefore != 6 || owner.FailedAfter != 3 {
		t.Errorf("owner failed before/after = %d/%d, want 6/3", owner.FailedBefore, owner.FailedAfter)
	}

	env := diff.ByTag[tagEnvironment]
	if env == nil || env.Delta() != -10 {
		t.Errorf("environment delta = %+v, want -10", env)
	}

	// A tag only in the baseline is still reported, with no "after" value.
	legacy := diff.ByTag["legacy-tag"]
	if legacy == nil || legacy.CompliancePctAfter != 0 || legacy.CompliancePctBefore != 50 {
		t.Errorf("legacy-tag = %+v, want before=50 after=0", legacy)
	}

	// A tag only in the current scan is reported too.
	newTag := diff.ByTag["new-tag"]
	if newTag == nil || newTag.CompliancePctBefore != 0 || newTag.CompliancePctAfter != 20 {
		t.Errorf("new-tag = %+v, want before=0 after=20", newTag)
	}
}

func TestDiff_AccountDeltas(t *testing.T) {
	baseline := scanWith()
	baseline.ByAccount = map[string]*types.AccountStats{
		"111": {Account: "111", CompliancePct: 88, Total: 100},
		"999": {Account: "999", CompliancePct: 100, Total: 5},
	}
	current := scanWith()
	current.ByAccount = map[string]*types.AccountStats{
		"111": {Account: "111", CompliancePct: 62, Total: 140},
		"222": {Account: "222", CompliancePct: 30, Total: 20},
	}

	diff := Diff(baseline, current)

	if got := diff.ByAccount["111"].Delta(); got != -26 {
		t.Errorf("account 111 delta = %v, want -26", got)
	}
	if diff.ByAccount["111"].TotalBefore != 100 || diff.ByAccount["111"].TotalAfter != 140 {
		t.Errorf("account 111 totals = %d/%d, want 100/140",
			diff.ByAccount["111"].TotalBefore, diff.ByAccount["111"].TotalAfter)
	}
	if diff.ByAccount["222"].TotalBefore != 0 {
		t.Errorf("new account 222 TotalBefore = %d, want 0", diff.ByAccount["222"].TotalBefore)
	}
	if diff.ByAccount["999"].TotalAfter != 0 {
		t.Errorf("removed account 999 TotalAfter = %d, want 0", diff.ByAccount["999"].TotalAfter)
	}
}

// Scans written by older versions carry failures under Violations.
func TestDiff_ReadsLegacyViolationsField(t *testing.T) {
	baseline := &types.ScanResult{
		Violations: []types.Violation{
			{
				Resource: types.Resource{ID: "i-1", Account: "111", Provider: "aws"},
				Tag:      tagOwner,
				Status:   types.StatusFailed,
				Reason:   types.ReasonMissing,
			},
		},
		ByTag:     map[string]*types.TagStats{},
		ByAccount: map[string]*types.AccountStats{},
	}
	current := scanWith(passing("i-1"))

	diff := Diff(baseline, current)

	if len(diff.Resolved) != 1 {
		t.Fatalf("got %d resolved from legacy violations, want 1", len(diff.Resolved))
	}
	if diff.Resolved[0].Tag != tagOwner {
		t.Errorf("resolved tag = %q, want owner", diff.Resolved[0].Tag)
	}
}

// Output order must not depend on map iteration order.
func TestDiff_RegressionsAreSorted(t *testing.T) {
	baseline := scanWith()
	current := scanWith(
		failing("222", "i-9", tagOwner),
		failing(testAccount, "i-5", "zone"),
		failing(testAccount, "i-5", tagOwner),
		failing(testAccount, "i-1", tagOwner),
	)

	for i := 0; i < 5; i++ {
		diff := Diff(baseline, current)
		got := make([]string, 0, len(diff.Regressions))
		for _, r := range diff.Regressions {
			got = append(got, r.Resource.Account+"/"+r.Resource.ID+"/"+r.Tag)
		}
		want := []string{"111/i-1/owner", "111/i-5/owner", "111/i-5/zone", "222/i-9/owner"}
		for j := range want {
			if got[j] != want[j] {
				t.Fatalf("run %d: order = %v, want %v", i, got, want)
			}
		}
	}
}

// The same ID in two regions is two resources; a fix in one region must not
// hide a regression in the other.
func TestDiff_SeparatesRegions(t *testing.T) {
	inRegion := func(region string, f types.Finding) types.Finding {
		f.Resource.Region = region
		return f
	}
	before := scanWith(
		inRegion("eu-west-1", failing(testAccount, "/aws/lambda/fn", tagOwner)),
		inRegion("us-east-1", passing("/aws/lambda/fn")),
	)
	after := scanWith(
		inRegion("eu-west-1", passing("/aws/lambda/fn")),
		inRegion("us-east-1", failing(testAccount, "/aws/lambda/fn", tagOwner)),
	)

	diff := Diff(before, after)

	if len(diff.Regressions) != 1 || diff.Regressions[0].Resource.Region != "us-east-1" {
		t.Errorf("regressions = %+v, want the us-east-1 copy", diff.Regressions)
	}
	if len(diff.Resolved) != 1 || diff.Resolved[0].Resource.Region != "eu-west-1" {
		t.Errorf("resolved = %+v, want the eu-west-1 copy", diff.Resolved)
	}
}
