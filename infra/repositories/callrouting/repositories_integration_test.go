package callrouting_repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"vozko/domain/callrouting"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func TestQueuesRoundTripAndStayInsideTheirWorkspace(t *testing.T) {
	db := repotest.IsolatedDB(t, "call_queue", &schema.CallQueue{})
	repo := NewQueueRepository(db)
	ctx := context.Background()
	owner, stranger := uuid.NewString(), uuid.NewString()

	queue := &callrouting.Queue{
		WorkspaceID: owner, Name: "Suporte", Strategy: callrouting.StrategyRoundRobin,
		MemberUserIDs: []string{"u1", "u2"}, RingSeconds: 20, MaxWaitSeconds: 600, WrapUpSeconds: 15,
		HoldMusic: callrouting.HoldMusicRef{PresetID: "bossa_nova"},
	}
	if err := repo.Create(ctx, queue); err != nil || queue.ID == "" {
		t.Fatalf("Create: %v (id %q)", err, queue.ID)
	}
	found, err := repo.FindInWorkspace(ctx, owner, queue.ID)
	if err != nil {
		t.Fatalf("FindInWorkspace: %v", err)
	}
	if found.Strategy != callrouting.StrategyRoundRobin || len(found.MemberUserIDs) != 2 || found.HoldMusic.PresetID != "bossa_nova" || found.WrapUpSeconds != 15 {
		t.Fatalf("found = %+v", found)
	}
	if _, err := repo.FindInWorkspace(ctx, stranger, queue.ID); !errors.Is(err, callrouting.ErrQueueNotFound) {
		t.Fatalf("stranger read err = %v", err)
	}

	queue.WorkspaceID = stranger
	if err := repo.Update(ctx, queue); !errors.Is(err, callrouting.ErrQueueNotFound) {
		t.Fatalf("stranger update err = %v", err)
	}
	if err := repo.Delete(ctx, stranger, queue.ID); !errors.Is(err, callrouting.ErrQueueNotFound) {
		t.Fatalf("stranger delete err = %v", err)
	}

	queue.WorkspaceID, queue.Name, queue.MemberUserIDs, queue.DepartmentID = owner, "Suporte N2", nil, "dept-1"
	if err := repo.Update(ctx, queue); err != nil {
		t.Fatalf("Update: %v", err)
	}
	listed, _ := repo.ListByWorkspace(ctx, owner)
	if len(listed) != 1 || listed[0].Name != "Suporte N2" || listed[0].DepartmentID != "dept-1" || len(listed[0].MemberUserIDs) != 0 {
		t.Fatalf("listed = %+v", listed)
	}
	if err := repo.Delete(ctx, owner, queue.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if rest, _ := repo.ListByWorkspace(ctx, owner); len(rest) != 0 {
		t.Fatal("a deleted queue is still listed")
	}
}

func TestTransfersAreLoggedWithTheirOutcome(t *testing.T) {
	db := repotest.IsolatedDB(t, "call_transfer", &schema.CallTransfer{})
	log := NewTransferLog(db)
	ctx := context.Background()
	workspace := uuid.NewString()
	record := callrouting.TransferRecord{
		ID: uuid.NewString(), WorkspaceID: workspace, CallID: "sip-in-1", FromUserID: "ana",
		Target: callrouting.TransferTarget{Kind: callrouting.TargetMember, UserID: "bia"},
		Notes:  "quer cancelar", Outcome: callrouting.OutcomePending, CreatedAt: time.Now(),
	}
	if err := log.Record(ctx, record); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if err := log.Finish(ctx, workspace, record.ID, callrouting.OutcomeConnected, "bia", time.Now()); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	var stored schema.CallTransfer
	if err := db.First(&stored, "id = ?", record.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Outcome != "connected" || stored.AnsweredBy != "bia" || stored.FinishedAt == nil || stored.Notes != "quer cancelar" {
		t.Fatalf("stored = %+v", stored)
	}
}

func TestRoutingSettingsDefaultToThePresetAndUpsert(t *testing.T) {
	db := repotest.IsolatedDB(t, "call_routing_settings", &schema.CallRoutingSettings{})
	repo := NewSettingsRepository(db)
	ctx := context.Background()
	workspace := uuid.NewString()

	settings, err := repo.Get(ctx, workspace)
	if err != nil || settings.HoldMusic.PresetID != callrouting.DefaultHoldPreset {
		t.Fatalf("default = %+v, %v", settings, err)
	}
	for _, ref := range []callrouting.HoldMusicRef{{PresetID: "lofi"}, {MediaID: "m1"}} {
		if err := repo.Save(ctx, callrouting.Settings{WorkspaceID: workspace, HoldMusic: ref}); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}
	settings, _ = repo.Get(ctx, workspace)
	if settings.HoldMusic.MediaID != "m1" || settings.HoldMusic.PresetID != "" {
		t.Fatalf("saved = %+v", settings)
	}
}

func TestQueueOutcomesAreReadForTheWindowWithTheirWait(t *testing.T) {
	db := repotest.IsolatedDB(t, "call_transfer_history", &schema.CallTransfer{})
	log := NewTransferLog(db)
	ctx := context.Background()
	workspace, other := uuid.NewString(), uuid.NewString()
	day := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)

	record := func(workspaceID string, target callrouting.TransferTarget, at time.Time, outcome callrouting.TransferOutcome, wait time.Duration) {
		t.Helper()
		id := uuid.NewString()
		if err := log.Record(ctx, callrouting.TransferRecord{ID: id, WorkspaceID: workspaceID, CallID: "c", Target: target, Outcome: callrouting.OutcomePending, CreatedAt: at}); err != nil {
			t.Fatal(err)
		}
		if outcome != callrouting.OutcomePending {
			if err := log.Finish(ctx, workspaceID, id, outcome, "", at.Add(wait)); err != nil {
				t.Fatal(err)
			}
		}
	}
	queue := callrouting.TransferTarget{Kind: callrouting.TargetQueue, QueueID: "q1"}
	record(workspace, queue, day, callrouting.OutcomeConnected, 15*time.Second)
	record(workspace, queue, day.Add(time.Hour), callrouting.OutcomeAbandoned, 40*time.Second)
	record(workspace, queue, day.Add(2*time.Hour), callrouting.OutcomePending, 0)
	record(workspace, callrouting.TransferTarget{Kind: callrouting.TargetMember, UserID: "bia"}, day, callrouting.OutcomeConnected, time.Second)
	record(workspace, queue, day.Add(-48*time.Hour), callrouting.OutcomeConnected, time.Second)
	record(other, queue, day, callrouting.OutcomeConnected, time.Second)

	got, err := log.QueueOutcomes(ctx, workspace, day.Add(-time.Hour), day.Add(12*time.Hour))
	if err != nil {
		t.Fatalf("QueueOutcomes: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("outcomes = %+v, want the three queue entries of the day", got)
	}
	byOutcome := map[callrouting.TransferOutcome]callrouting.QueueOutcome{}
	for _, o := range got {
		byOutcome[o.Outcome] = o
	}
	if o := byOutcome[callrouting.OutcomeConnected]; o.QueueID != "q1" || o.Wait != 15*time.Second {
		t.Fatalf("answered = %+v", o)
	}
	if o := byOutcome[callrouting.OutcomeAbandoned]; o.Wait != 40*time.Second {
		t.Fatalf("abandoned = %+v", o)
	}
	if _, ok := byOutcome[callrouting.OutcomePending]; !ok {
		t.Fatal("a caller still waiting was left out of the offered calls")
	}
}
