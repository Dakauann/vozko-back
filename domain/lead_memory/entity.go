// Package lead_memory is the per-lead persistent memory: small, discrete facts
// about the person an agent talks to, written by AI agents (via a tool) and by
// operators (via the CRM), and read back into every agent conversation with
// that lead on every channel.
//
// The load-bearing invariant: one write model, one read model, no matter who
// the actor is. The AI tool and the operator endpoints call the same use cases,
// which enforce the same limits, dedup, and attribution: there is no "AI
// memory" separate from "operator memory".
package lead_memory

import (
	"strings"
	"time"
	"unicode/utf8"

	"vozko/domain/actor"
)

const (
	// MaxContentLen bounds one memory, in runes, after trimming. Memories are
	// facts, not documents: the cap keeps the prompt block honest and starves
	// prompt-injection payloads of room.
	MaxContentLen = 600

	// MaxActiveMemoriesPerLead is the storage cap. Hitting it is a curation
	// signal: update or delete something, not a scaling problem to engineer
	// around.
	MaxActiveMemoriesPerLead = 100

	// MaxPromptMemories and MaxPromptChars bound the block injected into the
	// system prompt. Surplus context measurably degrades model behavior, so the
	// newest memories win and truncation is always announced, never silent.
	MaxPromptMemories = 40
	MaxPromptChars    = 6000

	// MinIDPrefixLen is the shortest memory-id prefix accepted when resolving a
	// reference (the prompt block renders 8-char prefixes).
	MinIDPrefixLen = 8
)

// Category classifies a memory so the tool schema, the database, and the UI
// filter agree on one vocabulary. Closed set with Other as the escape hatch.
type Category string

const (
	CategoryPersonal   Category = "personal"   // fatos pessoais (família, aniversário…)
	CategoryPreference Category = "preference" // preferências (canal, horário, pagamento…)
	CategoryDeal       Category = "deal"       // contexto de negociação (orçamento, prazo…)
	CategoryObjection  Category = "objection"  // objeções levantadas
	CategoryCommitment Category = "commitment" // combinados ("retornar sexta", "enviar proposta")
	CategoryEvent      Category = "event"      // episódico ("pediu 2ª via do boleto em 10/08")
	CategoryOther      Category = "other"
)

// AllCategories lists every valid category, in the order tool enums and UI
// selects should present them.
func AllCategories() []Category {
	return []Category{
		CategoryPersonal,
		CategoryPreference,
		CategoryDeal,
		CategoryObjection,
		CategoryCommitment,
		CategoryEvent,
		CategoryOther,
	}
}

func (c Category) Valid() bool {
	switch c {
	case CategoryPersonal, CategoryPreference, CategoryDeal,
		CategoryObjection, CategoryCommitment, CategoryEvent, CategoryOther:
		return true
	default:
		return false
	}
}

// LeadMemory is one remembered fact about a lead.
type LeadMemory struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	LeadID      string `json:"leadId"`

	Category Category `json:"category"`
	Content  string   `json:"content"`

	// ActorKind + ActorID attribute the write: a bare user UUID for humans,
	// "ai:{agentID}" for agents, "system" for platform actions. Everything an
	// agent stores must be attributable and operator-visible.
	ActorKind actor.Kind `json:"actorKind"`
	ActorID   string     `json:"actorId"`

	// SourceEntryID / SourceEntryType are provenance: the conversation the
	// write happened in, when it happened in one. Operator edits from the lead
	// page have no entry and leave them nil.
	SourceEntryID   *string `json:"sourceEntryId,omitempty"`
	SourceEntryType *string `json:"sourceEntryType,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Normalize trims free text, defaults the category, and canonicalizes the
// actor, so equivalent inputs produce identical rows.
func (m *LeadMemory) Normalize() {
	m.Content = strings.TrimSpace(m.Content)
	if m.Category == "" {
		m.Category = CategoryOther
	}
	m.ActorKind, m.ActorID = actor.Normalize(m.ActorKind, m.ActorID)
	m.SourceEntryID = trimmedOrNil(m.SourceEntryID)
	m.SourceEntryType = trimmedOrNil(m.SourceEntryType)
}

// Validate checks the invariants every writer, tool or HTTP, must satisfy.
func (m *LeadMemory) Validate() error {
	if strings.TrimSpace(m.WorkspaceID) == "" {
		return ErrWorkspaceRequired
	}
	if strings.TrimSpace(m.LeadID) == "" {
		return ErrLeadRequired
	}
	if m.Content == "" {
		return ErrContentRequired
	}
	if utf8.RuneCountInString(m.Content) > MaxContentLen {
		return ErrContentTooLong
	}
	if !m.Category.Valid() {
		return ErrInvalidCategory
	}
	if !m.ActorKind.Valid() || strings.TrimSpace(m.ActorID) == "" {
		return ErrActorRequired
	}
	return nil
}

// NormalizeContent reduces content to its dedup key: lowercased with
// whitespace collapsed. Two writers phrasing the same fact with different
// spacing or casing land on the same key; genuinely different wordings do not
// (semantic dedup is deliberately out of scope: deterministic beats clever).
func NormalizeContent(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

func trimmedOrNil(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
