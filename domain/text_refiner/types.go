package text_refiner

import (
	"context"
	"errors"

	"vozko/domain/ai"
)

type DiffOp string

const (
	DiffOpEqual DiffOp = "equal"

	DiffOpInsert DiffOp = "insert"

	DiffOpDelete DiffOp = "delete"
)

type DiffSegment struct {
	Op   DiffOp `json:"op"`
	Text string `json:"text"`
}

type TextKind string

const (
	TextKindGeneric TextKind = "generic"

	TextKindMessagingPrompt TextKind = "messaging_prompt"

	TextKindInitialMessage TextKind = "initial_message"
)

func (k TextKind) Normalize() TextKind {
	switch k {
	case TextKindMessagingPrompt, TextKindInitialMessage, TextKindGeneric:
		return k
	default:
		return TextKindGeneric
	}
}

type RefineInput struct {
	WorkspaceID  string
	UserID       string
	Model        string
	Kind         TextKind
	Instruction  string
	OriginalText string
	MaxTokens    int
	Temperature  float32
}

type RefineOutput struct {
	RequestID            string        `json:"requestId"`
	Model                string        `json:"model"`
	OriginalText         string        `json:"originalText"`
	RefinedText          string        `json:"refinedText"`
	Segments             []DiffSegment `json:"segments"`
	UnifiedDiff          string        `json:"unifiedDiff"`
	PromptTokens         int           `json:"promptTokens"`
	CompletionTokens     int           `json:"completionTokens"`
	EstimatedPriceMicros int64         `json:"estimatedPriceMicros"`
	ActualPriceMicros    int64         `json:"actualPriceMicros"`
	Usage                ai.Usage      `json:"-"`
}

type RefineTextUseCase interface {
	Execute(ctx context.Context, input RefineInput) (*RefineOutput, error)
}

var (
	ErrTextRequired        = errors.New("text_refiner: original text is required")
	ErrInstructionRequired = errors.New("text_refiner: instruction is required")
	ErrTextTooLong         = errors.New("text_refiner: original text exceeds maximum length")
	ErrInstructionTooLong  = errors.New("text_refiner: instruction exceeds maximum length")
	ErrInsufficientBalance = errors.New("text_refiner: insufficient workspace balance")
	ErrModelNotAllowed     = errors.New("text_refiner: model is not allowed")
	ErrAIUnavailable       = errors.New("text_refiner: AI service unavailable")
	ErrEmptyResponse       = errors.New("text_refiner: AI returned an empty response")
)
