package template

import (
	"strconv"
	"strings"

	"vozko/domain/conversation"
)

const (
	ButtonTypeQuickReply = "QUICK_REPLY"
	quickReplyPayloadTag = "vz1:"
)

type QuickReply struct {
	Index int
	Text  string
}

func (t *Template) QuickReplies() []QuickReply {
	if t == nil {
		return nil
	}
	var out []QuickReply
	for _, comp := range t.Components {
		if strings.ToUpper(strings.TrimSpace(comp.Type)) != "BUTTONS" {
			continue
		}
		for i, btn := range comp.Buttons {
			if strings.ToUpper(strings.TrimSpace(btn.Type)) == ButtonTypeQuickReply {
				out = append(out, QuickReply{Index: i, Text: strings.TrimSpace(btn.Text)})
			}
		}
	}
	return out
}

func QuickReplyPayload(index int) string {
	return quickReplyPayloadTag + strconv.Itoa(index)
}

func ParseQuickReplyPayload(payload string) (int, bool) {
	digits, found := strings.CutPrefix(payload, quickReplyPayloadTag)
	if !found || len(digits) != 1 || digits[0] < '0' || digits[0] > '9' {
		return 0, false
	}
	return int(digits[0] - '0'), true
}

func (t *Template) quickReplyParams() []conversation.TemplateButtonParam {
	replies := t.QuickReplies()
	out := make([]conversation.TemplateButtonParam, 0, len(replies))
	for _, reply := range replies {
		out = append(out, conversation.TemplateButtonParam{
			SubType: conversation.TemplateButtonSubTypeQuickReply,
			Index:   reply.Index,
			Payload: QuickReplyPayload(reply.Index),
		})
	}
	return out
}
