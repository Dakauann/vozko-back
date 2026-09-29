package livedecision_repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"vozko/domain/audience"
	"vozko/domain/decision"
	ld "vozko/domain/livedecision"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

var t0 = time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)

func read(ws, entry string, through time.Time, qualification audience.Qualification) ld.LiveRead {
	return ld.LiveRead{
		WorkspaceID: ws, EntryID: entry, EntryType: "whatsapp",
		Interest: audience.InterestInterested, Qualification: qualification, Sentiment: "positive",
		Disposition: audience.DispositionPending, NextAction: audience.NextActionContinue, Language: "pt",
		AttendanceQuality: 72, Certainty: map[string]float64{"qualification": 0.9},
		DecidedThrough: through, DecidedAt: through.Add(time.Second),
	}
}

func TestALateReadNeverOverwritesAFresherOne(t *testing.T) {
	db := repotest.IsolatedDB(t, "ld_reads", &schema.ConversationLiveRead{})
	store := NewReadStore(db)
	ctx := context.Background()
	ws, entry := uuid.NewString(), uuid.NewString()

	if saved, err := store.Save(ctx, read(ws, entry, t0.Add(time.Minute), audience.QualificationHotLead)); err != nil || !saved {
		t.Fatalf("first save: %v %v", saved, err)
	}
	if saved, err := store.Save(ctx, read(ws, entry, t0, audience.QualificationColdLead)); err != nil || saved {
		t.Fatalf("an older read must be refused: saved=%v err=%v", saved, err)
	}
	got, err := store.Get(ctx, ws, entry, "whatsapp")
	if err != nil || got == nil || got.Qualification != audience.QualificationHotLead {
		t.Fatalf("read = %+v, err = %v", got, err)
	}
	if saved, err := store.Save(ctx, read(ws, entry, t0.Add(2*time.Minute), audience.QualificationWarmLead)); err != nil || !saved {
		t.Fatalf("a newer read replaces it: %v %v", saved, err)
	}
	got, _ = store.Get(ctx, ws, entry, "whatsapp")
	if got.Qualification != audience.QualificationWarmLead || got.AttendanceQuality != 72 || got.Certainty["qualification"] != 0.9 || got.Language != "pt" {
		t.Fatalf("read = %+v", got)
	}
}

func TestReadsAreScopedToTheirWorkspace(t *testing.T) {
	db := repotest.IsolatedDB(t, "ld_reads", &schema.ConversationLiveRead{})
	store := NewReadStore(db)
	ctx := context.Background()
	ws, other, a, b := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	_, _ = store.Save(ctx, read(ws, a, t0, audience.QualificationHotLead))
	_, _ = store.Save(ctx, read(ws, b, t0, audience.QualificationColdLead))

	got, err := store.ForEntries(ctx, ws, []ld.EntryRef{{EntryID: a, EntryType: "whatsapp"}, {EntryID: b, EntryType: "whatsapp"}, {EntryID: b, EntryType: "instagram"}})
	if err != nil || len(got) != 2 || got[ld.EntryRef{EntryID: b, EntryType: "whatsapp"}].Qualification != audience.QualificationColdLead {
		t.Fatalf("reads = %+v, err = %v", got, err)
	}
	if leaked, _ := store.Get(ctx, other, a, "whatsapp"); leaked != nil {
		t.Fatal("another workspace must not see this read")
	}
	if none, err := store.ForEntries(ctx, ws, nil); err != nil || len(none) != 0 {
		t.Fatalf("no refs: %v %v", none, err)
	}
}

func TestTheLogSummarizesDecisionsPerWorkspace(t *testing.T) {
	db := repotest.IsolatedDB(t, "ld_log", &schema.LiveDecisionRecord{})
	log := NewLog(db)
	ctx := context.Background()
	ws := uuid.NewString()
	if err := db.Exec("CREATE TABLE workspaces (id uuid PRIMARY KEY, name text NOT NULL, deleted_at timestamptz)").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO workspaces (id, name) VALUES (?, 'Escola')", ws).Error; err != nil {
		t.Fatal(err)
	}
	records := []ld.Record{
		{WorkspaceID: ws, EntryID: "e1", EntryType: "whatsapp", Purpose: ld.PurposeLive, Effects: []string{ld.EffectStageMoved, ld.EffectStageUncertain},
			Answers: map[string]decision.Answer{"stage": {Kind: decision.KindChoice, Choice: "s1", Confidence: 0.9}}, CostMicros: 90, LatencyMillis: 200, CreatedAt: t0},
		{WorkspaceID: ws, EntryID: "e2", EntryType: "whatsapp", Purpose: ld.PurposeLive, Effects: []string{ld.EffectStageMoved}, CostMicros: 10, LatencyMillis: 400, CreatedAt: t0},
		{WorkspaceID: ws, Purpose: ld.PurposeLive, Failure: "unavailable", CreatedAt: t0},
		{WorkspaceID: ws, Purpose: ld.PurposeLive, CostMicros: 999, CreatedAt: t0.Add(-48 * time.Hour)},
	}
	for _, r := range records {
		if err := log.Append(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	summaries, err := log.Summarize(ctx, t0.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 {
		t.Fatalf("summaries = %+v", summaries)
	}
	s := summaries[0]
	if s.WorkspaceName != "Escola" || s.Decisions != 3 || s.Failures != 1 || s.CostMicros != 100 || s.AvgLatency != 300*time.Millisecond {
		t.Fatalf("summary = %+v", s)
	}
	if s.Effects[ld.EffectStageMoved] != 2 || s.Effects[ld.EffectStageUncertain] != 1 {
		t.Fatalf("effects = %v", s.Effects)
	}
}
