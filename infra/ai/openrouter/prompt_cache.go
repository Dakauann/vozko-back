package openrouter

import (
	"fmt"
	"slices"
	"strings"
	"time"

	openrouter "github.com/revrost/go-openrouter"

	"vozko/domain/ai"
)

const (
	explicitCacheModelPrefix = "anthropic/"
	tailBreakpoints          = 2
	finalAnswerToolChoice    = "none"
	ephemeralCache           = "ephemeral"
)

func cachesExplicitly(model string) bool {
	return strings.HasPrefix(model, explicitCacheModelPrefix)
}

func brazilClock(asOf time.Time) string {
	if asOf.IsZero() {
		asOf = time.Now()
	}
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		loc = time.FixedZone("BRT", -3*60*60)
	}
	return fmt.Sprintf("[Current Date/Time in Brazil: %s]", asOf.In(loc).Format("02/01/2006 15:04"))
}

func breakpoint() *openrouter.CacheControl {
	return &openrouter.CacheControl{Type: ephemeralCache}
}

func systemMessage(prompt, clock string, explicit bool) openrouter.ChatCompletionMessage {
	if !explicit {
		return openrouter.SystemMessage(strings.TrimSpace(prompt + "\n\n" + clock))
	}
	parts := []openrouter.ChatMessagePart{{Type: openrouter.ChatMessagePartTypeText, Text: prompt, CacheControl: breakpoint()}}
	if clock != "" {
		parts = append(parts, openrouter.ChatMessagePart{Type: openrouter.ChatMessagePartTypeText, Text: clock})
	}
	return openrouter.ChatCompletionMessage{Role: openrouter.ChatMessageRoleSystem, Content: openrouter.Content{Multi: parts}}
}

func withClockInTail(messages []ai.Message, clock string) []ai.Message {
	out := slices.Clone(messages)
	last := &out[len(out)-1]
	last.Content = strings.TrimSpace(last.Content + "\n\n" + clock)
	return out
}

func withBreakpoint(content openrouter.Content) (openrouter.Content, bool) {
	if len(content.Multi) == 0 {
		if strings.TrimSpace(content.Text) == "" {
			return content, false
		}
		return openrouter.Content{Multi: []openrouter.ChatMessagePart{{Type: openrouter.ChatMessagePartTypeText, Text: content.Text, CacheControl: breakpoint()}}}, true
	}
	parts := slices.Clone(content.Multi)
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i].Type == openrouter.ChatMessagePartTypeText && strings.TrimSpace(parts[i].Text) != "" {
			parts[i].CacheControl = breakpoint()
			return openrouter.Content{Multi: parts}, true
		}
	}
	return content, false
}

func withTailBreakpoints(messages []openrouter.ChatCompletionMessage, volatileTail int) []openrouter.ChatCompletionMessage {
	out := slices.Clone(messages)
	placed := 0
	for i := len(out) - 1 - max(volatileTail, 0); i >= 0 && placed < tailBreakpoints; i-- {
		if out[i].Role == openrouter.ChatMessageRoleSystem {
			continue
		}
		if content, ok := withBreakpoint(out[i].Content); ok {
			out[i].Content = content
			placed++
		}
	}
	return out
}

func withoutTailBreakpoints(messages []openrouter.ChatCompletionMessage) []openrouter.ChatCompletionMessage {
	out := slices.Clone(messages)
	for i, message := range out {
		if message.Role == openrouter.ChatMessageRoleSystem || len(message.Content.Multi) == 0 {
			continue
		}
		parts := slices.Clone(message.Content.Multi)
		for j := range parts {
			parts[j].CacheControl = nil
		}
		out[i].Content = openrouter.Content{Multi: parts}
	}
	return out
}

func withMovedTailBreakpoints(messages []openrouter.ChatCompletionMessage) []openrouter.ChatCompletionMessage {
	return withTailBreakpoints(withoutTailBreakpoints(messages), 0)
}
