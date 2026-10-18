package lead

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"vozko/domain/customfield"
	"vozko/domain/lead"
	"vozko/domain/leadaction"
	"vozko/domain/recordevent"
)

func TestUndoingOptOutsRestoresTheGrantAndRecordsTheRevertAgainstPostgres(t *testing.T) {
	db := leadStoreDB(t)
	repo := &repository{db: db, agg: newAggregateCache(nil)}
	ctx := context.Background()
	ws, operator := uuid.NewString(), uuid.NewString()
	granted := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	left := time.Date(2026, 10, 10, 10, 0, 0, 0, time.UTC)
	chosen := time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)

	store := func(number string, changes ...lead.ConsentChange) *lead.Lead {
		l := &lead.Lead{WorkspaceID: ws, Number: number, Source: lead.SourceManual}
		if err := repo.Insert(ctx, l, nil); err != nil {
			t.Fatal(err)
		}
		for _, c := range changes {
			current, err := repo.Load(ctx, ws, l.ID)
			if err != nil {
				t.Fatal(err)
			}
			next := *current
			c.ID = uuid.NewString()
			if _, err := next.ApplyConsentChange(c); err != nil {
				t.Fatal(err)
			}
			if err := repo.Save(ctx, &next, current.Version, []recordevent.Event{lead.ConsentEvent(c, current, &next)}); err != nil {
				t.Fatal(err)
			}
		}
		return l
	}
	grant := lead.ConsentChange{Kind: lead.ConsentGrant, Source: lead.ConsentManual, Purpose: "novidades",
		Actor: lead.ConsentActor{Type: lead.ConsentActorUser, ID: operator}, Evidence: lead.ConsentEvidence{Type: lead.EvidenceNote, Excerpt: "aceitou"}, At: granted}
	tap := func(at time.Time) lead.ConsentChange {
		return lead.ConsentChange{Kind: lead.ConsentWithdraw, Source: lead.ConsentButtonReply,
			Actor: lead.ConsentActor{Type: lead.ConsentActorLead, ID: "lead"}, Evidence: lead.ConsentEvidence{Type: lead.EvidenceButtonReply, Ref: "m-" + uuid.NewString()}, At: at}
	}
	operatorOut := lead.ConsentChange{Kind: lead.ConsentWithdraw, Source: lead.ConsentManual,
		Actor: lead.ConsentActor{Type: lead.ConsentActorUser, ID: operator}, At: left}

	restorable := store("5511900000001", grant, tap(left))
	byOperator := store("5511900000002", operatorOut)
	afterTheChoice := store("5511900000003", tap(chosen.Add(time.Hour)))
	stillIn := store("5511900000004", grant)

	edit, err := leadaction.EditFor(leadaction.ActionRestoreOptOut, leadaction.Params{Note: "botão errado", ConfirmCount: 4}, nil, customfield.Viewer{})
	if err != nil {
		t.Fatal(err)
	}
	edit.ChosenAt = chosen
	ids := []string{restorable.ID, byOperator.ID, afterTheChoice.ID, stillIn.ID}

	preview, err := repo.TallyBatch(ctx, ws, ids, edit)
	if err != nil || preview.Unchanged != 1 || preview.NotRestorable != 2 || preview.Gone != 0 {
		t.Fatalf("preview = %+v, %v", preview, err)
	}

	at := chosen.Add(time.Minute)
	tally, err := repo.ApplyBatch(ctx, leadaction.BatchWrite{WorkspaceID: ws, ActorID: operator, LeadIDs: ids, Edit: edit, At: at})
	if err != nil {
		t.Fatal(err)
	}
	if len(tally.Changed) != 1 || tally.Changed[0].LeadID != restorable.ID || tally.NotRestorable != 2 || tally.Unchanged != 1 {
		t.Fatalf("tally = %+v", tally)
	}

	got, err := repo.Load(ctx, ws, restorable.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.OptedOutAt != nil || got.OptOutSource != "" || got.WhatsAppOptIn == nil || !got.WhatsAppOptIn.GrantedAt.Equal(granted) ||
		got.WhatsAppOptIn.Source != lead.ConsentManual || got.WhatsAppOptIn.Purpose != "novidades" || got.Version != tally.Changed[0].Version {
		t.Fatalf("restored = %+v grant %+v", got, got.WhatsAppOptIn)
	}
	record := got.ConsentRecord
	if record == nil || record.Kind != lead.ConsentRevertWithdrawal || record.Source != lead.ConsentButtonReply || record.Evidence.Excerpt != "botão errado" ||
		record.Actor.ID != operator || record.Reverts == "" {
		t.Fatalf("record = %+v", record)
	}
	var event struct {
		Kind    string
		ActorID string
		Changes string
	}
	if err := db.Raw("SELECT kind, actor_id::text AS actor_id, changes::text AS changes FROM lead_events WHERE id = ?", record.EventID).Scan(&event).Error; err != nil {
		t.Fatal(err)
	}
	if event.Kind != string(lead.EventConsentWithdrawalReverted) || event.ActorID != operator {
		t.Fatalf("event = %+v", event)
	}

	for _, untouched := range []*lead.Lead{byOperator, afterTheChoice} {
		stored, err := repo.Load(ctx, ws, untouched.ID)
		if err != nil || stored.OptedOutAt == nil || stored.ConsentRecord.Kind != lead.ConsentWithdraw {
			t.Fatalf("a lead that could not be restored changed: %+v, %v", stored, err)
		}
	}

	again, err := repo.ApplyBatch(ctx, leadaction.BatchWrite{WorkspaceID: ws, ActorID: operator, LeadIDs: ids, Edit: edit, At: at})
	if err != nil || len(again.Changed) != 0 || again.Unchanged != 2 || again.NotRestorable != 2 {
		t.Fatalf("a second pass = %+v, %v", again, err)
	}
}
