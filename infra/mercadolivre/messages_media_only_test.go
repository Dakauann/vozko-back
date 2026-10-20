package mercadolivre

import (
	"context"
	"encoding/json"
	"testing"

	mldomain "vozko/domain/mercadolivre"
)

func TestSendCarriesAnAttachmentWithoutText(t *testing.T) {
	fs := newFakeServer(t, jsonReply(201, fixture(t, "option_sent_doc_example.json")))
	s := newMessages(t, fs, newFakeLimiters())
	if _, err := s.Send(context.Background(), sellerCreds, mldomain.SendMessage{
		PackKey:       "2000000089077943",
		ToUserID:      "3037675074",
		AttachmentIDs: []string{"415460047_a96d8dea-38cd-4402-938e-80a1c134fc5d.pdf"},
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	var body struct {
		Text        string   `json:"text"`
		Attachments []string `json:"attachments"`
	}
	if err := json.Unmarshal([]byte(fs.last().Body), &body); err != nil {
		t.Fatal(err)
	}
	if body.Text != "" || len(body.Attachments) != 1 {
		t.Fatalf("body = %s", fs.last().Body)
	}
}

func TestSendRefusesTooManyAttachmentsBeforeCalling(t *testing.T) {
	fs := newFakeServer(t, jsonReply(200, `{}`))
	s := newMessages(t, fs, newFakeLimiters())
	ids := make([]string, mldomain.MaxAttachmentsPerMessage+1)
	for i := range ids {
		ids[i] = "file.pdf"
	}
	if _, err := s.Send(context.Background(), sellerCreds, mldomain.SendMessage{PackKey: "1", ToUserID: "2", AttachmentIDs: ids}); err == nil {
		t.Fatal("too many attachments accepted")
	}
	if fs.count() != 0 {
		t.Fatal("refused sends must never reach Mercado Livre")
	}
}
