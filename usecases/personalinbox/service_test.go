package personalinbox_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/personalinbox"
	"vozko/domain/shared"
)

const homeWorkspace = "ws-1"

var (
	clock  = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	ana    = shared.Person{UserID: "ana"}
	bia    = shared.Person{UserID: "bia"}
	chat   = shared.EntryRef{EntryID: "entry-1", EntryType: shared.EntryTypeTelegram}
	hidden = shared.EntryRef{EntryID: "entry-2", EntryType: shared.EntryTypeTelegram}
)

type fixture struct {
	marks     *memoryMarks
	announcer *announcer
	service   *Service
}

func newFixture(t *testing.T, seed ...personalinbox.Mark) fixture {
	t.Helper()
	f := fixture{marks: newMemoryMarks(seed...), announcer: &announcer{}}
	service, err := New(Deps{
		Marks: f.marks,
		Access: access{
			readers: map[string]bool{"ana": true, "bia": true},
			sees: map[markKey]bool{
				{"ana", chat}: true,
				{"bia", chat}: true,
			},
		},
		Announcer: f.announcer,
		Now:       func() time.Time { return clock },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	f.service = service
	return f
}

func seeded(t *testing.T, user string, entry shared.EntryRef, change func(personalinbox.Mark) personalinbox.Mark) personalinbox.Mark {
	t.Helper()
	m, err := personalinbox.NewMark(user, entry)
	if err != nil {
		t.Fatal(err)
	}
	return change(m)
}

func TestNewRefusesToRunWithoutItsPorts(t *testing.T) {
	if _, err := New(Deps{}); err == nil {
		t.Fatal("a service without marks, access and announcer must refuse to start")
	}
}

func TestEveryWriteRefusesAConversationTheUserCannotSee(t *testing.T) {
	type write func(f fixture, by shared.Person, workspaceID string, entry shared.EntryRef) error
	writes := map[string]write{
		"pin": func(f fixture, by shared.Person, workspaceID string, entry shared.EntryRef) error {
			_, err := f.service.Pin(context.Background(), by, workspaceID, entry, true)
			return err
		},
		"mark unread": func(f fixture, by shared.Person, workspaceID string, entry shared.EntryRef) error {
			_, err := f.service.MarkUnread(context.Background(), by, workspaceID, entry)
			return err
		},
		"clear unread mark": func(f fixture, by shared.Person, workspaceID string, entry shared.EntryRef) error {
			_, err := f.service.ClearUnreadMark(context.Background(), by, workspaceID, entry)
			return err
		},
	}
	refusals := []struct {
		name        string
		by          shared.Person
		workspaceID string
		entry       shared.EntryRef
	}{
		{"on a conversation out of scope", ana, homeWorkspace, hidden},
		{"without conversations:read", shared.Person{UserID: "caio"}, homeWorkspace, chat},
		{"from another workspace", ana, "ws-2", chat},
		{"on an unknown channel", ana, homeWorkspace, shared.EntryRef{EntryID: "entry-1", EntryType: "fax"}},
	}
	for name, run := range writes {
		for _, r := range refusals {
			t.Run(name+" "+r.name, func(t *testing.T) {
				f := newFixture(t)
				if err := run(f, r.by, r.workspaceID, r.entry); !errors.Is(err, conversation.ErrUnauthorized) {
					t.Fatalf("err = %v, want ErrUnauthorized", err)
				}
				if len(f.marks.marks) != 0 || len(f.announcer.sent) != 0 {
					t.Fatal("a refused write stored or announced a mark")
				}
			})
		}
	}
}

func TestPinStoresTheMarkAndAnswersOnlyThatUser(t *testing.T) {
	f := newFixture(t)
	mark, err := f.service.Pin(context.Background(), ana, homeWorkspace, chat, true)
	if err != nil || !mark.Pinned() {
		t.Fatalf("Pin = %+v, %v", mark, err)
	}
	sent := f.announcer.to("ana")
	if len(sent) != 1 || sent[0].workspaceID != homeWorkspace || !sent[0].mark.Same(mark) {
		t.Fatalf("announced = %+v, want the new mark to ana in the conversation's workspace", f.announcer.sent)
	}
	if len(f.announcer.to("bia")) != 0 {
		t.Fatal("a personal mark reached another user")
	}

	unpinned, err := f.service.Pin(context.Background(), ana, homeWorkspace, chat, false)
	if err != nil || !unpinned.Empty() {
		t.Fatalf("unpin = %+v, %v", unpinned, err)
	}
	if _, stored := f.marks.get("ana", chat); stored {
		t.Fatal("an unpinned empty mark stayed stored")
	}
}

func TestMarkReadClearsOnlyAnExistingUnreadMarkAndAnnouncesIt(t *testing.T) {
	f := newFixture(t, seeded(t, "ana", chat, func(m personalinbox.Mark) personalinbox.Mark { return m.MarkUnread(clock).SetPinned(true, clock) }))

	f.service.MarkRead(context.Background(), ana, homeWorkspace, chat)
	stored, _ := f.marks.get("ana", chat)
	if stored.Unread() || !stored.Pinned() {
		t.Fatalf("after reading = %+v, want the unread mark cleared and the pin kept", stored)
	}
	if sent := f.announcer.to("ana"); len(sent) != 1 || sent[0].mark.Unread() {
		t.Fatalf("announced = %+v, want one mark without unread", sent)
	}

	f.service.MarkRead(context.Background(), ana, homeWorkspace, chat)
	if len(f.announcer.to("ana")) != 1 {
		t.Fatal("reading a conversation with no unread mark must not announce anything")
	}
}

func TestPageMarksReturnsTheRequestersMarksOnly(t *testing.T) {
	f := newFixture(t,
		seeded(t, "ana", chat, func(m personalinbox.Mark) personalinbox.Mark { return m.SetPinned(true, clock) }),
		seeded(t, "bia", chat, func(m personalinbox.Mark) personalinbox.Mark { return m.MarkUnread(clock) }),
	)
	marks := f.service.PageMarks(context.Background(), "ana", []shared.EntryRef{chat, hidden})
	if len(marks) != 1 || marks[0].UserID != "ana" || !marks[0].Pinned() {
		t.Fatalf("PageMarks = %+v, want ana's pin only", marks)
	}
	if none := f.service.PageMarks(context.Background(), "ana", nil); len(none) != 0 {
		t.Fatalf("an empty page has no marks: %+v", none)
	}
}

func TestClearUnreadMarkTakesOffOnlyTheCallersOwnMark(t *testing.T) {
	f := newFixture(t,
		seeded(t, "ana", chat, func(m personalinbox.Mark) personalinbox.Mark { return m.MarkUnread(clock).SetPinned(true, clock) }),
		seeded(t, "bia", chat, func(m personalinbox.Mark) personalinbox.Mark { return m.MarkUnread(clock) }),
	)

	mark, err := f.service.ClearUnreadMark(context.Background(), ana, homeWorkspace, chat)
	if err != nil || mark.Unread() || !mark.Pinned() {
		t.Fatalf("ClearUnreadMark = %+v, %v; want the unread mark gone and the pin kept", mark, err)
	}
	if stored, _ := f.marks.get("bia", chat); !stored.Unread() {
		t.Fatal("clearing ana's mark touched bia's")
	}
	if sent := f.announcer.to("ana"); len(sent) != 1 || sent[0].mark.Unread() {
		t.Fatalf("announced = %+v, want one answer without unread", sent)
	}

	if _, err := f.service.ClearUnreadMark(context.Background(), ana, homeWorkspace, chat); err != nil {
		t.Fatal(err)
	}
	if len(f.announcer.to("ana")) != 2 {
		t.Fatal("an undo that finds nothing to clear must still answer the caller, like every personal command")
	}
}
