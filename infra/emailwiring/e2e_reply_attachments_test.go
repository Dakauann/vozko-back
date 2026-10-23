package emailwiring_test

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"vozko/domain/conversation"
	"vozko/domain/emailaddress"
	em "vozko/domain/emailinbox"
	es "vozko/domain/emailsending"
	"vozko/domain/media"
	"vozko/domain/shared"
	"vozko/domain/workspace/workspace_pricing"
	conversation_repository "vozko/infra/repositories/conversation"
	conversation_usecase "vozko/usecases/conversation"
	emuc "vozko/usecases/emailinbox"
)

type cdnFiles struct {
	mu    sync.Mutex
	files map[string]media.RemoteFile
}

func newCDNFiles() *cdnFiles {
	return &cdnFiles{files: map[string]media.RemoteFile{}}
}

func (c *cdnFiles) put(url string, file media.RemoteFile) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.files[url] = file
}

func (c *cdnFiles) Download(_ context.Context, url string, maxBytes int64) (media.RemoteFile, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	file, ok := c.files[url]
	if !ok {
		return media.RemoteFile{}, media.ErrRemoteFileMissing
	}
	if int64(len(file.Data)) > maxBytes {
		return media.RemoteFile{}, fmt.Errorf("%w: over %d bytes", media.ErrMediaTooLarge, maxBytes)
	}
	return file, nil
}

func (w *world) upload(uploads conversation.ConversationMediaRepository, conversationID, fileName, contentType string, data []byte) *conversation.ConversationMedia {
	url := "https://cdn.vozko.test/conversations/email/" + conversationID + "/" + uuid.NewString() + "-" + fileName
	w.files.put(url, media.RemoteFile{Data: data, ContentType: contentType})
	row := &conversation.ConversationMedia{
		ID: uuid.NewString(), EntryID: conversationID, EntryType: shared.EntryTypeEmail, Type: conversation.MediaTypeImage,
		MimeType: contentType, URL: url, OriginalFilename: fileName, SizeBytes: int64(len(data)),
	}
	row.Normalize()
	w.must(row.Validate())
	w.must(uploads.Create(row))
	return row
}

func TestEmailE2E_AnOperatorReplyCarriesAnImageAndPaysForItsChunks(t *testing.T) {
	w := newWorld(t)
	ws := w.sendingTenant("Loja Anexos", roomyWallet)
	lead := w.seedLeads(ws, 1, "anexo")[0]
	first, err := w.sendFromWorkflow(ws, lead, "pedido:"+lead.ID)
	w.must(err)

	image := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0x42}, 300<<10)...)
	uploads := conversation_repository.NewMediaRepository(w.db)
	photo := w.upload(uploads, first.ConversationID, "vitrine.png", "image/png", image)
	program := w.upload(uploads, first.ConversationID, "instalador.exe", "application/octet-stream", []byte("MZ"))
	sender := conversation_usecase.NewMessageSenderService(conversation_repository.NewRepository(w.db), nil, nil, nil,
		uploads, nil, nil, nil, nil, nil, nil, nil)
	sender.SetChannelAdapters(conversation.NewAdapterRegistry(w.inbox.Adapter))

	message, err := w.pricer.PriceEmail(ws.ID, workspace_pricing.EmailSendPerMessage)
	w.must(err)
	chunk, err := w.pricer.PriceEmail(ws.ID, workspace_pricing.EmailSendPerChunk)
	w.must(err)
	chunks := es.AttachmentChunks(es.EncodedAttachmentBytes(int64(len(image))))
	if chunks != 2 {
		t.Fatalf("a 300 KB image encodes into %d chunks, want 2", chunks)
	}
	walletBefore := w.wallet(ws)

	reply, err := sender.SendMediaMessage(first.ConversationID, string(shared.EntryTypeEmail), photo.ID, string(conversation.MediaTypeImage), "Segue a vitrine.",
		conversation.SendOrigin{SenderUserID: ws.Actor.UserID})
	w.must(err)
	sends := w.ses.SentTo(emailaddress.Address(lead.Email))
	if len(sends) != 2 {
		t.Fatalf("provider sends = %d, want the first email and the reply", len(sends))
	}
	out := sends[1].Message
	if out.Text != "Segue a vitrine." || out.Kind != es.KindTransactional || !strings.HasPrefix(out.Subject, "Re: ") {
		t.Fatalf("reply = %q %q %s", out.Subject, out.Text, out.Kind)
	}
	if len(out.Attachments) != 1 || out.Attachments[0].FileName != "vitrine.png" || out.Attachments[0].ContentType != "image/png" || !bytes.Equal(out.Attachments[0].Data, image) {
		t.Fatalf("attachments = %+v", out.Attachments)
	}
	if reply.ExternalMessageID == nil || *reply.ExternalMessageID != sends[1].ProviderMessageID || reply.MediaID == nil || *reply.MediaID != photo.ID || reply.MediaType != conversation.MediaTypeImage {
		t.Fatalf("transcript message = %+v", reply)
	}
	var row transcriptRow
	for _, r := range w.transcript(first.ConversationID) {
		if r.ID == reply.ID {
			row = r
		}
	}
	if !strings.Contains(row.Metadata, `"`+em.MetadataFileName+`": "vitrine.png"`) && !strings.Contains(row.Metadata, `"`+em.MetadataFileName+`":"vitrine.png"`) {
		t.Fatalf("transcript metadata = %s, want the file name like an inbound attachment", row.Metadata)
	}

	debits := w.ledger(ws, "debit", "email_message:email_reply:"+first.ConversationID+":%")
	wantPrice := message.PriceMicros + chunk.PriceMicros*chunks
	wantCost := message.CostMicros + chunk.CostMicros*chunks
	if len(debits) != 1 || debits[0].Amount != wantPrice || debits[0].CostMicros != wantCost {
		t.Fatalf("reply debits = %+v, want one entry of %d (cost %d)", debits, wantPrice, wantCost)
	}
	if got := w.wallet(ws); got != walletBefore-wantPrice {
		t.Fatalf("wallet = %d, want %d", got, walletBefore-wantPrice)
	}

	_, err = sender.SendMediaMessage(first.ConversationID, string(shared.EntryTypeEmail), program.ID, string(conversation.MediaTypeDocument), "",
		conversation.SendOrigin{SenderUserID: ws.Actor.UserID})
	if code := conversation.SendFailureCode(err, "send_failed"); code != emuc.SendCodeAttachmentBlocked {
		t.Fatalf("a program attachment = %v (%s), want a coded refusal", err, code)
	}
	if len(w.ses.SentTo(emailaddress.Address(lead.Email))) != 2 || w.wallet(ws) != walletBefore-wantPrice {
		t.Fatal("a refused attachment reached the provider or was charged")
	}
	w.assertLedger(ws)
}
