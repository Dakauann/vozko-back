package leadexit

import (
	"testing"

	"vozko/domain/conversation"
)

func TestACampaignMessageIsWhatACampaignSentAsPlainText(t *testing.T) {
	sent := &conversation.Message{ID: "msg-1", MessageType: conversation.MessageTypeOperator, SentBy: conversation.SentByCampaign("camp-u"), Text: "Responda /sair"}
	got, ok := CampaignMessageOf(sent)
	if !ok || got.MessageID != "msg-1" || got.CampaignID != "camp-u" || got.Text != "Responda /sair" {
		t.Fatalf("got %+v, %v", got, ok)
	}
	if words := got.ShownWords(); len(words) != 1 || words[0] != "sair" {
		t.Fatalf("shown words = %v", words)
	}

	notCampaign := []*conversation.Message{
		nil,
		{ID: "msg-2", MessageType: conversation.MessageTypeOperator, SentBy: conversation.SentByPerson("user-1"), Text: "Responda /sair"},
		{ID: "msg-3", MessageType: conversation.MessageTypeTemplate, SentBy: conversation.SentByCampaign("camp-o"), Text: "Responda /sair"},
	}
	for _, m := range notCampaign {
		if got, ok := CampaignMessageOf(m); ok {
			t.Errorf("%+v: got %+v", m, got)
		}
	}
}
