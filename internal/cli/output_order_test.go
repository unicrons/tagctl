package cli

import (
	"fmt"
	"strings"
	"testing"

	"github.com/unicrons/tagctl/internal/types"
)

func assertInOrder(t *testing.T, out string, want []string) {
	t.Helper()
	last := -1
	for _, text := range want {
		at := strings.Index(out, text)
		if at < 0 {
			t.Fatalf("output lacks %q:\n%s", text, out)
		}
		if at < last {
			t.Fatalf("%q is printed out of order:\n%s", text, out)
		}
		last = at
	}
}

func TestOutputScanTable_ListsAccountsAndTagsInStableOrder(t *testing.T) {
	scan := &types.ScanResult{
		ByAccount: map[string]*types.AccountStats{},
		ByTag:     map[string]*types.TagStats{},
	}
	var accounts, tags []string
	for i := range 12 {
		account, tag := fmt.Sprintf("account-%02d", i), fmt.Sprintf("tag-%02d", i)
		scan.ByAccount["aws/"+account] = &types.AccountStats{Provider: "aws", Account: account, Total: 1}
		scan.ByTag[tag] = &types.TagStats{Tag: tag, Required: true}
		accounts, tags = append(accounts, "aws/"+account), append(tags, tag)
	}

	out := captureStdout(t, func() { _ = outputScanTable(scan, false) })

	assertInOrder(t, out, accounts)
	assertInOrder(t, out, tags)
}

func TestOutputPlanTable_ListsResourcesInPlanOrder(t *testing.T) {
	plan := &types.Plan{}
	var names []string
	for i := range 12 {
		name := fmt.Sprintf("resource-%02d", 11-i)
		plan.Changes = append(plan.Changes, types.TagChange{
			Resource: types.Resource{ID: name, Name: name, Type: "aws_instance", Provider: "aws"},
			Tag:      "owner", Action: types.ActionAdd, NewValue: "team", Reason: types.ReasonDefault,
		})
		names = append(names, name)
	}

	out := captureStdout(t, func() { outputPlanTable(plan) })

	assertInOrder(t, out, names)
}
