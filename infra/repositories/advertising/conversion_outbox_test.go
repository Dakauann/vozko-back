package advertising_repository

import (
	"context"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/advertising"
)

var pendingColumns = []string{"opportunity_id", "event", "at", "value_cents", "currency", "channel", "click_id", "waba_id",
	"page_id", "page_scoped_user_id", "instagram_user_id", "instagram_scoped", "phone"}

func numbered(sql string) string {
	var b strings.Builder
	n := 0
	for _, r := range sql {
		if r == '?' {
			n++
			b.WriteString("$" + strconv.Itoa(n))
			continue
		}
		b.WriteRune(r)
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func TestConversionOutboxPendingPinsTheQueryAndItsScope(t *testing.T) {
	db, mock := newMockDB(t)
	since := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)
	at := since.Add(time.Hour)
	mock.ExpectQuery(regexp.QuoteMeta(numbered(pendingSignalsSQL))).
		WithArgs(
			"ws", "created", "won", since,
			"created", "created", "won", "won",
			"created", "LeadSubmitted", "won", "Purchase",
			"ws",
			"whatsapp", "messenger", "instagram",
			"whatsapp", "ws",
			"facebook", "ws",
			"instagram", "ws",
			"ws",
			"ws", "sent", "skipped", "sending", "failed", 5,
			100,
		).
		WillReturnRows(sqlmock.NewRows(pendingColumns).
			AddRow("o-1", "won", at, int64(1500), "BRL", "whatsapp", "clid-1", "waba-1", "", "", "", "", "5511999990000").
			AddRow("o-2", "created", at, int64(0), "BRL", "", "", "", "", "", "", "", ""))
	pending, err := NewConversionOutbox(db).Pending(context.Background(), "ws", since, 100)
	if err != nil {
		t.Fatal(err)
	}
	want := []advertising.PendingSignal{
		{WorkspaceID: "ws", Signal: advertising.DealSignal{
			OpportunityID: "o-1", Event: advertising.DealWon, At: at, ValueCents: 1500, Currency: "BRL",
			Identity: advertising.MessagingIdentity{Channel: advertising.ChannelWhatsApp, ClickID: "clid-1", WABAID: "waba-1"},
			Phone:    "5511999990000",
		}},
		{WorkspaceID: "ws", Signal: advertising.DealSignal{OpportunityID: "o-2", Event: advertising.DealCreated, At: at, Currency: "BRL"}},
	}
	if len(pending) != len(want) || pending[0] != want[0] || pending[1] != want[1] {
		t.Fatalf("got %+v", pending)
	}
	expectationsMet(t, mock)
}

func TestConversionOutboxPendingRequiresWorkspace(t *testing.T) {
	db, mock := newMockDB(t)
	if _, err := NewConversionOutbox(db).Pending(context.Background(), " ", time.Now(), 10); !errors.Is(err, advertising.ErrWorkspaceRequired) {
		t.Fatalf("got %v", err)
	}
	expectationsMet(t, mock)
}

const recordConversionPattern = `INSERT INTO ad_conversion_records \(opportunity_id, event_name, workspace_id, status, reason, attempts, sent_at, updated_at\) .*` +
	`ON CONFLICT \(opportunity_id, event_name\) DO UPDATE SET .*attempts = ad_conversion_records\.attempts \+ EXCLUDED\.attempts.*` +
	`WHERE ad_conversion_records\.workspace_id = EXCLUDED\.workspace_id`

func TestConversionOutboxRecordUpsertsAndCountsFailures(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectExec(recordConversionPattern).
		WithArgs("o-1", "Purchase", "ws", "failed", "boom", "failed", "failed", nil).
		WillReturnResult(sqlmock.NewResult(0, 1))
	err := NewConversionOutbox(db).Record(context.Background(), advertising.ConversionRecord{
		OpportunityID: "o-1", EventName: "Purchase", WorkspaceID: "ws", Status: advertising.ConversionFailed, Reason: "boom",
	})
	if err != nil {
		t.Fatal(err)
	}
	expectationsMet(t, mock)
}

func TestConversionOutboxRecordOfAnotherWorkspaceIsRefused(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectExec(recordConversionPattern).WillReturnResult(sqlmock.NewResult(0, 0))
	err := NewConversionOutbox(db).Record(context.Background(), advertising.ConversionRecord{
		OpportunityID: "o-1", EventName: "Purchase", WorkspaceID: "ws", Status: advertising.ConversionSent,
	})
	if !errors.Is(err, errConversionRecordElsewhere) {
		t.Fatalf("got %v", err)
	}
}

func TestConversionOutboxRecordRequiresItsKey(t *testing.T) {
	db, mock := newMockDB(t)
	outbox := NewConversionOutbox(db)
	if err := outbox.Record(context.Background(), advertising.ConversionRecord{OpportunityID: "o-1", EventName: "Purchase"}); !errors.Is(err, advertising.ErrWorkspaceRequired) {
		t.Fatalf("got %v", err)
	}
	if err := outbox.Record(context.Background(), advertising.ConversionRecord{WorkspaceID: "ws", EventName: "Purchase"}); !errors.Is(err, errConversionKeyRequired) {
		t.Fatalf("got %v", err)
	}
	if err := outbox.Record(context.Background(), advertising.ConversionRecord{WorkspaceID: "ws", OpportunityID: "o-1"}); !errors.Is(err, errConversionKeyRequired) {
		t.Fatalf("got %v", err)
	}
	expectationsMet(t, mock)
}

func TestConversionOutboxRecentIsNewestFirst(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "ad_conversion_records" WHERE workspace_id = $1 ORDER BY updated_at DESC, opportunity_id, event_name LIMIT $2`)).
		WithArgs("ws", 20).
		WillReturnRows(sqlmock.NewRows([]string{"opportunity_id", "event_name", "workspace_id", "status", "attempts"}).
			AddRow("o-2", "Purchase", "ws", "failed", 2).
			AddRow("o-1", "LeadSubmitted", "ws", "sent", 0))
	records, err := NewConversionOutbox(db).Recent(context.Background(), "ws", 20)
	if err != nil || len(records) != 2 {
		t.Fatalf("got %v, %v", records, err)
	}
	if records[0].Status != advertising.ConversionFailed || records[0].Attempts != 2 || records[0].EventName != "Purchase" {
		t.Fatalf("got %+v", records[0])
	}
	if _, err := NewConversionOutbox(db).Recent(context.Background(), "", 20); !errors.Is(err, advertising.ErrWorkspaceRequired) {
		t.Fatalf("got %v", err)
	}
	expectationsMet(t, mock)
}
