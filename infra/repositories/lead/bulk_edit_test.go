package lead

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/lead"
	"vozko/domain/leadaction"
)

const (
	bulkActor = "4b1c8a52-0d9e-4f6a-9c3e-2a7d5b8e1f00"
	thirdLead = "2b7c3f1e-2d4a-4c5b-9e8f-1a2b3c4d5e6f"
)

var bulkAt = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

func customEdit(value any, recorded bool) leadaction.Edit {
	return leadaction.Edit{Kind: leadaction.EditCustomField, Field: lead.CustomFieldName("classificacao"), Key: "classificacao", Value: value, Recorded: recorded, EventKind: lead.EventUpdated}
}

func TestBulkStatements_BindEveryPlaceholder(t *testing.T) {
	edits := map[string]leadaction.Edit{
		"classify": customEdit("Positivo", true),
		"clear":    customEdit(nil, true),
		"assign":   {Kind: leadaction.EditOwner, Field: lead.FieldOwner, Value: "ai:1a2b3c4d-5e6f-4a7b-8c9d-0e1f2a3b4c5d", Recorded: true},
		"unassign": {Kind: leadaction.EditOwner, Field: lead.FieldOwner, Value: "", Recorded: true},
		"block":    {Kind: leadaction.EditBlocked, Field: lead.FieldBlocked, Value: true, Recorded: true},
		"unblock":  {Kind: leadaction.EditBlocked, Field: lead.FieldBlocked, Value: false, Recorded: true},
	}
	ids := []string{firstLead, secondLead}
	for name, e := range edits {
		b, err := newBulkEdit(e)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		sql, args := b.lockSQL(pageWorkspace, ids)
		assertPlaceholders(t, name+" lock", sql, args)
		sql, args = b.tallySQL(pageWorkspace, ids)
		assertPlaceholders(t, name+" tally", sql, args)
		sql, args = b.updateSQL(pageWorkspace, ids, bulkActor, bulkAt)
		assertPlaceholders(t, name+" update", sql, args)
		if !strings.Contains(sql, "version = leads.version + 1") {
			t.Fatalf("%s update does not bump the version: %s", name, sql)
		}
		if strings.Contains(sql, " ? ") && strings.Contains(sql, "->>") {
			t.Fatalf("%s update uses a jsonb question mark operator: %s", name, sql)
		}
	}
}

