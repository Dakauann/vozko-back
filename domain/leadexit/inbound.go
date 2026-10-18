package leadexit

import (
	"context"
	"strings"
	"time"

	"vozko/domain/workflow"
)

type Tap struct {
	Payload string
	Text    string
}

type Inbound struct {
	WorkspaceID      string
	LeadID           string
	SenderNumber     string
	EntryID          string
	EntryType        string
	Channel          string
	MessageID        string
	TriggerMessageID string
	Text             string
	ReceivedAt       time.Time
	Tap              *Tap
	ContextMessageID string
	LastTemplate     *SentTemplate
}

func (in Inbound) EvidenceRef() string {
	if row := strings.TrimSpace(in.TriggerMessageID); row != "" {
		return row
	}
	return strings.TrimSpace(in.MessageID)
}

type Result struct {
	OptedOut  bool
	Selection *workflow.OptionSelection
}

type Replier interface {
	Reply(ctx context.Context, text string) error
}

type Step interface {
	Run(ctx context.Context, in Inbound, replier Replier) (Result, error)
}
