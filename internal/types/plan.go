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
}

// IsEmpty returns true if the plan has no changes.
func (p *Plan) IsEmpty() bool {
	return len(p.Changes) == 0
}
