package buttonaction

import (
	"vozko/domain/shared"
	"vozko/domain/whatsapp/template"
)

func MatchButton(t *template.Template, payload, text string) (int, bool) {
	if t == nil {
		return 0, false
	}
	if index, ok := template.ParseQuickReplyPayload(payload); ok {
		return matchByPayload(t, index, text)
	}
	return matchByText(t, text)
}

func matchByPayload(t *template.Template, index int, text string) (int, bool) {
	reply, ok := quickReplyAt(t, index)
	if !ok {
		return 0, false
	}
	if text != "" && shared.FoldForMatch(text) != shared.FoldForMatch(reply.Text) {
		return 0, false
	}
	return index, true
}

func matchByText(t *template.Template, text string) (int, bool) {
	folded := shared.FoldForMatch(text)
	if folded == "" {
		return 0, false
	}
	found, matches := 0, 0
	for _, reply := range t.QuickReplies() {
		if shared.FoldForMatch(reply.Text) == folded {
			found = reply.Index
			matches++
		}
	}
	return found, matches == 1
}
