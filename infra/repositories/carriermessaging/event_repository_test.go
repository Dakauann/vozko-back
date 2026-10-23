package carriermessaging_repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/carriermessaging"
	"vozko/domain/rcs"
)

func TestEventRecordDeduplicates(t *testing.T) {
	cases := []struct {
		name     string
		affected int64
		want     bool
	}{
		{"first delivery", 1, true},
		{"redelivery", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newMockDB(t)
			mock.ExpectExec(`INSERT INTO "carrier_message_events" .* ON CONFLICT \("dedup_key"\) DO NOTHING`).
				WithArgs(sqlmock.AnyArg(), "ws1", "m1", nil, "5511999990000", "click", -1, 2, "Comprar", "evt-1",
					testNow, sqlmock.AnyArg(), sqlmock.AnyArg()).
				WillReturnResult(sqlmock.NewResult(0, tc.affected))
			e := &carriermessaging.MessageEvent{WorkspaceID: "ws1", MessageID: "m1", Phone: "5511999990000", Kind: carriermessaging.EventClick,
				Button: &rcs.ButtonKey{Card: -1, Index: 2}, Label: "Comprar", DedupKey: "evt-1", OccurredAt: testNow}
			got, err := NewEventRepository(db).Record(context.Background(), e)
			if err != nil || got != tc.want {
				t.Fatalf("got %v %v", got, err)
			}
			expectationsMet(t, mock)
		})
	}
}

func TestEventRecordRefusesAnUnknownKindOrMissingKey(t *testing.T) {
	cases := []struct {
		name string
		e    carriermessaging.MessageEvent
	}{
		{"kind", carriermessaging.MessageEvent{WorkspaceID: "ws1", Phone: "p", Kind: "like", DedupKey: "k"}},
		{"dedup key", carriermessaging.MessageEvent{WorkspaceID: "ws1", Phone: "p", Kind: carriermessaging.EventTap}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newMockDB(t)
			if _, err := NewEventRepository(db).Record(context.Background(), &tc.e); err == nil {
				t.Fatal("expected an error")
			}
			expectationsMet(t, mock)
		})
	}
}

func TestEventCountSince(t *testing.T) {
	db, mock := newMockDB(t)
	since := testNow.Add(-time.Hour)
	mock.ExpectQuery(`SELECT count\(\*\) FROM "carrier_message_events" WHERE workspace_id = \$1 AND phone = \$2 AND kind = \$3 AND occurred_at >= \$4`).
		WithArgs("ws1", "5511999990000", "inbound", since).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(31))
	got, err := NewEventRepository(db).CountSince(context.Background(), "ws1", "5511999990000", carriermessaging.EventInbound, since)
	if err != nil || got != 31 {
		t.Fatalf("got %d %v", got, err)
	}
	expectationsMet(t, mock)
}

func TestEventRepositoryReturnsErrors(t *testing.T) {
	boom := errors.New("boom")
	db, mock := newMockDB(t)
	mock.ExpectExec(`INSERT INTO "carrier_message_events"`).WillReturnError(boom)
	if _, err := NewEventRepository(db).Record(context.Background(), &carriermessaging.MessageEvent{WorkspaceID: "ws1", Phone: "p", Kind: carriermessaging.EventTap, DedupKey: "k", OccurredAt: testNow}); !errors.Is(err, boom) {
		t.Fatalf("got %v", err)
	}
	mock.ExpectQuery(`SELECT count`).WillReturnError(boom)
	if _, err := NewEventRepository(db).CountSince(context.Background(), "ws1", "p", carriermessaging.EventTap, testNow); !errors.Is(err, boom) {
		t.Fatalf("got %v", err)
	}
}
