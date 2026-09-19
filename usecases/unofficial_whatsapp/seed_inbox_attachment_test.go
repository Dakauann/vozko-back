package unofficial_whatsapp

import (
	"context"
	"errors"
	"sync"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/media"
	"vozko/domain/shared"
	uw "vozko/domain/unofficial_whatsapp"
)

// ---- fakes ----

type fakeAssetReader struct {
	byID map[string]*media.Media
	err  error
}

func (f *fakeAssetReader) GetMediaByID(id string) (*media.Media, error) {
	if f.err != nil {
		return nil, f.err
	}
	asset, ok := f.byID[id]
	if !ok {
		return nil, media.ErrMediaNotFound
	}
	return asset, nil
}

type fakeAttachmentWriter struct {
	mu   sync.Mutex
	rows []*conversation.ConversationMedia
	err  error
}

func (f *fakeAttachmentWriter) Create(row *conversation.ConversationMedia) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	clone := *row
	f.rows = append(f.rows, &clone)
	return nil
}

func (f *fakeAttachmentWriter) created() []*conversation.ConversationMedia {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*conversation.ConversationMedia(nil), f.rows...)
}

func workspaceAsset() *media.Media {
	return &media.Media{
		ID:          "media-1",
		WorkspaceID: "ws-1",
		URL:         "https://cdn.example/abc123.jpg",
		Description: "catalogo-setembro.jpg",
		Type:        media.MediaTypeProductImage,
	}
}

func scriptWithAttachment(kind uw.MediaKind) *uw.SeedScript {
	script := aScript()
	script.Attachment = &uw.SeedAttachment{MediaID: "media-1", Kind: kind}
	return script
}

// attachedSeedUseCase is the scripted harness plus the two media stores.
func attachedSeedUseCase(
	t *testing.T,
	writer *fakePlaceholderWriter,
	assets SeedAssetReader,
	attachments SeedAttachmentWriter,
) *SeedInboxUseCase {
	t.Helper()
	uc, _ := newScriptedSeedUseCase(t, writer, &fakeScripter{}, &fakeBalance{micros: 5_000_000})
	return uc.WithAttachments(assets, attachments)
}

func assetLibrary() *fakeAssetReader {
	return &fakeAssetReader{byID: map[string]*media.Media{"media-1": workspaceAsset()}}
}

// ---- the tests ----

