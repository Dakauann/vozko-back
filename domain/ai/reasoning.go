package ai

import (
	"errors"
	"slices"
	"strings"
)

type ReasoningLevel string

const (
	ReasoningNone    ReasoningLevel = "none"
	ReasoningMinimal ReasoningLevel = "minimal"
	ReasoningLow     ReasoningLevel = "low"
	ReasoningMedium  ReasoningLevel = "medium"
	ReasoningHigh    ReasoningLevel = "high"
	ReasoningXHigh   ReasoningLevel = "xhigh"
	ReasoningMax     ReasoningLevel = "max"
)

var ErrReasoningLevelUnsupported = errors.New("ai: the model does not offer this reasoning level")

func reasoningScale() []ReasoningLevel {
	return []ReasoningLevel{ReasoningNone, ReasoningMinimal, ReasoningLow, ReasoningMedium, ReasoningHigh, ReasoningXHigh, ReasoningMax}
}

type ReasoningFacts struct {
	Efforts   []string
	Default   string
	Mandatory bool
}

type ReasoningOptions struct {
	Levels  []ReasoningLevel `json:"levels"`
	Default ReasoningLevel   `json:"default"`
}

func ParseReasoningLevel(raw string) ReasoningLevel {
	return ReasoningLevel(strings.TrimSpace(raw))
}

func reasoningOptionsOf(f ModelFacts) *ReasoningOptions {
	if f.Reasoning == nil || !slices.Contains(f.Parameters, parameterReason) {
		return nil
	}
	levels := make([]ReasoningLevel, 0, len(f.Reasoning.Efforts))
	for _, level := range reasoningScale() {
		if level == ReasoningNone && f.Reasoning.Mandatory {
			continue
		}
		if slices.Contains(f.Reasoning.Efforts, string(level)) {
			levels = append(levels, level)
		}
	}
	def := ParseReasoningLevel(f.Reasoning.Default)
	if len(levels) == 0 || !slices.Contains(levels, def) {
		return nil
	}
	return &ReasoningOptions{Levels: levels, Default: def}
}

func (m ModelInfo) AcceptsReasoning(level ReasoningLevel) error {
	if level == "" {
		return nil
	}
	if m.Reasoning != nil && slices.Contains(m.Reasoning.Levels, level) {
		return nil
	}
	return ErrReasoningLevelUnsupported
}

func CheckReasoning(models []ModelInfo, modelID string, level ReasoningLevel) error {
	if level == "" {
		return nil
	}
	m, ok := FindModel(models, modelID)
	if !ok {
		return ErrReasoningLevelUnsupported
	}
	return m.AcceptsReasoning(level)
}
