package unofficial_whatsapp_group

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func validAnalysis() Analysis {
	return Analysis{
		WorkspaceID: "w1", GroupID: "g1", Name: "Monitor Vila Mariana",
		Prompt: "Reclamações sobre a loja", Trigger: TriggerMessageCount, MessagesPerCycle: 50,
	}
}

func TestAnalysisNormalizeFillsDefaults(t *testing.T) {
	a := Analysis{WorkspaceID: "w1", GroupID: "g1", Name: "  Monitor  ", Prompt: " foco "}
	a.Normalize()
	if a.Name != "Monitor" || a.Prompt != "foco" {
		t.Fatalf("trim failed: %+v", a)
	}
	if a.Status != AnalysisActive || a.Trigger != TriggerMessageCount || a.MessagesPerCycle != DefaultMessagesPerCycle {
		t.Fatalf("defaults = %+v", a)
	}
}

func TestAnalysisValidation(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Analysis)
		want   error
	}{
		{"valid", func(*Analysis) {}, nil},
		{"no name", func(a *Analysis) { a.Name = "" }, ErrAnalysisNameRequired},
		{"no prompt", func(a *Analysis) { a.Prompt = "" }, ErrAnalysisPromptRequired},
		{"no group", func(a *Analysis) { a.GroupID = "" }, ErrAnalysisGroupRequired},
		{"cycle below one", func(a *Analysis) { a.MessagesPerCycle = 0; a.Trigger = TriggerMessageCount }, ErrMessagesPerCycleRange},
		{"cycle too large", func(a *Analysis) { a.MessagesPerCycle = MaxMessagesPerCycle + 1 }, ErrMessagesPerCycleRange},
		{"schedule hour", func(a *Analysis) { a.Trigger = TriggerScheduled; a.ScheduleHour = 24 }, ErrScheduleInvalid},
		{"schedule minute", func(a *Analysis) { a.Trigger = TriggerScheduled; a.ScheduleMinute = 60 }, ErrScheduleInvalid},
		{"unknown trigger", func(a *Analysis) { a.Trigger = "weekly" }, ErrTriggerInvalid},
		{"unknown status", func(a *Analysis) { a.Status = "deleted" }, ErrAnalysisStatusInvalid},
		{"prompt too long", func(a *Analysis) { a.Prompt = strings.Repeat("x", MaxPromptRunes+1) }, ErrPromptTooLong},
		{"bad tracked field", func(a *Analysis) { a.TrackedFields = []TrackedField{{Key: "Bad Key", Type: FieldNumber}} }, ErrTrackedFieldKey},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := validAnalysis()
			a.Status = AnalysisActive
			c.mutate(&a)
			err := a.Validate()
			if c.want == nil && err != nil {
				t.Fatalf("Validate = %v, want nil", err)
			}
			if c.want != nil && !errors.Is(err, c.want) {
				t.Fatalf("Validate = %v, want %v", err, c.want)
			}
		})
	}
}

func TestTrackedFieldsValidation(t *testing.T) {
	ok := []TrackedField{{Key: "mencoes_marca", Type: FieldNumber}, {Key: "pediu_entrega", Type: FieldBoolean}}
	if err := ValidateTrackedFields(ok); err != nil {
		t.Fatalf("valid fields refused: %v", err)
	}
	dup := []TrackedField{{Key: "a1", Type: FieldText}, {Key: "a1", Type: FieldNumber}}
	if err := ValidateTrackedFields(dup); !errors.Is(err, ErrTrackedFieldDuplicate) {
		t.Fatalf("duplicate keys = %v, want ErrTrackedFieldDuplicate", err)
	}
	badType := []TrackedField{{Key: "a1", Type: "date"}}
	if err := ValidateTrackedFields(badType); !errors.Is(err, ErrTrackedFieldType) {
		t.Fatalf("bad type = %v, want ErrTrackedFieldType", err)
	}
	many := make([]TrackedField, MaxTrackedFields+1)
	for i := range many {
		many[i] = TrackedField{Key: "f" + strings.Repeat("x", i+1), Type: FieldText}
	}
	if err := ValidateTrackedFields(many); !errors.Is(err, ErrTrackedFieldsTooMany) {
		t.Fatalf("too many = %v, want ErrTrackedFieldsTooMany", err)
	}
}

