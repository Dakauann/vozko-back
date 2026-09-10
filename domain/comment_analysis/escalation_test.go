package comment_analysis

import (
	"errors"
	"strings"
	"testing"
	"time"

	"vozko/domain/shared"
)

func escalatedComment() *CommentAnalysis {
	return &CommentAnalysis{
		ID:               "row-1",
		WorkspaceID:      "ws-1",
		Source:           SourceInstagram,
		AccountID:        "acc-1",
		ContainerID:      "media-1",
		SourceCommentID:  "c-1",
		AuthorExternalID: "ig-99",
		AuthorHandle:     "fulano",
		Status:           StatusAnalyzed,
		Stance:           StanceHostile,
		Sentiment:        shared.SentimentNegative,
		Severity:         82,
		Excerpt:          "vocês são todos uns ladrões",
		CommentedAt:      time.Date(2026, 9, 2, 14, 3, 0, 0, time.UTC),
	}
}

// The whole point of the value object: whoever sends it, the recipient reads
// the same thing. So the message must carry the four facts the operator needs
// to act without opening the dashboard: who, what they said, where, how bad.
func TestEscalationMessageCarriesWhoWhatWhereAndSeverity(t *testing.T) {
	e := NewEscalation(escalatedComment(), "https://instagram.com/p/abc", "olha isso")
	msg := e.Message()

	for _, want := range []string{"@fulano", "vocês são todos uns ladrões", "https://instagram.com/p/abc", "82", "olha isso"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message is missing %q:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "\r") {
		t.Fatal("carriage returns break the line layout on some clients")
	}
	if strings.HasSuffix(msg, "\n") {
		t.Fatal("a trailing newline renders as an empty bubble line")
	}
}

// An author with no handle still has to be identifiable, or the recipient
// cannot tell who they are being warned about.
func TestEscalationFallsBackToTheExternalID(t *testing.T) {
	c := escalatedComment()
	c.AuthorHandle = ""
	msg := NewEscalation(c, "", "").Message()
	if !strings.Contains(msg, "ig-99") {
		t.Fatalf("message must identify the author:\n%s", msg)
	}
	// No permalink: the post is still named, by the only id we have.
	if !strings.Contains(msg, "media-1") {
		t.Fatalf("message must name the post:\n%s", msg)
	}
}

// A note is the operator's own words. Absent, it must not leave a dangling
// heading or blank block in the message.
func TestEscalationWithoutANoteHasNoEmptyBlock(t *testing.T) {
	msg := NewEscalation(escalatedComment(), "https://instagram.com/p/abc", "   ").Message()
	if strings.Contains(msg, "\n\n\n") {
		t.Fatalf("blank block left behind:\n%q", msg)
	}
}

// A comment that was never classified has no stance and no severity. It can
// still be forwarded (that is often exactly why someone forwards it), and the
// message must not claim a severity of zero as if it were measured.
func TestEscalationOfAnUnanalysedCommentStatesNoSeverity(t *testing.T) {
	c := escalatedComment()
	c.Status = StatusPending
	c.Stance = ""
	c.Severity = 0
	msg := NewEscalation(c, "", "").Message()
	if strings.Contains(msg, "Gravidade") {
		t.Fatalf("an unmeasured severity must not be reported at all:\n%s", msg)
	}
	if !strings.Contains(msg, "vocês são todos uns ladrões") {
		t.Fatalf("the comment itself must still be forwarded:\n%s", msg)
	}
}

func TestEscalationValidate(t *testing.T) {
	cases := map[string]struct {
		mutate  func(*CommentAnalysis)
		wantErr bool
	}{
		"complete":     {mutate: func(*CommentAnalysis) {}},
		"no excerpt":   {mutate: func(c *CommentAnalysis) { c.Excerpt = "  " }, wantErr: true},
		"no author":    {mutate: func(c *CommentAnalysis) { c.AuthorHandle, c.AuthorExternalID = "", "" }, wantErr: true},
		"no workspace": {mutate: func(c *CommentAnalysis) { c.WorkspaceID = "" }, wantErr: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c := escalatedComment()
			tc.mutate(c)
			err := NewEscalation(c, "", "").Validate()
			if tc.wantErr != (err != nil) {
				t.Fatalf("err = %v, wantErr = %v", err, tc.wantErr)
			}
			if tc.wantErr && !errors.Is(err, ErrInvalidFilter) {
				t.Fatalf("err = %v, want ErrInvalidFilter", err)
			}
		})
	}
}

// The note is the one part a person types, so it is the one part that can be
// abused to make the message say something else entirely. It is trimmed and
// bounded, and it always sits BELOW our own text, never above it.
func TestEscalationNoteIsBoundedAndComesLast(t *testing.T) {
	long := strings.Repeat("x", MaxEscalationNote+500)
	msg := NewEscalation(escalatedComment(), "", long).Message()
	if len(msg) > MaxEscalationNote+2000 {
		t.Fatalf("message length %d is unbounded", len(msg))
	}
	noteAt := strings.Index(msg, strings.Repeat("x", 20))
	commentAt := strings.Index(msg, "vocês são todos uns ladrões")
	if noteAt < commentAt {
		t.Fatal("the operator's note must not precede the forwarded comment")
	}
}
