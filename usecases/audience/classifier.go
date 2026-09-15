package audience_usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"vozko/domain/ai"
	ca "vozko/domain/audience"
)

// The classifier is the ONLY place the engine touches the AI port. It
// transports one batch and hands back raw results; the use case validates
// labels and reconciles refs.
//
// Every guard from plan §7.1 is set here:
//   - MaxTokens is the budget's hard cap, so a runaway generation costs a
//     bounded amount of the customer's money.
//   - ReasoningMaxTokens is capped: on reasoning models thinking counts
//     against MaxTokens and an uncapped budget can yield an empty turn.
//   - Temperature 0: classification, not generation.
//   - ResponseFormat is the strict JSON schema rendered from the rubric.
//   - Tools are nil (plan §2.3): the model returns data, the use case persists.
//   - WorkspaceID is set: THAT is the entire token-billing integration.

const (
	// reasoningCap is what a reasoning model may spend thinking per batch.
	reasoningCap = 256
	// schemaName is the JSON schema's provider-visible name.
	schemaName = "comment_batch_classification"
)

var errEmptyResponse = errors.New("comment analysis: empty model response")

type aiClassifier struct {
	ai           ai.Service
	defaultModel string
}

// NewClassifier builds the classifier over the AI port. defaultModel is used
// when the account's settings name none.
func NewClassifier(service ai.Service, defaultModel string) ca.Classifier {
	return &aiClassifier{ai: service, defaultModel: strings.TrimSpace(defaultModel)}
}

func (c *aiClassifier) Classify(ctx context.Context, req ca.ClassifyRequest) (*ca.ClassifyResult, error) {
	if strings.TrimSpace(req.WorkspaceID) == "" {
		// Refuse rather than leak: a call without a workspace is a call
		// nobody pays for, which the adapter logs as a revenue leak.
		return nil, ca.ErrWorkspaceRequired
	}
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = c.defaultModel
	}
	userMessage, err := BuildUserMessage(req.Batch)
	if err != nil {
		return nil, err
	}

	out, err := c.ai.Generate(ctx, ai.GenerateInput{
		WorkspaceID:        req.WorkspaceID,
		Model:              model,
		SystemPrompt:       BuildSystemPromptFor(req.SubjectKind, req.Topics, req.Context, req.Instructions),
		Messages:           []ai.Message{{Role: ai.RoleUser, Content: userMessage}},
		Temperature:        0,
		MaxTokens:          req.Batch.MaxOutputTokens(),
		ReasoningMaxTokens: reasoningCap,
		ResponseFormat: &ai.ResponseFormat{
			Type:                  ai.ResponseFormatJSONSchema,
			JSONSchemaName:        schemaName,
			JSONSchemaDescription: "Classificação de um lote de comentários, uma entrada por ref.",
			JSONSchema:            ca.BatchResponseSchemaFor(req.SubjectKind, req.Topics),
			JSONSchemaStrict:      true,
		},
		Tools: nil,
	})
	if err != nil {
		return nil, err
	}

	res := &ca.ClassifyResult{
		FinishReason:     out.FinishReason,
		Model:            model,
		PromptTokens:     out.Usage.PromptTokens,
		CompletionTokens: out.Usage.CompletionTokens,
	}
	// A truncated body is not parsed (plan §7.2): the caller halves and
	// retries. Returning the usage lets it still be recorded and billed.
	if out.FinishReason == "length" {
		return res, nil
	}
	results, err := parseBatchResponse(out.Message.Content)
	if err != nil {
		return res, err
	}
	res.Results = results
	return res, nil
}

// parseBatchResponse decodes the model's JSON.
//
// The markdown fence some providers still wrap strict-schema output in is
// handled by ai.UnfenceJSON, shared with every other strict-schema caller: it
// is the port's behaviour, not this pass's.
func parseBatchResponse(content string) ([]ca.BatchResult, error) {
	body := ai.UnfenceJSON(content)
	if body == "" {
		return nil, errEmptyResponse
	}
	var out ca.BatchResponse
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		return nil, fmt.Errorf("comment analysis: unparseable model response: %w", err)
	}
	return out.Results, nil
}