func TestFailuresBackOffAndPauseTheAnalysisAfterTheLimit(t *testing.T) {
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	a := validAnalysis()
	a.Status = AnalysisActive

	want := []time.Duration{15 * time.Minute, 30 * time.Minute, time.Hour, 2 * time.Hour}
	for i, wait := range want {
		a.RecordFailure(now)
		if a.Status != AnalysisActive || a.NextRunAt == nil || !a.NextRunAt.Equal(now.Add(wait)) {
			t.Fatalf("failure %d: status %s next %v, want active retry after %v", i+1, a.Status, a.NextRunAt, wait)
		}
	}
	a.RecordFailure(now)
	if a.Status != AnalysisPaused || a.PausedReason != PausedAfterFailures || a.NextRunAt != nil || a.Failures != MaxConsecutiveFailures {
		t.Fatalf("after %d failures = %+v, want paused for failures", MaxConsecutiveFailures, a)
	}

	a.SetStatus(AnalysisActive)
	if a.Failures != 0 || a.PausedReason != "" {
		t.Fatalf("resuming kept failures %d reason %q", a.Failures, a.PausedReason)
	}
}

func TestASuccessfulRunClearsTheFailureCount(t *testing.T) {
	a := validAnalysis()
	a.RecordFailure(time.Now())
	a.RecordSuccess()
	if a.Failures != 0 {
		t.Fatalf("failures = %d after a success", a.Failures)
	}
}

func TestTheCursorOrdersByTimeThenID(t *testing.T) {
	at := time.Date(2026, 10, 17, 12, 0, 7, 0, time.UTC)
	a := validAnalysis()
	a.AnalyzedUntil, a.AnalyzedUntilID = &at, "m-50"
	if c := a.Cursor(); !c.At.Equal(at) || c.ID != "m-50" {
		t.Fatalf("cursor = %+v", c)
	}
	a.AnalyzedUntil, a.AnalyzedUntilID = nil, ""
	if c := a.Cursor(); !c.At.Equal(a.CreatedAt) || c.ID != "" {
		t.Fatalf("cursor without a watermark = %+v, want the creation time", c)
	}
}

func TestRunSentimentScoreMirrorsVerus(t *testing.T) {
	cases := map[RunSentiment]int{SentimentPositive: 100, SentimentMixed: 60, SentimentNeutral: 50, SentimentNegative: 10, "": 50}
	for s, want := range cases {
		if got := s.Score(); got != want {
			t.Fatalf("%q.Score() = %d, want %d", s, got, want)
		}
	}
}

func TestResultNormalizeKeepsOnlyDeclaredFieldsWithTheirTypes(t *testing.T) {
	fields := []TrackedField{
		{Key: "mencoes", Type: FieldNumber},
		{Key: "pediu_entrega", Type: FieldBoolean},
		{Key: "nota", Type: FieldPercentage},
		{Key: "tema", Type: FieldText},
	}
	r := Result{
		Sentiment:  "furious",
		AlertLevel: "critical",
		KeyThemes:  []string{" fila ", "", "fila"},
		CustomFields: map[string]any{
			"mencoes": "46", "pediu_entrega": true, "nota": 140.0, "tema": 3, "extra": "x",
		},
		Participants: []ParticipantReading{{Name: "Rita", Messages: 0}, {Name: "Jorge", Messages: 3, Sentiment: "weird"}},
		Mentions:     []Mention{{Entity: " Bom Preço ", Frequency: -2, Sentiment: "loving"}, {Entity: ""}},
	}
	r.Normalize(fields)

	if r.Sentiment != SentimentNeutral || r.AlertLevel != AlertLow {
		t.Fatalf("enums not clamped: %s %s", r.Sentiment, r.AlertLevel)
	}
	if len(r.KeyThemes) != 1 || r.KeyThemes[0] != "fila" {
		t.Fatalf("themes = %v, want one trimmed fila", r.KeyThemes)
	}
	if _, ok := r.CustomFields["extra"]; ok {
		t.Fatalf("undeclared field kept")
	}
	if v, ok := r.CustomFields["mencoes"].(float64); !ok || v != 46 {
		t.Fatalf("number from text = %#v", r.CustomFields["mencoes"])
	}
	if v, ok := r.CustomFields["nota"].(float64); !ok || v != 100 {
		t.Fatalf("percentage not clamped to 100: %#v", r.CustomFields["nota"])
	}
	if v, ok := r.CustomFields["tema"].(string); !ok || v != "3" {
		t.Fatalf("text from number = %#v", r.CustomFields["tema"])
	}
	if len(r.Participants) != 1 || r.Participants[0].Name != "Jorge" || r.Participants[0].Sentiment != SentimentNeutral {
		t.Fatalf("participants = %+v, want only Jorge with a valid sentiment", r.Participants)
	}
	if len(r.Mentions) != 1 || r.Mentions[0].Entity != "Bom Preço" || r.Mentions[0].Frequency != 0 || r.Mentions[0].Sentiment != "neutral" {
		t.Fatalf("mentions = %+v", r.Mentions)
	}
}

