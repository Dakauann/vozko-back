package unofficial_whatsapp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"vozko/domain/shared"
)

// Scripting is the other half of seeding: instead of an empty chat, the
// conversation opens with the message the company would have sent and carries
// the short exchange that followed it.
//
// The first message is the OPERATOR's, deterministically chosen from variants
// they wrote. Everything after it is the model's. Nothing is sent: this writes
// rows into a conversation exactly as seeding already does, and never reaches
// WhatsApp.
//
// The rules below are the enforcement half of a prompt. The prompt ASKS the
// model for an alternating thread of at most N messages with no invented
// prices, dates or confirmations; AcceptTurns is what makes the count and the
// alternation true regardless of what came back. Instruction is not
// enforcement, and these rows land in a real CRM where an operator reads them
// as real.

const (
	// ScriptMinMessages is "the person answered once" — the operator's opening
	// and one reply. A shorter thread is the blank placeholder that already
	// exists, which is not what ticking this box asked for.
	ScriptMinMessages = 2

	// ScriptMaxMessages is where a seeded thread stops reading as an example
	// and starts reading as a transcript nobody wrote.
	ScriptMaxMessages = 8

	// MaxScriptVariants bounds how many openings one import may rotate.
	//
	// The same number campaigns cap their bodies at, and for the same reason:
	// each variant is a text somebody has to proofread, and ten is already the
	// point past which most of them have not been read.
	MaxScriptVariants = 10

	// MaxScriptedTargets is the single most important money guard: how many
	// conversations of one import may cost anything at all.
	//
	// Row 201 is not dropped. It gets today's plain empty chat, so every
	// imported number still lands in the inbox and only the first two hundred
	// are paid for. Two hundred is the number that makes an empty workspace
	// look worked without a bill anybody has to explain, and it lives here so
	// that judgement is in one place.
	MaxScriptedTargets = 200

	// ScriptedSeedBatchSize is how many scripted targets ride in one queue
	// message. Much smaller than SeedBatchSize because each one of these costs
	// model latency on top of the database work, and a batch is the unit of
	// retry: a redelivered batch of 25 is cheap, a redelivered batch of 500
	// with a model call per five is not.
	ScriptedSeedBatchSize = 25

	// ScriptSubjectsPerCall is how many threads one model call writes.
	//
	// Five is a bounded unit of spend and a bounded unit of loss: a call that
	// fails costs five targets their script, not twenty-five, and they fall
	// back to plain empty chats rather than failing the import.
	ScriptSubjectsPerCall = 5

	// MaxScriptContextRunes bounds the operator's free text about the business.
	// It is quoted into a prompt, so it is bounded for cost and truncated
	// rather than refused: a long paste should cost the tail of a sentence.
	MaxScriptContextRunes = 600

	// MaxScriptTurnRunes bounds one line the model wrote. A WhatsApp turn is a
	// sentence or two; anything past this is the model writing an essay into a
	// chat bubble.
	MaxScriptTurnRunes = 400
)

// ScriptSchemaKeyThreads is the top-level array the model returns. Named so the
// schema, the parser and the tests cannot disagree about it.
const ScriptSchemaKeyThreads = "threads"

// scriptNameParameter is the positional variable the lead's name renders into.
//
// {{1}}, the same vocabulary campaigns use, so there is one placeholder syntax
// in the product rather than one per feature. It is the ONLY variable this
// feature supplies: anything else an operator writes is left as typed, so a
// script asking for a column that does not exist is visibly wrong rather than
// silently blank.
const scriptNameParameter = 1

var (
	ErrScriptBodyRequired           = errors.New("unofficial whatsapp: the seed script needs at least one first message")
	ErrScriptBodyEmpty              = errors.New("unofficial whatsapp: a seed script message cannot be empty")
	ErrScriptBodyTooLong            = errors.New("unofficial whatsapp: a seed script message is too long")
	ErrScriptVariantMismatch        = errors.New("unofficial whatsapp: every seed script variant must use the same variables")
	ErrScriptTooManyVariants        = errors.New("unofficial whatsapp: too many seed script variants")
	ErrScriptMessageCountOutOfRange = errors.New("unofficial whatsapp: the seeded thread length is out of range")
)

