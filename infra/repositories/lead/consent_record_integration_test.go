package lead

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"vozko/domain/lead"
	"vozko/domain/recordevent"
)

func TestAWithdrawalAndItsConfirmationAreStoredWithTheirEventAgainstPostgres(t *testing.T) {
	db := leadStoreDB(t)
	repo := &repository{db: db}
	ctx := context.Background()
	ws := uuid.NewString()
	l := &lead.Lead{WorkspaceID: ws, Number: "5511987654321", Source: lead.SourceManual}
	if err := repo.Insert(ctx, l, nil); err != nil {
		t.Fatal(err)
	}
	current, err := repo.Load(ctx, ws, l.ID)
	if err != nil {
		t.Fatal(err)
	}
	next := *current
	withdrawal := lead.ConsentChange{ID: uuid.NewString(), Kind: lead.ConsentWithdraw, Source: lead.ConsentKeyword,
		Actor:    lead.ConsentActor{Type: lead.ConsentActorLead, ID: l.ID},
		Evidence: lead.ConsentEvidence{Type: lead.EvidenceMessage, Ref: "wamid.in", Excerpt: "SAIR"},
		At:       time.Now().UTC().Truncate(time.Microsecond)}
	if _, err := next.ApplyConsentChange(withdrawal); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, &next, current.Version, []recordevent.Event{lead.ConsentEvent(withdrawal, current, &next)}); err != nil {
		t.Fatalf("save: %v", err)
	}

	stored, err := repo.Load(ctx, ws, l.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.OptedOutAt == nil || stored.OptOutSource != lead.OptOutLeadRequest || stored.ConsentRecord == nil ||
		stored.ConsentRecord.EventID != withdrawal.ID || stored.ConsentRecord.Evidence.Excerpt != "SAIR" {
		t.Fatalf("stored = %+v record %+v", stored, stored.ConsentRecord)
	}
	var kind string
	if err := db.Raw("SELECT kind FROM lead_events WHERE id = ?", withdrawal.ID).Scan(&kind).Error; err != nil || kind != string(lead.EventConsentWithdrawn) {
		t.Fatalf("event kind = %q, %v", kind, err)
	}
	var leaked int64
	db.Raw("SELECT COUNT(*) FROM lead_events WHERE lead_id = ? AND changes::text LIKE '%SAIR%'", l.ID).Scan(&leaked)
	if leaked != 0 {
		t.Fatal("the excerpt must never reach the lead events")
	}

	claimed := *stored
	if err := claimed.ClaimOptOutConfirmation(withdrawal.ID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, &claimed, stored.Version, nil); err != nil {
		t.Fatalf("save claim: %v", err)
	}
	again, err := repo.Load(ctx, ws, l.ID)
	if err != nil || again.ConsentRecord.ConfirmationSentAt == nil {
		t.Fatalf("claim not stored: %+v, %v", again.ConsentRecord, err)
	}
	if err := again.ClaimOptOutConfirmation(withdrawal.ID, time.Now().UTC()); err == nil {
		t.Fatal("a stored claim must refuse a second confirmation")
	}
}

func TestAnEnrichingImportWritesANewGrantRecordAndKeepsAnyOtherAgainstPostgres(t *testing.T) {
	db := leadStoreDB(t)
	repo := &repository{db: db}
	ctx := context.Background()
	ws := uuid.NewString()
	kept := &lead.Lead{WorkspaceID: ws, Number: "5511987654321", Source: lead.SourceManual,
		ConsentRecord: &lead.ConsentRecord{Kind: lead.ConsentGrant, Source: lead.ConsentForm, At: time.Now().UTC()}}
	fresh := &lead.Lead{WorkspaceID: ws, Number: "5511987654322", Source: lead.SourceManual}
	for _, l := range []*lead.Lead{kept, fresh} {
		if err := repo.Insert(ctx, l, nil); err != nil {
			t.Fatal(err)
		}
	}
	grant, err := consentRecordColumn(&lead.ConsentRecord{Kind: lead.ConsentGrant, Source: lead.ConsentImport,
		Evidence: lead.ConsentEvidence{Type: lead.EvidenceImportLine, Ref: "job-1:3"}, At: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	texts := make([]pq.GenericArray, 11)
	for i := range texts {
		texts[i] = pq.GenericArray{A: []sql.NullString{{}, {}}}
	}
	texts[10] = pq.GenericArray{A: []sql.NullString{{}, {String: string(grant), Valid: true}}}
	args := []interface{}{time.Now().UTC(), pq.StringArray{kept.ID, fresh.ID}, pq.Int64Array{1, 1}, pq.StringArray{"", ""}}
	for _, column := range texts {
		args = append(args, column)
	}
	args = append(args, ws)
	var written []lockedVersion
	if err := db.Raw(importEnrichSQL, args...).Scan(&written).Error; err != nil || len(written) != 2 {
		t.Fatalf("enrich = %+v, %v", written, err)
	}

	keptNow, err := repo.FindByID(ws, kept.ID)
	if err != nil || keptNow.ConsentRecord == nil || keptNow.ConsentRecord.Source != lead.ConsentForm {
		t.Fatalf("an import without a new grant must keep the record: %+v, %v", keptNow, err)
	}
	freshNow, err := repo.FindByID(ws, fresh.ID)
	if err != nil || freshNow.ConsentRecord == nil || freshNow.ConsentRecord.Evidence.Ref != "job-1:3" {
		t.Fatalf("an imported grant must store its record: %+v, %v", freshNow, err)
	}
}
