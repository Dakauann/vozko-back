package unofficial_whatsapp_group_usecase

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"vozko/domain/ai"
	uwg "vozko/domain/unofficial_whatsapp_group"
)

const validModelOutput = `{"summary":"A fila dominou a conversa.","sentiment":"negative","key_themes":["Fila do caixa"],
"relevant_mentions":[{"entity":"Bom Preço","sentiment":"negative","frequency":3,"context":"fila"}],
"participant_sentiments":[{"name":"Rita","jid":"5511981124410@s.whatsapp.net","sentiment":"negative","messages":2,"summary":"reclamou","key_quote":"fila enorme"},
{"name":"Jorge","jid":"5511992307781@s.whatsapp.net","sentiment":"positive","messages":1,"summary":"elogiou","key_quote":"ótimo"}],
"insights":["Reclamações subiram"],"suggested_actions":[{"situation":"Fila","strategy":"Abrir caixa extra"}],
"alert_level":"high","custom_fields":{"mencoes_marca":3}}`

func (w *world) analysedGroup(t *testing.T, trigger uwg.Trigger, perCycle int) (*uwg.Group, *uwg.Analysis) {
	t.Helper()
	g := w.followed(t, "120363000000000001@g.us", participants("5511981124410", "5511992307781"), "n1")
	in := analysisInput(g.ID)
	in.Trigger, in.MessagesPerCycle = trigger, perCycle
	if trigger == uwg.TriggerScheduled {
		in.ScheduleHour = 8
	}
	w.at(analysisCreatedAt)
	a, err := w.svc.CreateAnalysis(context.Background(), everyone(), in)
	w.at(testNow)
	if err != nil {
		t.Fatal(err)
	}
	w.messages.conversations["n1|"+g.JID] = "conv-1"
	return g, a
}

func (w *world) say(lines ...string) {
	for i, text := range lines {
		author := "+5511981124410"
		if i%2 == 1 {
			author = "+5511992307781"
		}
		w.messages.lines["conv-1"] = append(w.messages.lines["conv-1"], uwg.TranscriptLine{
			ID: text, At: spokenAt.Add(time.Duration(len(w.messages.lines["conv-1"])+1) * time.Minute), Author: author, Text: text,
		})
	}
}

var (
	analysisCreatedAt = testNow.Add(-2 * time.Hour)
	spokenAt          = testNow.Add(-time.Hour)
)

func (w *world) pend(a *uwg.Analysis, n int) {
	_ = w.analyses.AddPending(context.Background(), a.GroupID, n)
}

func TestADueAnalysisRunsOnceBillsTheWorkspaceAndAdvances(t *testing.T) {
	w := newWorld()
	_, a := w.analysedGroup(t, uwg.TriggerMessageCount, 3)
	w.ai.content = validModelOutput
	w.say("Fila enorme", "Ótimo hortifruti", "25 minutos")
	w.pend(a, 3)

	if err := w.svc.RunDueAnalyses(context.Background()); err != nil {
		t.Fatal(err)
	}

	if w.ai.calls() != 1 {
		t.Fatalf("AI calls = %d, want 1", w.ai.calls())
	}
	in := w.ai.inputs[0]
	if in.WorkspaceID != ws || !strings.HasPrefix(in.BillingReference, "groupanalysis:"+a.ID+":") {
		t.Fatalf("billing = %q / %q, want the workspace billed under the analysis reference", in.WorkspaceID, in.BillingReference)
	}
	if in.ResponseFormat == nil || in.ResponseFormat.Type != ai.ResponseFormatJSONSchema || !in.ResponseFormat.JSONSchemaStrict {
		t.Fatalf("response format = %+v, want a strict JSON schema", in.ResponseFormat)
	}
	prompt := in.Messages[0].Content
	if !strings.Contains(prompt, "P10: Fila enorme") || !strings.Contains(prompt, "mencoes_marca") || !strings.Contains(prompt, "Reclamações sobre a loja") {
		t.Fatalf("prompt does not carry the transcript with names, the tracked field and the focus:\n%s", prompt)
	}

	runs := w.runs.all()
	if len(runs) != 1 || runs[0].Status != uwg.RunCompleted || runs[0].Cycle != 1 || runs[0].MessagesAnalyzed != 3 {
		t.Fatalf("runs = %+v", runs)
	}
	res := runs[0].Result
	if res.AlertLevel != uwg.AlertHigh || len(res.Participants) != 2 || res.CustomFields["mencoes_marca"] != float64(3) {
		t.Fatalf("result = %+v", res)
	}
	stored := w.analyses.get(a.ID)
	if stored.PendingMessages != 0 || stored.AnalyzedUntil == nil || !stored.AnalyzedUntil.Equal(spokenAt.Add(3*time.Minute)) || stored.NextRunAt != nil {
		t.Fatalf("analysis after the run = %+v", stored)
	}
}

