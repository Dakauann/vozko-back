package emailwiring_test

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"

	"vozko/domain/conversation"
	em "vozko/domain/emailinbox"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
	"vozko/infra/emailhtml"
	conversation_repository "vozko/infra/repositories/conversation"
	"vozko/infra/ses/sesfake"
	emailinbox_usecase "vozko/usecases/emailinbox"
	esuc "vozko/usecases/emailsending"
)

type everyConversation struct{}

func (everyConversation) CanView(shared.Person, string, string, string) (bool, error) {
	return true, nil
}
func (everyConversation) Scope(shared.Person, string) (conversation.VisibilityScope, error) {
	return conversation.VisibilityScope{}, nil
}

type noStoredFiles struct{}

func (noStoredFiles) KeyFromURL(string) (string, bool) { return "", false }
func (noStoredFiles) DownloadFile(context.Context, string) ([]byte, string, error) {
	return nil, "", errors.New("no files in this world")
}

func TestEmailE2E_TheCampaignOriginalRendersTheSentHTMLWithAnInertUnsubscribe(t *testing.T) {
	w := newWorld(t)
	ws := w.sendingTenant("Loja Original", roomyWallet)
	c := w.launch(ws, 1)
	w.dispatch(c, 1, 0)
	sent := w.ses.Sent()[0]
	w.providerEvent(sesfake.Delivery, sent)
	w.receive(w.replyFrom(schema.Lead{Name: "Cliente", Email: schema.OptionalText(sent.Message.To)}, sent.Message.ReplyTo[0], "Tenho interesse", 0))
	conv := w.conversationOf(ws, sent.Message.To)
	original := w.transcript(conv)[0]

	stored, err := w.messages.GetByProviderID(w.ctx, sent.ProviderMessageID)
	w.must(err)
	ref := conversation_repository.NewRepository(w.db)
	row, err := ref.GetByID(original.ID)
	w.must(err)
	rendered, err := w.campaigns.Service.OriginalHTML(w.ctx, *stored, *htmlRefMerge(t, row))
	w.must(err)
	live := liveUnsubscribeHref.FindStringSubmatch(sent.Message.HTML)
	if live == nil {
		t.Fatal("the sent email carries no working unsubscribe link")
	}
	inert := strings.ReplaceAll(sent.Message.HTML, live[1], "#")
	if rendered != inert || strings.TrimSpace(rendered) == "" || strings.Contains(rendered, esuc.UnsubscribePath) {
		t.Fatalf("the original renders %d bytes, want the %d bytes sent with an inert unsubscribe", len(rendered), len(sent.Message.HTML))
	}

	viewer, err := emailinbox_usecase.NewHTMLViewer(emailinbox_usecase.HTMLViewDeps{
		Visibility: everyConversation{}, Messages: ref, Media: conversation_repository.NewMediaRepository(w.db), Files: noStoredFiles{},
		Sent: w.messages, Originals: w.campaigns.Service, Sanitizer: emailhtml.New(),
	})
	w.must(err)
	view, err := viewer.View(w.ctx, emailinbox_usecase.HTMLRequest{Viewer: shared.Person{UserID: ws.Actor.UserID}, WorkspaceID: ws.ID, EntryID: conv, MessageID: original.ID})
	w.must(err)
	clean, err := emailhtml.New().Sanitize(inert, true)
	w.must(err)
	if !view.HasHTML || view.Inbound || view.HTML != clean.HTML || strings.Contains(view.HTML, esuc.UnsubscribePath) {
		t.Fatalf("view = has %v inbound %v, want the sent html made safe with no working unsubscribe", view.HasHTML, view.Inbound)
	}
	if len(w.ses.Sent()) != 1 {
		t.Fatal("viewing the original sent nothing")
	}
}

var liveUnsubscribeHref = regexp.MustCompile(`href="(https://[^"]*` + regexp.QuoteMeta(esuc.UnsubscribePath) + `[^"]*)"`)

func htmlRefMerge(t *testing.T, row *conversation.Message) *em.CampaignMerge {
	t.Helper()
	ref := em.HTMLRefOf(row.Metadata)
	if !ref.CampaignOriginal() {
		t.Fatalf("the original row carries no campaign merge: %s", row.Metadata)
	}
	return ref.Merge
}
