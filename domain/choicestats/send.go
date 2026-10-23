package choicestats

import (
	"errors"
	"hash/fnv"
	"strings"
	"time"
)

type SourceKind string

const (
	SourceWorkflow           SourceKind = "workflow"
	SourceWhatsAppCampaign   SourceKind = "whatsapp_campaign"
	SourceUnofficialCampaign SourceKind = "unofficial_campaign"
	SourceInbox              SourceKind = "inbox"
	SourceOutreach           SourceKind = "outreach"
)

func (k SourceKind) Valid() bool {
	switch k {
	case SourceWorkflow, SourceWhatsAppCampaign, SourceUnofficialCampaign, SourceInbox, SourceOutreach:
		return true
	}
	return false
}

const (
	OptionTotal    = ""
	OptionFreeText = "no_match"

	ShardCount           = 8
	AttributionWindow    = 30 * 24 * time.Hour
	CounterRetentionDays = 400
	MaxPeriodDays        = CounterRetentionDays
)

var (
	ErrNotASend     = errors.New("choice stats: not an accepted send")
	ErrInvalidClick = errors.New("choice stats: a click needs an option")
	ErrZoneUnknown  = errors.New("choice stats: the workspace timezone is unknown")

	ErrRecorderMissing = errors.New("choice stats: a sender that sends choices needs the recorder")
)

type Source struct {
	Kind       SourceKind
	ID         string
	NodeID     string
	Version    int
	TemplateID string
}

func WorkflowPromptKey(runID, nodeVisit string) string {
	return "wf:" + runID + ":" + nodeVisit
}

func WhatsAppCampaignPromptKey(entryID string) string {
	return "wc:" + entryID
}

func UnofficialCampaignPromptKey(entryID string) string {
	return "uwc:" + entryID
}

func TemplateAttemptPromptKey(attemptID string) string {
	return "tpl:" + attemptID
}

func ShardOf(promptKey string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(promptKey))
	return int(h.Sum32() % ShardCount)
}

func ShownOptions(offered, reported []string) []string {
	if reported == nil {
		return append([]string(nil), offered...)
	}
	shown := make(map[string]bool, len(reported))
	for _, id := range reported {
		shown[id] = true
	}
	out := make([]string, 0, len(reported))
	for _, id := range offered {
		if shown[id] {
			out = append(out, id)
		}
	}
	return out
}

type Send struct {
	WorkspaceID       string
	PromptKey         string
	Source            Source
	ProviderMessageID string
	Shown             []string
	SentAt            time.Time
}

type SendRecord struct {
	Send
	SentDay        time.Time
	Shard          int
	CounterOptions []string
}

func (s Send) Record(loc *time.Location) (SendRecord, error) {
	if strings.TrimSpace(s.WorkspaceID) == "" || strings.TrimSpace(s.PromptKey) == "" ||
		strings.TrimSpace(s.ProviderMessageID) == "" || !s.Source.Kind.Valid() || s.SentAt.IsZero() {
		return SendRecord{}, ErrNotASend
	}
	options := []string{OptionTotal}
	seen := map[string]bool{OptionTotal: true}
	for _, id := range s.Shown {
		id = strings.TrimSpace(id)
		if seen[id] {
			continue
		}
		seen[id] = true
		options = append(options, id)
	}
	if len(options) == 1 {
		return SendRecord{}, ErrNotASend
	}
	day, err := LocalDay(s.SentAt, loc)
	if err != nil {
		return SendRecord{}, err
	}
	return SendRecord{Send: s, SentDay: day, Shard: ShardOf(s.PromptKey), CounterOptions: options}, nil
}

func LocalDay(at time.Time, loc *time.Location) (time.Time, error) {
	if loc == nil {
		return time.Time{}, ErrZoneUnknown
	}
	y, m, d := at.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC), nil
}

func ResponseOption(offered []string, selected string) string {
	selected = strings.TrimSpace(selected)
	if selected == "" {
		return OptionFreeText
	}
	for _, id := range offered {
		if id == selected {
			return selected
		}
	}
	return OptionFreeText
}

func ClickCounterOptions(option string) ([]string, error) {
	option = strings.TrimSpace(option)
	switch option {
	case OptionTotal:
		return nil, ErrInvalidClick
	case OptionFreeText:
		return []string{OptionFreeText}, nil
	}
	return []string{option, OptionTotal}, nil
}

func AttributionCutoff(now time.Time) time.Time {
	return now.Add(-AttributionWindow)
}

func CounterCutoff(now time.Time) time.Time {
	return now.AddDate(0, 0, -CounterRetentionDays)
}
