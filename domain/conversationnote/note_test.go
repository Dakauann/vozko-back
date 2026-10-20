package conversationnote

import (
	"errors"
	"strings"
	"testing"
	"time"

	"vozko/domain/shared"
)

var (
	chat      = shared.EntryRef{EntryID: "entry-1", EntryType: shared.EntryTypeWhatsApp}
	writtenAt = time.Date(2026, 10, 18, 15, 4, 5, 0, time.FixedZone("BRT", -3*3600))
)

func validDraft() Draft {
	return Draft{WorkspaceID: "ws-1", Entry: chat, AuthorID: "author-1", Body: "Cliente pediu desconto"}
}

func TestNewNoteKeepsTheTrimmedBodyAndStampsUTC(t *testing.T) {
	d := validDraft()
	d.Body = "  ligar amanhã \n"

	note, err := New(d, "note-1", writtenAt)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if note.Body != "ligar amanhã" {
		t.Fatalf("body = %q, want the trimmed text", note.Body)
	}
	if note.ID != "note-1" || note.WorkspaceID != "ws-1" || note.AuthorID != "author-1" || note.Entry() != chat {
		t.Fatalf("identity not kept: %+v", note)
	}
	if note.CreatedAt.Location() != time.UTC || !note.CreatedAt.Equal(writtenAt) {
		t.Fatalf("created_at = %v, want %v in UTC", note.CreatedAt, writtenAt)
	}
	if note.DeletedAt != nil {
		t.Fatal("a new note is never deleted")
	}
	if note.Mentions == nil {
		t.Fatal("mentions must be an empty list, never null, so the wire shape is stable")
	}
}

func TestNewNoteRefusesWhatCannotBeStored(t *testing.T) {
	cases := []struct {
		name   string
		change func(*Draft)
		id     string
		want   error
	}{
		{"empty body", func(d *Draft) { d.Body = "" }, "note-1", ErrBodyRequired},
		{"only spaces is still empty", func(d *Draft) { d.Body = " \t\n " }, "note-1", ErrBodyRequired},
		{"one rune over the limit", func(d *Draft) { d.Body = strings.Repeat("é", MaxBodyRunes+1) }, "note-1", ErrBodyTooLong},
		{"no author", func(d *Draft) { d.AuthorID = " " }, "note-1", ErrAuthorRequired},
		{"no workspace", func(d *Draft) { d.WorkspaceID = "" }, "note-1", ErrWorkspaceRequired},
		{"no conversation", func(d *Draft) { d.Entry.EntryID = "" }, "note-1", ErrEntryRequired},
		{"a channel without a conversation view", func(d *Draft) { d.Entry.EntryType = "sms" }, "note-1", ErrEntryRequired},
		{"no id", func(d *Draft) {}, "", ErrIDRequired},
		{"too many distinct mentions", func(d *Draft) { d.Mentions = distinctIDs(MaxMentions + 1) }, "note-1", ErrTooManyMentions},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := validDraft()
			c.change(&d)
			if _, err := New(d, c.id, writtenAt); !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
		})
	}
}

func TestBodyOfExactlyTheLimitIsAccepted(t *testing.T) {
	d := validDraft()
	d.Body = strings.Repeat("é", MaxBodyRunes)
	if _, err := New(d, "note-1", writtenAt); err != nil {
		t.Fatalf("a body of %d runes must be accepted: %v", MaxBodyRunes, err)
	}
}

func distinctIDs(n int) []string {
	ids := make([]string, n)
	for i := range ids {
		ids[i] = "user-" + strings.Repeat("x", i+1)
	}
	return ids
}

func TestNormalizeMentionsDeduplicatesBeforeCounting(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
		err  error
	}{
		{"nil becomes empty", nil, []string{}, nil},
		{"blank ids are dropped", []string{"", "  "}, []string{}, nil},
		{"order of first appearance is kept", []string{" u-2", "u-1", "u-2 ", "u-1"}, []string{"u-2", "u-1"}, nil},
		{"repeats do not count toward the limit", append(distinctIDs(MaxMentions), distinctIDs(MaxMentions)...), distinctIDs(MaxMentions), nil},
		{"more distinct ids than the limit", distinctIDs(MaxMentions + 1), nil, ErrTooManyMentions},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := NormalizeMentions(c.in)
			if !errors.Is(err, c.err) {
				t.Fatalf("err = %v, want %v", err, c.err)
			}
			if c.err != nil {
				return
			}
			if strings.Join(got, ",") != strings.Join(c.want, ",") || got == nil {
				t.Fatalf("mentions = %#v, want %#v", got, c.want)
			}
		})
	}
}

func TestOnlyTheAuthorMayDeleteANote(t *testing.T) {
	note, err := New(validDraft(), "note-1", writtenAt)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		by   string
		want error
	}{
		{"the author", "author-1", nil},
		{"a teammate", "someone-else", ErrNotAuthor},
		{"nobody", "", ErrNotAuthor},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := note.DeletableBy(c.by); !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
		})
	}
}

func TestADeletedNoteCannotBeDeletedAgain(t *testing.T) {
	note, _ := New(validDraft(), "note-1", writtenAt)
	gone := writtenAt.Add(time.Minute)
	note.DeletedAt = &gone
	if err := note.DeletableBy("author-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestPageLimitDefaultsAndCaps(t *testing.T) {
	cases := []struct {
		name      string
		requested int
		want      int
	}{
		{"absent", 0, DefaultPageSize},
		{"within the range", 30, 30},
		{"exactly the cap", MaxPageSize, MaxPageSize},
		{"above the cap", MaxPageSize + 1, MaxPageSize},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := PageLimit(c.requested); got != c.want {
				t.Fatalf("PageLimit(%d) = %d, want %d", c.requested, got, c.want)
			}
		})
	}
}