// The opening carries the file, and the file belongs to the conversation it was
// written into.
func TestSeededOpeningCarriesItsAttachment(t *testing.T) {
	writer := newFakePlaceholderWriter()
	attachments := &fakeAttachmentWriter{}
	uc := attachedSeedUseCase(t, writer, assetLibrary(), attachments)

	out, err := uc.Execute(context.Background(),
		scriptedRequest(scriptWithAttachment(uw.MediaImage), "5511999999999"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Scripted != 1 {
		t.Fatalf("outcome = %+v, want 1 scripted", out)
	}

	rows := attachments.created()
	if len(rows) != 1 {
		t.Fatalf("wrote %d media rows, want 1", len(rows))
	}
	row := rows[0]

	opening := writer.writes()[0]
	if opening.MediaID == nil || *opening.MediaID != row.ID {
		t.Fatalf("the opening links to %v, want the media row %q", opening.MediaID, row.ID)
	}
	if opening.MediaType != conversation.MediaTypeImage {
		t.Fatalf("MediaType = %q, want image", opening.MediaType)
	}
	// The caption is still the operator own rendered text.
	if opening.Text == "" {
		t.Fatal("the opening lost its caption")
	}

	// The row belongs to THIS conversation, which is what the media endpoint
	// checks before it will serve the file.
	if row.EntryID != opening.EntryID {
		t.Fatalf("media row is on conversation %q, the message on %q", row.EntryID, opening.EntryID)
	}
	if row.EntryType != shared.EntryTypeUnofficialWhatsApp {
		t.Fatalf("media row EntryType = %q", row.EntryType)
	}
	if row.URL != workspaceAsset().URL {
		t.Fatalf("media row URL = %q, want the stored object", row.URL)
	}
	if row.MimeType != "image/jpeg" {
		t.Fatalf("MimeType = %q, want it derived from the stored object", row.MimeType)
	}
	if row.OriginalFilename != "catalogo-setembro.jpg" {
		t.Fatalf("OriginalFilename = %q", row.OriginalFilename)
	}
}

// Inbound-ness is decided from the message TYPE in SQL, so the opening must not
// be typed as media however much media it carries.
func TestSeededAttachmentStaysAnOutboundOperatorMessage(t *testing.T) {
	writer := newFakePlaceholderWriter()
	uc := attachedSeedUseCase(t, writer, assetLibrary(), &fakeAttachmentWriter{})

	if _, err := uc.Execute(context.Background(),
		scriptedRequest(scriptWithAttachment(uw.MediaImage), "5511999999999")); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	opening := writer.writes()[0]
	if opening.MessageType.IsInbound() {
		t.Fatalf("the opening is typed %q, which the unread count reads as the customer",
			opening.MessageType)
	}
	if opening.MessageType != conversation.MessageTypeOperator {
		t.Fatalf("MessageType = %q, want operator", opening.MessageType)
	}
	if opening.Direction != conversation.MessageDirectionOutbound {
		t.Fatalf("Direction = %q, want outbound", opening.Direction)
	}
}

// One row per conversation, because the endpoint the chat fetches through
// refuses a media row belonging to a different conversation.
func TestEachSeededConversationGetsItsOwnMediaRow(t *testing.T) {
	writer := newFakePlaceholderWriter()
	attachments := &fakeAttachmentWriter{}
	uc := attachedSeedUseCase(t, writer, assetLibrary(), attachments)

	if _, err := uc.Execute(context.Background(), scriptedRequest(
		scriptWithAttachment(uw.MediaImage),
		"5511999999991", "5511999999992", "5511999999993")); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	rows := attachments.created()
	if len(rows) != 3 {
		t.Fatalf("wrote %d media rows for 3 conversations, want 3", len(rows))
	}
	seen := map[string]struct{}{}
	for _, row := range rows {
		if _, dup := seen[row.EntryID]; dup {
			t.Fatalf("two media rows on conversation %q", row.EntryID)
		}
		seen[row.EntryID] = struct{}{}
		if row.URL != workspaceAsset().URL {
			t.Fatalf("row %q points at %q, want the one stored object", row.ID, row.URL)
		}
	}
}

// Every kind the channel can carry reaches the conversation as the media type
// the CRM renders.
func TestSeededAttachmentMapsEveryKind(t *testing.T) {
	cases := map[uw.MediaKind]conversation.MediaType{
		uw.MediaImage:    conversation.MediaTypeImage,
		uw.MediaVideo:    conversation.MediaTypeVideo,
		uw.MediaAudio:    conversation.MediaTypeAudio,
		uw.MediaVoice:    conversation.MediaTypeAudio,
		uw.MediaDocument: conversation.MediaTypeDocument,
		uw.MediaSticker:  conversation.MediaTypeSticker,
	}
	for kind, want := range cases {
		writer := newFakePlaceholderWriter()
		uc := attachedSeedUseCase(t, writer, assetLibrary(), &fakeAttachmentWriter{})

		if _, err := uc.Execute(context.Background(),
			scriptedRequest(scriptWithAttachment(kind), "5511999999999")); err != nil {
			t.Fatalf("kind %q: %v", kind, err)
		}
		if got := writer.writes()[0].MediaType; got != want {
			t.Fatalf("kind %q became %q, want %q", kind, got, want)
		}
	}
}

// A workspace may not seed another workspace file. The queue message carries no
// session, so this is the only layer that can refuse it.
func TestSeededAttachmentRefusesAnotherWorkspacesFile(t *testing.T) {
	foreign := workspaceAsset()
	foreign.WorkspaceID = "ws-2"

	writer := newFakePlaceholderWriter()
	attachments := &fakeAttachmentWriter{}
	uc := attachedSeedUseCase(t, writer,
		&fakeAssetReader{byID: map[string]*media.Media{"media-1": foreign}}, attachments)

	out, err := uc.Execute(context.Background(),
		scriptedRequest(scriptWithAttachment(uw.MediaImage), "5511999999999"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(attachments.created()) != 0 {
		t.Fatal("another workspace file was attached")
	}
	// Degraded, not dropped: the thread still lands, as the text it always was.
	if out.Scripted != 1 {
		t.Fatalf("outcome = %+v, want the thread seeded anyway", out)
	}
	if writer.writes()[0].MediaID != nil {
		t.Fatal("the opening links to a file it was refused")
	}
}

// An unresolvable or unwritable file costs the file, never the conversation.
func TestSeededAttachmentDegradesToText(t *testing.T) {
	cases := map[string]func() (SeedAssetReader, SeedAttachmentWriter){
		"asset not found": func() (SeedAssetReader, SeedAttachmentWriter) {
			return &fakeAssetReader{byID: map[string]*media.Media{}}, &fakeAttachmentWriter{}
		},
		"library unavailable": func() (SeedAssetReader, SeedAttachmentWriter) {
			return &fakeAssetReader{err: errors.New("database is down")}, &fakeAttachmentWriter{}
		},
		"media row rejected": func() (SeedAssetReader, SeedAttachmentWriter) {
			return assetLibrary(), &fakeAttachmentWriter{err: errors.New("constraint violation")}
		},
	}

	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			writer := newFakePlaceholderWriter()
			assets, attachments := build()
			uc := attachedSeedUseCase(t, writer, assets, attachments)

			out, err := uc.Execute(context.Background(),
				scriptedRequest(scriptWithAttachment(uw.MediaImage), "5511999999999"))
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if out.Seeded != 1 || out.Scripted != 1 {
				t.Fatalf("outcome = %+v, want the conversation seeded regardless", out)
			}
			opening := writer.writes()[0]
			if opening.MediaID != nil {
				t.Fatalf("the opening links to %q, want no attachment", *opening.MediaID)
			}
			if opening.Text == "" {
				t.Fatal("the opening lost its text as well as its file")
			}
		})
	}
}

// A deployment that never wired the media stores seeds exactly what it did
// before attachments existed.
func TestSeedingWithoutAttachmentStoresIsUnchanged(t *testing.T) {
	writer := newFakePlaceholderWriter()
	uc, _ := newScriptedSeedUseCase(t, writer, &fakeScripter{}, &fakeBalance{micros: 5_000_000})

	out, err := uc.Execute(context.Background(),
		scriptedRequest(scriptWithAttachment(uw.MediaImage), "5511999999999"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Scripted != 1 {
		t.Fatalf("outcome = %+v, want 1 scripted", out)
	}
	if writer.writes()[0].MediaID != nil {
		t.Fatal("an attachment was written with no store to write it to")
	}
}
