package unofficial_whatsapp_group

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/domain/shared"
)

type RunKind string

const (
	RunCycle RunKind = "cycle"
	RunDaily RunKind = "daily"
)

type RunStatus string

const (
	RunCompleted RunStatus = "completed"
	RunFailed    RunStatus = "failed"
)

type RunSentiment string

const (
	SentimentPositive RunSentiment = "positive"
	SentimentNegative RunSentiment = "negative"
	SentimentNeutral  RunSentiment = "neutral"
	SentimentMixed    RunSentiment = "mixed"
)

func (s RunSentiment) Valid() bool {
	switch s {
	case SentimentPositive, SentimentNegative, SentimentNeutral, SentimentMixed:
		return true
	}
	return false
}

func (s RunSentiment) Score() int {
	switch s {
	case SentimentPositive:
		return 100
	case SentimentMixed:
		return 60
	case SentimentNegative:
		return 10
	}
	return 50
}

func (s RunSentiment) Tally() shared.Sentiment {
	switch s {
	case SentimentPositive:
		return shared.SentimentPositive
	case SentimentNegative:
		return shared.SentimentNegative
	}
	return shared.SentimentNeutral
}

type AlertLevel string

const (
	AlertLow    AlertLevel = "low"
	AlertMedium AlertLevel = "medium"
	AlertHigh   AlertLevel = "high"
)

func (a AlertLevel) Valid() bool {
	return a == AlertLow || a == AlertMedium || a == AlertHigh
}

type Mention struct {
	Entity    string           `json:"entity"`
	Sentiment shared.Sentiment `json:"sentiment"`
	Frequency int              `json:"frequency"`
	Context   string           `json:"context"`
}

type ParticipantReading struct {
	Name      string       `json:"name"`
	JID       string       `json:"jid"`
	Sentiment RunSentiment `json:"sentiment"`
	Messages  int          `json:"messages"`
	Summary   string       `json:"summary"`
	KeyQuote  string       `json:"keyQuote"`
}

func (p ParticipantReading) Key() string {
	if p.JID != "" {
		return BareJID(p.JID)
	}
	if p.Name != "" {
		return "name:" + strings.ToLower(p.Name)
	}
	return ""
}

const OurTeamAuthor = "Nossa equipe"

func (p ParticipantReading) FromOurTeam() bool {
	if strings.TrimSpace(p.JID) != "" {
		return false
	}
	name := strings.TrimSpace(p.Name)
	return strings.EqualFold(name, OurTeamAuthor) || isSenderID(name)
}

func isSenderID(name string) bool {
	return uuid.Validate(name) == nil
}

func (r *Result) DropOurTeam() {
	readings := make([]ParticipantReading, 0, len(r.Participants))
	for _, p := range r.Participants {
		if !p.FromOurTeam() {
			readings = append(readings, p)
		}
	}
	r.Participants = readings
}

type SuggestedAction struct {
	Situation string `json:"situation"`
	Strategy  string `json:"strategy"`
}

type Result struct {
	Summary          string               `json:"summary"`
	Sentiment        RunSentiment         `json:"sentiment"`
	KeyThemes        []string             `json:"keyThemes"`
	Mentions         []Mention            `json:"relevantMentions"`
	Participants     []ParticipantReading `json:"participantSentiments"`
	Insights         []string             `json:"insights"`
	SuggestedActions []SuggestedAction    `json:"suggestedActions"`
	AlertLevel       AlertLevel           `json:"alertLevel"`
	CustomFields     map[string]any       `json:"customFields"`
}

