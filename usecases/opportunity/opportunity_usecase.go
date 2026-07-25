// Package opportunity_usecase implements the sales-deal lifecycle: create
// (standalone or from a conversation), update, delete, get, move stage (with
// won/lost + required lost reason enforced via the entity), and link/unlink
// conversations. It validates typed CustomFields against the workspace's custom
// field definitions. It depends only on the domain ports (opportunity.Repository,
// opportunity.LinkRepository, customfield.Repository).
//
// MONEY GUARDRAIL: ValueCents is the customer's own sales amount; it is never
// routed through Vozko wallet money. This layer treats it as a plain integer.
package opportunity_usecase

import (
	"errors"
	"time"

	"github.com/google/uuid"

	"vozko/domain/customfield"
	"vozko/domain/opportunity"
)

// objectType is the custom-field object discriminator for opportunities.
const objectType = "opportunity"

var (
	// ErrUnknownCustomField is returned when CustomFields carries a key with no
	// matching definition in the workspace.
	ErrUnknownCustomField = errors.New("opportunity: unknown custom field")
	// ErrEntryTypeRequired is returned when a link request omits the entry type.
	ErrEntryTypeRequired = errors.New("opportunity: entry id and type are required for a link")
)

// Service is the opportunity usecase surface.
type Service struct {
	repo   opportunity.Repository
	links  opportunity.LinkRepository
	fields customfield.Repository
}

// NewService wires the opportunity usecases from the domain ports. links and
// fields may be nil in contexts that do not need linking or custom fields.
func NewService(repo opportunity.Repository, links opportunity.LinkRepository, fields customfield.Repository) *Service {
	return &Service{repo: repo, links: links, fields: fields}
}

// CreateInput is the payload to open a deal. When OwnerID is empty and
// ConversationAssigneeID is set (create-from-conversation), the owner is seeded
// from the conversation assignee. When LinkEntryID/LinkEntryType are set, the
// conversation is linked to the new deal.
type CreateInput struct {
	LeadID       string
	PipelineID   string
	StageID      string
	OwnerID      string
	CarteiraID   string
	Title        string
	ValueCents   int64
	Currency     string
	Status       opportunity.Status // optional; defaults to open when empty
	LostReasonID string             // required by the entity when Status is lost
	Source       string
	CloseDate    *time.Time
	CustomFields map[string]any

	ConversationAssigneeID string
	LinkEntryID            string
	LinkEntryType          string

	// CreatorUserID is the authenticated user opening the deal. It becomes the
	// owner when no explicit owner (or conversation assignee) is given, so the
	// creator is the responsável by default.
	CreatorUserID string
}

// buildOpportunity constructs a normalized (not yet validated) entity from the
// create input. Shared by Create and ValidateCreate so both agree on defaults
// (owner seeding, default-open status).
func (s *Service) buildOpportunity(workspaceID string, in CreateInput) *opportunity.Opportunity {
	ownerID := in.OwnerID
	if ownerID == "" && in.ConversationAssigneeID != "" {
		ownerID = in.ConversationAssigneeID
	}
	if ownerID == "" {
		// Default the responsável to whoever is opening the deal.
		ownerID = in.CreatorUserID
	}
	status := in.Status
	if status == "" {
		status = opportunity.StatusOpen
	}

	o := &opportunity.Opportunity{
		ID:           uuid.New().String(),
		WorkspaceID:  workspaceID,
		LeadID:       in.LeadID,
		PipelineID:   in.PipelineID,
		StageID:      in.StageID,
		OwnerID:      ownerID,
		CarteiraID:   in.CarteiraID,
		Title:        in.Title,
		ValueCents:   in.ValueCents,
		Currency:     in.Currency,
		Status:       status,
		LostReasonID: in.LostReasonID,
		Source:       in.Source,
		CloseDate:    in.CloseDate,
		CustomFields: in.CustomFields,
	}
	o.Normalize()
	return o
}

