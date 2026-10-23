package emailinbox

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/emaildesign"
	em "vozko/domain/emailinbox"
	"vozko/domain/emailsending"
	"vozko/domain/shared"
	"vozko/infra/emailhtml"
)

type viewVisibility struct{ visible bool }

func (v viewVisibility) CanView(shared.Person, string, string, string) (bool, error) {
	return v.visible, nil
}
func (v viewVisibility) Scope(shared.Person, string) (conversation.VisibilityScope, error) {
	return conversation.VisibilityScope{}, nil
}

type viewMessages map[string]*conversation.Message

func (m viewMessages) GetByID(id string) (*conversation.Message, error) {
	if msg, ok := m[id]; ok {
		return msg, nil
	}
	return nil, conversation.ErrMessageNotFound
}

type viewMedia map[string]*conversation.ConversationMedia

func (m viewMedia) GetInEntry(entryID string, entryType shared.EntryType, id string) (*conversation.ConversationMedia, error) {
	row, ok := m[id]
	if !ok || !row.BelongsTo(entryID, entryType) {
		return nil, conversation.ErrMediaNotFound
	}
	return row, nil
}

type viewFiles map[string][]byte

func (f viewFiles) KeyFromURL(url string) (string, bool) {
	key, ok := strings.CutPrefix(url, "https://cdn.test/")
	return key, ok
}

func (f viewFiles) DownloadFile(_ context.Context, key string) ([]byte, string, error) {
	data, ok := f[key]
	if !ok {
		return nil, "", errors.New("missing")
	}
	return data, em.HTMLMimeType, nil
}

type viewOriginals struct {
	html  string
	asked []em.CampaignMerge
}

func (o *viewOriginals) OriginalHTML(_ context.Context, _ emailsending.Message, merge em.CampaignMerge) (string, error) {
	o.asked = append(o.asked, merge)
	return o.html, nil
}

const viewEntry = "c0000000-0000-0000-0000-0000000000e1"

