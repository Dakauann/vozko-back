package callsession

import (
	"context"
	"errors"

	"vozko/domain/lead"
)

type LeadDial struct {
	WorkspaceID string
	LeadID      string
	Number      string
	Purpose     lead.DialPurpose
}

type LeadDialTargets interface {
	CheckLead(ctx context.Context, dial LeadDial) error
	IdentityLead(ctx context.Context, workspaceID, number string) (string, error)
}

type CallListItemDial struct {
	WorkspaceID string
	UserID      string
	ItemID      string
	LeadID      string
	Number      string
}

type CallListItemStamp struct {
	WorkspaceID  string
	UserID       string
	ItemID       string
	CallRecordID string
}

var ErrCallListStampRefused = errors.New("call session: the call list item no longer takes this call")

type CallListItems interface {
	CheckItemDial(ctx context.Context, dial CallListItemDial) error
	StampLastCall(ctx context.Context, stamp CallListItemStamp) error
}
