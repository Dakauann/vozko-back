package pipeline

import (
	"errors"
	"strings"
	"time"
)

type ObjectType string

const (
	ObjectConversation ObjectType = "conversation"
	ObjectOpportunity  ObjectType = "opportunity"
)

func (o ObjectType) Valid() bool {
	return o == ObjectConversation || o == ObjectOpportunity
}

type Pipeline struct {
	ID           string     `json:"id"`
	WorkspaceID  string     `json:"workspaceId"`
	Name         string     `json:"name"`
	ObjectType   ObjectType `json:"objectType"`
	StageGroupID string     `json:"stageGroupId,omitempty"`
	DepartmentID string     `json:"departmentId,omitempty"`
	Position     int        `json:"position"`
	IsDefault    bool       `json:"isDefault"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
}

var (
	ErrWorkspaceRequired = errors.New("pipeline: workspace is required")
	ErrNameRequired      = errors.New("pipeline: name is required")
	ErrInvalidObject     = errors.New("pipeline: invalid object type")
	ErrNotFound          = errors.New("pipeline: not found")
	ErrUnauthorized      = errors.New("pipeline: unauthorized access to this pipeline")

	ErrDeleteDefault = errors.New("pipeline: the default funnel cannot be deleted")

	ErrDeleteBound = errors.New("pipeline: funnel is still in use")

	ErrDeleteNeedsDestination = errors.New("pipeline: funnel holds conversations; name a destination funnel")

	ErrDeleteDestinationInvalid = errors.New("pipeline: destination must be a different funnel of the same kind")

	ErrDefaultRequired = errors.New("pipeline: a workspace needs one default funnel; promote another instead")
)

type Usage struct {
	Entries       int64 `json:"entries"`
	Campaigns     int64 `json:"campaigns"`
	Channels      int64 `json:"channels"`
	Opportunities int64 `json:"opportunities"`
}

func (u Usage) Bindings() int64 {
	return u.Campaigns + u.Channels + u.Opportunities
}

func (u Usage) Deletable() bool { return u.Bindings() == 0 }

func (p *Pipeline) Normalize() {
	p.Name = strings.TrimSpace(p.Name)
	if p.ObjectType == "" {
		p.ObjectType = ObjectConversation
	}
}

func (p *Pipeline) Validate() error {
	if strings.TrimSpace(p.WorkspaceID) == "" {
		return ErrWorkspaceRequired
	}
	if strings.TrimSpace(p.Name) == "" {
		return ErrNameRequired
	}
	if !p.ObjectType.Valid() {
		return ErrInvalidObject
	}
	return nil
}
