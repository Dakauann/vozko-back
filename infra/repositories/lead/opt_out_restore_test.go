package lead

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/customfield"
	"vozko/domain/lead"
	"vozko/domain/leadaction"
)

var restoreChosenAt = time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)

func restoreEdit() leadaction.Edit {
	edit, err := leadaction.EditFor(leadaction.ActionRestoreOptOut, leadaction.Params{Note: "botão errado", ConfirmCount: 3}, nil, customfield.Viewer{})
	if err != nil {
		panic(err)
	}
	edit.ChosenAt = restoreChosenAt
	return edit
}

var restoreRowColumns = []string{"id", "workspace_id", "version", "whatsapp_opt_in_at", "whatsapp_opt_in_source", "whatsapp_opt_in_purpose", "opted_out_at", "opted_out_source", "whatsapp_consent_record"}

func TestOptOutRestoreStatementsBindEveryPlaceholder(t *testing.T) {
	ids := []string{firstLead, secondLead}
	assertPlaceholders(t, "read", optOutRestoreReadSQL, []interface{}{pageWorkspace, ids})
	assertPlaceholders(t, "lock", optOutRestoreLockSQL, []interface{}{pageWorkspace, ids})
	sql, args, err := restoreUpdate(pageWorkspace, []plannedRestore{{after: &lead.Lead{ID: firstLead, Version: 2}}}, bulkAt)
	if err != nil {
		t.Fatal(err)
	}
	assertPlaceholders(t, "update", sql, args)
	if !strings.Contains(sql, "version = leads.version + 1") || !strings.Contains(sql, "opted_out_at = NULL") {
		t.Fatalf("update = %s", sql)
	}
}

func TestApplyBatchRestoresOnlyTheCurrentAutomatedWithdrawalsAndSkipsTheRest(t *testing.T) {
	db, mock, _ := newMockDB(t)
	r := &repository{db: db, agg: newAggregateCache(nil), newID: func() string { return "ev-revert" }}
	out := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(postgresPlaceholders(optOutRestoreLockSQL))).
		WithArgs(pageWorkspace, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows(restoreRowColumns).
			AddRow(firstLead, pageWorkspace, int64(3), nil, nil, nil, out, "lead_request",
				[]byte(`{"kind":"withdraw","source":"button_reply","actor":{"type":"lead","id":"l"},"evidence":{"type":"button_reply","ref":"m-1"},"at":"2026-10-15T12:00:00Z","eventId":"ev-out","priorGrant":{"grantedAt":"2026-10-01T10:00:00Z","source":"form"}}`)).
			AddRow(secondLead, pageWorkspace, int64(5), nil, nil, nil, out, "operator",
				[]byte(`{"kind":"withdraw","source":"manual","actor":{"type":"user","id":"u"},"evidence":{},"at":"2026-10-15T12:00:00Z","eventId":"ev-op"}`)).
			AddRow(thirdLead, pageWorkspace, int64(1), nil, nil, nil, nil, nil, nil))
	mock.ExpectQuery(`^UPDATE leads SET whatsapp_opt_in_at = v.opt_in_at`).
		WithArgs(bulkAt, sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), pageWorkspace).
		WillReturnRows(sqlmock.NewRows([]string{"id", "version"}).AddRow(firstLead, int64(4)))
	mock.ExpectExec(`^INSERT INTO "lead_events"`).
		WithArgs("ev-revert", pageWorkspace, firstLead, bulkActor, "human", "consent_withdrawal_reverted", sqlmock.AnyArg(), bulkAt).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	tally, err := r.ApplyBatch(context.Background(), leadaction.BatchWrite{
		WorkspaceID: pageWorkspace, ActorID: bulkActor, LeadIDs: []string{firstLead, secondLead, thirdLead, "3b7c3f1e-2d4a-4c5b-9e8f-1a2b3c4d5e6f"}, Edit: restoreEdit(), At: bulkAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(tally.Changed) != 1 || tally.Changed[0].LeadID != firstLead || tally.Changed[0].Version != 4 ||
		tally.NotRestorable != 1 || tally.Unchanged != 1 || tally.Gone != 1 {
		t.Fatalf("tally = %+v", tally)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestARestoreWithoutTheMomentItWasChosenWritesNothing(t *testing.T) {
	db, _, _ := newMockDB(t)
	r := &repository{db: db, agg: newAggregateCache(nil)}
	edit := restoreEdit()
	edit.ChosenAt = time.Time{}
	if _, err := r.ApplyBatch(context.Background(), leadaction.BatchWrite{WorkspaceID: pageWorkspace, ActorID: bulkActor, LeadIDs: []string{firstLead}, Edit: edit, At: bulkAt}); err == nil {
		t.Fatal("a restore without its chosen time must fail closed")
	}
	if _, err := r.TallyBatch(context.Background(), pageWorkspace, []string{firstLead}, edit); err == nil {
		t.Fatal("a restore tally without its chosen time must fail closed")
	}
}

func postgresPlaceholders(sql string) string {
	var b strings.Builder
	n := 0
	for _, r := range sql {
		if r == '?' {
			n++
			b.WriteString("$" + itoa(int64(n)))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
