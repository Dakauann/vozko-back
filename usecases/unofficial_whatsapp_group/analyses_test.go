package unofficial_whatsapp_group_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/shared"
	uwg "vozko/domain/unofficial_whatsapp_group"
)

func analysisInput(groupID string) AnalysisInput {
	return AnalysisInput{GroupID: groupID, Name: "Monitor Vila Mariana", Prompt: "Reclamações sobre a loja",
		Trigger: uwg.TriggerMessageCount, MessagesPerCycle: 50,
		TrackedFields: []uwg.TrackedField{{Key: "mencoes_marca", Type: uwg.FieldNumber}}}
}

func TestAnAnalysisNeedsAFollowedGroupInScope(t *testing.T) {
	w := newWorld()
	unfollowed := w.groupWithSeats(t, "120363000000000001@g.us", "Moradores", "n1")
	foreign := w.followed(t, "120363000000000002@g.us", participants("5511981124410"), "n2")

	if _, err := w.svc.CreateAnalysis(context.Background(), everyone(), analysisInput(unfollowed.ID)); !errors.Is(err, uwg.ErrGroupNotFollowed) {
		t.Fatalf("unfollowed = %v, want ErrGroupNotFollowed", err)
	}
	if _, err := w.svc.CreateAnalysis(context.Background(), salesOnly(), analysisInput(foreign.ID)); !errors.Is(err, uwg.ErrGroupNotFound) {
		t.Fatalf("out of scope = %v, want ErrGroupNotFound", err)
	}
}

func TestANewAnalysisStartsFromNow(t *testing.T) {
	w := newWorld()
	g := w.followed(t, "120363000000000001@g.us", participants("5511981124410"), "n1")

	a, err := w.svc.CreateAnalysis(context.Background(), everyone(), analysisInput(g.ID))
	if err != nil {
		t.Fatal(err)
	}
	if a.AnalyzedUntil == nil || !a.AnalyzedUntil.Equal(testNow) || a.CreatedByID != "u1" || a.Status != uwg.AnalysisActive {
		t.Fatalf("analysis = %+v, want old messages left out and the author recorded", a)
	}
}

func TestAScheduledAnalysisKnowsItsNextRunInTheWorkspaceZone(t *testing.T) {
	w := newWorld()
	g := w.followed(t, "120363000000000001@g.us", participants("5511981124410"), "n1")
	in := analysisInput(g.ID)
	in.Trigger, in.ScheduleHour = uwg.TriggerScheduled, 18

	a, err := w.svc.CreateAnalysis(context.Background(), everyone(), in)
	if err != nil {
		t.Fatal(err)
	}
	loc, _ := time.LoadLocation("America/Sao_Paulo")
	if a.NextRunAt == nil || !a.NextRunAt.Equal(time.Date(2026, 10, 17, 18, 0, 0, 0, loc)) {
		t.Fatalf("next run = %v, want today at 18:00 in São Paulo", a.NextRunAt)
	}
}

func TestUpdatingAnAnalysisValidatesAndReschedules(t *testing.T) {
	w := newWorld()
	g := w.followed(t, "120363000000000001@g.us", participants("5511981124410"), "n1")
	a, _ := w.svc.CreateAnalysis(context.Background(), everyone(), analysisInput(g.ID))

	bad := []uwg.TrackedField{{Key: "Bad Key", Type: uwg.FieldText}}
	if _, err := w.svc.UpdateAnalysis(context.Background(), everyone(), a.ID, AnalysisPatch{TrackedFields: &bad}); !errors.Is(err, uwg.ErrTrackedFieldKey) {
		t.Fatalf("bad field = %v", err)
	}
	scheduled, hour := uwg.TriggerScheduled, 7
	updated, err := w.svc.UpdateAnalysis(context.Background(), everyone(), a.ID, AnalysisPatch{Trigger: &scheduled, ScheduleHour: &hour})
	if err != nil || updated.NextRunAt == nil || updated.NextRunAt.UTC().Hour() != 10 {
		t.Fatalf("updated = %+v, %v; want tomorrow 07:00 local", updated, err)
	}
	paused := uwg.AnalysisPaused
	if p, err := w.svc.UpdateAnalysis(context.Background(), everyone(), a.ID, AnalysisPatch{Status: &paused}); err != nil || p.Status != uwg.AnalysisPaused {
		t.Fatalf("pause = %+v, %v", p, err)
	}
}

