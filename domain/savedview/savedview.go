// Package savedview defines a persisted, named board/list configuration: a
// filter (reused from crmfilter), a group-by axis, a sort and a set of visible
// columns. A SavedView is the single source of truth that both the kanban and
// the list view render from, so switching between them never changes what set
// of records is shown. Views are per-object (conversation entries or sales
// opportunities) and either private to their owner or shared with the workspace.
package savedview

import (
	"errors"
	"strings"
	"time"

	"vozko/domain/crmfilter"
)

// ObjectType is the record kind a view targets.
type ObjectType string

const (
	ObjectConversation ObjectType = "conversation"
	ObjectOpportunity  ObjectType = "opportunity"
	// ObjectLead targets the leads list. It has no board axis, so its views
	// are saved with GroupByNone: a lead view is a named segment ("bloqueados
	// com memória de objeção") plus its sort and columns.
	ObjectLead ObjectType = "lead"
)

func (o ObjectType) Valid() bool {
	return o == ObjectConversation || o == ObjectOpportunity || o == ObjectLead
}

// Visibility governs who can see a view.
type Visibility string

const (
	VisibilityPrivate Visibility = "private"
	VisibilityShared  Visibility = "shared"
)

func (v Visibility) Valid() bool {
	return v == VisibilityPrivate || v == VisibilityShared
}

// GroupBy is the board's column axis. GroupByLabel is what delivers the
// "global per label kanban" users asked for; GroupByOwner gives per rep
// swimlanes; GroupByStage (default) is the classic pipeline board.
type GroupBy string

const (
	GroupByStage    GroupBy = "stage"
	GroupByLabel    GroupBy = "label"
	GroupByOwner    GroupBy = "owner"
	GroupByCarteira GroupBy = "carteira"
	GroupByCustom   GroupBy = "custom" // uses GroupByKey to name a single-select custom field
	GroupByNone     GroupBy = "none"   // list-only view, no columns
)

func (g GroupBy) Valid() bool {
	switch g {
	case GroupByStage, GroupByLabel, GroupByOwner, GroupByCarteira, GroupByCustom, GroupByNone:
		return true
	}
	return false
}

// SortDir is the sort direction. Empty is treated as descending.
type SortDir string

const (
	SortAsc  SortDir = "asc"
	SortDesc SortDir = "desc"
)

func (s SortDir) Valid() bool {
	return s == SortAsc || s == SortDesc || s == ""
}

// SavedView is a named, reusable board/list configuration.
type SavedView struct {
	ID          string           `json:"id"`
	WorkspaceID string           `json:"workspaceId"`
	OwnerID     string           `json:"ownerId"`
	ObjectType  ObjectType       `json:"objectType"`
	PipelineID  string           `json:"pipelineId,omitempty"`
	Name        string           `json:"name"`
	Filter      crmfilter.Filter `json:"filter"`
	GroupBy     GroupBy          `json:"groupBy"`
	GroupByKey  string           `json:"groupByKey,omitempty"`
	SortField   string           `json:"sortField,omitempty"`
	SortDir     SortDir          `json:"sortDir,omitempty"`
	Columns     []string         `json:"columns,omitempty"`
	Visibility  Visibility       `json:"visibility"`
	IsDefault   bool             `json:"isDefault"`
	Position    int              `json:"position"`
	CreatedAt   time.Time        `json:"createdAt"`
	UpdatedAt   time.Time        `json:"updatedAt"`
}

var (
	ErrWorkspaceRequired = errors.New("savedview: workspace is required")
	ErrNameRequired      = errors.New("savedview: name is required")
	ErrInvalidObject     = errors.New("savedview: invalid object type")
	ErrInvalidGroupBy    = errors.New("savedview: invalid group_by")
	ErrGroupByKeyMissing = errors.New("savedview: group_by=custom requires a group_by key")
	ErrInvalidVisibility = errors.New("savedview: invalid visibility")
	ErrInvalidSortDir    = errors.New("savedview: invalid sort direction")
	ErrNotFound          = errors.New("savedview: not found")
	ErrUnauthorized      = errors.New("savedview: unauthorized access to this view")
)

// Normalize applies the sensible defaults a client may omit.
func (v *SavedView) Normalize() {
	v.Name = strings.TrimSpace(v.Name)
	if v.GroupBy == "" {
		// A lead has no pipeline to be grouped along, so its default axis is
		// "no columns"; every other object keeps the classic stage board.
		if v.ObjectType == ObjectLead {
			v.GroupBy = GroupByNone
		} else {
			v.GroupBy = GroupByStage
		}
	}
	if v.Visibility == "" {
		v.Visibility = VisibilityPrivate
	}
	if v.SortDir == "" {
		v.SortDir = SortDesc
	}
}

// Validate checks the view is coherent, including its embedded filter.
func (v *SavedView) Validate() error {
	if strings.TrimSpace(v.WorkspaceID) == "" {
		return ErrWorkspaceRequired
	}
	if strings.TrimSpace(v.Name) == "" {
		return ErrNameRequired
	}
	if !v.ObjectType.Valid() {
		return ErrInvalidObject
	}
	if !v.GroupBy.Valid() {
		return ErrInvalidGroupBy
	}
	if v.GroupBy == GroupByCustom && strings.TrimSpace(v.GroupByKey) == "" {
		return ErrGroupByKeyMissing
	}
	if !v.Visibility.Valid() {
		return ErrInvalidVisibility
	}
	if !v.SortDir.Valid() {
		return ErrInvalidSortDir
	}
	return v.Filter.Validate()
}
