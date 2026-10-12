package aichat

import "strings"

func (t *Thread) ChooseModel(model, reasoningLevel string) {
	model, reasoningLevel = strings.TrimSpace(model), strings.TrimSpace(reasoningLevel)
	switch {
	case model != "":
		t.Model, t.ReasoningLevel = model, reasoningLevel
	case reasoningLevel != "":
		t.ReasoningLevel = reasoningLevel
	}
}

func (t *Thread) ReasoningLevelFor(model string) string {
	if model != t.Model {
		return ""
	}
	return t.ReasoningLevel
}
