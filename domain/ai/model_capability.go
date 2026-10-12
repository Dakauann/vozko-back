package ai

import "slices"

type ModelCapability string

const (
	CapabilityVision      ModelCapability = "vision"
	CapabilityTools       ModelCapability = "tools"
	CapabilityReasoning   ModelCapability = "reasoning"
	CapabilityLongContext ModelCapability = "long_context"
	CapabilityImageOutput ModelCapability = "image_output"
	CapabilityAudioInput  ModelCapability = "audio_input"
	CapabilityLowPrice    ModelCapability = "low_price"
	CapabilityFree        ModelCapability = "free"
)

const (
	LongContextTokens            int64   = 1_000_000
	LowPromptPricePerMillion     float64 = 0.30
	LowCompletionPricePerMillion float64 = 1.50
)

const (
	modalityImage   = "image"
	modalityAudio   = "audio"
	parameterTools  = "tools"
	parameterReason = "reasoning"
)

func ModelCapabilities() []ModelCapability {
	return []ModelCapability{
		CapabilityVision,
		CapabilityTools,
		CapabilityReasoning,
		CapabilityLongContext,
		CapabilityImageOutput,
		CapabilityAudioInput,
		CapabilityLowPrice,
		CapabilityFree,
	}
}

type ModelPrice struct {
	PromptPerMillion     float64
	CompletionPerMillion float64
}

func (p *ModelPrice) known() bool {
	return p != nil && p.PromptPerMillion >= 0 && p.CompletionPerMillion >= 0
}

type ModelFacts struct {
	ID               string
	Name             string
	Created          int64
	InputModalities  []string
	OutputModalities []string
	Parameters       []string
	ContextLength    int64
	Price            *ModelPrice
	Reasoning        *ReasoningFacts
}

func DescribeModel(f ModelFacts) ModelInfo {
	info := ModelInfo{
		ID:            f.ID,
		Name:          f.Name,
		Created:       f.Created,
		ContextLength: max(f.ContextLength, 0),
		Capabilities:  capabilitiesOf(f),
		Reasoning:     reasoningOptionsOf(f),
	}
	if f.Price.known() {
		info.PromptPrice = f.Price.PromptPerMillion
		info.CompletionPrice = f.Price.CompletionPerMillion
	}
	return info
}

func capabilitiesOf(f ModelFacts) []ModelCapability {
	held := map[ModelCapability]bool{
		CapabilityVision:      slices.Contains(f.InputModalities, modalityImage),
		CapabilityTools:       slices.Contains(f.Parameters, parameterTools),
		CapabilityReasoning:   slices.Contains(f.Parameters, parameterReason),
		CapabilityLongContext: f.ContextLength >= LongContextTokens,
		CapabilityImageOutput: slices.Contains(f.OutputModalities, modalityImage),
		CapabilityAudioInput:  slices.Contains(f.InputModalities, modalityAudio),
		CapabilityLowPrice:    f.Price.known() && f.Price.PromptPerMillion <= LowPromptPricePerMillion && f.Price.CompletionPerMillion <= LowCompletionPricePerMillion,
		CapabilityFree:        f.Price.known() && f.Price.PromptPerMillion == 0 && f.Price.CompletionPerMillion == 0,
	}
	out := make([]ModelCapability, 0, len(held))
	for _, c := range ModelCapabilities() {
		if held[c] {
			out = append(out, c)
		}
	}
	return out
}

func (m ModelInfo) Has(c ModelCapability) bool {
	return slices.Contains(m.Capabilities, c)
}

func (m ModelInfo) SeesImages() bool {
	return m.Has(CapabilityVision)
}

func (m ModelInfo) HearsAudio() bool {
	return m.Has(CapabilityAudioInput)
}

func FindModel(models []ModelInfo, id string) (ModelInfo, bool) {
	for _, m := range models {
		if m.ID == id {
			return m, true
		}
	}
	return ModelInfo{}, false
}
