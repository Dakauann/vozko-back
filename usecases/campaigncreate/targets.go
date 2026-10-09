package campaigncreate

import (
	"errors"
	"strings"

	"vozko/domain/campaign"
	"vozko/domain/lead"
)

type LeadsByID interface {
	FindByIDs(workspaceID string, ids []string) ([]*lead.Lead, error)
}

type LeadsByNumber interface {
	FindOrCreateMany(workspaceID string, inputs []lead.BulkLeadInput) (map[string]*lead.Lead, error)
}

type Target struct {
	LeadID string
	Number string
	Name   string
}

type Resolved struct {
	ByNumber map[string]*lead.Lead
	ByID     map[string]*lead.Lead
}

func (r Resolved) Of(t Target) *lead.Lead {
	if t.LeadID != "" {
		return r.ByID[t.LeadID]
	}
	if l := r.ByNumber[t.Number]; l != nil {
		return l
	}
	return r.ByNumber[lead.NormalizeNumber(t.Number)]
}

func (r Resolved) LeadIDs() []string {
	ids := lead.IDsOf(r.ByNumber)
	for id := range r.ByID {
		ids = append(ids, id)
	}
	return ids
}

func Resolve(byID LeadsByID, byNumber LeadsByNumber, workspaceID string, targets []Target) (Resolved, error) {
	resolved := Resolved{ByNumber: map[string]*lead.Lead{}, ByID: map[string]*lead.Lead{}}
	var leadIDs []string
	var bulk []lead.BulkLeadInput
	for _, t := range targets {
		if t.LeadID != "" {
			leadIDs = append(leadIDs, t.LeadID)
			continue
		}
		bulk = append(bulk, lead.BulkLeadInput{Source: lead.SourceImport, Number: t.Number, Name: t.Name})
	}
	if len(leadIDs) > 0 {
		if byID == nil {
			return Resolved{}, campaign.ErrLeadTargetsUnavailable
		}
		found, err := byID.FindByIDs(workspaceID, leadIDs)
		if err != nil {
			return Resolved{}, err
		}
		for _, l := range found {
			if l != nil && l.WorkspaceID == workspaceID {
				resolved.ByID[l.ID] = l
			}
		}
	}
	if len(bulk) > 0 {
		if byNumber == nil {
			return Resolved{}, campaign.ErrLeadTargetsUnavailable
		}
		found, err := byNumber.FindOrCreateMany(workspaceID, bulk)
		if err != nil {
			return Resolved{}, err
		}
		for number, l := range found {
			if l != nil {
				resolved.ByNumber[number] = l
			}
		}
	}
	return resolved, nil
}

type KeyLookup[C any] interface {
	FindByIdempotencyKey(workspaceID, key string) (C, error)
}

func FindKeyed[C any](lookup KeyLookup[C], workspaceID, key string, notFound error) (C, bool, error) {
	var none C
	if strings.TrimSpace(key) == "" {
		return none, false, nil
	}
	if lookup == nil {
		return none, false, campaign.ErrIdempotencyUnavailable
	}
	found, err := lookup.FindByIdempotencyKey(workspaceID, key)
	if errors.Is(err, notFound) {
		return none, false, nil
	}
	if err != nil {
		return none, false, err
	}
	return found, true, nil
}
