package emailwiring_test

import (
	"encoding/json"
	"strings"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/emailaddress"
	em "vozko/domain/emailinbox"
	"vozko/infra/database/schema"
	"vozko/infra/ses/sesfake"
)

func viewedText(t *testing.T, sent sesfake.Sent) string {
	t.Helper()
	text := strings.TrimSpace(sent.Message.Text)
	for _, h := range sent.Message.Headers {
		if h.Name != "List-Unsubscribe" {
			continue
		}
		url := strings.TrimSuffix(strings.TrimPrefix(strings.SplitN(h.Value, ",", 2)[0], "<"), ">")
		if !strings.Contains(text, " ("+url+")") {
			t.Fatalf("the sent text %q does not carry its unsubscribe link %s", text, url)
		}
		return strings.ReplaceAll(text, " ("+url+")", "")
	}
	return text
}

func (w *world) assertOriginalFirst(t *testing.T, rows []transcriptRow, sent sesfake.Sent, campaignID, campaignName, status string) {
	t.Helper()
	if len(rows) < 2 {
		t.Fatalf("rows = %d, want the original email before the reply", len(rows))
	}
	original := rows[0]
	by := conversation.SentByCampaign(campaignID)
	if original.ExternalMessageID != sent.ProviderMessageID || original.SenderKind != string(by.Kind()) || original.SenderID != by.ID() ||
		original.MessageType != string(conversation.MessageTypeOperator) || original.DeliveryStatus != status ||
		original.Text != viewedText(t, sent) {
		t.Fatalf("original row = %+v, want the campaign email %s as sent", original, sent.ProviderMessageID)
	}
	if !original.CreatedAt.Before(rows[1].CreatedAt) || original.CreatedAt.Sub(sent.At).Abs() > 2e9 {
		t.Fatalf("original at %v, reply at %v, sent at %v: the original keeps its send time and sorts first", original.CreatedAt, rows[1].CreatedAt, sent.At)
	}
	var meta map[string]any
	if err := json.Unmarshal([]byte(original.Metadata), &meta); err != nil {
		t.Fatal(err)
	}
	if meta[em.MetadataSubject] != sent.Message.Subject || meta[em.MetadataCampaignID] != campaignID || meta[em.MetadataCampaignName] != campaignName {
		t.Fatalf("original metadata = %v", meta)
	}
	if rows[1].SenderKind != string(conversation.SenderContact) {
		t.Fatalf("second row = %+v, want the reply", rows[1])
	}
}

func TestEmailE2E_AReplyToACampaignShowsTheOriginalEmailFirstAndOnlyOnce(t *testing.T) {
	w := newWorld(t)
	ws := w.sendingTenant("Loja Campanha", roomyWallet)
	c := w.launch(ws, 1)
	w.dispatch(c, 1, 0)
	sent := w.ses.Sent()[0]
	w.providerEvent(sesfake.Delivery, sent)
	customer := schema.Lead{Name: "Cliente", Email: schema.OptionalText(sent.Message.To)}

	w.receive(w.replyFrom(customer, sent.Message.ReplyTo[0], "Tenho interesse", 0))
	conv := w.conversationOf(ws, sent.Message.To)
	rows := w.transcript(conv)
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want the original and the reply", len(rows))
	}
	w.assertOriginalFirst(t, rows, sent, c.ID, c.Name, string(conversation.DeliveryStatusDelivered))

	w.receive(w.replyFrom(customer, sent.Message.ReplyTo[0], "Ainda tenho interesse", 0))
	rows = w.transcript(conv)
	originals := 0
	for _, r := range rows {
		if r.ExternalMessageID == sent.ProviderMessageID {
			originals++
		}
	}
	if len(rows) != 3 || originals != 1 {
		t.Fatalf("rows = %d with %d copies of the original, want it once", len(rows), originals)
	}
	if w.conversationsOf(ws) != 1 {
		t.Fatalf("conversations = %d", w.conversationsOf(ws))
	}
	w.assertLedger(ws)
}

func TestEmailE2E_AReplyToATestSendShowsTheTestEmailFirst(t *testing.T) {
	w := newWorld(t)
	ws := w.sendingTenant("Loja Teste", roomyWallet)
	c := w.launch(ws, 1)
	tester := emailaddress.Address("revisora@loja-teste.example")
	if _, err := w.campaigns.Service.TestSend(w.ctx, ws.Actor, c.ID, []string{string(tester)}); err != nil {
		t.Fatal(err)
	}
	sent := w.onlySentTo(tester)
	w.receive(w.replyFrom(schema.Lead{Name: "Revisora", Email: schema.OptionalText(tester)}, sent.Message.ReplyTo[0], "Aprovado", 0))
	rows := w.transcript(w.conversationOf(ws, tester))
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want the test email and the reply", len(rows))
	}
	w.assertOriginalFirst(t, rows, sent, c.ID, c.Name, string(conversation.DeliveryStatusSent))
}
