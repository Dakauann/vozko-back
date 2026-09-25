package audience

import (
	"math"
	"strings"
	"time"

	"vozko/domain/shared"
)

const (
	MaxDigestSubjects       = 10
	MaxDigestExamples       = 5
	MaxExampleSummaryRunes  = 280
	digestDayLayout         = "2006-01-02"
	truncatedSummaryEllipse = "…"
)

func LatestConversationAnalyses(workspaceID string, from, to *time.Time) ListInput {
	return ListInput{
		WorkspaceID:  workspaceID,
		SubjectKinds: []SubjectKind{SubjectKindConversation},
		Statuses:     []Status{StatusAnalyzed},
		LatestOnly:   true,
		From:         from,
		To:           to,
	}
}

type QualityDigest struct {
	Avg float64 `json:"avg"`
	Min int     `json:"min"`
	Max int     `json:"max"`
}

type ConversationDigest struct {
	Analyzed       int            `json:"analyzed_conversations"`
	Quality        *QualityDigest `json:"attendance_quality,omitempty"`
	Interest       map[string]int `json:"interest"`
	Disposition    map[string]int `json:"disposition"`
	Qualification  map[string]int `json:"qualification"`
	NextAction     map[string]int `json:"next_action"`
	Sentiment      map[string]int `json:"sentiment"`
	Subjects       []SubjectCount `json:"top_subjects"`
	LastAnalyzedAt *time.Time     `json:"last_analyzed_at,omitempty"`
}

func DigestConversations(s *Stats) ConversationDigest {
	if s == nil {
		s = &Stats{}
	}
	c := s.Counters
	out := ConversationDigest{
		Analyzed: c.ConversationAnalyzed,
		Interest: map[string]int{
			string(InterestInterested):    c.InterestInterested,
			string(InterestNotInterested): c.InterestNotInterested,
			string(InterestUndecided):     c.InterestUndecided,
		},
		Disposition: map[string]int{
			string(DispositionSale):        c.DispositionSale,
			string(DispositionFillingInfo): c.DispositionFillingInfo,
			string(DispositionCallback):    c.DispositionCallback,
			string(DispositionDeclined):    c.DispositionDeclined,
			string(DispositionPending):     c.DispositionPending,
		},
		Qualification: map[string]int{
			string(QualificationHotLead):  c.QualificationHotLead,
			string(QualificationWarmLead): c.QualificationWarmLead,
			string(QualificationColdLead): c.QualificationColdLead,
		},
		NextAction: map[string]int{
			string(NextActionScheduleCallback): c.NextActionScheduleCallback,
			string(NextActionSendWhatsApp):     c.NextActionSendWhatsApp,
			string(NextActionClose):            c.NextActionClose,
			string(NextActionEscalate):         c.NextActionEscalate,
			string(NextActionContinue):         c.NextActionContinue,
		},
		Sentiment: map[string]int{
			string(shared.SentimentPositive): c.SentimentPositive,
			string(shared.SentimentNeutral):  c.SentimentNeutral,
			string(shared.SentimentNegative): c.SentimentNegative,
		},
		Subjects:       []SubjectCount{},
		LastAnalyzedAt: c.LastAnalyzedAt,
	}
	if c.ConversationAnalyzed > 0 {
		out.Quality = &QualityDigest{
			Avg: math.Round(c.AttendanceQualityAvg*100) / 100,
			Min: c.AttendanceQualityMin,
			Max: c.AttendanceQualityMax,
		}
	}
	subjects := s.Subjects
	if len(subjects) > MaxDigestSubjects {
		subjects = subjects[:MaxDigestSubjects]
	}
	out.Subjects = append(out.Subjects, subjects...)
	return out
}

type ConversationExample struct {
	ConversationID string `json:"conversation_id"`
	AnalyzedAt     string `json:"analyzed_at,omitempty"`
	Summary        string `json:"summary"`
	Subject        string `json:"subject,omitempty"`
	Interest       string `json:"interest,omitempty"`
	Disposition    string `json:"disposition,omitempty"`
	Qualification  string `json:"qualification,omitempty"`
	NextAction     string `json:"next_action,omitempty"`
	Sentiment      string `json:"sentiment,omitempty"`
	Quality        int    `json:"attendance_quality"`
	MessageCount   int    `json:"message_count"`
}

func DigestExample(a *Analysis) ConversationExample {
	out := ConversationExample{
		ConversationID: a.SubjectID,
		Summary:        trimRunes(strings.TrimSpace(a.Summary), MaxExampleSummaryRunes),
		Subject:        a.ProductInterest,
		Interest:       string(a.Interest),
		Disposition:    string(a.Disposition),
		Qualification:  string(a.Qualification),
		NextAction:     string(a.NextAction),
		Sentiment:      string(a.Sentiment),
		Quality:        a.AttendanceQuality,
		MessageCount:   a.MessageCount,
	}
	if a.AnalyzedAt != nil {
		out.AnalyzedAt = a.AnalyzedAt.UTC().Format(digestDayLayout)
	}
	return out
}

func trimRunes(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit]) + truncatedSummaryEllipse
}
