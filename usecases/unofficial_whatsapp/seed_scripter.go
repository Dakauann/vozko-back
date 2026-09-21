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

const (
	scriptReasoningCap = 256

	scriptTemperature = 0.9

	scriptTokensPerMessage = 80

	scriptTokenFloor = 512

	scriptSchemaName = "seeded_conversation_threads"
)

var (
	ErrScriptWorkspaceRequired = errors.New("unofficial whatsapp: a scripted seed needs a workspace id")
	ErrScriptNoSubjects        = errors.New("unofficial whatsapp: a scripted seed needs at least one subject")
	errScriptEmptyResponse     = errors.New("unofficial whatsapp: empty model response")
)

type aiConversationScripter struct {
	ai           ai.Service
	defaultModel string
}

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
	replies := maxMessages - 1

	out, err := s.ai.Generate(ctx, ai.GenerateInput{
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

func scriptMaxTokens(subjects, maxMessages int) int {
	ceiling := subjects * maxMessages * scriptTokensPerMessage
	if ceiling < scriptTokenFloor {
		return scriptTokenFloor
	}
	return ceiling
}

type scriptResponse struct {
	Threads []uw.ScriptedThread `json:"threads"`
}

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
