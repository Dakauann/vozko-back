package ai

import (
	"context"

	"vozko/domain/tools"
)

type Service interface {
	Generate(ctx context.Context, input GenerateInput) (*GenerateOutput, error)
	GenerateStream(ctx context.Context, input GenerateInput) (<-chan StreamEvent, error)
	GetAvaibleModels(ctx context.Context) ([]string, error)
	GetModelsWithPricing(ctx context.Context) ([]ModelInfo, error)
}

type ModelInfo struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	PromptPrice     float64 `json:"promptPrice"`
	CompletionPrice float64 `json:"completionPrice"`
	// Created is the OpenRouter model creation time as a Unix timestamp (seconds).
	// Used by the UI to flag recently-added models with a "New" badge. 0 when unknown.
	Created int64 `json:"created,omitempty"`
	// ContextLength is the model's maximum context window in tokens, surfaced in the
	// model picker. 0 when the provider doesn't report one.
	ContextLength int64 `json:"contextLength,omitempty"`
}

type StreamEventType int

const (
	StreamEventToken StreamEventType = iota
	StreamEventToolCall
	StreamEventToolResult
	StreamEventDone
	StreamEventError
	// StreamEventReasoning carries the model's internal reasoning/thinking tokens
	// (OpenRouter `reasoning` / `reasoning_content` deltas), distinct from the
	// visible answer in StreamEventToken. Callers may surface it for a live
	// "thinking" view; it is never part of the assistant message content.
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
	// FinishReason is the provider's stop reason for the turn (e.g. "stop",
	// "tool_calls", "length"). Set on StreamEventDone. "length" means the output
	// was truncated by the token budget, for reasoning models this commonly means
	// thinking consumed the whole budget and no usable output (or tool call) was
	// produced. Callers should treat that differently from a deliberate stop.
	FinishReason string
}

// ResponseFormatType is the OpenAI/OpenRouter chat response_format.type value.
type ResponseFormatType string

const (
	// ResponseFormatJSONObject forces a single JSON object (no markdown fences).
	// Provider rule: at least one message must mention "json" (our floor prompts do).
	ResponseFormatJSONObject ResponseFormatType = "json_object"
	// ResponseFormatJSONSchema forces a schema-constrained JSON object when supported.
	ResponseFormatJSONSchema ResponseFormatType = "json_schema"
	// ResponseFormatText is the default free-form text (explicit).
	ResponseFormatText ResponseFormatType = "text"
)

// ResponseFormat maps to provider chat completions response_format.
// Nil on GenerateInput means provider default (text).
type ResponseFormat struct {
	Type ResponseFormatType
	// JSONSchema* used only when Type is ResponseFormatJSONSchema.
	JSONSchemaName        string
	JSONSchemaDescription string
	// JSONSchema is a JSON Schema document (object). Prefer strict when the model supports it.
	JSONSchema       map[string]any
	JSONSchemaStrict bool
}

type GenerateInput struct {
	Model             string
	SystemPrompt      string
	ClearEmojis       bool
	Messages          []Message
	SegmentedResponse bool
	Temperature       float32
	MaxTokens         int
	// ReasoningMaxTokens caps the model's internal reasoning/thinking budget for a
	// turn (OpenRouter `reasoning.max_tokens`). 0 leaves the provider/model default.
	// Reasoning models (Gemini 3, etc.) count thinking against MaxTokens, so an
	// uncapped reasoning budget can consume the whole output budget and yield an
	// empty turn with no tool call; capping it reserves room for actual output.
	ReasoningMaxTokens int
	// ResponseFormat optionally forces JSON object / JSON schema mode (OpenAI-compatible).
	ResponseFormat    *ResponseFormat
	Tools             []tools.Definition
	ToolConfigs       map[string]map[string]interface{}
	ToolExecutionMode ToolExecutionMode
	ToolChoice        string
	MaxToolIterations int
	WorkspaceID       string
}

type GenerateOutput struct {
	Message      Message
	Messages     []string
	ToolCalls    []ToolCall
	Usage        Usage
	FinishReason string // provider stop reason: "stop" | "tool_calls" | "length" | ...
}

type Usage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
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
