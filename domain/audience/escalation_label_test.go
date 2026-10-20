package audience

import (
	"strings"
	"testing"

	"vozko/domain/shared"
)

func TestEscalationNamesTheChannelOfEveryCommentSource(t *testing.T) {
	want := map[shared.EntryType]string{
		shared.EntryTypeInstagram: "Comentário sinalizado no Instagram",
		shared.EntryTypeFacebook:  "Comentário sinalizado no Facebook",
		shared.EntryTypeTikTok:    "Comentário sinalizado no TikTok",
	}
	for _, e := range shared.CommentAnalysableEntryTypes() {
		t.Run(string(e), func(t *testing.T) {
			c := escalatedComment()
			c.Source = SourceOf(e)
			msg := NewEscalation(c, "", "").Message()
			expected, ok := want[e]
			if !ok {
				t.Fatalf("%s analyses comments but has no expected escalation label", e)
			}
			if !strings.HasPrefix(msg, expected+"\n") {
				t.Fatalf("message starts with %q, want %q", strings.SplitN(msg, "\n", 2)[0], expected)
			}
		})
	}
}

func TestEscalationLabelsEveryOtherSourceByItsDisplayLabel(t *testing.T) {
	for _, e := range shared.ConversationAnalysableEntryTypes() {
		if e.SupportsCommentAnalysis() {
			continue
		}
		if got := sourceLabel(SourceOf(e)); got != e.DisplayLabel() {
			t.Errorf("sourceLabel(%s) = %q, want %q", e, got, e.DisplayLabel())
		}
	}
}
