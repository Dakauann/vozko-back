package tiktok

import (
	"errors"
	"testing"
	"time"
)

func TestParseCommentAction(t *testing.T) {
	cases := map[string]CommentAction{
		"insert":              CommentInserted,
		"delete":              CommentDeleted,
		"set_to_hidden":       CommentSetHidden,
		"set_to_friends_only": CommentSetFriendsOnly,
		"set_to_public":       CommentSetPublic,
	}
	for raw, want := range cases {
		got, err := ParseCommentAction(raw)
		if err != nil || got != want {
			t.Errorf("ParseCommentAction(%q) = %q, %v", raw, got, err)
		}
	}
	for _, raw := range []string{"", "INSERT", "update"} {
		if _, err := ParseCommentAction(raw); !errors.Is(err, ErrInvalidContent) {
			t.Errorf("ParseCommentAction(%q) err = %v", raw, err)
		}
	}
}

func TestCommentActionVisibility(t *testing.T) {
	cases := map[CommentAction]CommentVisibility{
		CommentInserted:       VisibilityPublic,
		CommentDeleted:        VisibilityDeleted,
		CommentSetHidden:      VisibilityHidden,
		CommentSetFriendsOnly: VisibilityFriendsOnly,
		CommentSetPublic:      VisibilityPublic,
	}
	for action, want := range cases {
		if got := action.Visibility(); got != want {
			t.Errorf("%s.Visibility() = %q, want %q", action, got, want)
		}
	}
}

func TestVisibilityFromListStatus(t *testing.T) {
	if VisibilityFromStatus("HIDDEN") != VisibilityHidden || VisibilityFromStatus("PUBLIC") != VisibilityPublic {
		t.Fatal("list statuses map to visibilities")
	}
	if VisibilityFromStatus("WHATEVER") != VisibilityUnknown {
		t.Fatal("an unknown status is unknown, never public")
	}
}

func TestCommentEventsApplyOnlyWhenNewer(t *testing.T) {
	at := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	c := Comment{}
	if !c.AcceptsEventAt(at) {
		t.Fatal("a comment with no events accepts the first one")
	}
	c.LastEventAt = &at
	if c.AcceptsEventAt(at) {
		t.Error("the same timestamp must not apply twice")
	}
	if c.AcceptsEventAt(at.Add(-time.Second)) {
		t.Error("an older event must not overwrite a newer state")
	}
	if !c.AcceptsEventAt(at.Add(time.Second)) {
		t.Error("a newer event applies")
	}
}

func TestLookupBackoffGrowsAndCaps(t *testing.T) {
	prev := time.Duration(0)
	for attempt := 1; attempt <= MaxLookupAttempts; attempt++ {
		d := LookupBackoff(attempt)
		if d < prev {
			t.Fatalf("attempt %d backoff %v shrank from %v", attempt, d, prev)
		}
		if d > MaxLookupBackoff {
			t.Fatalf("attempt %d backoff %v exceeds the cap", attempt, d)
		}
		prev = d
	}
	if LookupBackoff(1) != LookupBackoffBase {
		t.Errorf("first retry = %v", LookupBackoff(1))
	}
	if LookupBackoff(MaxLookupAttempts) != MaxLookupBackoff {
		t.Errorf("last retry = %v, want the cap", LookupBackoff(MaxLookupAttempts))
	}
}

func TestCommentNeedsRulesOnlyWhenTheAuthorIsKnownAndNotOurs(t *testing.T) {
	yes, no := true, false
	cases := []struct {
		name string
		c    Comment
		want bool
	}{
		{"unknown author", Comment{RulesDue: true}, false},
		{"ours", Comment{RulesDue: true, IsOurs: &yes}, false},
		{"theirs", Comment{RulesDue: true, IsOurs: &no}, true},
		{"already evaluated", Comment{IsOurs: &no}, false},
		{"deleted", Comment{RulesDue: true, IsOurs: &no, Visibility: VisibilityDeleted}, false},
	}
	for _, tc := range cases {
		if got := tc.c.ReadyForRules(); got != tc.want {
			t.Errorf("%s: ReadyForRules = %v", tc.name, got)
		}
	}
}

func TestCommentReplyText(t *testing.T) {
	if err := ValidateCommentText(" "); !errors.Is(err, ErrCommentTextEmpty) {
		t.Errorf("empty: %v", err)
	}
	long := make([]rune, MaxCommentRunes+1)
	for i := range long {
		long[i] = 'á'
	}
	if err := ValidateCommentText(string(long)); !errors.Is(err, ErrCommentTextTooLong) {
		t.Errorf("long: %v", err)
	}
	if err := ValidateCommentText(string(long[:MaxCommentRunes])); err != nil {
		t.Errorf("at the limit: %v", err)
	}
}
