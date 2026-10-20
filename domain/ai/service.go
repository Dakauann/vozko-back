package ai

import (
	"context"
	"time"

	"vozko/domain/tools"
)

type Service interface {
	Generate(ctx context.Context, input GenerateInput) (*GenerateOutput, error)
	GenerateStream(ctx context.Context, input GenerateInput) (<-chan StreamEvent, error)
	GetAvaibleModels(ctx context.Context) ([]string, error)
	GetModelsWithPricing(ctx context.Context) ([]ModelInfo, error)
}

type ModelInfo struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	PromptPrice     float64           `json:"promptPrice"`
	CompletionPrice float64           `json:"completionPrice"`
	Created         int64             `json:"created,omitempty"`
	ContextLength   int64             `json:"contextLength,omitempty"`
	Capabilities    []ModelCapability `json:"capabilities"`
	Reasoning       *ReasoningOptions `json:"reasoning,omitempty"`
}

type StreamEventType int

const (
	StreamEventToken StreamEventType = iota
	StreamEventToolCall
	StreamEventToolResult
	StreamEventDone
	StreamEventError
	StreamEventReasoning
)

type StreamEvent struct {
	Type             StreamEventType
	Token            string
	ToolCall         *ToolCall
	ToolResult       *tools.ExecutionResult
	Error            error
	FullText         string
	AllToolCalls     []ToolCall
	ShouldEndSession bool
	Usage            *Usage
	FinishReason     string
	Reasoning        []ReasoningBlock
}

type ResponseFormatType string

const (
	ResponseFormatJSONObject ResponseFormatType = "json_object"
	ResponseFormatJSONSchema ResponseFormatType = "json_schema"
	ResponseFormatText       ResponseFormatType = "text"
)

type ResponseFormat struct {
	Type                  ResponseFormatType
	JSONSchemaName        string
	JSONSchemaDescription string
	JSONSchema            map[string]any
	JSONSchemaStrict      bool
}

type GenerateInput struct {
	Model              string
	SystemPrompt       string
	ClearEmojis        bool
	Messages           []Message
	SegmentedResponse  bool
	Temperature        float32
	MaxTokens          int
	ReasoningMaxTokens int
	ReasoningEffort    ReasoningLevel
	ResponseFormat     *ResponseFormat
	Tools              []tools.Definition
	ToolConfigs        map[string]map[string]interface{}
	ToolExecutionMode  ToolExecutionMode
	ToolChoice         string
	MaxToolIterations  int
	WorkspaceID        string
	BillingReference   string
	SessionID          string
	AsOf               time.Time
	VolatileTail       int
	AgentTurn          AgentTurn
}

type AgentTurn struct {
	EntryID    string
	EntryType  string
	AgentID    string
	CampaignID string
}

func (t AgentTurn) Identified() bool {
	return t.EntryID != "" && t.EntryType != "" && t.AgentID != ""
}

type GenerateOutput struct {
	Message      Message
	Messages     []string
	ToolCalls    []ToolCall
	Usage        Usage
	FinishReason string
}

type Usage struct {
	PromptTokens       int
	CompletionTokens   int
	TotalTokens        int
	CachedTokens       int
	CacheWriteTokens   int
	ProviderCostMicros int64
}

func (in GenerateInput) ExecutionModeOrDefault() ToolExecutionMode {
	if in.ToolExecutionMode == "" {
		return ToolExecutionModeAuto
	}
	return in.ToolExecutionMode
}

func (in GenerateInput) ToolIterationsOrDefault() int {
	if in.MaxToolIterations <= 0 {
		return DefaultMaxToolIterations
	}
	return in.MaxToolIterations
}