func TestWithoutBalanceNothingIsSentToTheModel(t *testing.T) {
	w := newWorld()
	w.svc = New(Deps{Groups: w.groups, Members: w.members, Stats: fakeStats{}, Analyses: w.analyses, Runs: w.runs, Joins: w.joins,
		Messages: w.messages, Campaigns: fakeCampaigns{}, Instances: w.instances, Servers: fakeServers{}, Seats: w.seats,
		GroupAPI: w.api, SeatSync: w.seatSync, Leads: w.leads, Locations: fakeLocations{}, AI: w.ai, Model: "m",
		Balance: &fakeBalance{micros: 0}, Clock: func() time.Time { return testNow }})
	_, a := w.analysedGroup(t, uwg.TriggerMessageCount, 2)
	w.say("um", "dois")
	w.pend(a, 2)

	if err := w.svc.RunDueAnalyses(context.Background()); err != nil {
		t.Fatal(err)
	}
	if w.ai.calls() != 0 || len(w.runs.all()) != 0 {
		t.Fatalf("calls = %d runs = %d, want nothing spent", w.ai.calls(), len(w.runs.all()))
	}
	stored := w.analyses.get(a.ID)
	if stored.PendingMessages != 2 || stored.NextRunAt == nil || !stored.NextRunAt.After(testNow) {
		t.Fatalf("analysis = %+v, want the messages kept and a later retry", stored)
	}
}

func TestAProviderFailureIsRecordedAndRetriedLater(t *testing.T) {
	w := newWorld()
	_, a := w.analysedGroup(t, uwg.TriggerMessageCount, 2)
	w.ai.err = errors.New("provider unavailable")
	w.say("um", "dois")
	w.pend(a, 2)

	_ = w.svc.RunDueAnalyses(context.Background())

	runs := w.runs.all()
	if len(runs) != 1 || runs[0].Status != uwg.RunFailed || runs[0].FailureReason == "" {
		t.Fatalf("runs = %+v, want the failure recorded", runs)
	}
	stored := w.analyses.get(a.ID)
	if stored.PendingMessages != 2 || !stored.AnalyzedUntil.Equal(analysisCreatedAt) || stored.NextRunAt == nil {
		t.Fatalf("analysis = %+v, want nothing consumed and a backoff", stored)
	}
	if again, _ := w.analyses.ClaimNext(context.Background(), testNow, "x", testNow); again != nil {
		t.Fatalf("a failing analysis is due again immediately")
	}
}

func TestAnUnreadableAnswerFailsTheRun(t *testing.T) {
	w := newWorld()
	_, a := w.analysedGroup(t, uwg.TriggerMessageCount, 1)
	w.ai.content = "not json"
	w.say("um")
	w.pend(a, 1)

	_ = w.svc.RunDueAnalyses(context.Background())

	if runs := w.runs.all(); len(runs) != 1 || runs[0].Status != uwg.RunFailed {
		t.Fatalf("runs = %+v", runs)
	}
}

func TestADailyAnalysisDrainsInBatchesAndWaitsForTomorrow(t *testing.T) {
	w := newWorld()
	_, a := w.analysedGroup(t, uwg.TriggerScheduled, 2)
	w.ai.content = validModelOutput
	w.say("um", "dois", "três", "quatro", "cinco")
	due := testNow.Add(-time.Minute)
	w.analyses.get(a.ID).NextRunAt = &due

	if err := w.svc.RunDueAnalyses(context.Background()); err != nil {
		t.Fatal(err)
	}
	runs := w.runs.all()
	if len(runs) != 3 || runs[2].Cycle != 3 || runs[0].Kind != uwg.RunDaily {
		t.Fatalf("runs = %+v, want three daily batches", runs)
	}
	stored := w.analyses.get(a.ID)
	loc, _ := time.LoadLocation("America/Sao_Paulo")
	if stored.NextRunAt == nil || !stored.NextRunAt.Equal(time.Date(2026, 10, 18, 8, 0, 0, 0, loc)) {
		t.Fatalf("next run = %v, want tomorrow at 08:00", stored.NextRunAt)
	}
}

