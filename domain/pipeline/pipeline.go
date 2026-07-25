// Package pipeline defines the Pipeline entity: a workspace-global, reusable
// ordered set of stages that promotes the old per-campaign stage set (and
// absorbs the StageGroup template concept going forward). A Pipeline decouples
// the board from a single campaign so a coherent, stable-ID board can be
// rendered for either conversation entries or sales opportunities.
package pipeline

import (
	"errors"
	"strings"
	"time"
)

// ObjectType is the record kind a pipeline organizes.
type ObjectType string

const (
	ObjectConversation ObjectType = "conversation"
	ObjectOpportunity  ObjectType = "opportunity"
)

func (o ObjectType) Valid() bool {
	return o == ObjectConversation || o == ObjectOpportunity
}

// Pipeline is a workspace-global, ordered process (a set of stages).
type Pipeline struct {
	ID          string     `json:"id"`
	WorkspaceID string     `json:"workspaceId"`
	Name        string     `json:"name"`
	ObjectType  ObjectType `json:"objectType"`
	// StageGroupID is the template this pipeline was stamped from (empty = created
	// directly / default). Lets campaigns reusing the same group share one pipeline.
	StageGroupID string    `json:"stageGroupId,omitempty"`
	DepartmentID string    `json:"departmentId,omitempty"`
	Position     int       `json:"position"`
	IsDefault    bool      `json:"isDefault"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

var (
	ErrWorkspaceRequired = errors.New("pipeline: workspace is required")
	ErrNameRequired      = errors.New("pipeline: name is required")
	ErrInvalidObject     = errors.New("pipeline: invalid object type")
	ErrNotFound          = errors.New("pipeline: not found")
	ErrUnauthorized      = errors.New("pipeline: unauthorized access to this pipeline")
)

// Normalize applies the sensible defaults a client may omit.
func (p *Pipeline) Normalize() {
	p.Name = strings.TrimSpace(p.Name)
	if p.ObjectType == "" {
		p.ObjectType = ObjectConversation
	}
}

// Validate checks the pipeline is coherent.
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
