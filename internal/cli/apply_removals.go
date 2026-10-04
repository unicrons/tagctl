package cli

import (
	"fmt"
	"io"

	"github.com/unicrons/tagctl/internal/types"
)

const (
	allowRemovalsFlag = "allow-removals"

	// maxListedRemovals caps the removals spelled out before the prompt.
	maxListedRemovals = 20
)

// withoutRemovals returns the plan apply will run and how many removals it
// leaves out: all of them unless removals are allowed.
func withoutRemovals(plan *types.Plan, allowRemovals bool) (*types.Plan, int) {
	if allowRemovals {
		return plan, 0
	}
	kept := *plan
	kept.Changes = make([]types.TagChange, 0, len(plan.Changes))
	for _, c := range plan.Changes {
		if c.Action != types.ActionRemove {
			kept.Changes = append(kept.Changes, c)
		}
	}
	return &kept, len(plan.Changes) - len(kept.Changes)
}

// printRemovalSummary ends the plan summary: the removals left out, or each
// removal about to happen, since a removal cannot be undone from the plan.
func printRemovalSummary(w io.Writer, planFile string, plan *types.Plan, skippedRemovals int) {
	if skippedRemovals > 0 {
		fmt.Fprintf(w, "  • %d removals will be SKIPPED (pass --%s to perform them)\n", skippedRemovals, allowRemovalsFlag)
	}
	listRemovals(w, planFile, plan)
	fmt.Fprintln(w)
}

func listRemovals(w io.Writer, planFile string, plan *types.Plan) {
	removed := plan.Summarize().TagsRemoved
	if removed == 0 {
		return
	}
	fmt.Fprintf(w, "  • %d tags will be REMOVED:\n", removed)
	listed := 0
	for _, c := range plan.Changes {
		if c.Action != types.ActionRemove {
			continue
		}
		if listed == maxListedRemovals {
			fmt.Fprintf(w, "      ... and %d more, see %s\n", removed-listed, planFile)
			break
		}
		fmt.Fprintf(w, "      - %s=%q on %s %s (%s)\n", printable(c.Tag), printable(c.OldValue),
			printable(c.Resource.Type), printable(c.Resource.ID), printable(string(c.Reason)))
		listed++
	}
}

// printSkippedRemovals reports the removals apply left out and how to run them.
func printSkippedRemovals(w io.Writer, skipped int) {
	if skipped == 0 {
		return
	}
	fmt.Fprintf(w, "Skipped: %d removal(s), no tag was removed. Run again with --%s to perform them.\n", skipped, allowRemovalsFlag)
}

// describeChange names the tag and what happens to it in a progress line.
func describeChange(c types.TagChange) string {
	if c.Action == types.ActionRemove {
		return "remove " + printable(c.Tag)
	}
	return printable(c.Tag) + ": " + printable(c.NewValue)
}
