package opportunity_usecase

import (
	"errors"
	"time"

	"github.com/google/uuid"

	"vozko/domain/customfield"
	"vozko/domain/opportunity"
)

const objectType = "opportunity"

var (
	ErrUnknownCustomField = errors.New("opportunity: unknown custom field")
	ErrEntryTypeRequired  = errors.New("opportunity: entry id and type are required for a link")
)

type Service struct {
	repo   opportunity.Repository
	links  opportunity.LinkRepository
	fields customfield.Repository
}

func NewService(repo opportunity.Repository, links opportunity.LinkRepository, fields customfield.Repository) *Service {
	return &Service{repo: repo, links: links, fields: fields}
}

type CreateInput struct {
	LeadID       string
	PipelineID   string
	StageID      string
	OwnerID      string
	CarteiraID   string
	Title        string
	ValueCents   int64
	Currency     string
	Status       opportunity.Status
	LostReasonID string
	Source       string
	CloseDate    *time.Time
	CustomFields map[string]any

	ConversationAssigneeID string
	LinkEntryID            string
	LinkEntryType          string

	CreatorUserID string
}

func (s *Service) buildOpportunity(workspaceID string, in CreateInput) *opportunity.Opportunity {
	ownerID := in.OwnerID
	if ownerID == "" && in.ConversationAssigneeID != "" {
		ownerID = in.ConversationAssigneeID
	}
	if ownerID == "" {
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

func (s *Service) ValidateCreate(workspaceID string, in CreateInput) error {
	o := s.buildOpportunity(workspaceID, in)
	if err := s.validateCustomFields(workspaceID, o.CustomFields); err != nil {
		return err
	}
	return o.Validate()
}

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

type MoveStageInput struct {
	StageID      string
	Status       opportunity.Status
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

func (s *Service) ListByPipelineScoped(workspaceID, pipelineID string, departmentIDs []string, restrict bool, assigneeOverrideUserID string) ([]*opportunity.Opportunity, error) {
	return s.repo.ListByPipelineScoped(workspaceID, pipelineID, departmentIDs, restrict, assigneeOverrideUserID)
}

func (s *Service) Delete(workspaceID, id string) error {
	return s.repo.Delete(workspaceID, id)
}

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
			continue
		}
		out = append(out, o)
	}
	return out, nil
}

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
