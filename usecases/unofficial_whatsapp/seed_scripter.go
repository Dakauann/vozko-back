package unofficial_whatsapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"vozko/domain/ai"
	uw "vozko/domain/unofficial_whatsapp"
)

// The scripter is the ONLY place seeding touches the AI port. It transports one
// batch of subjects and hands back what the model wrote; the use case decides
// what is acceptable and what gets written.
//
// Line for line the shape of audience_usecase.NewClassifier, with two
// deliberate differences:
//
//   - Temperature is WARM, not zero. The classifier wants the same answer every
//     time; this wants two hundred threads that do not read as one thread
//     copied two hundred times, which is exactly what a seeded inbox must not
//     look like.
//   - The model returns TEXT, not refs. The audience pass returns refs
//     precisely so the model cannot restate the comment; here the text is the
//     product.

const (
	// scriptReasoningCap is what a reasoning model may spend thinking per call.
	//
	// On those models thinking counts against MaxTokens, so an uncapped budget
	// can consume the whole output allowance and return an empty turn. This is
	// a short writing task; it does not need a long think.
	scriptReasoningCap = 256

	// scriptTemperature is warm enough that five subjects in one call do not
	// come back as five copies of the same exchange.
	scriptTemperature = 0.9

	// scriptTokensPerMessage is the per-message output estimate the ceiling is
	// derived from. A WhatsApp turn is a sentence or two; eighty tokens is
	// generous for one and bounded for two hundred.
	scriptTokensPerMessage = 80

	// scriptTokenFloor keeps a very small call from being given a ceiling too
	// tight to answer under, which would burn the tokens and return nothing.
	scriptTokenFloor = 512

	// scriptSchemaName is the JSON schema's provider-visible name.
	scriptSchemaName = "seeded_conversation_threads"
)

var (
	// ErrScriptWorkspaceRequired refuses rather than leaks: a call with no
	// workspace is a call nobody pays for, which the AI adapter logs as a
	// revenue leak.
	ErrScriptWorkspaceRequired = errors.New("unofficial whatsapp: a scripted seed needs a workspace id")
	ErrScriptNoSubjects        = errors.New("unofficial whatsapp: a scripted seed needs at least one subject")
	errScriptEmptyResponse     = errors.New("unofficial whatsapp: empty model response")
)

type aiConversationScripter struct {
	ai           ai.Service
	defaultModel string
}

// NewConversationScripter builds the scripter over the AI port.
//
// A nil service yields a NIL scripter, not a broken one. Seeding takes the
// scripter as an optional dependency and skips it when absent, so a deployment
// without an AI service keeps writing plain empty chats exactly as before
// rather than failing every scripted batch.
func NewConversationScripter(service ai.Service, defaultModel string) uw.ConversationScripter {
	if service == nil {
		return nil
	}
	return &aiConversationScripter{ai: service, defaultModel: strings.TrimSpace(defaultModel)}
}

func (s *aiConversationScripter) Script(ctx context.Context, req uw.ScriptRequest) (*uw.ScriptResult, error) {
	if strings.TrimSpace(req.WorkspaceID) == "" {
		return nil, ErrScriptWorkspaceRequired
	}
	if len(req.Subjects) == 0 {
		return nil, ErrScriptNoSubjects
	}

	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = s.defaultModel
	}
	userMessage, err := buildScriptUserMessage(req.Subjects)
	if err != nil {
		return nil, err
	}

	maxMessages := req.MaxMessages
	if maxMessages < uw.ScriptMinMessages {
		maxMessages = uw.ScriptMinMessages
	}
	if maxMessages > uw.ScriptMaxMessages {
		maxMessages = uw.ScriptMaxMessages
	}
	// The replies, not the messages: the operator's opening already exists and
	// the model is not asked to write it.
	replies := maxMessages - 1

	out, err := s.ai.Generate(ctx, ai.GenerateInput{
		// THE billing integration. Everything else here bounds the spend; this
		// is what makes it the workspace's own balance that pays.
		WorkspaceID:        req.WorkspaceID,
		Model:              model,
		SystemPrompt:       buildScriptSystemPrompt(req.Context, maxMessages),
		Messages:           []ai.Message{{Role: ai.RoleUser, Content: userMessage}},
		Temperature:        scriptTemperature,
		MaxTokens:          scriptMaxTokens(len(req.Subjects), maxMessages),
		ReasoningMaxTokens: scriptReasoningCap,
		ResponseFormat: &ai.ResponseFormat{
			Type:                  ai.ResponseFormatJSONSchema,
			JSONSchemaName:        scriptSchemaName,
			JSONSchemaDescription: "Conversas de exemplo, uma por contato recebido.",
			JSONSchema:            uw.ScriptResponseSchema(replies),
			JSONSchemaStrict:      true,
		},
		// The model returns data; this use case writes it. A tool call here
		// would be a model reaching into the CRM.
		Tools: nil,
	})
	if err != nil {
		return nil, err
	}

	res := &uw.ScriptResult{
		Model:            model,
		FinishReason:     out.FinishReason,
		PromptTokens:     out.Usage.PromptTokens,
		CompletionTokens: out.Usage.CompletionTokens,
	}
	// A truncated body is not parsed: half a JSON document is not half a
	// thread. The usage is still returned so what was spent stays visible, and
	// the caller falls back to plain empty chats for these targets.
	if out.FinishReason == "length" {
		return res, nil
	}
	threads, err := parseScriptResponse(out.Message.Content)
	if err != nil {
		return res, err
	}
	res.Threads = threads
	return res, nil
}

// scriptMaxTokens is the per-call output ceiling.
//
// Derived from the ask rather than fixed, because five subjects asking for
// eight messages is a much larger answer than two asking for two, and one
// constant would be either wasteful or truncating. The generous side of the
// estimate on purpose: a ceiling too tight spends the tokens and returns
// nothing usable, which is the most expensive possible outcome.
func scriptMaxTokens(subjects, maxMessages int) int {
	ceiling := subjects * maxMessages * scriptTokensPerMessage
	if ceiling < scriptTokenFloor {
		return scriptTokenFloor
	}
	return ceiling
}

// scriptResponse is the wire shape of the model's answer.
type scriptResponse struct {
	Threads []uw.ScriptedThread `json:"threads"`
}

// parseScriptResponse decodes the model's JSON.
//
// The markdown fence some providers still wrap strict-schema output in is
// handled by ai.UnfenceJSON, shared with the audience classifier: it is the
// same provider behaviour, and two copies would be two chances to fix only one.
func parseScriptResponse(content string) ([]uw.ScriptedThread, error) {
	body := ai.UnfenceJSON(content)
	if body == "" {
		return nil, errScriptEmptyResponse
	}
	var out scriptResponse
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		return nil, fmt.Errorf("unofficial whatsapp: unparseable script response: %w", err)
	}
	return out.Threads, nil
}
