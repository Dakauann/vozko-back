package audience

import (
	"strings"
	"testing"
	"time"

	"vozko/domain/shared"
)

func TestLatestConversationAnalysesIsTheDashboardQuery(t *testing.T) {
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	in := LatestConversationAnalyses("ws-1", &from, nil)
	// The engine keeps a timeline per conversation; totals must count each conversation once, at its newest revision.
	if !in.LatestOnly {
		t.Fatal("LatestOnly must be set or a conversation is counted once per revision")
	}
	if len(in.Statuses) != 1 || in.Statuses[0] != StatusAnalyzed {
		t.Fatalf("Statuses = %v, want only analyzed", in.Statuses)
	}
	if len(in.SubjectKinds) != 1 || in.SubjectKinds[0] != SubjectKindConversation {
		t.Fatalf("SubjectKinds = %v, want only conversations", in.SubjectKinds)
	}
	if in.WorkspaceID != "ws-1" || in.From != &from {
		t.Fatalf("input = %+v", in)
	}
}

func TestDigestConversationsKeepsOutcomesAndDropsCommentCounters(t *testing.T) {
	stats := &Stats{Counters: Counters{
		ConversationAnalyzed:  40,
		InterestInterested:    25,
		InterestNotInterested: 10,
		DispositionSale:       7,
		QualificationHotLead:  9,
		NextActionEscalate:    3,
		SentimentNegative:     6,
		AttendanceQualityAvg:  71.456,
		AttendanceQualityMin:  12,
		AttendanceQualityMax:  98,
		StanceHostile:         99,
	}}
	for i := 0; i < 15; i++ {
		stats.Subjects = append(stats.Subjects, SubjectCount{Key: strings.Repeat("x", i+1), Label: "s", Count: 15 - i})
	}

	got := DigestConversations(stats)
	if got.Analyzed != 40 || got.Interest["interested"] != 25 || got.Disposition["sale"] != 7 {
		t.Fatalf("digest = %+v", got)
	}
	if got.Qualification["hot_lead"] != 9 || got.NextAction["escalate"] != 3 || got.Sentiment["negative"] != 6 {
		t.Fatalf("digest = %+v", got)
	}
	if got.Quality == nil || got.Quality.Avg != 71.46 || got.Quality.Min != 12 || got.Quality.Max != 98 {
		t.Fatalf("quality = %+v", got.Quality)
	}
	if len(got.Subjects) != MaxDigestSubjects {
		t.Fatalf("subjects = %d, want the cap %d", len(got.Subjects), MaxDigestSubjects)
	}
}

func TestDigestConversationsWithNothingAnalysedHasNoQuality(t *testing.T) {
	got := DigestConversations(&Stats{})
	// A quality of 0 would read as "terrible"; with no analysed conversation it is simply unknown.
	if got.Quality != nil {
		t.Fatalf("quality = %+v, want nil", got.Quality)
	}
	if got.Subjects == nil {
		t.Fatal("subjects must be an empty list, not null")
	}
}

func TestDigestExampleTrimsTheSummary(t *testing.T) {
	at := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)
	a := &Analysis{
		SubjectID:         "conv-1",
		Summary:           strings.Repeat("á", MaxExampleSummaryRunes+50),
		Interest:          InterestInterested,
		Disposition:       DispositionCallback,
		Qualification:     QualificationWarmLead,
		NextAction:        NextActionScheduleCallback,
		ProductInterest:   "plano anual",
		Sentiment:         shared.SentimentPositive,
		AttendanceQuality: 80,
		MessageCount:      14,
		AnalyzedAt:        &at,
		Transcript:        "User: secret",
	}
	got := DigestExample(a)
	if n := len([]rune(got.Summary)); n != MaxExampleSummaryRunes+1 {
		t.Fatalf("summary runes = %d, want %d plus the ellipsis", n, MaxExampleSummaryRunes)
	}
	if got.ConversationID != "conv-1" || got.Subject != "plano anual" || got.Quality != 80 || got.AnalyzedAt != "2026-09-20" {
		t.Fatalf("example = %+v", got)
	}
}
