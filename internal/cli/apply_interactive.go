package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/unicrons/tagctl/internal/types"
)

// Where apply reads answers from and how it detects a terminal; tests replace both.
var (
	applyInput      io.Reader = os.Stdin
	stdinIsTerminal           = func() bool { return term.IsTerminal(int(os.Stdin.Fd())) }
)

type reviewAnswer int

const (
	answerYes reviewAnswer = iota
	answerNo
	answerAll
	answerQuit
)

// resourceChanges are the changes a plan holds for one resource.
type resourceChanges struct {
	resource types.Resource
	changes  []types.TagChange
}

// planReview is the outcome of an interactive review, in plan order.
type planReview struct {
	approved []resourceChanges
	skipped  []resourceChanges
}

func (r *planReview) skippedChanges() int {
	return countChanges(r.skipped)
}

func countChanges(groups []resourceChanges) int {
	total := 0
	for _, g := range groups {
		total += len(g.changes)
	}
	return total
}

// approvedPlan returns a copy of plan holding only the approved changes.
func (r *planReview) approvedPlan(plan *types.Plan) *types.Plan {
	approved := make(map[string]bool, len(r.approved))
	for _, g := range r.approved {
		approved[g.resource.Identity()] = true
	}

	filtered := &types.Plan{ID: plan.ID, CreatedAt: plan.CreatedAt}
	for _, c := range plan.Changes {
		if approved[c.Resource.Identity()] {
			filtered.Changes = append(filtered.Changes, c)
		}
	}
	filtered.Summary = filtered.Summarize()
	return filtered
}

// groupChangesByIdentity groups changes per resource, keeping the order in
// which each resource first appears in the plan.
func groupChangesByIdentity(changes []types.TagChange) []resourceChanges {
	index := make(map[string]int)
	var groups []resourceChanges
	for _, c := range changes {
		key := c.Resource.Identity()
		i, ok := index[key]
		if !ok {
			i = len(groups)
			index[key] = i
			groups = append(groups, resourceChanges{resource: c.Resource})
		}
		groups[i].changes = append(groups[i].changes, c)
	}
	return groups
}

// reviewChanges asks about every resource in the plan and returns what was
// approved and skipped. It writes nothing to the cloud.
func reviewChanges(ctx context.Context, out io.Writer, in io.Reader, plan *types.Plan) (*planReview, error) {
	groups := groupChangesByIdentity(plan.Changes)
	lines := bufio.NewReader(in)
	review := &planReview{}

	for i, group := range groups {
		printResourceChanges(out, i+1, len(groups), group)

		answer, err := askReviewAnswer(ctx, out, lines)
		if err != nil {
			return nil, err
		}
		switch answer {
		case answerYes:
			review.approved = append(review.approved, group)
		case answerNo:
			review.skipped = append(review.skipped, group)
		case answerAll:
			review.approved = append(review.approved, groups[i:]...)
			return review, nil
		case answerQuit:
			review.skipped = append(review.skipped, groups[i:]...)
			return review, nil
		}
	}
	return review, nil
}

func resourceLabel(r types.Resource) string {
	label := fmt.Sprintf("%s %s", printable(r.Type), printable(r.DisplayName()))
	if identity := r.Identity(); identity != r.DisplayName() {
		label += fmt.Sprintf(" (%s)", printable(identity))
	}
	return label
}

func printResourceChanges(out io.Writer, position, total int, group resourceChanges) {
	fmt.Fprintf(out, "\n[%d/%d] %s\n", position, total, resourceLabel(group.resource))
	for _, c := range group.changes {
		switch c.Action {
		case types.ActionUpdate:
			fmt.Fprintf(out, "  ~ %s: %q -> %q\n", printable(c.Tag), printable(c.OldValue), printable(c.NewValue))
		case types.ActionRemove:
			fmt.Fprintf(out, "  - %s: %q\n", printable(c.Tag), printable(c.OldValue))
		default:
			fmt.Fprintf(out, "  + %s: %q\n", printable(c.Tag), printable(c.NewValue))
		}
	}
}

// askReviewAnswer repeats the prompt until the answer is one it knows. Input
// that ends first is an error, so a truncated pipe approves nothing.
func askReviewAnswer(ctx context.Context, out io.Writer, lines *bufio.Reader) (reviewAnswer, error) {
	for {
		fmt.Fprint(out, "Apply these changes? [y]es / [n]o / [a]ll remaining / [q]uit: ")

		line, err := readLine(ctx, lines)
		if errors.Is(err, io.EOF) && strings.TrimSpace(line) == "" {
			return 0, errors.New("input ended before every resource was answered: nothing was applied")
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return 0, err
		}

		switch strings.ToLower(strings.TrimSpace(line)) {
		case "y", "yes":
			return answerYes, nil
		case "n", "no":
			return answerNo, nil
		case "a", "all":
			return answerAll, nil
		case "q", "quit":
			return answerQuit, nil
		}
		fmt.Fprintln(out, "Answer y, n, a or q.")
	}
}

// readLine returns as soon as ctx is cancelled, so Ctrl-C at a prompt does
// not wait for Enter.
func readLine(ctx context.Context, lines *bufio.Reader) (string, error) {
	type answer struct {
		line string
		err  error
	}
	answers := make(chan answer, 1)
	go func() {
		line, err := lines.ReadString('\n')
		answers <- answer{line: line, err: err}
	}()

	select {
	case <-ctx.Done():
		return "", fmt.Errorf("apply cancelled: %w", ctx.Err())
	case got := <-answers:
		if got.err != nil && !errors.Is(got.err, io.EOF) {
			return "", fmt.Errorf("failed to read response: %w", got.err)
		}
		return got.line, got.err
	}
}

func printReviewSummary(out io.Writer, review *planReview) {
	fmt.Fprintf(out, "\nReview: %d resource(s) approved (%d changes), %d skipped (%d changes)\n",
		len(review.approved), countChanges(review.approved), len(review.skipped), review.skippedChanges())
	if len(review.skipped) == 0 {
		return
	}
	fmt.Fprintln(out, "Skipped:")
	for _, group := range review.skipped {
		tags := make([]string, len(group.changes))
		for i, c := range group.changes {
			tags[i] = printable(c.Tag)
		}
		fmt.Fprintf(out, "  • %s: %s\n", resourceLabel(group.resource), strings.Join(tags, ", "))
	}
}
