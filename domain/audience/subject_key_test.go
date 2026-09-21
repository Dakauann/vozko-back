package audience

import (
	"testing"
	"time"
)

func subjectNow() time.Time { return time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC) }

func TestSubjectKeyCollapsesTheSameSubject(t *testing.T) {
	same := []string{
		"Plano Família",
		"plano familia",
		"  PLANO   FAMÍLIA  ",
		"Plano família!",
		"plano, familia",
	}
	want := SubjectKey(same[0])
	if want == "" {
		t.Fatal("a real subject produced an empty key")
	}
	for _, variant := range same[1:] {
		if got := SubjectKey(variant); got != want {
			t.Errorf("SubjectKey(%q) = %q, want %q", variant, got, want)
		}
	}
}

func TestSubjectKeyKeepsDifferentSubjectsApart(t *testing.T) {
	pairs := [][2]string{
		{"plano familia", "plano empresarial"},
		{"agendamento", "cancelamento"},
		{"clareamento dental", "implante dental"},
	}
	for _, pair := range pairs {
		if SubjectKey(pair[0]) == SubjectKey(pair[1]) {
			t.Errorf("%q and %q collapsed to the same key %q", pair[0], pair[1], SubjectKey(pair[0]))
		}
	}
}

func TestSubjectKeyKeepsAtMostThreeWords(t *testing.T) {
	long := "clareamento dental a laser com desconto para dois"
	got := SubjectKey(long)
	words := 1
	for _, r := range got {
		if r == ' ' {
			words++
		}
	}
	if words > MaxSubjectWords {
		t.Errorf("SubjectKey(%q) = %q, which is %d words (max %d)", long, got, words, MaxSubjectWords)
	}
	if got != "clareamento dental a" {
		t.Errorf("SubjectKey kept %q, want the first three words", got)
	}
}

func TestSubjectKeyRefusesAnEmptySubject(t *testing.T) {
	for _, blank := range []string{"", "   ", "-", "...", "n/a", "N/A", "nenhum", "none"} {
		if got := SubjectKey(blank); got != "" {
			t.Errorf("SubjectKey(%q) = %q, want it uncounted", blank, got)
		}
	}
}

func TestApplyDerivesTheSubjectKey(t *testing.T) {
	row := conversationRowForSubjectTest(t)
	if err := row.Apply(Classification{
		Sentiment: "positive", Interest: InterestInterested,
		Disposition: DispositionSale, Qualification: QualificationHotLead,
		NextAction: NextActionClose, ProductInterest: "  Plano Família  ",
		Summary: "Fechou.", Language: "pt",
		Quality: ConversationQuality{
			GoalProgress: "high", CustomerEngagement: "high",
			AgentConduct: "high", Professionalism: "high",
		},
	}, ActionPolicy{}, Provenance{BatchID: "b-1", Model: "m"}, subjectNow()); err != nil {
		t.Fatal(err)
	}
	if row.ProductInterest != "Plano Família" {
		t.Errorf("display text = %q, want the model's words, trimmed", row.ProductInterest)
	}
	if row.ProductInterestKey != SubjectKey("Plano Família") {
		t.Errorf("key = %q, want it derived from the text", row.ProductInterestKey)
	}
}

func TestApplyLeavesAnAbsentSubjectEmpty(t *testing.T) {
	row := conversationRowForSubjectTest(t)
	if err := row.Apply(Classification{
		Sentiment: "neutral", Interest: InterestUndecided,
		Disposition: DispositionPending, Qualification: QualificationColdLead,
		NextAction: NextActionContinue, ProductInterest: "  ",
		Summary: "Sem assunto claro.", Language: "pt",
		Quality: ConversationQuality{
			GoalProgress: "low", CustomerEngagement: "low",
			AgentConduct: "medium", Professionalism: "medium",
		},
	}, ActionPolicy{}, Provenance{BatchID: "b-1", Model: "m"}, subjectNow()); err != nil {
		t.Fatal(err)
	}
	if row.ProductInterest != "" || row.ProductInterestKey != "" {
		t.Errorf("stored %q/%q for a conversation with no subject", row.ProductInterest, row.ProductInterestKey)
	}
}

func conversationRowForSubjectTest(t *testing.T) *Analysis {
	t.Helper()
	row, err := NewPending(NewInput{
		WorkspaceID: "ws-1",
		Container: ContainerRef{
			Kind: SubjectKindConversation, Source: SourceWhatsApp,
			AccountID: "acc-1", ContainerID: "camp-1",
		},
		SubjectID: "entry-1", AuthorExternalID: "contact-1",
		Text: "cliente: quanto custa?", Now: subjectNow(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := row.Claim(subjectNow()); err != nil {
		t.Fatal(err)
	}
	return row
}
