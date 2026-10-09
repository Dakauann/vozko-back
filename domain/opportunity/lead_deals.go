package opportunity

import (
	"context"
	"errors"

	"vozko/domain/shared"
)

var ErrEntryLeadMismatch = errors.New("opportunity: the lead given is not the lead of the linked conversation")

type LeadDealsQuery struct {
	WorkspaceID string
	LeadID      string
	Scope       DealScope
	Before      *shared.Keyset
	Limit       int
}

type LeadDealReader interface {
	DealsOfLead(ctx context.Context, q LeadDealsQuery) ([]*Opportunity, error)
}

type LeadDealCounter interface {
	CountDealsOfLead(ctx context.Context, workspaceID, leadID string, scope DealScope) (int, error)
}

func LeadForLinkedEntry(given, ofEntry string) (string, error) {
	switch {
	case ofEntry == "":
		return given, nil
	case given == "" || given == ofEntry:
		return ofEntry, nil
	}
	return "", ErrEntryLeadMismatch
}
