package mercadolivre_repository

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	mldomain "vozko/domain/mercadolivre"
)

func TestRawConversationStatementsBindEveryPlaceholder(t *testing.T) {
	assertPlaceholders(t, "claim", claimOutboundSQL, 1)
	assertPlaceholders(t, "release", releaseOutboundSQL, 1)
	assertPlaceholders(t, "record inbound", recordInboundSQL, 6)
	assertPlaceholders(t, "department", departmentForEntrySQL, 1)
}

func TestClaimOutboundIsOneConditionalUpdate(t *testing.T) {
	db, mock, _ := newMockDB(t)
	repo := NewConversationRepository(db)
	pattern := `UPDATE mercadolivre_conversations SET outbound_since_inbound = outbound_since_inbound \+ 1, updated_at = now\(\) ` +
		`WHERE id = \$1 AND deleted_at IS NULL AND window_open AND \(outbound_budget = 0 OR outbound_since_inbound < outbound_budget\) ` +
		`RETURNING outbound_since_inbound`

	mock.ExpectQuery(pattern).WithArgs("c-1").WillReturnRows(sqlmock.NewRows([]string{"outbound_since_inbound"}).AddRow(1))
	claimed, err := repo.ClaimOutbound(context.Background(), "c-1")
	if err != nil || !claimed {
		t.Fatalf("claim = %v, %v", claimed, err)
	}

	mock.ExpectQuery(pattern).WithArgs("c-1").WillReturnRows(sqlmock.NewRows([]string{"outbound_since_inbound"}))
	claimed, err = repo.ClaimOutbound(context.Background(), "c-1")
	if err != nil || claimed {
		t.Fatalf("closed window or spent budget must refuse: %v, %v", claimed, err)
	}

	mock.ExpectQuery(pattern).WithArgs("c-1").WillReturnError(errors.New("db down"))
	claimed, err = repo.ClaimOutbound(context.Background(), "c-1")
	if err == nil || claimed {
		t.Fatalf("a claim that cannot be written must refuse the send: %v, %v", claimed, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestReleaseOutboundNeverGoesBelowZero(t *testing.T) {
	db, mock, _ := newMockDB(t)
	mock.ExpectExec(regexp.QuoteMeta(`SET outbound_since_inbound = GREATEST(outbound_since_inbound - 1, 0)`)).
		WithArgs("c-1").WillReturnResult(sqlmock.NewResult(0, 1))
	if err := NewConversationRepository(db).ReleaseOutbound(context.Background(), "c-1"); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRecordInboundResetsTheBudgetOnlyForANewerMessage(t *testing.T) {
	db, mock, _ := newMockDB(t)
	at := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	mock.ExpectExec(regexp.QuoteMeta(`outbound_since_inbound = CASE WHEN last_customer_message_at IS NULL OR last_customer_message_at < $1 THEN 0 ELSE outbound_since_inbound END`)).
		WithArgs(at, at, at, at, at, "c-1").WillReturnResult(sqlmock.NewResult(0, 1))
	if err := NewConversationRepository(db).RecordInbound(context.Background(), "c-1", at); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRecordInboundOnAMissingConversationIsNotFound(t *testing.T) {
	db, mock, _ := newMockDB(t)
	mock.ExpectExec(`UPDATE mercadolivre_conversations`).WillReturnResult(sqlmock.NewResult(0, 0))
	err := NewConversationRepository(db).RecordInbound(context.Background(), "c-1", time.Now())
	if !errors.Is(err, mldomain.ErrConversationNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestFindOrCreateByPackReportsWhetherItCreated(t *testing.T) {
	cases := []struct {
		name     string
		inserted int64
		created  bool
	}{
		{"new pack", 1, true},
		{"pack created by a concurrent delivery", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, _ := newMockDB(t)
			mock.ExpectExec(`INSERT INTO "mercadolivre_conversations" .* ON CONFLICT \("account_id","pack_key"\) WHERE deleted_at IS NULL DO NOTHING`).
				WillReturnResult(sqlmock.NewResult(0, tc.inserted))
			mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "mercadolivre_conversations" WHERE (account_id = $1 AND pack_key = $2)`)).
				WithArgs("a-1", "2000000089077943", 1).
				WillReturnRows(sqlmock.NewRows([]string{"id", "account_id", "pack_key", "workspace_id"}).AddRow("c-1", "a-1", "2000000089077943", "ws-1"))
			conv, created, err := NewConversationRepository(db).FindOrCreateByPack(context.Background(), mldomain.FindOrCreateConversationInput{
				WorkspaceID: "ws-1", AccountID: "a-1", ContactID: "ct-1", PackKey: "2000000089077943", BuyerMLUserID: "123",
			})
			if err != nil || conv.ID != "c-1" || created != tc.created {
				t.Fatalf("conv=%+v created=%v err=%v", conv, created, err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestFindOrCreateByPackRefusesAnInvalidPack(t *testing.T) {
	db, _, _ := newMockDB(t)
	_, _, err := NewConversationRepository(db).FindOrCreateByPack(context.Background(), mldomain.FindOrCreateConversationInput{
		WorkspaceID: "ws-1", AccountID: "a-1", ContactID: "ct-1", PackKey: "x",
	})
	if !errors.Is(err, mldomain.ErrPackUnknown) {
		t.Fatalf("err = %v", err)
	}
}

func TestFindConversationsByIDsIsOneQuery(t *testing.T) {
	db, mock, _ := newMockDB(t)
	repo := NewConversationRepository(db)
	got, err := repo.FindByIDs(context.Background(), nil)
	if err != nil || len(got) != 0 {
		t.Fatalf("empty ids = %v, %v", got, err)
	}
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "mercadolivre_conversations" WHERE id IN ($1,$2)`)).
		WithArgs("c-1", "c-2").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("c-1").AddRow("c-2"))
	got, err = repo.FindByIDs(context.Background(), []string{"c-1", "c-2", "c-1", " "})
	if err != nil || len(got) != 2 {
		t.Fatalf("got %v, %v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestApplyStatusNeverRewindsToAnOlderRead(t *testing.T) {
	db, mock, _ := newMockDB(t)
	mediated := true
	readAt := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	mock.ExpectExec(`UPDATE "mercadolivre_conversations" SET "agent_mediated"=\$1,"ml_status"=\$2,"ml_status_read_at"=\$3,"ml_substatus"=\$4,"outbound_budget"=\$5,"window_open"=\$6,"updated_at"=\$7 WHERE \(id = \$8 AND \(ml_status_read_at IS NULL OR ml_status_read_at <= \$9\)\)`).
		WithArgs(&mediated, "blocked", readAt, "blocked_by_mediation", 1, false, sqlmock.AnyArg(), "c-1", readAt).
		WillReturnResult(sqlmock.NewResult(0, 0))
	err := NewConversationRepository(db).ApplyStatus(context.Background(), "c-1", mldomain.ThreadState{
		Status: "blocked", Substatus: "blocked_by_mediation", AgentMediated: &mediated, OutboundBudget: 1, WindowOpen: false, ReadAt: readAt,
	})
	if err != nil {
		t.Fatalf("an older read is skipped, not an error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestApplyStatusRefusesAStateWithoutAReadTime(t *testing.T) {
	db, _, _ := newMockDB(t)
	if err := NewConversationRepository(db).ApplyStatus(context.Background(), "c-1", mldomain.ThreadState{Status: "active"}); err == nil {
		t.Fatal("a state without a read time cannot be ordered against other reads")
	}
}

func TestPackSummaryKeepsTheShipmentSearchable(t *testing.T) {
	db, mock, _ := newMockDB(t)
	at := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	mock.ExpectExec(`UPDATE "mercadolivre_conversations" SET "order_ids"=\$1,"pack_summary"=\$2,"pack_summary_refreshed_at"=\$3,"shipment_id"=\$4`).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), at, "40000000001", sqlmock.AnyArg(), "c-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	err := NewConversationRepository(db).UpdatePackSummary(context.Background(), "c-1", mldomain.PackSummary{OrderIDs: []string{"1"}, ShipmentID: "40000000001"}, at)
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "mercadolivre_conversations" WHERE (account_id = $1 AND shipment_id = $2)`)).
		WithArgs("a-1", "40000000001", 1).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("c-1"))
	conv, err := NewConversationRepository(db).FindByShipment(context.Background(), "a-1", "40000000001")
	if err != nil || conv.ID != "c-1" {
		t.Fatalf("conv = %+v, %v", conv, err)
	}
	if _, err := NewConversationRepository(db).FindByShipment(context.Background(), "a-1", ""); !errors.Is(err, mldomain.ErrConversationNotFound) {
		t.Fatalf("an empty shipment must never match: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
