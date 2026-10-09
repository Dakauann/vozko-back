package lead_usecase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"

	"vozko/domain/lead"
	"vozko/domain/recordevent"
	"vozko/domain/workspace"
)

type DuplicateWarning struct {
	LeadID  string
	Reasons []lead.DuplicateReason
	Lead    *lead.Lead
}

type CreateResult struct {
	Lead       *lead.Lead
	Duplicates []DuplicateWarning
}

type AddRelativeInput struct {
	Kind               lead.RelationKind
	Relative           lead.Draft
	CopyPrimaryAddress bool
}

type RelativeResult struct {
	Lead       *lead.Lead
	Relative   *lead.Lead
	Relation   lead.Relation
	Duplicates []DuplicateWarning
}

type RelationResult struct {
	Lead     *lead.Lead
	Relation lead.Relation
}

func (c *Commands) duplicatesOf(ctx context.Context, v lead.Viewer, l *lead.Lead, anchorID string) ([]DuplicateWarning, error) {
	numbers, fingerprints := l.DuplicateLookup(v)
	if len(numbers) == 0 && len(fingerprints) == 0 {
		return nil, nil
	}
	existing, err := c.deps.Duplicates.FindByNumbersOrAddresses(ctx, l.WorkspaceID, numbers, fingerprints)
	if err != nil {
		return nil, fmt.Errorf("lead duplicates: %w", err)
	}
	reads := v.ReadsLeads
	if holder := lead.IdentityHolder(l, existing); holder != nil {
		taken := &lead.IdentityTaken{}
		if reads {
			taken.LeadID = holder.ID
		}
		return nil, taken
	}
	byID := make(map[string]*lead.Lead, len(existing))
	for _, other := range existing {
		byID[other.ID] = other
	}
	var warnings []DuplicateWarning
	for _, candidate := range lead.DuplicateCandidates(v, l, existing) {
		if candidate.LeadID == anchorID {
			continue
		}
		warning := DuplicateWarning{LeadID: candidate.LeadID, Reasons: candidate.Reasons}
		if reads {
			warning.Lead = byID[candidate.LeadID]
		}
		warnings = append(warnings, warning)
	}
	return warnings, nil
}

func (c *Commands) AddRelative(ctx context.Context, a Actor, id string, in AddRelativeInput) (RelativeResult, error) {
	v, err := c.authorize(a, workspace.ActionUpdate, workspace.ActionCreate)
	if err != nil {
		return RelativeResult{}, err
	}
	anchor, err := c.writer.load(ctx, a.WorkspaceID, id)
	if err != nil {
		return RelativeResult{}, err
	}
	relative, err := c.draft(a.WorkspaceID, v, in.Relative)
	if err != nil {
		return RelativeResult{}, err
	}
	relative.ID = c.deps.NewID()
	if in.CopyPrimaryAddress {
		if err := relative.AdoptPrimaryAddressOf(anchor); err != nil {
			return RelativeResult{}, err
		}
	}
	relation, err := relative.Relate(anchor, in.Kind.Inverse(), a.UserID)
	if err != nil {
		return RelativeResult{}, err
	}
	duplicates, err := c.duplicatesOf(ctx, v, relative, anchor.ID)
	if err != nil {
		return RelativeResult{}, err
	}
	created := lead.Changes(lead.EventCreated, a.UserID, nil, relative, v.Definitions)
	if err := c.deps.Store.Insert(ctx, relative, []recordevent.Event{created, lead.RelationEvent(lead.EventRelationAdded, a.UserID, relative.ID, relation)}); err != nil {
		return RelativeResult{}, err
	}
	relation, _ = relative.RelationWith(anchor.ID, relation.Dimension())
	c.writer.notify(relative, append(created.Fields(), lead.FieldRelations))
	updated := c.announceCounterpart(a.WorkspaceID, anchor.ID)
	if updated == nil {
		updated = anchor
	}
	return RelativeResult{
		Lead:       updated.VisibleTo(v, []string{lead.FieldRelations}),
		Relative:   relative.VisibleTo(v, created.Fields()),
		Relation:   relation,
		Duplicates: duplicates,
	}, nil
}

func (c *Commands) LinkRelation(ctx context.Context, a Actor, id, otherID string, kind lead.RelationKind) (RelationResult, error) {
	v, err := c.authorize(a, workspace.ActionUpdate)
	if err != nil {
		return RelationResult{}, err
	}
	subject, err := c.writer.record(a.WorkspaceID, id)
	if err != nil {
		return RelationResult{}, err
	}
	other, err := c.relative(a.WorkspaceID, otherID)
	if err != nil {
		return RelationResult{}, err
	}
	relation, err := lead.RelationBetween(subject, other, kind, a.UserID)
	if err != nil {
		return RelationResult{}, err
	}
	written, err := c.deps.Relations.AddRelation(ctx, a.WorkspaceID, relation)
	if err != nil {
		return RelationResult{}, err
	}
	c.announceTallies(a.WorkspaceID, written.Leads)
	if tally, ok := written.Leads[subject.ID]; ok {
		subject.Version, subject.RelativesCount, subject.ReferredCount = tally.Version, tally.Counts.Relatives, tally.Counts.Referred
	}
	return RelationResult{Lead: subject.VisibleTo(v, []string{lead.FieldRelations}), Relation: written.Relation}, nil
}

func (c *Commands) RemoveRelation(ctx context.Context, a Actor, relationID string) (lead.Relation, error) {
	if err := c.permit(a, workspace.ActionUpdate); err != nil {
		return lead.Relation{}, err
	}
	written, err := c.deps.Relations.RemoveRelation(ctx, a.WorkspaceID, relationID, a.UserID)
	if err != nil {
		return lead.Relation{}, err
	}
	c.announceTallies(a.WorkspaceID, written.Leads)
	return written.Relation, nil
}

func (c *Commands) relative(workspaceID, id string) (*lead.Lead, error) {
	other, err := c.deps.Store.FindByID(workspaceID, id)
	if errors.Is(err, lead.ErrLeadNotFound) || errors.Is(err, lead.ErrLeadRequired) {
		return nil, lead.ErrRelativeNotFound
	}
	if err != nil {
		return nil, err
	}
	return other, nil
}

func (c *Commands) announceTallies(workspaceID string, tallies map[string]lead.RelationTally) {
	ids := make([]string, 0, len(tallies))
	for id := range tallies {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		c.deps.Notifier.LeadChanged(lead.Change{WorkspaceID: workspaceID, LeadID: id, Version: tallies[id].Version, Fields: []string{lead.FieldRelations}})
	}
}

func (c *Commands) announceCounterpart(workspaceID, id string) *lead.Lead {
	counterpart, err := c.deps.Store.FindByID(workspaceID, id)
	if err != nil {
		log.Printf("[leads] the relation was saved but lead %s could not be read to announce it: %v", id, err)
		return nil
	}
	c.writer.notify(counterpart, []string{lead.FieldRelations})
	return counterpart
}
