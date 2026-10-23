package carriermessaging

import (
	"errors"
	"strings"
	"time"
)

var (
	ErrContactNotFound      = errors.New("rcs: contact not found")
	ErrConversationNotFound = errors.New("rcs: conversation not found")
)

type Contact struct {
	ID          string
	WorkspaceID string
	Phone       string
	DisplayName string
	LeadID      *string
	FloodingAt  *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (c Contact) Name() string {
	if name := strings.TrimSpace(c.DisplayName); name != "" {
		return name
	}
	return c.Phone
}

type Conversation struct {
	ID                    string
	WorkspaceID           string
	Channel               Channel
	SenderID              string
	ContactID             string
	ConversationStatus    string
	AutomationEnabled     *bool
	LastMessageAt         *time.Time
	LastCustomerMessageAt *time.Time
	LastAgentMessageAt    *time.Time
	ConversationOpenedAt  *time.Time
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

const ConversationWindow = 24 * time.Hour

func (c Conversation) BilledConversationOpen(now time.Time) bool {
	return c.ConversationOpenedAt != nil && now.Before(c.ConversationOpenedAt.Add(ConversationWindow))
}

type Suppression struct {
	WorkspaceID string
	Phone       string
	Source      SuppressionSource
	MessageID   string
	CreatedAt   time.Time
}

const (
	SuppressionKeyword  SuppressionSource = "keyword"
	SuppressionButton   SuppressionSource = "button"
	SuppressionOperator SuppressionSource = "operator"
)