func (s *Service) Create(workspaceID string, in CreateInput) (*opportunity.Opportunity, error) {
	o := s.buildOpportunity(workspaceID, in)

	if err := s.validateCustomFields(workspaceID, o.CustomFields); err != nil {
		return nil, err
	}
	if err := o.Validate(); err != nil {
		return nil, err
	}
	if err := s.repo.Create(o); err != nil {
		return nil, err
	}

	if in.LinkEntryID != "" && s.links != nil {
		if err := s.links.Link(opportunity.ConversationLink{
			OpportunityID: o.ID,
			EntryID:       in.LinkEntryID,
			EntryType:     in.LinkEntryType,
		}); err != nil {
			return nil, err
		}
	}
	return s.repo.GetByID(workspaceID, o.ID)
}

// ValidateCreate runs the same custom-field and entity validation as Create
// without persisting anything. It backs the CSV import dry-run so a caller can
// preview which rows would be accepted without mutating state.
func (s *Service) ValidateCreate(workspaceID string, in CreateInput) error {
	o := s.buildOpportunity(workspaceID, in)
	if err := s.validateCustomFields(workspaceID, o.CustomFields); err != nil {
		return err
	}
	return o.Validate()
}

// UpdateInput patches the editable fields of a deal. Nil pointer fields are left
// unchanged; a non-nil CustomFields map replaces the stored custom fields. Stage
// and status changes should go through MoveStage so won/lost rules are enforced,
// but Status/LostReasonID here are still validated by the entity.
type UpdateInput struct {
	Title        *string
	ValueCents   *int64
	Currency     *string
	OwnerID      *string
	CarteiraID   *string
	Source       *string
	CloseDate    *time.Time
	CustomFields map[string]any
	StageID      *string
	Status       *opportunity.Status
	LostReasonID *string
}

func (s *Service) Update(workspaceID, id string, in UpdateInput) (*opportunity.Opportunity, error) {
	o, err := s.repo.GetByID(workspaceID, id)
	if err != nil {
		return nil, err
	}

	if in.Title != nil {
		o.Title = *in.Title
	}
	if in.ValueCents != nil {
		o.ValueCents = *in.ValueCents
	}
	if in.Currency != nil {
		o.Currency = *in.Currency
	}
	if in.OwnerID != nil {
		o.OwnerID = *in.OwnerID
	}
	if in.CarteiraID != nil {
		o.CarteiraID = *in.CarteiraID
	}
	if in.Source != nil {
		o.Source = *in.Source
	}
	if in.CloseDate != nil {
		o.CloseDate = in.CloseDate
	}
	if in.CustomFields != nil {
		o.CustomFields = in.CustomFields
	}
	if in.StageID != nil {
		o.StageID = *in.StageID
	}
	if in.Status != nil {
		o.Status = *in.Status
	}
	if in.LostReasonID != nil {
		o.LostReasonID = *in.LostReasonID
	}
	o.Normalize()

	if in.CustomFields != nil {
		if err := s.validateCustomFields(workspaceID, o.CustomFields); err != nil {
			return nil, err
		}
	}
	if err := o.Validate(); err != nil {
		return nil, err
	}
	if err := s.repo.Update(o); err != nil {
		return nil, err
	}
	return s.repo.GetByID(workspaceID, id)
}

// MoveStageInput moves a deal to a stage and optionally closes it. When Status is
// won/lost, the entity requires a lost reason for a loss; CloseDate is stamped on
// close when not already set.
type MoveStageInput struct {
	StageID      string
	Status       opportunity.Status // empty keeps the current status
	LostReasonID string
}

func (s *Service) MoveStage(workspaceID, id string, in MoveStageInput) (*opportunity.Opportunity, error) {
	o, err := s.repo.GetByID(workspaceID, id)
	if err != nil {
		return nil, err
	}
	if in.StageID != "" {
		o.StageID = in.StageID
	}
	if in.Status != "" {
		o.Status = in.Status
	}
	if in.LostReasonID != "" {
		o.LostReasonID = in.LostReasonID
	}
	o.Normalize()
	if o.IsClosed() && o.CloseDate == nil {
		now := time.Now().UTC()
		o.CloseDate = &now
	}
	if err := o.Validate(); err != nil {
		return nil, err
	}
	if err := s.repo.Update(o); err != nil {
		return nil, err
	}
	return s.repo.GetByID(workspaceID, id)
}