func (r *Result) Normalize(fields []TrackedField) {
	r.Summary = strings.TrimSpace(r.Summary)
	if !r.Sentiment.Valid() {
		r.Sentiment = SentimentNeutral
	}
	if !r.AlertLevel.Valid() {
		r.AlertLevel = AlertLow
	}
	r.KeyThemes = distinctTrimmed(r.KeyThemes)
	r.Insights = distinctTrimmed(r.Insights)

	mentions := make([]Mention, 0, len(r.Mentions))
	for _, m := range r.Mentions {
		m.Entity = strings.TrimSpace(m.Entity)
		if m.Entity == "" {
			continue
		}
		if !m.Sentiment.Valid() {
			m.Sentiment = shared.SentimentNeutral
		}
		if m.Frequency < 0 {
			m.Frequency = 0
		}
		m.Context = strings.TrimSpace(m.Context)
		mentions = append(mentions, m)
	}
	r.Mentions = mentions

	readings := make([]ParticipantReading, 0, len(r.Participants))
	for _, p := range r.Participants {
		p.Name, p.JID = strings.TrimSpace(p.Name), strings.TrimSpace(p.JID)
		if p.Messages < 1 || p.Key() == "" {
			continue
		}
		if !p.Sentiment.Valid() {
			p.Sentiment = SentimentNeutral
		}
		p.Summary, p.KeyQuote = strings.TrimSpace(p.Summary), strings.TrimSpace(p.KeyQuote)
		readings = append(readings, p)
	}
	r.Participants = readings
	r.DropOurTeam()

	actions := make([]SuggestedAction, 0, len(r.SuggestedActions))
	for _, a := range r.SuggestedActions {
		a.Situation, a.Strategy = strings.TrimSpace(a.Situation), strings.TrimSpace(a.Strategy)
		if a.Situation == "" && a.Strategy == "" {
			continue
		}
		actions = append(actions, a)
	}
	r.SuggestedActions = actions

	r.CustomFields = coerceFields(r.CustomFields, fields)
}

func distinctTrimmed(items []string) []string {
	out := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		key := strings.ToLower(trimmed)
		if trimmed == "" {
			continue
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func coerceFields(values map[string]any, fields []TrackedField) map[string]any {
	out := make(map[string]any, len(fields))
	for _, f := range fields {
		raw, present := values[f.Key]
		if !present || raw == nil {
			out[f.Key] = nil
			continue
		}
		out[f.Key] = coerceValue(raw, f.Type)
	}
	return out
}

func coerceValue(raw any, t FieldType) any {
	switch t {
	case FieldNumber, FieldPercentage:
		n, ok := numberOf(raw)
		if !ok {
			return nil
		}
		if t == FieldPercentage {
			if n < 0 {
				n = 0
			}
			if n > 100 {
				n = 100
			}
		}
		return n
	case FieldBoolean:
		switch v := raw.(type) {
		case bool:
			return v
		case string:
			if b, err := strconv.ParseBool(strings.TrimSpace(v)); err == nil {
				return b
			}
		}
		return nil
	}
	if s, ok := raw.(string); ok {
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(fmt.Sprint(raw))
}

func numberOf(raw any) (float64, bool) {
	switch v := raw.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case string:
		n, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(v), ",", "."), 64)
		return n, err == nil
	}
	return 0, false
}

type Run struct {
	ID               string     `json:"id"`
	AnalysisID       string     `json:"analysisId"`
	WorkspaceID      string     `json:"workspaceId"`
	GroupID          string     `json:"groupId"`
	Cycle            int        `json:"cycle"`
	Kind             RunKind    `json:"kind"`
	Status           RunStatus  `json:"status"`
	MessagesAnalyzed int        `json:"messagesAnalyzed"`
	PeriodFrom       *time.Time `json:"periodFrom,omitempty"`
	PeriodTo         *time.Time `json:"periodTo,omitempty"`
	Result           *Result    `json:"result,omitempty"`
	Model            string     `json:"model,omitempty"`
	PromptTokens     int        `json:"promptTokens"`
	CompletionTokens int        `json:"completionTokens"`
	FailureReason    string     `json:"failureReason,omitempty"`
	StartedAt        time.Time  `json:"startedAt"`
	CompletedAt      time.Time  `json:"completedAt"`
}

func (r Run) Completed() bool {
	return r.Status == RunCompleted && r.Result != nil
}

func (r Run) Duration() time.Duration {
	if r.CompletedAt.Before(r.StartedAt) {
		return 0
	}
	return r.CompletedAt.Sub(r.StartedAt)
}
