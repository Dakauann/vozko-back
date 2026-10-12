package openrouter

import (
	"slices"

	openrouter "github.com/revrost/go-openrouter"

	"vozko/domain/ai"
)

var reasoningKinds = map[openrouter.ChatCompletionReasoningDetailsType]ai.ReasoningKind{
	openrouter.ReasoningDetailsTypeText:      ai.ReasoningText,
	openrouter.ReasoningDetailsTypeSummary:   ai.ReasoningSummary,
	openrouter.ReasoningDetailsTypeEncrypted: ai.ReasoningEncrypted,
}

type reasoningAccumulator struct {
	byIndex map[int]*ai.ReasoningBlock
}

func (a *reasoningAccumulator) add(details []openrouter.ChatCompletionReasoningDetails) {
	for _, d := range details {
		kind, known := reasoningKinds[d.Type]
		if !known {
			continue
		}
		if a.byIndex == nil {
			a.byIndex = map[int]*ai.ReasoningBlock{}
		}
		block, seen := a.byIndex[d.Index]
		if !seen {
			block = &ai.ReasoningBlock{Index: d.Index, Kind: kind}
			a.byIndex[d.Index] = block
		}
		block.Text += d.Text
		block.Summary += d.Summary
		block.Data += d.Data
		block.ID = firstNonEmpty(d.ID, block.ID)
		block.Format = firstNonEmpty(d.Format, block.Format)
		block.Signature = firstNonEmpty(d.Signature, block.Signature)
	}
}

func (a *reasoningAccumulator) blocks() []ai.ReasoningBlock {
	if len(a.byIndex) == 0 {
		return nil
	}
	indexes := make([]int, 0, len(a.byIndex))
	for i := range a.byIndex {
		indexes = append(indexes, i)
	}
	slices.Sort(indexes)
	out := make([]ai.ReasoningBlock, len(indexes))
	for i, index := range indexes {
		out[i] = *a.byIndex[index]
	}
	return out
}

func reasoningDetailsOf(blocks []ai.ReasoningBlock) []openrouter.ChatCompletionReasoningDetails {
	if len(blocks) == 0 {
		return nil
	}
	out := make([]openrouter.ChatCompletionReasoningDetails, 0, len(blocks))
	for _, b := range blocks {
		for sdkKind, kind := range reasoningKinds {
			if kind == b.Kind {
				out = append(out, openrouter.ChatCompletionReasoningDetails{ID: b.ID, Index: b.Index, Type: sdkKind, Text: b.Text, Summary: b.Summary, Data: b.Data, Format: b.Format, Signature: b.Signature})
			}
		}
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