func (s *Service) Get(workspaceID, id string) (*opportunity.Opportunity, error) {
	return s.repo.GetByID(workspaceID, id)
}

func (s *Service) ListByPipeline(workspaceID, pipelineID string) ([]*opportunity.Opportunity, error) {
	return s.repo.ListByPipeline(workspaceID, pipelineID)
}

// ListByPipelineScoped is the department-scoped whole-pipeline read used by the flat
// GET endpoint and the CSV export, so neither leaks deals across departments.
func (s *Service) ListByPipelineScoped(workspaceID, pipelineID string, departmentIDs []string, restrict bool, assigneeOverrideUserID string) ([]*opportunity.Opportunity, error) {
	return s.repo.ListByPipelineScoped(workspaceID, pipelineID, departmentIDs, restrict, assigneeOverrideUserID)
}

func (s *Service) Delete(workspaceID, id string) error {
	return s.repo.Delete(workspaceID, id)
}

// LinkConversation attaches a conversation (entry) to a deal so its timeline
// aggregates the WhatsApp/voice history. It verifies the deal exists in the
// workspace first.
func (s *Service) LinkConversation(workspaceID, opportunityID, entryID, entryType string) error {
	if entryID == "" || entryType == "" {
		return ErrEntryTypeRequired
	}
	if _, err := s.repo.GetByID(workspaceID, opportunityID); err != nil {
		return err
	}
	return s.links.Link(opportunity.ConversationLink{
		OpportunityID: opportunityID,
		EntryID:       entryID,
		EntryType:     entryType,
	})
}

func (s *Service) UnlinkConversation(workspaceID, opportunityID, entryID, entryType string) error {
	if _, err := s.repo.GetByID(workspaceID, opportunityID); err != nil {
		return err
	}
	return s.links.Unlink(opportunityID, entryID, entryType)
}

func (s *Service) ListConversations(workspaceID, opportunityID string) ([]opportunity.ConversationLink, error) {
	return s.links.ListByOpportunity(workspaceID, opportunityID)
}

// ListOpportunitiesForEntry returns the deals linked to a conversation entry,
// HYDRATED (title / value / stage / status) so the conversation side panel can
// render deal cards. It is the reverse of ListConversations and is workspace-scoped
// end to end: both the link lookup and every GetByID filter by workspaceID, so it
// can never surface a deal from another workspace. A link whose deal was hard-deleted
// is skipped rather than failing the whole panel.
func (s *Service) ListOpportunitiesForEntry(workspaceID, entryID, entryType string) ([]*opportunity.Opportunity, error) {
	if s.links == nil {
		return nil, nil
	}
	links, err := s.links.ListByEntry(workspaceID, entryID, entryType)
	if err != nil {
		return nil, err
	}
	out := make([]*opportunity.Opportunity, 0, len(links))
	for _, l := range links {
		o, err := s.repo.GetByID(workspaceID, l.OpportunityID)
		if err != nil || o == nil {
			continue // dangling link (deal deleted) — omit, don't fail the panel
		}
		out = append(out, o)
	}
	return out, nil
}

// validateCustomFields checks each provided value against its definition and
// enforces required fields, using customfield.Definition.ValidateValue. Unknown
// keys are rejected so deal data stays clean.
func (s *Service) validateCustomFields(workspaceID string, values map[string]any) error {
	if s.fields == nil {
		return nil
	}
	defs, err := s.fields.ListByObject(workspaceID, objectType)
	if err != nil {
		return err
	}
	byKey := make(map[string]*customfield.Definition, len(defs))
	for _, d := range defs {
		byKey[d.Key] = d
	}

	for key, val := range values {
		def, ok := byKey[key]
		if !ok {
			return ErrUnknownCustomField
		}
		if err := def.ValidateValue(val); err != nil {
			return err
		}
	}

	// Enforce required fields that were not supplied.
	for _, def := range defs {
		if !def.Required {
			continue
		}
		if _, present := values[def.Key]; !present {
			if err := def.ValidateValue(nil); err != nil {
				return err
			}
		}
	}
	return nil
}