func viewMetadata(t *testing.T, fields map[string]any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func htmlViewer(t *testing.T, visible bool, originals *viewOriginals) *HTMLViewer {
	t.Helper()
	inbound := `<p>Oi</p><img src="https://tracker.test/pixel.gif"><script>alert(1)</script>`
	messages := viewMessages{
		"in-1": {ID: "in-1", EntryID: viewEntry, EntryType: shared.EntryTypeEmail, Text: "Oi", SentBy: conversation.SentByContact("ana@cliente.com"),
			Metadata: viewMetadata(t, map[string]any{em.MetadataHTMLMediaID: "html-1"})},
		"out-1": {ID: "out-1", EntryID: viewEntry, EntryType: shared.EntryTypeEmail, Text: "Oferta", SentBy: conversation.SentByCampaign("camp-1"),
			Metadata: viewMetadata(t, map[string]any{em.MetadataCampaignID: "camp-1", em.MetadataSentMessageID: "sent-1",
				em.MetadataCampaignMerge: em.CampaignMerge{Subject: "Oferta", Personalization: emaildesign.Personalization{FirstName: "Ana"}}})},
		"plain": {ID: "plain", EntryID: viewEntry, EntryType: shared.EntryTypeEmail, Text: "Só texto", SentBy: conversation.SentByPerson("u-1")},
		"wa":    {ID: "wa", EntryID: viewEntry, EntryType: shared.EntryTypeWhatsApp, Text: "oi"},
		"bad":   {ID: "bad", EntryID: viewEntry, EntryType: shared.EntryTypeEmail, Text: "texto de reserva", SentBy: conversation.SentByContact("x@y.test"), Metadata: viewMetadata(t, map[string]any{em.MetadataHTMLMediaID: "html-2"})},
	}
	viewer, err := NewHTMLViewer(HTMLViewDeps{
		Visibility: viewVisibility{visible: visible},
		Messages:   messages,
		Media: viewMedia{
			"html-1": {ID: "html-1", EntryID: viewEntry, EntryType: shared.EntryTypeEmail, MimeType: em.HTMLMimeType, URL: "https://cdn.test/a.html"},
			"html-2": {ID: "html-2", EntryID: viewEntry, EntryType: shared.EntryTypeEmail, MimeType: em.HTMLMimeType, URL: "https://cdn.test/b.html"},
		},
		Files:     viewFiles{"a.html": []byte(inbound), "b.html": []byte("<script>alert(1)</script>")},
		Sent:      &fakeSent{byID: map[string]*emailsending.Message{"sent-1": {ID: "sent-1", WorkspaceID: wsA, RunKind: emailsending.RunCampaign, RunID: "camp-1"}}},
		Originals: originals,
		Sanitizer: emailhtml.New(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return viewer
}

func view(t *testing.T, v *HTMLViewer, messageID string, showImages bool) (em.HTMLView, error) {
	t.Helper()
	return v.View(context.Background(), HTMLRequest{Viewer: shared.Person{UserID: "u-1"}, WorkspaceID: wsA, EntryID: viewEntry, MessageID: messageID, ShowImages: showImages})
}

func TestAnInboundEmailIsSanitizedWithItsRemoteImagesBlockedUntilAsked(t *testing.T) {
	v := htmlViewer(t, true, &viewOriginals{})
	blocked, err := view(t, v, "in-1", false)
	if err != nil {
		t.Fatal(err)
	}
	if !blocked.HasHTML || !blocked.Inbound || !blocked.ImagesBlocked || strings.Contains(blocked.HTML, "tracker.test") || strings.Contains(blocked.HTML, "script") {
		t.Fatalf("blocked = %+v", blocked)
	}
	shown, err := view(t, v, "in-1", true)
	if err != nil || shown.ImagesBlocked || !strings.Contains(shown.HTML, "tracker.test") {
		t.Fatalf("shown = %+v %v", shown, err)
	}
}

func TestACampaignOriginalIsRenderedFromItsStoredMergeValues(t *testing.T) {
	originals := &viewOriginals{html: `<p>Oi Ana</p><img src="https://cdn.loja.test/banner.png">`}
	got, err := view(t, htmlViewer(t, true, originals), "out-1", false)
	if err != nil {
		t.Fatal(err)
	}
	if !got.HasHTML || got.Inbound || got.ImagesBlocked || !strings.Contains(got.HTML, "banner.png") {
		t.Fatalf("view = %+v, our own email shows its images", got)
	}
	if len(originals.asked) != 1 || originals.asked[0].Personalization.FirstName != "Ana" || originals.asked[0].Subject != "Oferta" {
		t.Fatalf("rendered with %+v", originals.asked)
	}
}

func TestWithoutHTMLOrWhenItCannotBeMadeSafeTheTextIsShown(t *testing.T) {
	v := htmlViewer(t, true, &viewOriginals{})
	for id, text := range map[string]string{"plain": "Só texto", "bad": "texto de reserva"} {
		got, err := view(t, v, id, false)
		if err != nil || got.HasHTML || got.HTML != "" || got.Text != text {
			t.Fatalf("%s = %+v %v, want the text alone", id, got, err)
		}
	}
}

func TestTheEmailHTMLFollowsTheConversationScope(t *testing.T) {
	if _, err := view(t, htmlViewer(t, false, &viewOriginals{}), "in-1", false); !errors.Is(err, conversation.ErrNotVisible) {
		t.Fatalf("err = %v, want ErrNotVisible", err)
	}
	v := htmlViewer(t, true, &viewOriginals{})
	for _, id := range []string{"wa", "missing"} {
		if _, err := view(t, v, id, false); !errors.Is(err, em.ErrNotAnEmail) {
			t.Fatalf("%s: err = %v, want ErrNotAnEmail", id, err)
		}
	}
	other, err := v.View(context.Background(), HTMLRequest{Viewer: shared.Person{UserID: "u-1"}, WorkspaceID: wsA, EntryID: "another-entry", MessageID: "in-1"})
	if !errors.Is(err, em.ErrNotAnEmail) || other.HTML != "" {
		t.Fatalf("a message of another conversation = %+v %v", other, err)
	}
}