func TestResultNormalizeKeepsNullForUnknownValues(t *testing.T) {
	fields := []TrackedField{{Key: "nota", Type: FieldPercentage}}
	r := Result{CustomFields: map[string]any{"nota": nil}}
	r.Normalize(fields)
	v, ok := r.CustomFields["nota"]
	if !ok || v != nil {
		t.Fatalf("unknown value = %#v, %v; want an explicit null", v, ok)
	}
}

func TestResponseSchemaDeclaresEveryTrackedFieldAsNullable(t *testing.T) {
	schema := ResponseSchema([]TrackedField{{Key: "mencoes", Type: FieldNumber}, {Key: "ok", Type: FieldBoolean}})
	props := schema["properties"].(map[string]any)
	custom := props["custom_fields"].(map[string]any)
	cprops := custom["properties"].(map[string]any)
	required := custom["required"].([]string)
	if len(required) != 2 {
		t.Fatalf("required = %v", required)
	}
	types := cprops["mencoes"].(map[string]any)["type"].([]string)
	if types[0] != "number" || types[1] != "null" {
		t.Fatalf("number field type = %v", types)
	}
	if custom["additionalProperties"] != false {
		t.Fatalf("custom fields allow extra keys")
	}
}

func run(cycle int, sentiment RunSentiment, alert AlertLevel, messages int, at time.Time, mut func(*Result)) Run {
	r := &Result{Sentiment: sentiment, AlertLevel: alert}
	if mut != nil {
		mut(r)
	}
	return Run{Cycle: cycle, Status: RunCompleted, MessagesAnalyzed: messages, Result: r, CompletedAt: at}
}

func TestBuildOverviewAggregatesLikeTheVerusRunsPanel(t *testing.T) {
	base := time.Date(2026, 10, 10, 18, 0, 0, 0, time.UTC)
	runs := []Run{
		run(1, SentimentPositive, AlertLow, 50, base, func(r *Result) {
			r.KeyThemes = []string{"Fila do caixa", "Hortifruti"}
			r.Mentions = []Mention{{Entity: "Bom Preço", Sentiment: "negative", Frequency: 3}}
			r.Participants = []ParticipantReading{{Name: "Rita", JID: "r@s", Sentiment: SentimentNegative, Messages: 4}}
			r.SuggestedActions = []SuggestedAction{{Situation: "Fila", Strategy: "Avisar caixa extra"}}
		}),
		run(2, SentimentNegative, AlertHigh, 30, base.Add(24*time.Hour), func(r *Result) {
			r.KeyThemes = []string{"fila do caixa"}
			r.Mentions = []Mention{{Entity: "Bom Preço", Sentiment: "negative", Frequency: 2}}
			r.Participants = []ParticipantReading{{Name: "Rita", JID: "r@s", Sentiment: SentimentNegative, Messages: 2}}
			r.SuggestedActions = []SuggestedAction{{Situation: "fila", Strategy: "Responder no grupo"}}
		}),
		{Cycle: 3, Status: RunFailed, CompletedAt: base.Add(48 * time.Hour)},
	}

	o := BuildOverview(runs, 8)

	if o.Cycles != 3 || o.Completed != 2 || o.Messages != 80 {
		t.Fatalf("counts = %+v", o)
	}
	if o.AvgPositivity != 55 {
		t.Fatalf("avg positivity = %d, want (100+10)/2", o.AvgPositivity)
	}
	if o.HighAlertPct != 50 || o.AlertCounts[AlertHigh] != 1 || o.AlertCounts[AlertLow] != 1 {
		t.Fatalf("alerts = %+v pct %d", o.AlertCounts, o.HighAlertPct)
	}
	if len(o.Trend) != 2 || o.Trend[0].Positivity != 100 || o.Trend[1].Positivity != 10 {
		t.Fatalf("trend = %+v", o.Trend)
	}
	if o.ParticipantCount != 1 {
		t.Fatalf("participant count = %d, want every distinct person who spoke", o.ParticipantCount)
	}
	if capped := BuildOverview(runs, 0); capped.ParticipantCount != 1 {
		t.Fatalf("uncapped participant count = %d", capped.ParticipantCount)
	}
	second := o.Trend[1]
	if second.Messages != 30 || second.Alert != AlertHigh || second.Sentiment != SentimentNegative || second.Participants != 1 {
		t.Fatalf("second cycle = %+v, want its messages, alert, sentiment and who spoke", second)
	}
	if len(o.Themes) == 0 || o.Themes[0].Label != "Fila do caixa" || o.Themes[0].Count != 2 {
		t.Fatalf("themes = %+v, want the two spellings merged", o.Themes)
	}
	if len(o.Mentions) != 1 || o.Mentions[0].Frequency != 5 || o.Mentions[0].Dominant != "negative" {
		t.Fatalf("mentions = %+v", o.Mentions)
	}
	if len(o.Participants) != 1 || o.Participants[0].Messages != 6 || o.Participants[0].Sentiments[SentimentNegative] != 6 {
		t.Fatalf("participants = %+v", o.Participants)
	}
	if len(o.Suggestions) != 1 || o.Suggestions[0].Count != 2 {
		t.Fatalf("suggestions = %+v", o.Suggestions)
	}
}

