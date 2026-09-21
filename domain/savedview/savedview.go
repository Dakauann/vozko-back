package savedview

import (
	"errors"
	"strings"
	"time"

	"vozko/domain/crmfilter"
)

type ObjectType string

const (
	ObjectConversation ObjectType = "conversation"
	ObjectOpportunity  ObjectType = "opportunity"
	ObjectLead         ObjectType = "lead"
)

func (o ObjectType) Valid() bool {
	return o == ObjectConversation || o == ObjectOpportunity || o == ObjectLead
}

type Visibility string

const (
	VisibilityPrivate Visibility = "private"
	VisibilityShared  Visibility = "shared"
)

func (v Visibility) Valid() bool {
	return v == VisibilityPrivate || v == VisibilityShared
}

type GroupBy string

const (
	GroupByStage    GroupBy = "stage"
	GroupByLabel    GroupBy = "label"
	GroupByOwner    GroupBy = "owner"
	GroupByCarteira GroupBy = "carteira"
	GroupByCustom   GroupBy = "custom"
	GroupByNone     GroupBy = "none"
)

func (g GroupBy) Valid() bool {
	switch g {
	case GroupByStage, GroupByLabel, GroupByOwner, GroupByCarteira, GroupByCustom, GroupByNone:
		return true
	}
	return false
}

type SortDir string

const (
	SortAsc  SortDir = "asc"
	SortDesc SortDir = "desc"
)

func (s SortDir) Valid() bool {
	return s == SortAsc || s == SortDesc || s == ""
}

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

func (v *SavedView) Normalize() {
	v.Name = strings.TrimSpace(v.Name)
	if v.GroupBy == "" {
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