func TestApplyBatch_LocksInIDOrderUpdatesOnlyWhatChangesAndWritesOneEventPerLead(t *testing.T) {
	db, mock, _ := newMockDB(t)
	r := &repository{db: db, agg: newAggregateCache(nil)}
	edit := customEdit("Positivo", true)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT leads.id::text AS id, (leads.custom_fields -> $1::text) AS before_value, leads.owner_id::text AS before_owner_id, leads.owner_kind AS before_owner_kind, ((leads.custom_fields -> $2::text) IS NOT DISTINCT FROM $3::jsonb) AS unchanged FROM leads WHERE leads.workspace_id = $4 AND leads.id = ANY($5::uuid[]) AND leads.deleted_at IS NULL ORDER BY leads.id FOR UPDATE")).
		WithArgs("classificacao", "classificacao", `"Positivo"`, pageWorkspace, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "before_value", "before_owner_id", "before_owner_kind", "unchanged"}).
			AddRow(firstLead, []byte(`"Negativo"`), nil, nil, false).
			AddRow(secondLead, []byte(`"Positivo"`), nil, nil, true))
	mock.ExpectQuery(regexp.QuoteMeta("UPDATE leads SET custom_fields = jsonb_set(COALESCE(leads.custom_fields, '{}'::jsonb), ARRAY[$1::text], $2::jsonb, true), version = leads.version + 1, updated_at = $3 WHERE leads.workspace_id = $4 AND leads.id = ANY($5::uuid[]) AND leads.deleted_at IS NULL RETURNING leads.id::text AS id, leads.version")).
		WithArgs("classificacao", `"Positivo"`, bulkAt, pageWorkspace, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "version"}).AddRow(firstLead, int64(4)))
	mock.ExpectExec(`^INSERT INTO "lead_events"`).
		WithArgs(sqlmock.AnyArg(), pageWorkspace, firstLead, bulkActor, "human", "updated", sqlmock.AnyArg(), bulkAt).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	tally, err := r.ApplyBatch(context.Background(), leadaction.BatchWrite{
		WorkspaceID: pageWorkspace, ActorID: bulkActor, LeadIDs: []string{firstLead, secondLead, thirdLead}, Edit: edit, At: bulkAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(tally.Changed) != 1 || tally.Changed[0] != (leadaction.Changed{LeadID: firstLead, Version: 4}) || tally.Unchanged != 1 || tally.Gone != 1 {
		t.Fatalf("tally = %+v", tally)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestApplyBatch_NothingToChangeWritesNothing(t *testing.T) {
	db, mock, _ := newMockDB(t)
	r := &repository{db: db, agg: newAggregateCache(nil)}

	mock.ExpectBegin()
	mock.ExpectQuery(`^SELECT leads\.id::text AS id, `).
		WillReturnRows(sqlmock.NewRows([]string{"id", "before_value", "before_owner_id", "before_owner_kind", "unchanged"}).AddRow(firstLead, nil, nil, nil, true))
	mock.ExpectCommit()

	tally, err := r.ApplyBatch(context.Background(), leadaction.BatchWrite{
		WorkspaceID: pageWorkspace, ActorID: bulkActor, LeadIDs: []string{firstLead},
		Edit: leadaction.Edit{Kind: leadaction.EditBlocked, Field: lead.FieldBlocked, Value: true, Recorded: true, EventKind: lead.EventBlocked}, At: bulkAt,
	})
	if err != nil || len(tally.Changed) != 0 || tally.Unchanged != 1 {
		t.Fatalf("tally = %+v, %v", tally, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestApplyBatch_ASensitiveValueNeverReachesTheEvents(t *testing.T) {
	db, mock, _ := newMockDB(t)
	r := &repository{db: db, agg: newAggregateCache(nil)}
	var written []byte

	mock.ExpectBegin()
	mock.ExpectQuery(`^SELECT leads\.id::text AS id, `).
		WillReturnRows(sqlmock.NewRows([]string{"id", "before_value", "before_owner_id", "before_owner_kind", "unchanged"}).AddRow(firstLead, []byte(`"Negativo"`), nil, nil, false))
	mock.ExpectQuery(`^UPDATE leads SET custom_fields`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "version"}).AddRow(firstLead, int64(2)))
	mock.ExpectExec(`^INSERT INTO "lead_events"`).
		WithArgs(sqlmock.AnyArg(), pageWorkspace, firstLead, bulkActor, "human", "updated", capture(&written), bulkAt).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if _, err := r.ApplyBatch(context.Background(), leadaction.BatchWrite{WorkspaceID: pageWorkspace, ActorID: bulkActor, LeadIDs: []string{firstLead}, Edit: customEdit("Positivo", false), At: bulkAt}); err != nil {
		t.Fatal(err)
	}
	var changes []map[string]any
	if err := json.Unmarshal(written, &changes); err != nil {
		t.Fatalf("changes %s: %v", written, err)
	}
	if len(changes) != 1 || changes[0]["redacted"] != true || changes[0]["before"] != nil || changes[0]["after"] != nil {
		t.Fatalf("a sensitive change was stored as %s", written)
	}
}

func TestApplyBatch_RefusesMoreThanOneBatch(t *testing.T) {
	r := &repository{}
	ids := make([]string, leadaction.BatchSize+1)
	for i := range ids {
		ids[i] = firstLead
	}
	if _, err := r.ApplyBatch(context.Background(), leadaction.BatchWrite{WorkspaceID: pageWorkspace, ActorID: bulkActor, LeadIDs: ids, Edit: customEdit("x", true)}); err == nil {
		t.Fatal("a batch above the batch size was accepted")
	}
}

func TestTallyBatch_CountsWhatWouldNotChange(t *testing.T) {
	db, mock, _ := newMockDB(t)
	r := &repository{db: db, agg: newAggregateCache(nil)}

	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) AS existing, COUNT(*) FILTER (WHERE leads.blocked = $1) AS unchanged FROM leads WHERE leads.workspace_id = $2 AND leads.id = ANY($3::uuid[]) AND leads.deleted_at IS NULL")).
		WithArgs(true, pageWorkspace, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"existing", "unchanged"}).AddRow(2, 1))

	tally, err := r.TallyBatch(context.Background(), pageWorkspace, []string{firstLead, secondLead, thirdLead},
		leadaction.Edit{Kind: leadaction.EditBlocked, Field: lead.FieldBlocked, Value: true})
	if err != nil || tally.Unchanged != 1 || tally.Gone != 1 {
		t.Fatalf("tally = %+v, %v", tally, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestBlockTargets_ReadsTheNumbersInTheRunState(t *testing.T) {
	db, mock, _ := newMockDB(t)
	r := &repository{db: db, agg: newAggregateCache(nil)}

	mock.ExpectQuery(regexp.QuoteMeta("SELECT leads.id::text AS lead_id, leads.number FROM leads WHERE leads.workspace_id = $1 AND leads.id = ANY($2::uuid[]) AND leads.deleted_at IS NULL AND leads.blocked = $3 AND leads.number IS NOT NULL ORDER BY leads.id")).
		WithArgs(pageWorkspace, sqlmock.AnyArg(), true).
		WillReturnRows(sqlmock.NewRows([]string{"lead_id", "number"}).AddRow(firstLead, "5511987654321"))

	targets, err := r.BlockTargets(context.Background(), pageWorkspace, []string{firstLead}, true)
	if err != nil || len(targets) != 1 || targets[0] != (leadaction.BlockTarget{LeadID: firstLead, Number: "5511987654321"}) {
		t.Fatalf("BlockTargets = %v, %v", targets, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

type captured struct{ into *[]byte }

func capture(into *[]byte) captured { return captured{into: into} }

func (c captured) Match(v driver.Value) bool {
	switch value := v.(type) {
	case []byte:
		*c.into = value
	case string:
		*c.into = []byte(value)
	default:
		raw, _ := json.Marshal(value)
		*c.into = raw
	}
	return true
}
