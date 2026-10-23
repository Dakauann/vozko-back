package campaignaudience

import (
	"strings"
	"time"

	"vozko/domain/campaign"
)

const (
	MaxFileBytes = 20 << 20
	ListTTL      = 24 * time.Hour
	MaxRowsPage  = 200
)

type List struct {
	ID          string
	WorkspaceID string
	CreatedBy   string
	Channel     Channel
	FileName    string
	Layout      campaign.ListLayout
	Lines       int
	IssueCounts map[campaign.ListIssueReason]int
	Issues      []campaign.ListIssue
	AttachedTo  string
	ExpiresAt   *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Row struct {
	ID     string
	ListID string
	campaign.ListRow
}

func NewList(id, workspaceID, actorID string, channel Channel, fileName string, parsed campaign.ContactList, now time.Time) List {
	l := List{
		ID: id, WorkspaceID: workspaceID, CreatedBy: actorID, Channel: channel, FileName: strings.TrimSpace(fileName),
		Layout: parsed.Layout, Lines: parsed.Lines, IssueCounts: parsed.IssueCounts, Issues: parsed.Issues,
		CreatedAt: now, UpdatedAt: now,
	}
	if l.IssueCounts == nil {
		l.IssueCounts = map[campaign.ListIssueReason]int{}
	}
	l.Touch(now)
	return l
}

func EmptyLayout() campaign.ListLayout {
	return campaign.ListLayout{Contact: 0, Name: 1}
}

func (l *List) Touch(now time.Time) {
	l.UpdatedAt = now
	if l.AttachedTo != "" {
		l.ExpiresAt = nil
		return
	}
	expires := now.Add(ListTTL)
	l.ExpiresAt = &expires
}

func (l List) Expired(now time.Time) bool {
	return l.AttachedTo == "" && l.ExpiresAt != nil && !now.Before(*l.ExpiresAt)
}

func (l List) SkippedTotal() int {
	total := 0
	for _, n := range l.IssueCounts {
		total += n
	}
	return total
}

func (l List) ReadableBy(workspaceID, actorID string, channel Channel, now time.Time) error {
	if l.ID == "" || l.WorkspaceID != workspaceID {
		return ErrListNotFound
	}
	if l.AttachedTo == "" && l.CreatedBy != actorID {
		return ErrListNotFound
	}
	if l.Channel != channel {
		return ErrListChannelMismatch
	}
	if l.Expired(now) {
		return ErrListExpired
	}
	return nil
}

func (l List) OwnedBy(workspaceID, owner string, channel Channel) error {
	if l.ID == "" || l.WorkspaceID != workspaceID || owner == "" || l.AttachedTo != owner {
		return ErrListNotFound
	}
	if l.Channel != channel {
		return ErrListChannelMismatch
	}
	return nil
}

func EmailCampaignOwner(campaignID string) string {
	return "email_campaign:" + strings.TrimSpace(campaignID)
}

type RowCursor struct {
	Line int
	ID   string
}

func FirstRow() RowCursor {
	return RowCursor{Line: -1}
}

type RowQuery struct {
	Search string
	Offset int
	Limit  int
}
