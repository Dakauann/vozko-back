package conversationnote_usecase

import (
	"context"
	"errors"
	"strings"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/conversationnote"
	"vozko/domain/shared"
)

var pngBytes = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")

func upload(fileName string, data []byte) conversationnote.ImageUpload {
	return conversationnote.ImageUpload{WorkspaceID: "ws-1", Entry: chat, FileName: fileName, Data: data}
}

func attach(t *testing.T, h harness) conversationnote.Image {
	t.Helper()
	img, err := h.service.AttachImage(context.Background(), author, upload("print.png", pngBytes))
	if err != nil {
		t.Fatalf("AttachImage: %v", err)
	}
	return img
}

func TestAttachImageStoresTheSniffedImageThroughTheSharedUpload(t *testing.T) {
	h := newHarness(t)

	img, err := h.service.AttachImage(context.Background(), author, upload("relatorio.pdf", pngBytes))
	if err != nil {
		t.Fatalf("AttachImage: %v", err)
	}

	if len(h.uploader.received) != 1 {
		t.Fatalf("uploads = %d, want 1", len(h.uploader.received))
	}
	in := h.uploader.received[0]
	if in.EntryID != chat.EntryID || in.EntryType != string(chat.EntryType) || in.MediaType != conversation.MediaTypeImage {
		t.Fatalf("upload = %+v", in)
	}
	if in.MimeType != "image/png" || in.Filename != "relatorio.png" {
		t.Fatalf("mime = %q filename = %q, want the sniffed type and its extension", in.MimeType, in.Filename)
	}
	if img.ID == "" || img.URL != "https://cdn/"+img.ID || img.Layout.Width != 640 || img.Layout.Thumbhash == "" {
		t.Fatalf("image = %+v", img)
	}
}

func TestANoteImageIsStoredAsNoteMediaSoItCanNeverBeSent(t *testing.T) {
	h := newHarness(t)

	attach(t, h)

	if got := h.uploader.received[0].Purpose; got != conversation.MediaPurposeNote {
		t.Fatalf("purpose = %q, want note", got)
	}
}

func TestAttachImageRefusesBeforeStoringAnything(t *testing.T) {
	cases := []struct {
		name string
		by   shared.Person
		in   conversationnote.ImageUpload
		want error
	}{
		{"cannot see the conversation", shared.Person{UserID: "u-bia"}, upload("a.png", pngBytes), conversation.ErrUnauthorized},
		{"no conversations:read", shared.Person{UserID: "u-noperm"}, upload("a.png", pngBytes), conversation.ErrUnauthorized},
		{"another workspace", author, conversationnote.ImageUpload{WorkspaceID: "ws-2", Entry: chat, Data: pngBytes}, conversation.ErrUnauthorized},
		{"a channel without a conversation view", author, conversationnote.ImageUpload{WorkspaceID: "ws-1", Entry: shared.EntryRef{EntryID: chat.EntryID, EntryType: "voice"}, Data: pngBytes}, conversation.ErrUnauthorized},
		{"html named as a png", author, upload("a.png", []byte("<html><body>oi</body></html>")), conversationnote.ErrImageType},
		{"empty file", author, upload("a.png", nil), conversationnote.ErrImageRequired},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			if _, err := h.service.AttachImage(context.Background(), c.by, c.in); !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
			if len(h.uploader.received) != 0 {
				t.Fatal("a refused image must never reach storage")
			}
		})
	}
}

func TestAttachImageWithoutStorageIsACodedRefusal(t *testing.T) {
	h := newHarness(t)
	h.uploader.err = conversation.ErrMediaURLRequired
	if _, err := h.service.AttachImage(context.Background(), author, upload("a.png", pngBytes)); !errors.Is(err, conversationnote.ErrStorageMissing) {
		t.Fatalf("err = %v, want ErrStorageMissing", err)
	}
}

func TestCreateLinksTheImagesInOrderAndAnnouncesThem(t *testing.T) {
	h := newHarness(t)
	first, second := attach(t, h), attach(t, h)

	in := draft("")
	in.ImageIDs = []string{second.ID, first.ID, second.ID}
	got, err := h.service.Create(context.Background(), author, in)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if strings.Join(got.ImageIDs, ",") != second.ID+","+first.ID {
		t.Fatalf("stored ids = %v, want distinct ids in the order given", got.ImageIDs)
	}
	if len(got.Images) != 2 || got.Images[0].ID != second.ID || got.Images[1].URL != first.URL {
		t.Fatalf("images = %+v", got.Images)
	}
	stored, _ := h.notes.Find(context.Background(), "ws-1", got.ID)
	if strings.Join(stored.ImageIDs, ",") != second.ID+","+first.ID {
		t.Fatalf("persisted ids = %v", stored.ImageIDs)
	}
	if len(h.announcer.created) != 1 || len(h.announcer.created[0].Images) != 2 {
		t.Fatalf("the live event must carry the images: %+v", h.announcer.created)
	}
}

