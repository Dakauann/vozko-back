package mercadolivre

import (
	"errors"
	"testing"
	"time"
)

func TestParseQuestionStatus(t *testing.T) {
	for _, raw := range []string{"UNANSWERED", "ANSWERED", "CLOSED_UNANSWERED", "UNDER_REVIEW", "BANNED", "DELETED", "DISABLED"} {
		got, err := ParseQuestionStatus(raw)
		if err != nil || string(got) != raw {
			t.Errorf("ParseQuestionStatus(%q) = %q, %v", raw, got, err)
		}
	}
	for _, raw := range []string{"", "unanswered", "OPEN", "ACTIVE"} {
		if _, err := ParseQuestionStatus(raw); !errors.Is(err, ErrUnknownQuestionStatus) {
			t.Errorf("ParseQuestionStatus(%q) err = %v", raw, err)
		}
	}
}

func TestParseAnswerStatus(t *testing.T) {
	for _, raw := range []string{"ACTIVE", "DISABLED", "BANNED"} {
		got, err := ParseAnswerStatus(raw)
		if err != nil || string(got) != raw {
			t.Errorf("ParseAnswerStatus(%q) = %q, %v", raw, got, err)
		}
	}
	for _, raw := range []string{"", "active", "DELETED"} {
		if _, err := ParseAnswerStatus(raw); !errors.Is(err, ErrUnknownAnswerStatus) {
			t.Errorf("ParseAnswerStatus(%q) err = %v", raw, err)
		}
	}
}

func TestIsRuleEligible(t *testing.T) {
	cases := []struct {
		name    string
		q       *Question
		created bool
		want    bool
	}{
		{"first sight of an open question", &Question{Status: QuestionUnanswered}, true, true},
		{"update of an open question", &Question{Status: QuestionUnanswered}, false, false},
		{"first sight of an answered question", &Question{Status: QuestionAnswered}, true, false},
		{"first sight under review", &Question{Status: QuestionUnderReview}, true, false},
		{"first sight banned", &Question{Status: QuestionBanned}, true, false},
		{"nil question", nil, true, false},
	}
	for _, tc := range cases {
		if got := IsRuleEligible(tc.q, tc.created); got != tc.want {
			t.Errorf("%s: IsRuleEligible = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestQuestionCanBeAnsweredOnlyWhileUnanswered(t *testing.T) {
	for _, s := range []QuestionStatus{QuestionAnswered, QuestionClosedUnanswered, QuestionUnderReview, QuestionBanned, QuestionDeleted, QuestionDisabled} {
		if err := (&Question{Status: s}).CanAnswer(); !errors.Is(err, ErrQuestionNotOpen) {
			t.Errorf("CanAnswer(%q) = %v", s, err)
		}
	}
	if err := (&Question{Status: QuestionUnanswered}).CanAnswer(); err != nil {
		t.Fatalf("CanAnswer(UNANSWERED) = %v", err)
	}
}

func TestQuestionBelongsToItsAccount(t *testing.T) {
	q := &Question{AccountID: "acc-1", ItemID: "MLB1"}
	if err := q.OwnedBy("acc-1"); err != nil {
		t.Fatalf("OwnedBy = %v", err)
	}
	for _, other := range []string{"acc-2", ""} {
		if err := q.OwnedBy(other); !errors.Is(err, ErrQuestionNotOwned) {
			t.Errorf("OwnedBy(%q) = %v", other, err)
		}
	}
}

func TestAnswerSourceIsClosed(t *testing.T) {
	for _, s := range []AnswerSource{AnsweredByOperator, AnsweredByRule, AnsweredByMercadoLivreApp} {
		if !s.Valid() {
			t.Errorf("%q must be valid", s)
		}
	}
	if AnswerSource("agent").Valid() || AnswerSource("").Valid() {
		t.Fatal("unknown answer sources must be refused")
	}
}

func TestValidItemID(t *testing.T) {
	cases := map[string]bool{
		"MLB3737188005": true,
		"MLB1":          true,
		"MLA686791111":  false,
		"MLB":           false,
		"mlb123":        false,
		"MLB12a":        false,
		" MLB123":       false,
		"":              false,
	}
	for id, want := range cases {
		if got := ValidItemID(id); got != want {
			t.Errorf("ValidItemID(%q) = %v, want %v", id, got, want)
		}
	}
}

func TestListingFreshness(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		synced *time.Time
		want   bool
	}{
		{nil, true},
		{ptrTime(now.Add(-time.Hour)), false},
		{ptrTime(now.Add(-6 * time.Hour)), true},
	}
	for _, tc := range cases {
		if got := (&Listing{SyncedAt: tc.synced}).NeedsRefresh(now); got != tc.want {
			t.Errorf("NeedsRefresh(%v) = %v, want %v", tc.synced, got, tc.want)
		}
	}
}

func TestContactProfileFreshness(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		refreshed *time.Time
		want      bool
	}{
		{nil, true},
		{ptrTime(now.Add(-23 * time.Hour)), false},
		{ptrTime(now.Add(-24 * time.Hour)), true},
	}
	for _, tc := range cases {
		if got := (&Contact{ProfileRefreshedAt: tc.refreshed}).ProfileIsStale(now); got != tc.want {
			t.Errorf("ProfileIsStale(%v) = %v, want %v", tc.refreshed, got, tc.want)
		}
	}
	if got := (&Contact{MLUserID: "1944693439"}).DisplayName(); got != "1944693439" {
		t.Fatalf("DisplayName without nickname = %q", got)
	}
	if got := (&Contact{MLUserID: "1", Nickname: " TETE2870021 "}).DisplayName(); got != "TETE2870021" {
		t.Fatalf("DisplayName = %q", got)
	}
}

func TestValidateAttachment(t *testing.T) {
	cases := []struct {
		name     string
		fileName string
		mime     string
		size     int64
		err      error
	}{
		{"jpeg", "foto.jpg", "image/jpeg", 1024, nil},
		{"png", "foto.png", "image/png", 1024, nil},
		{"pdf", "nota.pdf", "application/pdf", 1024, nil},
		{"txt", "nota.txt", "text/plain", 1024, nil},
		{"txt with charset", "nota.txt", "text/plain; charset=utf-8", 1024, nil},
		{"exactly 25 MB", "nota.pdf", "application/pdf", 25 << 20, nil},
		{"over 25 MB", "nota.pdf", "application/pdf", 25<<20 + 1, ErrMediaTooLarge},
		{"empty file", "nota.pdf", "application/pdf", 0, ErrMediaUnsupported},
		{"gif", "anim.gif", "image/gif", 1024, ErrMediaUnsupported},
		{"video", "clip.mp4", "video/mp4", 1024, ErrMediaUnsupported},
		{"slash in name", "a/b.pdf", "application/pdf", 1024, ErrMediaUnsupported},
		{"backslash in name", "a\\b.pdf", "application/pdf", 1024, ErrMediaUnsupported},
		{"blank name", " ", "application/pdf", 1024, ErrMediaUnsupported},
		{"name over 200 runes", repeatRune('a', 197) + ".pdf", "application/pdf", 1024, ErrMediaUnsupported},
		{"name of 200 runes", repeatRune('a', 196) + ".pdf", "application/pdf", 1024, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateAttachment(tc.fileName, tc.mime, tc.size)
			if tc.err == nil && err != nil {
				t.Fatalf("err = %v", err)
			}
			if tc.err != nil && !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
		})
	}
}

func repeatRune(r rune, n int) string {
	out := make([]rune, n)
	for i := range out {
		out[i] = r
	}
	return string(out)
}
