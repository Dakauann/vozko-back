package textrefiner

import (
	"vozko/domain/ai"
	"vozko/domain/text_refiner"
)

type RefineRequest struct {
	Text        string  `json:"text" example:"Segue o retorno sobre o seu pedido."`
	Instruction string  `json:"instruction" example:"Deixe o texto mais formal e cordial."`
	Kind        string  `json:"kind,omitempty" example:"messaging_prompt"`
	Model       string  `json:"model,omitempty" example:"openai/gpt-4o-mini"`
	MaxTokens   int     `json:"maxTokens,omitempty" example:"512"`
	Temperature float32 `json:"temperature,omitempty" example:"0.2"`
}

type DiffSegmentResponse struct {
	Op   string `json:"op" example:"insert"`
	Text string `json:"text" example:"Prezado cliente, "`
}

type RefineResponse struct {
	RequestID            string                `json:"requestId" example:"req_a1b2c3d4"`
	Model                string                `json:"model" example:"openai/gpt-4o-mini"`
	OriginalText         string                `json:"originalText" example:"Segue o retorno sobre o seu pedido."`
	RefinedText          string                `json:"refinedText" example:"Prezado cliente, segue o retorno sobre o seu pedido."`
	Segments             []DiffSegmentResponse `json:"segments"`
	UnifiedDiff          string                `json:"unifiedDiff" example:"@@ -1 +1 @@"`
	PromptTokens         int                   `json:"promptTokens" example:"120"`
	CompletionTokens     int                   `json:"completionTokens" example:"48"`
	EstimatedPriceMicros int64                 `json:"estimatedPriceMicros" example:"1500"`
	ActualPriceMicros    int64                 `json:"actualPriceMicros" example:"1420"`
}

type ModelInfoResponse struct {
	ID              string  `json:"id" example:"openai/gpt-4o-mini"`
	Name            string  `json:"name" example:"GPT-4o mini"`
	PromptPrice     float64 `json:"promptPrice" example:"0.15"`
	CompletionPrice float64 `json:"completionPrice" example:"0.6"`
	Created         int64   `json:"created,omitempty" example:"1700000000"`
	ContextLength   int64   `json:"contextLength,omitempty" example:"128000"`
}

func toRefineResponse(o *text_refiner.RefineOutput) RefineResponse {
	resp := RefineResponse{
		RequestID:            o.RequestID,
		Model:                o.Model,
		OriginalText:         o.OriginalText,
		RefinedText:          o.RefinedText,
		UnifiedDiff:          o.UnifiedDiff,
		PromptTokens:         o.PromptTokens,
		CompletionTokens:     o.CompletionTokens,
		EstimatedPriceMicros: o.EstimatedPriceMicros,
		ActualPriceMicros:    o.ActualPriceMicros,
	}
	if o.Segments != nil {
		resp.Segments = make([]DiffSegmentResponse, len(o.Segments))
		for i, s := range o.Segments {
			resp.Segments[i] = DiffSegmentResponse{Op: string(s.Op), Text: s.Text}
		}
	}
	return resp
}

func toModelInfoResponses(models []ai.ModelInfo) []ModelInfoResponse {
	if models == nil {
		return nil
	}
	out := make([]ModelInfoResponse, len(models))
	for i, m := range models {
		out[i] = ModelInfoResponse{
			ID:              m.ID,
			Name:            m.Name,
			PromptPrice:     m.PromptPrice,
			CompletionPrice: m.CompletionPrice,
			Created:         m.Created,
			ContextLength:   m.ContextLength,
		}
	}
	return out
}