func TestCreateRefusesImagesThatAreNotThisConversationsNoteImages(t *testing.T) {
	cases := []struct {
		name  string
		setup func(h harness) string
		want  error
	}{
		{"unknown id", func(harness) string { return "media-nobody" }, conversationnote.ErrImageNotFound},
		{"an image of another conversation", func(h harness) string {
			h.media.rows["media-elsewhere"] = &conversation.ConversationMedia{ID: "media-elsewhere", EntryID: "entry-2", EntryType: chat.EntryType, Type: conversation.MediaTypeImage, MimeType: "image/png", SizeBytes: 10}
			return "media-elsewhere"
		}, conversationnote.ErrImageNotFound},
		{"a message document", func(h harness) string {
			h.media.rows["media-doc"] = &conversation.ConversationMedia{ID: "media-doc", EntryID: chat.EntryID, EntryType: chat.EntryType, Type: conversation.MediaTypeDocument, MimeType: "application/pdf", SizeBytes: 10}
			return "media-doc"
		}, conversationnote.ErrImageNotFound},
		{"an svg uploaded through the message path", func(h harness) string {
			h.media.rows["media-svg"] = &conversation.ConversationMedia{ID: "media-svg", EntryID: chat.EntryID, EntryType: chat.EntryType, Type: conversation.MediaTypeImage, MimeType: "image/svg+xml", SizeBytes: 10}
			return "media-svg"
		}, conversationnote.ErrImageType},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			good := attach(t, h)
			in := draft("veja")
			in.ImageIDs = []string{good.ID, c.setup(h)}
			if _, err := h.service.Create(context.Background(), author, in); !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
			if len(h.notes.notes) != 0 || len(h.announcer.created) != 0 {
				t.Fatal("a refused note must leave no trace")
			}
		})
	}
}

func TestTooManyImagesIsRefusedBeforeLookingThemUp(t *testing.T) {
	h := newHarness(t)
	in := draft("muitas")
	for i := 0; i <= conversationnote.MaxImages; i++ {
		in.ImageIDs = append(in.ImageIDs, "media-"+strings.Repeat("x", i+1))
	}
	if _, err := h.service.Create(context.Background(), author, in); !errors.Is(err, conversationnote.ErrTooManyImages) {
		t.Fatalf("err = %v, want ErrTooManyImages", err)
	}
	if len(h.media.asks) != 0 {
		t.Fatal("the bound is checked before any lookup")
	}
}

func TestCreateWithImagesStillRequiresSight(t *testing.T) {
	h := newHarness(t)
	img := attach(t, h)
	in := draft("")
	in.ImageIDs = []string{img.ID}
	if _, err := h.service.Create(context.Background(), shared.Person{UserID: "u-bia"}, in); !errors.Is(err, conversation.ErrUnauthorized) {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}
	if len(h.media.asks) != 0 {
		t.Fatal("nobody learns whether an image exists before passing the scope check")
	}
}

func TestListAttachesEveryPagesImagesInOneLookup(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	a, b := attach(t, h), attach(t, h)
	for _, ids := range [][]string{{a.ID}, {}, {b.ID, a.ID}} {
		in := draft("nota")
		in.ImageIDs = ids
		if _, err := h.service.Create(ctx, author, in); err != nil {
			t.Fatal(err)
		}
	}
	h.media.asks = nil

	page, err := h.service.List(ctx, shared.Person{UserID: "u-ana"}, conversationnote.PageQuery{WorkspaceID: "ws-1", Entry: chat})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(h.media.asks) != 1 {
		t.Fatalf("lookups = %d, want one per page", len(h.media.asks))
	}
	counts := map[int]int{}
	for _, note := range page.Notes {
		if note.Images == nil {
			t.Fatal("images must be an empty list, never null")
		}
		counts[len(note.Images)]++
	}
	if counts[0] != 1 || counts[1] != 1 || counts[2] != 1 {
		t.Fatalf("image counts = %v", counts)
	}
}

func TestListWithoutImagesNeverAsksForMedia(t *testing.T) {
	h := newHarness(t)
	if _, err := h.service.Create(context.Background(), author, draft("só texto")); err != nil {
		t.Fatal(err)
	}
	if _, err := h.service.List(context.Background(), author, conversationnote.PageQuery{WorkspaceID: "ws-1", Entry: chat}); err != nil {
		t.Fatal(err)
	}
	if len(h.media.asks) != 0 {
		t.Fatalf("asks = %v", h.media.asks)
	}
}

func TestListFailsLoudlyWhenTheImagesCannotBeRead(t *testing.T) {
	h := newHarness(t)
	img := attach(t, h)
	in := draft("com imagem")
	in.ImageIDs = []string{img.ID}
	if _, err := h.service.Create(context.Background(), author, in); err != nil {
		t.Fatal(err)
	}
	h.media.err = errors.New("db down")
	if _, err := h.service.List(context.Background(), author, conversationnote.PageQuery{WorkspaceID: "ws-1", Entry: chat}); err == nil {
		t.Fatal("a note must never silently lose its images")
	}
}

func TestDeletingANoteHidesItsImagesAndLeavesTheFilesToTheEntryPurge(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	img := attach(t, h)
	in := draft("")
	in.ImageIDs = []string{img.ID}
	note, err := h.service.Create(ctx, author, in)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.service.Delete(ctx, author, "ws-1", note.ID); err != nil {
		t.Fatal(err)
	}
	page, _ := h.service.List(ctx, author, conversationnote.PageQuery{WorkspaceID: "ws-1", Entry: chat})
	if len(page.Notes) != 0 {
		t.Fatal("the deleted note and its images are gone from the thread")
	}
	if _, kept := h.media.rows[img.ID]; !kept {
		t.Fatal("media rows stay with the conversation like message media; the entry purge removes rows and files")
	}
}