func TestBuildOverviewWithoutCompletedRunsIsNeutral(t *testing.T) {
	o := BuildOverview(nil, 8)
	if o.Completed != 0 || o.AvgPositivity != 50 || o.Dominant != SentimentNeutral || o.HighAlertPct != 0 {
		t.Fatalf("empty overview = %+v", o)
	}
}

func TestMessageSentimentWeighsParticipantReadingsByTheirMessages(t *testing.T) {
	runs := []Run{
		run(1, SentimentMixed, AlertLow, 10, time.Now(), func(r *Result) {
			r.Participants = []ParticipantReading{
				{Name: "a", Sentiment: SentimentPositive, Messages: 3},
				{Name: "b", Sentiment: SentimentNegative, Messages: 5},
				{Name: "c", Sentiment: SentimentMixed, Messages: 2},
			}
		}),
		run(2, SentimentNeutral, AlertLow, 4, time.Now(), nil),
	}
	got := MessageSentiment(runs)
	if got.Positive != 3 || got.Negative != 5 || got.Neutral != 2+4 {
		t.Fatalf("tally = %+v, want positives 3, negatives 5, neutral 2 mixed + 4 from the run without readings", got)
	}
}

func TestOurTeamIsNeverReadAsAParticipant(t *testing.T) {
	r := &Result{Participants: []ParticipantReading{
		{Name: "Rita", JID: "5511981124410@s.whatsapp.net", Sentiment: SentimentNegative, Messages: 2},
		{Name: OurTeamAuthor, Sentiment: SentimentNeutral, Messages: 1},
		{Name: "dba9547c-89dd-43db-8911-166fdc8cec8c", Sentiment: SentimentNeutral, Messages: 1},
	}}
	r.DropOurTeam()
	if len(r.Participants) != 1 || r.Participants[0].Name != "Rita" {
		t.Fatalf("participants = %+v, want only the group member", r.Participants)
	}
}

func TestOnlyMessagesPastTheArrivalGraceAreSettled(t *testing.T) {
	now := time.Date(2026, 10, 18, 3, 0, 0, 0, time.UTC)
	lines := []TranscriptLine{
		{ID: "a", At: now.Add(-2 * ArrivalGrace)},
		{ID: "b", At: now.Add(-ArrivalGrace)},
		{ID: "c", At: now.Add(-ArrivalGrace / 2)},
	}
	settled, waiting := SettledLines(lines, now)
	if len(settled) != 2 || settled[1].ID != "b" || !waiting {
		t.Fatalf("settled = %+v waiting = %v, want the first two and the third still waiting", settled, waiting)
	}
	if all, more := SettledLines(lines[:2], now); len(all) != 2 || more {
		t.Fatalf("settled = %+v waiting = %v, want everything settled", all, more)
	}
}
