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

	// The delete guard. Deleting a funnel used to be a bare repository call, which
	// meant the default funnel, a funnel a campaign was actively routing into, and
	// a funnel holding live conversations were all one click from disappearing —
	// and the conversations kept an entry_stages row pointing at a stage that no
	// longer existed, so they vanished from every board at once.
	//
	// Each of these is a REFUSAL the UI can act on, not a generic 500: the message
	// names what blocks the delete, and the accompanying Usage says how much.

	// ErrDeleteDefault guards the workspace default. Something has to receive a
	// conversation that names no funnel, so the default is the one funnel that
	// cannot be removed. Promote another funnel to default first.
	ErrDeleteDefault = errors.New("pipeline: the default funnel cannot be deleted")

	// ErrDeleteBound means a campaign, channel or opportunity still routes into
	// this funnel. Unlinking is the operator's decision, not a cascade to infer:
	// silently repointing a running campaign's funnel changes where every new
	// conversation lands.
	ErrDeleteBound = errors.New("pipeline: funnel is still in use")

	// ErrDeleteNeedsDestination means the funnel holds conversations and no
	// destination was named. Conversations are never dropped with the funnel.
	ErrDeleteNeedsDestination = errors.New("pipeline: funnel holds conversations; name a destination funnel")

	// ErrDeleteDestinationInvalid means the named destination is the funnel being
	// deleted, or organizes a different object kind (moving a conversation onto a
	// sales stage is not a move, it is corruption).
	ErrDeleteDestinationInvalid = errors.New("pipeline: destination must be a different funnel of the same kind")

	// ErrDefaultRequired refuses to clear the flag on the last default funnel.
	//
	// A workspace with no default is not a neutral state: the stage repository
	// self-heals by MINTING one on the next stage read, which is another funnel
	// nobody asked for and, being default, another one that cannot be deleted.
	// Promoting a different funnel is the way to change the default; there is
	// deliberately no way to have none.
	ErrDefaultRequired = errors.New("pipeline: a workspace needs one default funnel; promote another instead")
)

// Usage is what still points at a funnel, counted per binding.
//
// It is split by kind rather than summed because the answer changes what the
// operator does next: conversations are MOVED, everything else is UNLINKED at
// its own source. A single "in use" count would tell them they are blocked
// without telling them where to go.
type Usage struct {
	// Entries counts conversations currently sitting on this funnel's stages.
	Entries int64 `json:"entries"`
	// Campaigns counts WhatsApp campaigns, official and unofficial, routing here.
	Campaigns int64 `json:"campaigns"`
	// Channels counts connected accounts and numbers routing here.
	Channels int64 `json:"channels"`
	// Opportunities counts deals on this funnel (sales funnels only).
	Opportunities int64 `json:"opportunities"`
}

// Bindings counts everything that must be unlinked at its own source before the
// funnel can go. Conversations are excluded on purpose: they are movable, and
// naming a destination is how the operator moves them.
func (u Usage) Bindings() int64 {
	return u.Campaigns + u.Channels + u.Opportunities
}

// Deletable reports whether this funnel can be removed at all, ignoring the
// destination question. A bound funnel is blocked no matter what is named.
func (u Usage) Deletable() bool { return u.Bindings() == 0 }

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