func TestAnAnalysisOfAGroupOutsideTheScopeIsNotFound(t *testing.T) {
	w := newWorld()
	g := w.followed(t, "120363000000000002@g.us", participants("5511981124410"), "n2")
	a, _ := w.svc.CreateAnalysis(context.Background(), everyone(), analysisInput(g.ID))

	if _, err := w.svc.GetAnalysis(context.Background(), salesOnly(), a.ID, 0); !errors.Is(err, uwg.ErrAnalysisNotFound) {
		t.Fatalf("GetAnalysis = %v, want not found", err)
	}
	if err := w.svc.DeleteAnalysis(context.Background(), salesOnly(), a.ID); !errors.Is(err, uwg.ErrAnalysisNotFound) {
		t.Fatalf("DeleteAnalysis = %v, want not found", err)
	}
}

func TestAnAnalysisDetailShowsWhichParticipantsAreInSeveralGroups(t *testing.T) {
	w := newWorld()
	g := w.followed(t, "120363000000000001@g.us", participants("5511981124410", "5511960213375"), "n1")
	w.followed(t, "120363000000000002@g.us", participants("5511981124410"), "n2")
	a, _ := w.svc.CreateAnalysis(context.Background(), everyone(), analysisInput(g.ID))
	_ = w.runs.Create(context.Background(), &uwg.Run{AnalysisID: a.ID, WorkspaceID: ws, GroupID: g.ID, Cycle: 1, Status: uwg.RunCompleted,
		MessagesAnalyzed: 10, CompletedAt: testNow, Result: &uwg.Result{Sentiment: uwg.SentimentNegative, AlertLevel: uwg.AlertHigh,
			Participants: []uwg.ParticipantReading{
				{Name: "Rita", JID: "5511981124410@s.whatsapp.net", Sentiment: uwg.SentimentNegative, Messages: 6},
				{Name: "Camila", JID: "5511960213375@s.whatsapp.net", Sentiment: uwg.SentimentNeutral, Messages: 4},
			}}})

	d, err := w.svc.GetAnalysis(context.Background(), everyone(), a.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if d.Overview.Completed != 1 || d.Overview.HighAlertPct != 100 {
		t.Fatalf("overview = %+v", d.Overview)
	}
	if len(d.ParticipantGroups["5511981124410@s.whatsapp.net"]) != 2 || d.ParticipantGroups["5511960213375@s.whatsapp.net"] != nil {
		t.Fatalf("participant groups = %+v, want only Rita who is in two groups", d.ParticipantGroups)
	}
}

func TestTheOverviewPeriodCanCoverEveryCycle(t *testing.T) {
	w := newWorld()
	g := w.followed(t, "120363000000000001@g.us", participants("5511981124410"), "n1")
	w.followed(t, "120363000000000002@g.us", participants("5511981124410"), "n2")
	a, _ := w.svc.CreateAnalysis(context.Background(), everyone(), analysisInput(g.ID))
	old := testNow.Add(-90 * 24 * time.Hour)
	_ = w.runs.Create(context.Background(), &uwg.Run{AnalysisID: a.ID, WorkspaceID: ws, GroupID: g.ID, Cycle: 1, Status: uwg.RunCompleted,
		MessagesAnalyzed: 3, CompletedAt: old, Result: &uwg.Result{Sentiment: uwg.SentimentPositive,
			Participants: []uwg.ParticipantReading{{Name: "Rita", JID: "5511981124410@s.whatsapp.net", Messages: 3}}}})

	recent, err := w.svc.GetAnalysis(context.Background(), everyone(), a.ID, 0)
	if err != nil || recent.Overview.Completed != 0 || recent.WindowDays != uwg.OverviewWindowDays {
		t.Fatalf("default window = %+v, %v, want the last %d days only", recent, err, uwg.OverviewWindowDays)
	}
	all, err := w.svc.GetAnalysis(context.Background(), everyone(), a.ID, -1)
	if err != nil || all.Overview.Completed != 1 || all.WindowDays != 0 || all.Timezone == "" {
		t.Fatalf("all time = %+v, %v, want every cycle and the workspace zone", all, err)
	}
	if len(all.ParticipantGroups["5511981124410@s.whatsapp.net"]) != 2 {
		t.Fatalf("participant groups = %+v", all.ParticipantGroups)
	}

	history, err := w.svc.AnalysisRuns(context.Background(), everyone(), a.ID, shared.Pagination{Page: 1, PageSize: 10})
	if err != nil || len(history.Runs.Items) != 1 || len(history.ParticipantGroups["5511981124410@s.whatsapp.net"]) != 2 {
		t.Fatalf("history = %+v, %v, want the run and its participants' other groups", history, err)
	}
}
