package types

import "time"

// ChangeAction describes what action to take on a tag.
type ChangeAction string

const (
	// ActionAdd indicates a new tag will be added.
	ActionAdd ChangeAction = "add"

	// ActionUpdate indicates an existing tag will be modified.
	ActionUpdate ChangeAction = "update"

	// ActionRemove indicates a tag will be removed.
	ActionRemove ChangeAction = "remove"
)

// ChangeReason describes why the change is being made.
type ChangeReason string

const (
	// ReasonInferred indicates the value was inferred from naming convention.
	ReasonInferred ChangeReason = "inferred"

	// ReasonInherited indicates the value was inherited from parent resource.
	ReasonInherited ChangeReason = "inherited"

	// ReasonDefault indicates a default value was applied.
	ReasonDefault ChangeReason = "default"

	// ReasonManual indicates the change was manually specified.
	ReasonManual ChangeReason = "manual"

	// ReasonRenamed indicates the tag key is being renamed.
	ReasonRenamed ChangeReason = "renamed"

	// ReasonForbiddenTag indicates the policy forbids the tag being removed.
	ReasonForbiddenTag ChangeReason = "forbidden"
)

// TagChange represents a planned tag modification.
type TagChange struct {
	// Resource is the resource to modify.
	Resource Resource `json:"resource"`

	// Tag is the name of the tag to modify.
	Tag string `json:"tag"`

	// Action is what to do (add, update, remove).
	Action ChangeAction `json:"action"`

	// OldValue is the current value (for update/remove).
	OldValue string `json:"old_value,omitempty"`

	// NewValue is the new value (for add/update).
	NewValue string `json:"new_value,omitempty"`

	// Reason explains why this change is being made.
	Reason ChangeReason `json:"reason"`

	// Source provides context about where the value came from.
	Source string `json:"source,omitempty"`
}

// Plan represents a set of planned tag changes.
type Plan struct {
	// ID is a unique identifier for this plan.
	ID string `json:"id"`

	// CreatedAt is when the plan was generated.
	CreatedAt time.Time `json:"created_at"`

	// Changes is the list of tag changes to apply.
	Changes []TagChange `json:"changes"`

	// Summary contains aggregate statistics.
	Summary PlanSummary `json:"summary"`

	// Warnings explains rules the planner could not apply.
	Warnings []string `json:"warnings,omitempty"`

	// Conflicts lists the renames skipped because the target key already
	// holds a different value.
	Conflicts []RenameConflict `json:"conflicts,omitempty"`
}

// RenameConflict is a rename the planner refused to plan.
type RenameConflict struct {
	// Resource is the resource carrying both keys.
	Resource Resource `json:"resource"`

	// From is the key that was to be renamed and Value what it holds.
	From  string `json:"from"`
	Value string `json:"value"`

	// To is the target key and ExistingValue what it already holds.
	To            string `json:"to"`
	ExistingValue string `json:"existing_value"`
}

// Message returns a human-readable description of the conflict.
func (c *RenameConflict) Message() string {
	return "cannot rename '" + c.From + "' to '" + c.To + "': '" + c.To + "' is already '" +
		c.ExistingValue + "', not '" + c.Value + "'"
}

// PlanSummary contains aggregate plan statistics.
type PlanSummary struct {
	// TotalResources is the number of resources affected.
	TotalResources int `json:"total_resources"`

	// TotalChanges is the total number of tag changes.
	TotalChanges int `json:"total_changes"`

	// TagsAdded is the number of tags being added.
	TagsAdded int `json:"tags_added"`

	// TagsUpdated is the number of tags being updated.
	TagsUpdated int `json:"tags_updated"`

	// TagsRemoved is the number of tags being removed.
	TagsRemoved int `json:"tags_removed"`

	// Conflicts is the number of renames skipped as conflicts.
	Conflicts int `json:"conflicts,omitempty"`
}

// IsEmpty returns true if the plan has no changes.
func (p *Plan) IsEmpty() bool {
	return len(p.Changes) == 0
}

// Summarize recounts the summary from the changes, ignoring whatever the
// plan file claims.
func (p *Plan) Summarize() PlanSummary {
	resources := make(map[string]bool, len(p.Changes))
	summary := PlanSummary{TotalChanges: len(p.Changes), Conflicts: len(p.Conflicts)}
	for _, c := range p.Changes {
		resources[c.Resource.Identity()] = true
		switch c.Action {
		case ActionAdd:
			summary.TagsAdded++
		case ActionUpdate:
			summary.TagsUpdated++
		case ActionRemove:
			summary.TagsRemoved++
		}
	}
	summary.TotalResources = len(resources)
	return summary
}