// SeedScript is what a system administrator wrote: the opening message, its
// variants, and how far the thread may run.
//
// It travels IN the seed request rather than being stored. The act is per
// import and the checkbox is in the import dialog; a stored template would be a
// table, a CRUD surface and a migration for something nobody asked to reuse.
type SeedScript struct {
	// Bodies is the operator's opening message, in variants.
	//
	// A list for the same reason campaigns carry one, minus the ban argument:
	// nothing is sent here, so this is not spam avoidance. It is so two hundred
	// seeded conversations do not read as one conversation copied two hundred
	// times, which is exactly what a demo inbox must not look like.
	Bodies []string `json:"bodies"`

	// MaxMessages is the whole thread's ceiling, counting the operator's
	// opening. Clamped into [ScriptMinMessages, ScriptMaxMessages].
	MaxMessages int `json:"maxMessages"`

	// Context is optional free text about what the business sells.
	//
	// Without it the model has only a one-line opener to reason from, which is
	// thin: two hundred characters of context is the difference between a
	// plausible thread and a generic one. Quoted into the prompt as DATA, never
	// as instructions.
	Context string `json:"context,omitempty"`
}

// Normalize cleans the script into the shape the rest of the code may assume.
//
// Nil-safe, because the field is optional on the request and "no script" has to
// stay distinguishable from "a broken script".
func (s *SeedScript) Normalize() {
	if s == nil {
		return
	}
	s.Bodies = shared.NonEmptyTrimmed(s.Bodies)
	s.Context, _ = shared.TruncateRunes(strings.TrimSpace(s.Context), MaxScriptContextRunes)

	// Clamped rather than rejected. An out-of-range cap is a caller bug and the
	// nearest legal value is obvious, so it should not cost the operator their
	// import. Validate still refuses one on the way in, where the caller can
	// still be told.
	switch {
	case s.MaxMessages <= 0:
		// The default is the middle of the range, not the minimum: four
		// messages is the shortest thread that reads as a conversation rather
		// than as a question and an answer.
		s.MaxMessages = 4
	case s.MaxMessages < ScriptMinMessages:
		s.MaxMessages = ScriptMinMessages
	case s.MaxMessages > ScriptMaxMessages:
		s.MaxMessages = ScriptMaxMessages
	}
}

// Validate reports whether this script can be acted on at all.
//
// A nil script is valid: it means the import did not ask for scripting.
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
	// Every variant must use the SAME variables, not merely the same count. A
	// variant reading {{1}} beside one reading {{2}} would write a raw "{{2}}"
	// into a real CRM conversation for everyone assigned the second.
	if !shared.PositionalParametersAgree(s.Bodies) {
		return ErrScriptVariantMismatch
	}
	if s.MaxMessages < ScriptMinMessages || s.MaxMessages > ScriptMaxMessages {
		return fmt.Errorf("%w: %d to %d", ErrScriptMessageCountOutOfRange, ScriptMinMessages, ScriptMaxMessages)
	}
	return nil
}

// OpeningFor is the first message this target receives, rendered.
//
// Keyed on the NUMBER rather than on a random draw, so a re-import gives the
// same person the same opening line and "which text did this contact get" is
// answerable from the row.
func (s SeedScript) OpeningFor(target SeedTarget) string {
	if len(s.Bodies) == 0 {
		return ""
	}
	variant := shared.VariantIndexFor(target.Number, len(s.Bodies))
	return shared.RenderPositional(s.Bodies[variant], []string{target.Name})
}

