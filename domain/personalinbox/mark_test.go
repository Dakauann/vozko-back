package personalinbox

import (
	"errors"
	"testing"
	"time"

	"vozko/domain/shared"
)

var (
	now     = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	chat    = shared.EntryRef{EntryID: "entry-1", EntryType: shared.EntryTypeTelegram}
	earlier = now.Add(-time.Hour)
)

func at(t time.Time) *time.Time { return &t }

func blank(t *testing.T) Mark {
	t.Helper()
	m, err := NewMark("user-1", chat)
	if err != nil {
		t.Fatalf("NewMark: %v", err)
	}
	return m
}

func TestNewMarkRefusesAMarkWithoutItsOwnerOrConversation(t *testing.T) {
	cases := []struct {
		name  string
		user  string
		entry shared.EntryRef
		want  error
	}{
		{"no user", " ", chat, ErrUserRequired},
		{"no entry id", "user-1", shared.EntryRef{EntryType: shared.EntryTypeTelegram}, ErrEntryRequired},
		{"unknown channel", "user-1", shared.EntryRef{EntryID: "e", EntryType: "fax"}, ErrEntryRequired},
		{"valid", "user-1", chat, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, err := NewMark(c.user, c.entry)
			if !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
			if err == nil && (m.UserID != "user-1" || m.Entry() != chat || !m.Empty()) {
				t.Fatalf("a new mark must start empty on its conversation: %+v", m)
			}
		})
	}
}

func TestPinningKeepsTheFirstPinTimeSoThePinnedOrderIsStable(t *testing.T) {
	m := blank(t).SetPinned(true, earlier)
	again := m.SetPinned(true, now)
	if !again.PinnedAt.Equal(earlier) {
		t.Fatalf("re-pinning moved the pin to %v", again.PinnedAt)
	}
	if again.SetPinned(false, now).Pinned() {
		t.Fatal("unpinning left the conversation pinned")
	}
}

func TestReadingClearsTheUnreadMarkOnly(t *testing.T) {
	m := blank(t).SetPinned(true, now).MarkUnread(now)
	if !m.Unread() {
		t.Fatal("mark unread did not mark")
	}
	read := m.MarkRead()
	if read.Unread() || !read.Pinned() {
		t.Fatalf("reading must clear the unread mark and keep the pin: %+v", read)
	}
}

func TestEmptyMeansNothingWorthStoring(t *testing.T) {
	cases := []struct {
		name string
		mark Mark
		want bool
	}{
		{"new", blank(t), true},
		{"pinned", blank(t).SetPinned(true, now), false},
		{"unread", blank(t).MarkUnread(now), false},
		{"pinned then unpinned", blank(t).SetPinned(true, now).SetPinned(false, now), true},
		{"unread then read", blank(t).MarkUnread(now).MarkRead(), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.mark.Empty(); got != c.want {
				t.Fatalf("Empty() = %v, want %v", got, c.want)
			}
		})
	}
}

func TestSameComparesEveryFieldByInstant(t *testing.T) {
	a := blank(t).SetPinned(true, now)
	b := blank(t).SetPinned(true, now.In(time.FixedZone("BRT", -3*60*60)))
	if !a.Same(b) {
		t.Fatal("the same instant in another zone is the same mark")
	}
	if a.Same(a.MarkUnread(now)) {
		t.Fatal("a mark with an unread mark differs from one without")
	}
	other, _ := NewMark("user-2", chat)
	if blank(t).Same(other) {
		t.Fatal("marks of different users differ")
	}
}

func TestParseInboxViewFailsClosedOnUnknownValues(t *testing.T) {
	cases := []struct {
		raw  string
		want View
		err  error
	}{
		{"", ViewInbox, nil},
		{"pinned", ViewPinned, nil},
		{" pinned ", ViewPinned, nil},
		{"archived", ViewNone, ErrViewUnknown},
		{"inbox", ViewNone, ErrViewUnknown},
	}
	for _, c := range cases {
		t.Run(c.raw, func(t *testing.T) {
			got, err := ParseInboxView(c.raw)
			if got != c.want || !errors.Is(err, c.err) {
				t.Fatalf("ParseInboxView(%q) = %q, %v; want %q, %v", c.raw, got, err, c.want, c.err)
			}
		})
	}
}

func TestOnlyInboxViewsApplyPersonalMarks(t *testing.T) {
	if ViewNone.Applies() {
		t.Error("surfaces that never asked for personal marks must not order pinned conversations first")
	}
	for _, v := range []View{ViewInbox, ViewPinned} {
		if !v.Applies() {
			t.Errorf("%q must apply personal marks", v)
		}
	}
}
