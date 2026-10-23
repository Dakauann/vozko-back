package choicestats

import (
	"context"
	"strings"
	"time"
)

type Click struct {
	WorkspaceID       string
	PromptKey         string
	ProviderMessageID string
	Option            string
	At                time.Time
}

type ClickRecord struct {
	Click
	CounterOptions []string
	NotBefore      time.Time
}

func (c Click) Record() (ClickRecord, error) {
	byKey := strings.TrimSpace(c.PromptKey) != ""
	byMessage := strings.TrimSpace(c.ProviderMessageID) != ""
	if strings.TrimSpace(c.WorkspaceID) == "" || byKey == byMessage || c.At.IsZero() {
		return ClickRecord{}, ErrInvalidClick
	}
	options, err := ClickCounterOptions(c.Option)
	if err != nil {
		return ClickRecord{}, err
	}
	c.Option = strings.TrimSpace(c.Option)
	return ClickRecord{Click: c, CounterOptions: options, NotBefore: AttributionCutoff(c.At)}, nil
}

type CountQuery struct {
	WorkspaceID string
	Kind        SourceKind
	SourceID    string
	Version     *int
	TemplateID  string
	From        time.Time
	To          time.Time
}

type Store interface {
	RecordSend(ctx context.Context, send SendRecord) (bool, error)
	RecordClick(ctx context.Context, click ClickRecord) (bool, error)
	Counts(ctx context.Context, q CountQuery) ([]CountRow, error)
	PrunePrompts(before time.Time, limit int) (int64, error)
	PruneCounts(before time.Time, limit int) (int64, error)
}

type Recorder interface {
	RecordSend(ctx context.Context, send Send)
	RecordClick(ctx context.Context, click Click)
}

type LinkClickRecorder interface {
	RecordLinkClick(ctx context.Context, workspaceID, token string, at time.Time)
}
