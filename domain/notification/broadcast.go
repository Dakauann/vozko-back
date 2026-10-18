package notification

import (
	"strings"
	"time"

	"vozko/domain/shared"
)

const MaxBroadcastWorkspaces = 1000

type BroadcastTarget struct {
	All          bool     `json:"all"`
	WorkspaceIDs []string `json:"workspaceIds,omitempty"`
}

func (t BroadcastTarget) Validate() error {
	if t.All == (len(t.WorkspaceIDs) > 0) {
		return ErrTargetInvalid
	}
	if len(t.WorkspaceIDs) > MaxBroadcastWorkspaces {
		return ErrTargetInvalid
	}
	return nil
}

type BroadcastDraft struct {
	Target      BroadcastTarget
	Audience    Audience
	Category    Category
	Severity    Severity
	Title       string
	Body        string
	ImageURL    string
	ActionURL   string
	ActionLabel string
	Email       bool
}

func (b BroadcastDraft) Normalize() BroadcastDraft {
	if !b.Target.All {
		b.Target.WorkspaceIDs = uniqueTrimmed(b.Target.WorkspaceIDs)
	}
	b.Title = strings.TrimSpace(b.Title)
	b.Body = strings.TrimSpace(b.Body)
	b.ImageURL = strings.TrimSpace(b.ImageURL)
	b.ActionURL = strings.TrimSpace(b.ActionURL)
	b.ActionLabel = strings.TrimSpace(b.ActionLabel)
	if b.Severity == "" {
		b.Severity = SeverityInfo
	}
	b.Audience = b.Audience.Normalize()
	return b
}

func (b BroadcastDraft) Validate() error {
	if err := b.Target.Validate(); err != nil {
		return err
	}
	if err := validateContent(b.Category, b.Severity, b.Title, b.Body, b.ImageURL, b.ActionURL, b.ActionLabel); err != nil {
		return err
	}
	return b.Audience.Validate()
}

type BroadcastStatus string

const (
	BroadcastQueued    BroadcastStatus = "queued"
	BroadcastCompleted BroadcastStatus = "completed"
)

type Broadcast struct {
	BroadcastDraft
	ID             string
	CreatedBy      string
	Status         BroadcastStatus
	WorkspaceCount int
	CreatedAt      time.Time
	CompletedAt    *time.Time
	Stats          BroadcastStats
}

func (b Broadcast) DraftFor(workspaceID string) Draft {
	return Draft{
		WorkspaceID: workspaceID,
		Category:    b.Category,
		Severity:    b.Severity,
		Title:       b.Title,
		Body:        b.Body,
		ImageURL:    b.ImageURL,
		ActionURL:   b.ActionURL,
		ActionLabel: b.ActionLabel,
		Audience:    b.Audience,
		Email:       b.Email,
		DedupKey:    "broadcast:" + b.ID,
		Source:      SourceBroadcast,
		BroadcastID: b.ID,
		CreatedBy:   b.CreatedBy,
	}
}

type BroadcastPage struct {
	Items []Broadcast
	Next  string
}

func BroadcastPageOf(rows []Broadcast, limit int) BroadcastPage {
	shown, next := pageOf(rows, limit, func(b Broadcast) shared.Keyset { return shared.Keyset{At: b.CreatedAt, ID: b.ID} })
	return BroadcastPage{Items: shown, Next: next}
}