// UsesName reports whether any variant renders the lead's name.
//
// It decides what happens to a row with no name: under a body carrying {{1}},
// "Oi , tudo bem?" is worse than an empty chat, so that target is seeded plain
// and counted. Any body using it is enough, because which variant a given
// target receives is not known until OpeningFor.
func (s SeedScript) UsesName() bool {
	for _, body := range s.Bodies {
		if _, ok := shared.PositionalParameters(body)[scriptNameParameter]; ok {
			return true
		}
	}
	return false
}

// ScriptTurn is one message in a seeded thread, after the operator's opening.
type ScriptTurn struct {
	// FromLead is whose side of the conversation this is. The operator's
	// opening is not a ScriptTurn, so the first accepted turn is always the
	// lead's.
	FromLead bool   `json:"fromLead"`
	Text     string `json:"text"`
}

// AcceptTurns decides what actually gets written, whatever the model returned.
//
// Four rules, in order, and each one closes a way a model answer could turn
// into a thread nobody asked for:
//
//   - Empty lines are dropped. A blank bubble in a seeded thread reads as a
//     failed send.
//   - Each line is capped. A turn is a sentence or two; the model writing five
//     hundred words into a chat bubble is not a conversation.
//   - Alternation is enforced starting with the LEAD, because the operator's
//     opening is ours. A turn on the wrong side is DROPPED, not relabelled:
//     putting our words on the lead's side of a real CRM thread is worse than a
//     shorter thread, and relabelling is exactly how "seu pagamento foi
//     aprovado" ends up attributed to the customer.
//   - The whole list is cut so the thread, INCLUDING the opening, never exceeds
//     MaxMessages.
//
// An empty result is a legitimate answer and means the caller should fall back
// to a plain empty chat.
func (s SeedScript) AcceptTurns(turns []ScriptTurn) []ScriptTurn {
	max := s.MaxMessages
	if max < ScriptMinMessages {
		max = ScriptMinMessages
	}
	if max > ScriptMaxMessages {
		max = ScriptMaxMessages
	}
	// The operator's opening is message one, so this is what is left.
	room := max - 1
	if room <= 0 {
		return nil
	}

	out := make([]ScriptTurn, 0, room)
	// The first message after ours is theirs.
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

// ---- The AI contract ----
//
// Declared in the domain so the domain owns the shape of the question and the
// answer, and the use case only implements the transport. Same posture as
// domain/audience's ports: the engine never knows which provider answered.

// ScriptSubject is one person the model is asked to write a thread for.
type ScriptSubject struct {
	// Ref is how the answer is matched back to the target. An integer, not the
	// phone number: the model is given no reason to echo a real number back,
	// and a ref it invents matches nothing rather than matching the wrong
	// person.
	Ref          int    `json:"ref"`
	Name         string `json:"name"`
	FirstMessage string `json:"firstMessage"`
}

// ScriptRequest is one model call: several subjects, one set of rules.
type ScriptRequest struct {
	// WorkspaceID is not metadata. It IS the billing integration: the AI
	// adapter publishes a completion event against it and the balance use case
	// debits. A call without one is a call nobody pays for.
	WorkspaceID string
	Model       string
	Context     string
	MaxMessages int
	Subjects    []ScriptSubject
}

// ScriptedThread is the model's answer for one subject.
type ScriptedThread struct {
	Ref   int          `json:"ref"`
	Turns []ScriptTurn `json:"turns"`
}

// ScriptResult is one call's answer plus what it cost, so the caller can log
// and account for it.
type ScriptResult struct {
	Threads          []ScriptedThread
	Model            string
	PromptTokens     int
	CompletionTokens int
	FinishReason     string
}

// ConversationScripter writes the threads. One method, so a deployment without
// an AI service simply has none and seeding behaves exactly as it did before.
type ConversationScripter interface {
	Script(ctx context.Context, req ScriptRequest) (*ScriptResult, error)
}

// ScriptResponseSchema is the strict JSON schema the model answers against.
//
// maxTurns is put IN the schema rather than only in the prose, because a
// strict-schema provider enforces it: that is one more thing standing between
// the model and a longer thread than the operator asked for, ahead of
// AcceptTurns which enforces it regardless.
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
