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

const (
	reasoningCap = 256
	schemaName   = "comment_batch_classification"
)

var errEmptyResponse = errors.New("comment analysis: empty model response")

type aiClassifier struct {
	ai           ai.Service
	defaultModel string
}

func NewClassifier(service ai.Service, defaultModel string) ca.Classifier {
	return &aiClassifier{ai: service, defaultModel: strings.TrimSpace(defaultModel)}
}

func (c *aiClassifier) Classify(ctx context.Context, req ca.ClassifyRequest) (*ca.ClassifyResult, error) {
	if strings.TrimSpace(req.WorkspaceID) == "" {
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
