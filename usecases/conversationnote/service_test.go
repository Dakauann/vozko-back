package conversationnote_usecase

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/conversationnote"
	"vozko/domain/notification"
	"vozko/domain/shared"
)

var (
	chat   = shared.EntryRef{EntryID: "entry-1", EntryType: shared.EntryTypeWhatsApp}
	now    = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	author = shared.Person{UserID: "u-author"}
)

type harness struct {
	service   *Service
	notes     *memoryNotes
	announcer *recordingAnnouncer
	publisher *recordingPublisher
}

func newHarness(t *testing.T) harness {
	t.Helper()
	notes := newMemoryNotes()
	announcer := &recordingAnnouncer{}
	publisher := &recordingPublisher{}
	ids := 0
	service, err := New(Deps{
		Notes: notes,
		Access: fakeAccess{
			workspace: "ws-1",
			readers:   map[string]bool{"u-author": true, "u-ana": true, "u-bia": true, "u-noread": true, "u-other": true},
			seers:     map[string]bool{"u-author": true, "u-ana": true, "u-noread": true, "u-other": true},
			members:   map[string]bool{"u-author": true, "u-ana": true, "u-bia": true, "u-noread": true, "u-other": true},
		},
		People:    fakePeople{"u-author": "Carla", "u-ana": "Ana", "u-bia": "Bia", "u-other": "Otto"},
		Announcer: announcer,
		Notifier:  publisher,
		Contacts:  fakeContacts{chat: {LeadName: "João Lima"}},
		Now:       func() time.Time { return now },
		NewID: func() string {
			ids++
			return "note-" + strings.Repeat("i", ids)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return harness{service: service, notes: notes, announcer: announcer, publisher: publisher}
}

func draft(body string, mentions ...string) conversationnote.Draft {
	return conversationnote.Draft{WorkspaceID: "ws-1", Entry: chat, Body: body, Mentions: mentions}
}

func TestNewRefusesToRunWithoutItsCollaborators(t *testing.T) {
	if _, err := New(Deps{}); err == nil {
		t.Fatal("a service without its ports must not be built")
	}
}

func TestCreateStoresTheNoteAsTheCallerAndAnnouncesItWithTheAuthorName(t *testing.T) {
	h := newHarness(t)
	in := draft("  combinar entrega  ")
	in.AuthorID = "u-forged"

	got, err := h.service.Create(context.Background(), author, in)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got.AuthorID != "u-author" {
		t.Fatalf("author = %q, the caller is always the author", got.AuthorID)
	}
	if got.AuthorName != "Carla" || got.Body != "combinar entrega" || !got.CreatedAt.Equal(now) {
		t.Fatalf("note = %+v", got)
	}
	if _, err := h.notes.Find(context.Background(), "ws-1", got.ID); err != nil {
		t.Fatalf("the note was not stored: %v", err)
	}
	if len(h.announcer.created) != 1 || h.announcer.created[0].ID != got.ID || h.announcer.created[0].AuthorName != "Carla" {
		t.Fatalf("announced = %+v", h.announcer.created)
	}
}

func TestEveryNoteOperationRefusesWhoCannotReadTheConversation(t *testing.T) {
	cases := []struct {
		name string
		by   shared.Person
		ws   string
	}{
		{"no conversations:read", shared.Person{UserID: "u-noperm"}, "ws-1"},
		{"reads conversations but cannot see this one", shared.Person{UserID: "u-bia"}, "ws-1"},
		{"another workspace", author, "ws-2"},
		{"anonymous", shared.Person{}, "ws-1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			in := draft("oi")
			in.WorkspaceID = c.ws
			if _, err := h.service.Create(context.Background(), c.by, in); !errors.Is(err, conversation.ErrUnauthorized) {
				t.Fatalf("create err = %v, want ErrUnauthorized", err)
			}
			if _, err := h.service.List(context.Background(), c.by, conversationnote.PageQuery{WorkspaceID: c.ws, Entry: chat}); !errors.Is(err, conversation.ErrUnauthorized) {
				t.Fatalf("list err = %v, want ErrUnauthorized", err)
			}
			if len(h.announcer.created) != 0 || len(h.notes.notes) != 0 {
				t.Fatal("a refused note must leave no trace")
			}
		})
	}
}

func TestCreateRejectsAnInvalidBodyBeforeStoringAnything(t *testing.T) {
	h := newHarness(t)
	_, err := h.service.Create(context.Background(), author, draft("   "))
	if !errors.Is(err, conversationnote.ErrBodyRequired) {
		t.Fatalf("err = %v, want ErrBodyRequired", err)
	}
	if len(h.notes.notes) != 0 || len(h.announcer.created) != 0 {
		t.Fatal("nothing may be stored or announced")
	}
}

func TestMentionsKeepOnlyWorkspaceMembersAndNotifyOnlyThoseWhoSeeTheConversation(t *testing.T) {
	h := newHarness(t)

	got, err := h.service.Create(context.Background(), author, draft("@Ana @Bia olhem", "u-ana", "u-bia", "u-stranger", "u-ana", "u-author", "u-other"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if strings.Join(got.Mentions, ",") != "u-ana,u-bia,u-author,u-other" {
		t.Fatalf("stored mentions = %v, want members only, deduplicated, in order", got.Mentions)
	}
	var notified []string
	for _, d := range h.publisher.drafts {
		notified = append(notified, d.Audience.UserIDs...)
	}
	if strings.Join(notified, ",") != "u-ana,u-other" {
		t.Fatalf("notified = %v; Bia cannot see the conversation, the stranger is not a member and the author never notifies themself", notified)
	}
}

func TestAMentionNotificationNamesTheAuthorAndTheContactAndLinksToTheConversation(t *testing.T) {
	h := newHarness(t)
	note, err := h.service.Create(context.Background(), author, draft("segredo interno", "u-ana"))
	if err != nil {
		t.Fatal(err)
	}
	if len(h.publisher.drafts) != 1 {
		t.Fatalf("drafts = %d, want 1", len(h.publisher.drafts))
	}
	d := h.publisher.drafts[0]
	if d.WorkspaceID != "ws-1" || d.Category != notification.CategoryConversations {
		t.Fatalf("draft = %+v", d)
	}
	if !strings.Contains(d.Title, "Carla") || !strings.Contains(d.Title, "João Lima") {
		t.Fatalf("title = %q, want the author and the contact", d.Title)
	}
	if strings.Contains(d.Title, "segredo") || strings.Contains(d.Body, "segredo") {
		t.Fatal("the note text must stay in the note: a deleted note cannot linger in the notification center")
	}
	if d.ActionURL != conversation.DashboardLink(chat) {
		t.Fatalf("action url = %q", d.ActionURL)
	}
	if d.DedupKey != "conversation_note_mention:"+note.ID+":u-ana" {
		t.Fatalf("dedup key = %q, want one per note and user", d.DedupKey)
	}
	if d.Source != notification.SourceConversationNoteMention {
		t.Fatalf("source = %q", d.Source)
	}
	if err := d.Normalize().Validate(); err != nil {
		t.Fatalf("the draft must pass the notification rules: %v", err)
	}
}

func TestAFailedNotificationDoesNotUndoTheNote(t *testing.T) {
	h := newHarness(t)
	h.publisher.err = errors.New("queue down")
	if _, err := h.service.Create(context.Background(), author, draft("oi", "u-ana")); err != nil {
		t.Fatalf("the note is stored even when a notification fails: %v", err)
	}
	if len(h.notes.notes) != 1 {
		t.Fatal("note missing")
	}
}

func TestTooManyMentionsIsRefusedBeforeLookingAnyoneUp(t *testing.T) {
	h := newHarness(t)
	ids := make([]string, conversationnote.MaxMentions+1)
	for i := range ids {
		ids[i] = "u-" + strings.Repeat("m", i+1)
	}
	if _, err := h.service.Create(context.Background(), author, draft("oi", ids...)); !errors.Is(err, conversationnote.ErrTooManyMentions) {
		t.Fatalf("err = %v, want ErrTooManyMentions", err)
	}
}

func TestListReturnsNewestFirstWithAuthorNamesAndPagesWithBefore(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	for i, body := range []string{"primeira", "segunda", "terceira"} {
		h.service.deps.Now = func() time.Time { return now.Add(time.Duration(i) * time.Minute) }
		if _, err := h.service.Create(ctx, author, draft(body)); err != nil {
			t.Fatal(err)
		}
	}

	first, err := h.service.List(ctx, shared.Person{UserID: "u-ana"}, conversationnote.PageQuery{WorkspaceID: "ws-1", Entry: chat, Limit: 2})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(first.Notes) != 2 || first.Notes[0].Body != "terceira" || first.Notes[1].Body != "segunda" || !first.HasMore {
		t.Fatalf("first page = %+v", first)
	}
	if first.Notes[0].AuthorName != "Carla" {
		t.Fatalf("author name = %q", first.Notes[0].AuthorName)
	}

	before := first.Notes[1].CreatedAt
	second, err := h.service.List(ctx, shared.Person{UserID: "u-ana"}, conversationnote.PageQuery{WorkspaceID: "ws-1", Entry: chat, Limit: 2, Before: &before})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Notes) != 1 || second.Notes[0].Body != "primeira" || second.HasMore {
		t.Fatalf("second page = %+v", second)
	}
}

func TestDeleteIsForTheAuthorOnlyAndAnnouncedToTheConversation(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	note, err := h.service.Create(ctx, author, draft("apagar depois"))
	if err != nil {
		t.Fatal(err)
	}

	if err := h.service.Delete(ctx, shared.Person{UserID: "u-ana"}, "ws-1", note.ID); !errors.Is(err, conversationnote.ErrNotAuthor) {
		t.Fatalf("a teammate deleting: err = %v, want ErrNotAuthor", err)
	}
	if err := h.service.Delete(ctx, shared.Person{UserID: "u-bia"}, "ws-1", note.ID); !errors.Is(err, conversationnote.ErrNotFound) {
		t.Fatalf("someone who cannot see the conversation must not learn the note exists: err = %v", err)
	}
	if err := h.service.Delete(ctx, author, "ws-2", note.ID); !errors.Is(err, conversationnote.ErrNotFound) {
		t.Fatalf("another workspace: err = %v, want ErrNotFound", err)
	}
	if len(h.announcer.deleted) != 0 {
		t.Fatal("refused deletes announce nothing")
	}

	if err := h.service.Delete(ctx, author, "ws-1", note.ID); err != nil {
		t.Fatalf("author delete: %v", err)
	}
	if len(h.announcer.deleted) != 1 || h.announcer.deleted[0].ID != note.ID || h.announcer.deleted[0].Entry() != chat {
		t.Fatalf("deleted announcements = %+v", h.announcer.deleted)
	}
	if err := h.service.Delete(ctx, author, "ws-1", note.ID); !errors.Is(err, conversationnote.ErrNotFound) {
		t.Fatalf("second delete: err = %v, want ErrNotFound", err)
	}
	page, _ := h.service.List(ctx, author, conversationnote.PageQuery{WorkspaceID: "ws-1", Entry: chat})
	if len(page.Notes) != 0 {
		t.Fatal("a deleted note never comes back in the list")
	}
}
