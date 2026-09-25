package database

import (
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"vozko/infra/database/schema"
)

func TestRepairClosesOnlyValuedDealsLeftOpenOnAWonStage(t *testing.T) {
	tx := repairTx(t)
	if err := tx.AutoMigrate(&schema.Opportunity{}, &schema.OpportunityEvent{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	ws, pipeline := uuid.New().String(), uuid.New().String()
	won := schema.Stage{ID: uuid.New().String(), WorkspaceID: ws, PipelineID: pipeline, Name: "ganho", IsWon: true}
	open := schema.Stage{ID: uuid.New().String(), WorkspaceID: ws, PipelineID: pipeline, Name: "proposta"}
	for _, s := range []*schema.Stage{&won, &open} {
		if err := tx.Create(s).Error; err != nil {
			t.Fatalf("stage: %v", err)
		}
	}
	updated := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	deal := func(stageID, status string, value int64) string {
		id := uuid.New().String()
		row := schema.Opportunity{ID: id, WorkspaceID: ws, PipelineID: pipeline, StageID: stageID,
			Title: "deal", ValueCents: value, Currency: "BRL", Status: status}
		if err := tx.Create(&row).Error; err != nil {
			t.Fatalf("deal: %v", err)
		}
		if err := tx.Exec("UPDATE opportunities SET updated_at = ? WHERE id = ?", updated, id).Error; err != nil {
			t.Fatalf("deal clock: %v", err)
		}
		return id
	}
	valued := deal(won.ID, "open", 7900)
	unvalued := deal(won.ID, "open", 0)
	alreadyWon := deal(won.ID, "won", 5000)
	stillOpen := deal(open.ID, "open", 9000)

	for range 2 {
		if err := closeValuedDealsOnWonStages(tx); err != nil {
			t.Fatalf("repair: %v", err)
		}
	}

	var got schema.Opportunity
	tx.First(&got, "id = ?", valued)
	if got.Status != "won" || got.CloseDate == nil || !got.CloseDate.Equal(updated) || got.ClosedByKind != "system" {
		t.Fatalf("valued deal = status %s closed %v by %q, want won at %v by system", got.Status, got.CloseDate, got.ClosedByKind, updated)
	}
	for id, want := range map[string]string{unvalued: "open", alreadyWon: "won", stillOpen: "open"} {
		var row schema.Opportunity
		tx.First(&row, "id = ?", id)
		if row.Status != want || row.ClosedByKind != "" {
			t.Fatalf("deal %s = %s closed by %q, want %s and untouched", id, row.Status, row.ClosedByKind, want)
		}
	}

	var events []schema.OpportunityEvent
	tx.Where("workspace_id = ?", ws).Find(&events)
	if len(events) != 1 || events[0].OpportunityID != valued || events[0].Type != "won" || events[0].ActorKind != "system" {
		t.Fatalf("repair events = %+v, want exactly one system win for the valued deal", events)
	}
}

func TestRepairIsRegistered(t *testing.T) {
	if !slices.Contains(repairNames(), "opp_close_valued_deals_on_won_stages") {
		t.Fatalf("the repair is not run at boot")
	}
}
