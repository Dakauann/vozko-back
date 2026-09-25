package readiness

import "context"

type Capability string

const (
	OfficialWhatsApp   Capability = "official_whatsapp"
	ApprovedTemplates  Capability = "approved_templates"
	UnofficialWhatsApp Capability = "unofficial_whatsapp"
	Instagram          Capability = "instagram"
	Telegram           Capability = "telegram"
	KnowledgeBases     Capability = "knowledge_bases"
)

type Blocker string

const (
	BlockerNoPermission   Blocker = "no_permission"
	BlockerAtLimit        Blocker = "at_limit"
	BlockerNoSubscription Blocker = "no_subscription"
	BlockerUnavailable    Blocker = "unavailable"
	BlockerNeedsOfficial  Blocker = "needs_official_whatsapp"
)

type Usage struct {
	Used  int `json:"used"`
	Total int `json:"total"`
}

type Status struct {
	Capability Capability `json:"capability"`
	Count      int        `json:"count"`
	Usage      *Usage     `json:"usage,omitempty"`
	CanAdd     bool       `json:"canAdd"`
	Blocker    Blocker    `json:"blocker,omitempty"`
}

type Snapshot struct {
	SubscriptionActive bool     `json:"subscriptionActive"`
	BalanceMicros      int64    `json:"balanceMicros"`
	Capabilities       []Status `json:"capabilities"`
}

func (s *Snapshot) Get(c Capability) (Status, bool) {
	for _, status := range s.Capabilities {
		if status.Capability == c {
			return status, true
		}
	}
	return Status{}, false
}

type Person struct {
	WorkspaceID string
	UserID      string
	SystemAdmin bool
}

type Probe interface {
	Capability() Capability
	Probe(ctx context.Context, person Person) (Status, error)
}

type SnapshotUseCase interface {
	Snapshot(ctx context.Context, person Person) (*Snapshot, error)
}

func Unavailable(c Capability) Status {
	return Status{Capability: c, Blocker: BlockerUnavailable}
}
