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
	ErrUnknownCustomField     = errors.New("opportunity: unknown custom field")
	ErrEntryTypeRequired      = errors.New("opportunity: entry id and type are required for a link")
	ErrActorRequired          = errors.New("opportunity: the acting user, agent or workflow is required")
	ErrOwnerOutsideWorkspace  = errors.New("opportunity: the owner does not belong to this workspace")
	ErrOwnerDirectoryMissing  = errors.New("opportunity: owners cannot be verified")
	ErrPipelineNotFound       = errors.New("opportunity: pipeline not found")
	ErrNotOpportunityPipeline = errors.New("opportunity: the pipeline is not a deals pipeline")
	ErrStageNotFound          = errors.New("opportunity: stage not found")
)

type Deps struct {
	Repo      opportunity.Repository
	Links     opportunity.LinkRepository
	Fields    customfield.Repository
	Stages    StageReader
	Pipelines PipelineReader
	Owners    opportunity.OwnerDirectory
	Clock     func() time.Time
}

type Service struct {
	repo      opportunity.Repository
	links     opportunity.LinkRepository
	fields    customfield.Repository
	stages    StageReader
	pipelines PipelineReader
	owners    opportunity.OwnerDirectory
	now       func() time.Time
}

func NewService(deps Deps) *Service {
	clock := deps.Clock
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	return &Service{
		repo:      deps.Repo,
		links:     deps.Links,
		fields:    deps.Fields,
		stages:    deps.Stages,
		pipelines: deps.Pipelines,
		owners:    deps.Owners,
		now:       clock,
	}
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
	LostReasonID string
	Source       string
	CloseDate    *time.Time
	CustomFields map[string]any

	LinkEntryID   string
	LinkEntryType string

	Actor string
}

func (s *Service) Create(workspaceID string, in CreateInput) (*opportunity.Opportunity, error) {
	o, links, events, err := s.prepareCreate(workspaceID, in)
	if err != nil {
		return nil, err
	}
	if err := s.repo.Create(o, links, events); err != nil {
		return nil, err
	}
	return s.repo.GetByID(workspaceID, o.ID)
}

func (s *Service) ValidateCreate(workspaceID string, in CreateInput) error {
	_, _, _, err := s.prepareCreate(workspaceID, in)
	return err
}

func (s *Service) prepareCreate(workspaceID string, in CreateInput) (*opportunity.Opportunity, []opportunity.ConversationLink, []opportunity.Event, error) {
	if in.Actor == "" {
		return nil, nil, nil, ErrActorRequired
	}
	if (in.LinkEntryID == "") != (in.LinkEntryType == "") {
		return nil, nil, nil, ErrEntryTypeRequired
	}
	stageRef, err := s.placement(workspaceID, in.PipelineID, in.StageID)
	if err != nil {
		return nil, nil, nil, err
	}
	owner := in.OwnerID
	if owner == "" {
		owner = in.Actor
	}
	if err := s.checkOwner(workspaceID, owner); err != nil {
		return nil, nil, nil, err
	}

	o := &opportunity.Opportunity{
		ID:           uuid.New().String(),
		WorkspaceID:  workspaceID,
		LeadID:       in.LeadID,
		PipelineID:   in.PipelineID,
		OwnerID:      owner,
		CreatedBy:    in.Actor,
		CarteiraID:   in.CarteiraID,
		Title:        in.Title,
		ValueCents:   in.ValueCents,
		Currency:     in.Currency,
		LostReasonID: in.LostReasonID,
		Source:       in.Source,
		CustomFields: in.CustomFields,
	}
	o.Normalize()
	now := s.now()
	if err := o.PlaceOn(stageRef, in.Actor, now); err != nil {
		return nil, nil, nil, err
	}
	if in.CloseDate != nil && o.IsClosed() {
		closed := *in.CloseDate
		o.CloseDate = &closed
	}
	if err := s.validateCustomFields(workspaceID, o.CustomFields); err != nil {
		return nil, nil, nil, err
	}
	if err := o.Validate(); err != nil {
		return nil, nil, nil, err
	}

	events := opportunity.Changes(nil, o, in.Actor, now)
	var links []opportunity.ConversationLink
	if in.LinkEntryID != "" {
		links = append(links, opportunity.ConversationLink{OpportunityID: o.ID, EntryID: in.LinkEntryID, EntryType: in.LinkEntryType})
		events = append(events, opportunity.LinkedEvent(o, in.LinkEntryID, in.LinkEntryType, in.Actor, now))
	}
	return o, links, events, nil
}

