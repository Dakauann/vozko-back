package unofficial_whatsapp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"vozko/domain/shared"
)

const (
	ScriptMinMessages = 2

	ScriptMaxMessages = 8

	MaxScriptVariants = 10

	MaxScriptedTargets = 200

	ScriptedSeedBatchSize = 25

	ScriptSubjectsPerCall = 5

	MaxScriptContextRunes = 600

	MaxScriptTurnRunes = 400
)

const ScriptSchemaKeyThreads = "threads"

const scriptNameParameter = 1

var (
	ErrScriptBodyRequired           = errors.New("unofficial whatsapp: the seed script needs at least one first message")
	ErrScriptBodyEmpty              = errors.New("unofficial whatsapp: a seed script message cannot be empty")
	ErrScriptBodyTooLong            = errors.New("unofficial whatsapp: a seed script message is too long")
	ErrScriptVariantMismatch        = errors.New("unofficial whatsapp: every seed script variant must use the same variables")
	ErrScriptTooManyVariants        = errors.New("unofficial whatsapp: too many seed script variants")
	ErrScriptMessageCountOutOfRange = errors.New("unofficial whatsapp: the seeded thread length is out of range")
	ErrScriptMediaIDRequired        = errors.New("unofficial whatsapp: the seed script attachment needs a media id")
	ErrScriptMediaKindInvalid       = errors.New("unofficial whatsapp: the seed script attachment is not a kind this channel can carry")
)

type SeedAttachment struct {
	MediaID string    `json:"mediaId"`
	Kind    MediaKind `json:"kind"`
}

func (a *SeedAttachment) Normalize() {
	if a == nil {
		return
	}
	a.MediaID = strings.TrimSpace(a.MediaID)
	a.Kind = MediaKind(strings.ToLower(strings.TrimSpace(string(a.Kind))))
}

func (a *SeedAttachment) Validate() error {
	if a == nil {
		return nil
	}
	if a.MediaID == "" {
		return ErrScriptMediaIDRequired
	}
	if !a.Kind.CanAttach() {
		return ErrScriptMediaKindInvalid
	}
	return nil
}

type SeedScript struct {
	Bodies []string `json:"bodies"`

	MaxMessages int `json:"maxMessages"`

	Attachment *SeedAttachment `json:"attachment,omitempty"`

	Context string `json:"context,omitempty"`
}

func (s *SeedScript) Normalize() {
	if s == nil {
		return
	}
	s.Bodies = shared.NonEmptyTrimmed(s.Bodies)
	s.Context, _ = shared.TruncateRunes(strings.TrimSpace(s.Context), MaxScriptContextRunes)

	s.Attachment.Normalize()
	if s.Attachment != nil && s.Attachment.MediaID == "" {
		s.Attachment = nil
	}

	switch {
	case s.MaxMessages <= 0:
		s.MaxMessages = 4
	case s.MaxMessages < ScriptMinMessages:
		s.MaxMessages = ScriptMinMessages
	case s.MaxMessages > ScriptMaxMessages:
		s.MaxMessages = ScriptMaxMessages
	}
}

func (s *SeedScript) Validate() error {
	if s == nil {
		return nil
	}
	if len(s.Bodies) == 0 {
		return ErrScriptBodyRequired
	}
	if len(s.Bodies) > MaxScriptVariants {
		return fmt.Errorf("%w: at most %d", ErrScriptTooManyVariants, MaxScriptVariants)
	}
	for _, body := range s.Bodies {
		if strings.TrimSpace(body) == "" {
			return ErrScriptBodyEmpty
		}
		if TextTooLong(body) {
			return ErrScriptBodyTooLong
		}
	}
	if !shared.PositionalParametersAgree(s.Bodies) {
		return ErrScriptVariantMismatch
	}
	if s.MaxMessages < ScriptMinMessages || s.MaxMessages > ScriptMaxMessages {
		return fmt.Errorf("%w: %d to %d", ErrScriptMessageCountOutOfRange, ScriptMinMessages, ScriptMaxMessages)
	}
	return s.Attachment.Validate()
}

func (s SeedScript) OpeningFor(target SeedTarget) string {
	if len(s.Bodies) == 0 {
		return ""
	}
	variant := shared.VariantIndexFor(target.Number, len(s.Bodies))
	return shared.RenderPositional(s.Bodies[variant], []string{target.Name})
}

func (s SeedScript) UsesName() bool {
	for _, body := range s.Bodies {
		if _, ok := shared.PositionalParameters(body)[scriptNameParameter]; ok {
			return true
		}
	}
	return false
}

type ScriptTurn struct {
	FromLead bool   `json:"fromLead"`
	Text     string `json:"text"`
}

func (s SeedScript) AcceptTurns(turns []ScriptTurn) []ScriptTurn {
	max := s.MaxMessages
	if max < ScriptMinMessages {
		max = ScriptMinMessages
	}
	if max > ScriptMaxMessages {
		max = ScriptMaxMessages
	}
	room := max - 1
	if room <= 0 {
		return nil
	}

	out := make([]ScriptTurn, 0, room)
	expectLead := true
	for _, turn := range turns {
		if len(out) >= room {
			break
		}
		text := strings.TrimSpace(turn.Text)
		if text == "" {
			continue
		}
		if turn.FromLead != expectLead {
			continue
		}
		capped, _ := shared.TruncateRunes(text, MaxScriptTurnRunes)
		out = append(out, ScriptTurn{FromLead: turn.FromLead, Text: capped})
		expectLead = !expectLead
	}
	return out
}

type ScriptSubject struct {
	Ref          int    `json:"ref"`
	Name         string `json:"name"`
	FirstMessage string `json:"firstMessage"`
}

type ScriptRequest struct {
	WorkspaceID string
	Model       string
	Context     string
	MaxMessages int
	Subjects    []ScriptSubject
}

type ScriptedThread struct {
	Ref   int          `json:"ref"`
	Turns []ScriptTurn `json:"turns"`
}

type ScriptResult struct {
	Threads          []ScriptedThread
	Model            string
	PromptTokens     int
	CompletionTokens int
	FinishReason     string
}

type ConversationScripter interface {
	Script(ctx context.Context, req ScriptRequest) (*ScriptResult, error)
}

func ScriptResponseSchema(maxTurns int) map[string]any {
	if maxTurns < 1 {
		maxTurns = 1
	}
	turn := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"fromLead": map[string]any{
				"type":        "boolean",
				"description": "true se a mensagem e do lead, false se e da empresa.",
			},
			"text": map[string]any{
				"type":        "string",
				"description": "O texto da mensagem, curto como uma mensagem de WhatsApp.",
				"maxLength":   MaxScriptTurnRunes,
			},
		},
		"required":             []string{"fromLead", "text"},
		"additionalProperties": false,
	}

	thread := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ref": map[string]any{
				"type":        "integer",
				"description": "O numero (ref) do contato, exatamente como recebido.",
			},
			"turns": map[string]any{
				"type":        "array",
				"description": "As mensagens DEPOIS da primeira, alternando: lead, empresa, lead...",
				"items":       turn,
				"maxItems":    maxTurns,
			},
		},
		"required":             []string{"ref", "turns"},
		"additionalProperties": false,
	}

	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			ScriptSchemaKeyThreads: map[string]any{
				"type":        "array",
				"description": "Uma entrada por contato recebido. Cada ref deve aparecer exatamente uma vez.",
				"items":       thread,
			},
		},
		"required":             []string{ScriptSchemaKeyThreads},
		"additionalProperties": false,
	}
}