func TestAnAnalysisOfAGroupNoLongerFollowedDoesNotRun(t *testing.T) {
	w := newWorld()
	g, a := w.analysedGroup(t, uwg.TriggerMessageCount, 1)
	w.say("um")
	w.pend(a, 1)
	if _, err := w.svc.Unfollow(context.Background(), everyone(), g.ID); err != nil {
		t.Fatal(err)
	}

	_ = w.svc.RunDueAnalyses(context.Background())

	if w.ai.calls() != 0 {
		t.Fatalf("an unfollowed group was sent to the model")
	}
}

func (w *world) sayAt(at time.Time, ids ...string) {
	for _, id := range ids {
		w.messages.lines["conv-1"] = append(w.messages.lines["conv-1"], uwg.TranscriptLine{ID: id, At: at, Author: "+5511981124410", Text: "mensagem " + id})
	}
}

func (w *world) analyzed() int {
	total := 0
	for _, r := range w.runs.all() {
		if r.Status == uwg.RunCompleted {
			total += r.MessagesAnalyzed
		}
	}
	return total
}

func TestMessagesSharingASecondAreAllAnalyzedAcrossCycles(t *testing.T) {
	w := newWorld()
	_, a := w.analysedGroup(t, uwg.TriggerMessageCount, 2)
	w.ai.content = validModelOutput
	second := spokenAt.Add(time.Minute)
	w.sayAt(second, "m1", "m2", "m3")
	w.pend(a, 3)
	if err := w.svc.RunDueAnalyses(context.Background()); err != nil {
		t.Fatal(err)
	}
	w.sayAt(second, "m4")
	w.pend(a, 1)
	if err := w.svc.RunDueAnalyses(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := w.analyzed(); got != 4 {
		t.Fatalf("analyzed %d messages, want all 4 that share one second", got)
	}
}

func TestEveryBatchIsCheckedAgainstTheBalance(t *testing.T) {
	w := newWorld()
	_, a := w.analysedGroup(t, uwg.TriggerScheduled, 2)
	w.ai.content = validModelOutput
	w.say("um", "dois", "três", "quatro", "cinco")
	due := testNow.Add(-time.Minute)
	w.analyses.get(a.ID).NextRunAt = &due
	w.balance.limited, w.balance.paidChecks = true, 1

	if err := w.svc.RunDueAnalyses(context.Background()); err != nil {
		t.Fatal(err)
	}
	if w.ai.calls() != 1 {
		t.Fatalf("AI calls = %d, want only the batch the balance covered", w.ai.calls())
	}
	stored := w.analyses.get(a.ID)
	if stored.AnalyzedUntil == nil || !stored.AnalyzedUntil.Equal(spokenAt.Add(2*time.Minute)) || stored.Failures != 0 {
		t.Fatalf("analysis = %+v, want the paid batch kept and no failure counted", stored)
	}
	if stored.NextRunAt == nil || !stored.NextRunAt.After(testNow) {
		t.Fatalf("next run = %v, want a later retry", stored.NextRunAt)
	}
}

func TestRepeatedFailuresPauseTheAnalysis(t *testing.T) {
	w := newWorld()
	_, a := w.analysedGroup(t, uwg.TriggerMessageCount, 1)
	w.ai.err = errors.New("provider unavailable")
	w.say("um")
	w.pend(a, 1)

	for i := 1; i <= uwg.MaxConsecutiveFailures+1; i++ {
		w.at(testNow.Add(time.Duration(i) * 24 * time.Hour))
		_ = w.svc.RunDueAnalyses(context.Background())
	}
	stored := w.analyses.get(a.ID)
	if stored.Status != uwg.AnalysisPaused || stored.PausedReason != uwg.PausedAfterFailures {
		t.Fatalf("analysis = %+v, want it paused after repeated failures", stored)
	}
	if w.ai.calls() != uwg.MaxConsecutiveFailures {
		t.Fatalf("AI calls = %d, want the model asked %d times and then left alone", w.ai.calls(), uwg.MaxConsecutiveFailures)
	}
}

func TestAPaidResultIsStoredWithoutAskingTheModelAgain(t *testing.T) {
	w := newWorld()
	_, a := w.analysedGroup(t, uwg.TriggerMessageCount, 1)
	w.ai.content = validModelOutput
	w.runs.failCreates = 1
	w.say("um")
	w.pend(a, 1)

	_ = w.svc.RunDueAnalyses(context.Background())

	if w.ai.calls() != 1 {
		t.Fatalf("AI calls = %d, want one", w.ai.calls())
	}
	if runs := w.runs.all(); len(runs) != 1 || runs[0].Status != uwg.RunCompleted {
		t.Fatalf("runs = %+v, want the paid result stored", runs)
	}
}

func TestTheLeaseIsRenewedBeforeEachBatch(t *testing.T) {
	w := newWorld()
	_, a := w.analysedGroup(t, uwg.TriggerScheduled, 2)
	w.ai.content = validModelOutput
	w.say("um", "dois", "três", "quatro")
	due := testNow.Add(-time.Minute)
	w.analyses.get(a.ID).NextRunAt = &due

	_ = w.svc.RunDueAnalyses(context.Background())

	if w.analyses.extended < 2 {
		t.Fatalf("lease renewed %d times, want once per batch", w.analyses.extended)
	}
}

func TestOurOwnMessagesAreContextAndNeverAParticipant(t *testing.T) {
	w := newWorld()
	_, a := w.analysedGroup(t, uwg.TriggerMessageCount, 2)
	w.ai.content = `{"summary":"s","sentiment":"neutral","key_themes":[],"relevant_mentions":[],
"participant_sentiments":[{"name":"Rita","jid":"5511981124410@s.whatsapp.net","sentiment":"negative","messages":1,"summary":"x","key_quote":""},
{"name":"` + uwg.OurTeamAuthor + `","jid":"","sentiment":"neutral","messages":1,"summary":"y","key_quote":""}],
"insights":[],"suggested_actions":[],"alert_level":"low","custom_fields":{}}`
	w.say("Fila enorme")
	w.messages.lines["conv-1"] = append(w.messages.lines["conv-1"], uwg.TranscriptLine{
		ID: "ours", At: spokenAt.Add(5 * time.Minute), Author: "dba9547c-89dd-43db-8911-166fdc8cec8c", Text: "Vamos verificar", Outbound: true,
	})
	w.pend(a, 2)

	if err := w.svc.RunDueAnalyses(context.Background()); err != nil {
		t.Fatal(err)
	}

	prompt := w.ai.inputs[0].Messages[0].Content
	if strings.Contains(prompt, "dba9547c") || !strings.Contains(prompt, uwg.OurTeamAuthor+": Vamos verificar") {
		t.Fatalf("our message should be labelled as our team, never by the sender id:\n%s", prompt)
	}
	res := w.runs.all()[0].Result
	if len(res.Participants) != 1 || res.Participants[0].Name != "Rita" {
		t.Fatalf("participants = %+v, want our team left out", res.Participants)
	}
}

func TestAMessageArrivingLateInTheSameSecondIsNotSkipped(t *testing.T) {
	w := newWorld()
	_, a := w.analysedGroup(t, uwg.TriggerMessageCount, 2)
	w.ai.content = validModelOutput
	second := testNow.Add(-time.Hour)
	w.sayAt(second.Add(-30*time.Second), "p")
	w.sayAt(second, "z")
	w.pend(a, 2)
	w.at(second.Add(5 * time.Second))
	if err := w.svc.RunDueAnalyses(context.Background()); err != nil {
		t.Fatal(err)
	}
	if w.ai.calls() != 0 {
		t.Fatalf("AI calls = %d, want the cycle to wait while messages of that second may still arrive", w.ai.calls())
	}
	waiting := w.analyses.get(a.ID)
	if waiting.PendingMessages != 2 || waiting.NextRunAt == nil || !waiting.NextRunAt.Equal(second.Add(5*time.Second+uwg.ArrivalGrace)) {
		t.Fatalf("analysis = %+v, want the pending messages kept and a retry after the grace", waiting)
	}

	w.sayAt(second, "m")
	w.pend(a, 1)
	w.at(second.Add(2 * time.Minute))
	if err := w.svc.RunDueAnalyses(context.Background()); err != nil {
		t.Fatal(err)
	}
	w.sayAt(second.Add(time.Minute), "q")
	w.pend(a, 1)
	w.at(second.Add(5 * time.Minute))
	if err := w.svc.RunDueAnalyses(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := w.analyzed(); got != 4 {
		t.Fatalf("analyzed %d messages, want all 4 including the one that arrived late", got)
	}
}