type UpdateInput struct {
	Title        *string
	ValueCents   *int64
	Currency     *string
	OwnerID      *string
	CarteiraID   *string
	Source       *string
	CustomFields map[string]any
	StageID      *string
	LostReasonID *string
}

func (s *Service) Update(workspaceID, id string, in UpdateInput, actorID string) (*opportunity.Opportunity, error) {
	return s.change(workspaceID, id, actorID, func(o *opportunity.Opportunity, now time.Time) error {
		if in.Title != nil {
			o.Title = *in.Title
		}
		if in.ValueCents != nil {
			o.ValueCents = *in.ValueCents
		}
		if in.Currency != nil {
			o.Currency = *in.Currency
		}
		if in.CarteiraID != nil {
			o.CarteiraID = *in.CarteiraID
		}
		if in.Source != nil {
			o.Source = *in.Source
		}
		if in.LostReasonID != nil {
			o.LostReasonID = *in.LostReasonID
		}
		if in.OwnerID != nil {
			if err := s.checkOwner(workspaceID, *in.OwnerID); err != nil {
				return err
			}
			o.OwnerID = *in.OwnerID
		}
		if in.CustomFields != nil {
			if err := s.validateCustomFields(workspaceID, in.CustomFields); err != nil {
				return err
			}
			o.CustomFields = in.CustomFields
		}
		if in.StageID != nil {
			return s.moveTo(workspaceID, o, *in.StageID, actorID, now)
		}
		return nil
	})
}

type MoveStageInput struct {
	StageID      string
	LostReasonID string
}

func (s *Service) MoveStage(workspaceID, id string, in MoveStageInput, actorID string) (*opportunity.Opportunity, error) {
	return s.change(workspaceID, id, actorID, func(o *opportunity.Opportunity, now time.Time) error {
		if in.LostReasonID != "" {
			o.LostReasonID = in.LostReasonID
		}
		return s.moveTo(workspaceID, o, in.StageID, actorID, now)
	})
}

func (s *Service) change(
	workspaceID, id, actorID string,
	mutate func(o *opportunity.Opportunity, now time.Time) error,
) (*opportunity.Opportunity, error) {
	if actorID == "" {
		return nil, ErrActorRequired
	}
	current, err := s.repo.GetByID(workspaceID, id)
	if err != nil {
		return nil, err
	}
	before := *current
	now := s.now()
	if err := mutate(current, now); err != nil {
		return nil, err
	}
	current.Normalize()
	if err := current.Validate(); err != nil {
		return nil, err
	}
	if err := s.repo.Update(current, opportunity.Changes(&before, current, actorID, now)); err != nil {
		return nil, err
	}
	return s.repo.GetByID(workspaceID, id)
}

func (s *Service) Get(workspaceID, id string) (*opportunity.Opportunity, error) {
	return s.repo.GetByID(workspaceID, id)
}

func (s *Service) ListEvents(workspaceID, id string) ([]opportunity.Event, error) {
	if _, err := s.repo.GetByID(workspaceID, id); err != nil {
		return nil, err
	}
	return s.repo.ListEvents(workspaceID, id)
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

func (s *Service) LinkConversation(workspaceID, opportunityID, entryID, entryType, actorID string) error {
	if entryID == "" || entryType == "" {
		return ErrEntryTypeRequired
	}
	if actorID == "" {
		return ErrActorRequired
	}
	o, err := s.repo.GetByID(workspaceID, opportunityID)
	if err != nil {
		return err
	}
	return s.repo.Link(
		opportunity.ConversationLink{OpportunityID: opportunityID, EntryID: entryID, EntryType: entryType},
		[]opportunity.Event{opportunity.LinkedEvent(o, entryID, entryType, actorID, s.now())},
	)
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
	links, err := s.links.ListByEntry(workspaceID, entryID, entryType)
	if err != nil {
		return nil, err
	}
	out := make([]*opportunity.Opportunity, 0, len(links))
	for _, l := range links {
		o, err := s.repo.GetByID(workspaceID, l.OpportunityID)
		if err != nil {
			return nil, err
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
