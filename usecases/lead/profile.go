package lead_usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"vozko/domain/actor"
	"vozko/domain/address"
	"vozko/domain/cep"
	"vozko/domain/customfield"
	"vozko/domain/lead"
	"vozko/domain/recordevent"
	"vozko/domain/shared"
)

var errProfilesIncomplete = errors.New("lead profiles: a store, a notifier, the lead fields, the conversation directory and the CEP lookup are required")

type ProfileDeps struct {
	Store       lead.Store
	Notifier    lead.ChangeNotifier
	Definitions DefinitionSource
	EntryLeads  lead.EntryLeads
	CEP         cep.CEPSearchUseCase
	Now         func() time.Time
}

type Profiles struct {
	writer      recordWriter
	definitions DefinitionSource
	entryLeads  lead.EntryLeads
	cep         cep.CEPSearchUseCase
	now         func() time.Time
}

type ProfileUpdate struct {
	WorkspaceID string
	LeadID      string
	Actor       string
	Source      lead.ProfileSource
	Profile     lead.Profile
}

type ProfileResult struct {
	LeadID    string
	Version   int64
	Changed   []string
	Conflicts []string
}

func NewProfiles(deps ProfileDeps) (*Profiles, error) {
	if deps.Store == nil || deps.Notifier == nil || deps.Definitions == nil || deps.EntryLeads == nil || deps.CEP == nil {
		return nil, errProfilesIncomplete
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}
	return &Profiles{
		writer:      recordWriter{store: deps.Store, notifier: deps.Notifier},
		definitions: deps.Definitions,
		entryLeads:  deps.EntryLeads,
		cep:         deps.CEP,
		now:         deps.Now,
	}, nil
}

func (p *Profiles) Update(ctx context.Context, in ProfileUpdate) (ProfileResult, error) {
	if err := requireRecordRef(in.WorkspaceID, in.LeadID); err != nil {
		return ProfileResult{}, err
	}
	if err := checkProfileUpdate(in); err != nil {
		return ProfileResult{}, err
	}
	profile, err := p.checkedCEP(ctx, in.Profile)
	if err != nil {
		return ProfileResult{}, err
	}
	defs, err := p.definitions.ListByObject(in.WorkspaceID, customfield.ObjectLead)
	if err != nil {
		return ProfileResult{}, fmt.Errorf("lead fields of workspace %s: %w", in.WorkspaceID, err)
	}
	actorID := strings.TrimSpace(in.Actor)
	if actorID == "" {
		actorID = actor.SystemID
	}
	now := p.now().UTC()
	var outcome lead.ProfileOutcome
	saved, err := p.writer.retryingEdit(ctx, in.WorkspaceID, in.LeadID, func(current, next *lead.Lead) (recordEdit, error) {
		applied, err := next.ApplyProfile(profile, defs, now)
		if err != nil {
			return recordEdit{}, err
		}
		outcome = applied
		if len(applied.Changed) == 0 {
			return recordEdit{}, nil
		}
		event := lead.Changes(in.Source.EventKind(), actorID, current, next, defs)
		return recordEdit{events: []recordevent.Event{event}, fields: applied.Changed}, nil
	})
	if err != nil {
		return ProfileResult{}, err
	}
	return ProfileResult{LeadID: saved.ID, Version: saved.Version, Changed: outcome.Changed, Conflicts: outcome.Conflicts}, nil
}

func (p *Profiles) UpdateOfEntry(ctx context.Context, ref shared.EntryRef, in ProfileUpdate) (ProfileResult, error) {
	if strings.TrimSpace(in.WorkspaceID) == "" {
		return ProfileResult{}, lead.ErrLeadWorkspaceRequired
	}
	if err := checkProfileUpdate(in); err != nil {
		return ProfileResult{}, err
	}
	if strings.TrimSpace(ref.EntryID) == "" {
		return ProfileResult{}, lead.ErrLeadNotFound
	}
	leadID, err := p.entryLeads.LeadOfEntry(ctx, in.WorkspaceID, ref)
	if err != nil {
		return ProfileResult{}, err
	}
	in.LeadID = leadID
	return p.Update(ctx, in)
}

func (p *Profiles) WritableFields(workspaceID string) ([]*customfield.Definition, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return nil, lead.ErrLeadWorkspaceRequired
	}
	defs, err := p.definitions.ListByObject(workspaceID, customfield.ObjectLead)
	if err != nil {
		return nil, fmt.Errorf("lead fields of workspace %s: %w", workspaceID, err)
	}
	return lead.ProfileWritableFields(defs), nil
}

func checkProfileUpdate(in ProfileUpdate) error {
	if !in.Source.Valid() {
		return lead.ErrProfileSourceInvalid
	}
	if in.Profile.Empty() {
		return lead.ErrProfileEmpty
	}
	return nil
}

func (p *Profiles) checkedCEP(ctx context.Context, profile lead.Profile) (lead.Profile, error) {
	raw := strings.TrimSpace(profile.Address.ZipCode)
	if raw == "" {
		return profile, nil
	}
	invalid := address.InvalidFieldError{Field: address.FieldZipCode, Rule: address.RuleFormat}
	code, err := cep.Parse(raw)
	if err != nil {
		return lead.Profile{}, invalid
	}
	info, err := p.cep.Execute(ctx, code)
	switch {
	case errors.Is(err, cep.ErrNotFound):
		return lead.Profile{}, lead.ErrProfileCEPUnknown
	case errors.Is(err, cep.ErrInvalidCEP):
		return lead.Profile{}, invalid
	case err != nil:
		return lead.Profile{}, fmt.Errorf("%w: %w", lead.ErrProfileCEPUnchecked, err)
	case info == nil:
		return lead.Profile{}, lead.ErrProfileCEPUnchecked
	}
	completed, err := profile.Address.CompletedBy(*info)
	if err != nil {
		return lead.Profile{}, err
	}
	profile.Address = completed
	return profile, nil
}
