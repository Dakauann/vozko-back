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

func TestMessageCreateStoresContentAndFallbackAsJSON(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectExec(`INSERT INTO "carrier_messages"`).
		WithArgs(sqlmock.AnyArg(), "ws1", "rcs", "ref1", "s1", "5511999990000", nil, "c1",
			"campaign", true, "run1", 2, `{"text":"oi"}`, "", 0, "basic", "rcs", "pending", "",
			nil, nil, `{"whatsapp":{"actor_id":"u1","business_phone_id":"bp","template_id":"t1"}}`, "chg", nil,
			nil, nil, nil, nil, nil, sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	m := &carriermessaging.Message{
		WorkspaceID: "ws1", Channel: carriermessaging.ChannelRCS, Reference: "ref1", SenderID: "s1", Phone: "5511999990000", ConversationID: "c1",
		RunKind: carriermessaging.RunCampaign, Marketing: true, RunID: "run1", Variant: 2, Content: rcs.Content{Text: "oi"}, Class: rcs.ClassBasic,
		Step: carriermessaging.StepRCS, Status: carriermessaging.StatusPending, ChargeReference: "chg",
		Fallback: carriermessaging.FallbackSpec{WhatsApp: &carriermessaging.WhatsAppFallback{ActorID: "u1", BusinessPhoneID: "bp", TemplateID: "t1"}},
	}
	if err := NewMessageRepository(db).Create(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	if m.ID == "" || m.CreatedAt.IsZero() {
		t.Fatalf("create did not write back the id and creation time: %+v", m)
	}
	expectationsMet(t, mock)
}

func TestMessageCreateStoresAnSMSWithoutABillingClass(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectExec(`INSERT INTO "carrier_messages"`).
		WithArgs(sqlmock.AnyArg(), "ws1", "sms", "ref1", "r1", "5511999990000", nil, nil,
			"operator", false, "", 0, sqlmock.AnyArg(), "Loja: oi", 1, "", "sms", "pending", "",
			nil, nil, sqlmock.AnyArg(), "", nil,
			nil, nil, nil, nil, nil, sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	m := &carriermessaging.Message{
		WorkspaceID: "ws1", Channel: carriermessaging.ChannelSMS, Reference: "ref1", SenderID: "r1", Phone: "5511999990000",
		RunKind: carriermessaging.RunOperator, Text: "Loja: oi", Segments: 1, Step: carriermessaging.StepSMS, Status: carriermessaging.StatusPending,
	}
	if err := NewMessageRepository(db).Create(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	expectationsMet(t, mock)
}

func TestMessageCreateRefusesUnknownValues(t *testing.T) {
	cases := []struct {
		name   string
		modify func(*carriermessaging.Message)
	}{
		{"status", func(m *carriermessaging.Message) { m.Status = "sent" }},
		{"step", func(m *carriermessaging.Message) { m.Step = "email" }},
		{"run kind", func(m *carriermessaging.Message) { m.RunKind = "blast" }},
		{"class", func(m *carriermessaging.Message) { m.Class = "premium" }},
		{"rcs step without class", func(m *carriermessaging.Message) { m.Class = "" }},
		{"channel", func(m *carriermessaging.Message) { m.Channel = "fax" }},
		{"missing channel", func(m *carriermessaging.Message) { m.Channel = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newMockDB(t)
			m := &carriermessaging.Message{WorkspaceID: "ws1", Channel: carriermessaging.ChannelRCS, Reference: "ref1", SenderID: "s1", Phone: "5511999990000",
				RunKind: carriermessaging.RunCampaign, Class: rcs.ClassBasic, Step: carriermessaging.StepRCS, Status: carriermessaging.StatusPending}
			tc.modify(m)
			if err := NewMessageRepository(db).Create(context.Background(), m); !errors.Is(err, errUnknownStoredValue) {
				t.Fatalf("got %v", err)
			}
			expectationsMet(t, mock)
		})
	}
}

func TestMessageReadsFailClosedOnUnknownStoredValues(t *testing.T) {
	cases := []struct {
		name   string
		modify func(*messageRow)
		want   error
	}{
		{"status", func(r *messageRow) { r.status = "sent" }, errUnknownStoredValue},
		{"step", func(r *messageRow) { r.step = "email" }, errUnknownStoredValue},
		{"run kind", func(r *messageRow) { r.runKind = "blast" }, errUnknownStoredValue},
		{"class", func(r *messageRow) { r.class = "premium" }, errUnknownStoredValue},
		{"rcs step without class", func(r *messageRow) { r.class = "" }, errUnknownStoredValue},
		{"channel", func(r *messageRow) { r.channel = "fax" }, errUnknownStoredValue},
		{"content", func(r *messageRow) { r.content = `{"text":` }, nil},
		{"fallback", func(r *messageRow) { r.fallback = `[` }, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newMockDB(t)
			row := defaultMessageRow()
			tc.modify(&row)
			mock.ExpectQuery(`SELECT \* FROM "carrier_messages" WHERE id = \$1`).WithArgs("m1", 1).WillReturnRows(messageRows(row))
			got, err := NewMessageRepository(db).FindByID(context.Background(), "m1")
			if err == nil || got != nil {
				t.Fatalf("got %+v, %v", got, err)
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestMessageFindMapsEveryField(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "carrier_messages" WHERE reference = \$1`).WithArgs("ref1", 1).WillReturnRows(messageRows(defaultMessageRow()))
	got, err := NewMessageRepository(db).FindByReference(context.Background(), "ref1")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "m1" || got.Status != carriermessaging.StatusAccepted || got.Content.Text != "oi" || got.Fallback.SMSText != "oi" ||
		got.RunKind != carriermessaging.RunCampaign || !got.Marketing || got.Channel != carriermessaging.ChannelRCS || got.Text != "Loja: oi" || got.Segments != 1 || got.Step != carriermessaging.StepRCS || got.AcceptedAt == nil || got.ExpiresAt == nil || got.LeadID != "" {
		t.Fatalf("got %+v", got)
	}
}

func TestMessageFindUnknownIsNotFound(t *testing.T) {
	cases := []struct {
		name string
		run  func(carriermessaging.MessageRepository) error
	}{
		{"by id", func(r carriermessaging.MessageRepository) error {
			_, err := r.FindByID(context.Background(), "m1")
			return err
		}},
		{"by reference", func(r carriermessaging.MessageRepository) error {
			_, err := r.FindByReference(context.Background(), "ref1")
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newMockDB(t)
			mock.ExpectQuery(`SELECT \* FROM "carrier_messages"`).WillReturnRows(sqlmock.NewRows(messageColumns))
			if err := tc.run(NewMessageRepository(db)); !errors.Is(err, carriermessaging.ErrMessageNotFound) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestAdvanceLocksTheRowAndPersistsOnlyWhenTheDomainMovesIt(t *testing.T) {
	cases := []struct {
		name      string
		stored    string
		next      carriermessaging.Status
		wantMoved bool
		want      carriermessaging.Status
	}{
		{"accepted to delivered moves", "accepted", carriermessaging.StatusDelivered, true, carriermessaging.StatusDelivered},
		{"read then delivered stays read", "read", carriermessaging.StatusDelivered, false, carriermessaging.StatusRead},
		{"delivered then failed stays delivered", "delivered", carriermessaging.StatusFailed, false, carriermessaging.StatusDelivered},
		{"accepted to failed moves", "accepted", carriermessaging.StatusFailed, true, carriermessaging.StatusFailed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newMockDB(t)
			row := defaultMessageRow()
			row.status = tc.stored
			mock.ExpectBegin()
			mock.ExpectQuery(`SELECT \* FROM "carrier_messages" WHERE reference = \$1 LIMIT \$2 FOR UPDATE`).WithArgs("ref1", 1).WillReturnRows(messageRows(row))
			if tc.wantMoved {
				mock.ExpectExec(`UPDATE "carrier_messages" SET .*"status"=\$\d.* WHERE id = \$\d+`).WillReturnResult(sqlmock.NewResult(0, 1))
			}
			mock.ExpectCommit()
			got, moved, err := NewMessageRepository(db).Advance(context.Background(), "ref1", tc.next, "E1", testNow.Add(time.Minute))
			if err != nil {
				t.Fatal(err)
			}
			if moved != tc.wantMoved || got.Status != tc.want {
				t.Fatalf("moved %v status %s, want %v %s", moved, got.Status, tc.wantMoved, tc.want)
			}
			expectationsMet(t, mock)
		})
	}
}

func TestAdvanceUnknownReferenceIsNotFound(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT \* FROM "carrier_messages" WHERE reference = \$1 LIMIT \$2 FOR UPDATE`).WillReturnRows(sqlmock.NewRows(messageColumns))
	mock.ExpectRollback()
	got, moved, err := NewMessageRepository(db).Advance(context.Background(), "nope", carriermessaging.StatusDelivered, "", testNow)
	if !errors.Is(err, carriermessaging.ErrMessageNotFound) || moved || got != nil {
		t.Fatalf("got %+v %v %v", got, moved, err)
	}
	expectationsMet(t, mock)
}

func TestAdvanceReturnsTheUpdateError(t *testing.T) {
	db, mock := newMockDB(t)
	boom := errors.New("boom")
	mock.ExpectBegin()
	mock.ExpectQuery(`FOR UPDATE`).WillReturnRows(messageRows(defaultMessageRow()))
	mock.ExpectExec(`UPDATE "carrier_messages"`).WillReturnError(boom)
	mock.ExpectRollback()
	if _, _, err := NewMessageRepository(db).Advance(context.Background(), "ref1", carriermessaging.StatusRead, "", testNow); !errors.Is(err, boom) {
		t.Fatalf("got %v", err)
	}
	expectationsMet(t, mock)
}

func TestLinkFallbackOnlyWhenUnlinked(t *testing.T) {
	cases := []struct {
		name     string
		affected int64
		exists   *int
		want     bool
		wantErr  error
	}{
		{"links", 1, nil, true, nil},
		{"already linked", 0, ptr(1), false, nil},
		{"missing message", 0, ptr(0), false, carriermessaging.ErrMessageNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newMockDB(t)
			mock.ExpectExec(`UPDATE "carrier_messages" SET "fallback_message_id"=\$1,"updated_at"=\$2 WHERE id = \$3 AND fallback_message_id IS NULL`).
				WithArgs("f1", sqlmock.AnyArg(), "m1").WillReturnResult(sqlmock.NewResult(0, tc.affected))
			if tc.exists != nil {
				mock.ExpectQuery(`SELECT count\(\*\) FROM "carrier_messages" WHERE id = \$1`).WithArgs("m1").
					WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(*tc.exists))
			}
			got, err := NewMessageRepository(db).LinkFallback(context.Background(), "m1", "f1")
			if got != tc.want || !errors.Is(err, tc.wantErr) {
				t.Fatalf("got %v %v", got, err)
			}
			expectationsMet(t, mock)
		})
	}
}

func TestListOverdueScansOpenMessagesByExpiry(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "carrier_messages" WHERE status IN \(\$1,\$2\) AND expires_at <= \$3 ORDER BY expires_at ASC LIMIT \$4`).
		WithArgs("pending", "accepted", testNow, 50).
		WillReturnRows(messageRows(defaultMessageRow()))
	got, err := NewMessageRepository(db).ListOverdue(context.Background(), testNow, 50)
	if err != nil || len(got) != 1 {
		t.Fatalf("got %v %v", got, err)
	}
	expectationsMet(t, mock)
}

func TestCountMarketingSinceCountsTopLevelMarketingSendsOfEveryChannelThatWereNotRejected(t *testing.T) {
	db, mock := newMockDB(t)
	since := testNow.Add(-24 * time.Hour)
	mock.ExpectQuery(`SELECT count\(\*\) FROM "carrier_messages" WHERE workspace_id = \$1 AND phone = \$2 AND marketing = \$3 AND parent_message_id IS NULL AND status <> \$4 AND created_at >= \$5`).
		WithArgs("ws1", "5511999990000", true, "rejected", since).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))
	got, err := NewMessageRepository(db).CountMarketingSince(context.Background(), "ws1", "5511999990000", since)
	if err != nil || got != 3 {
		t.Fatalf("got %d %v", got, err)
	}
	expectationsMet(t, mock)
}

func TestCountRunKindSince(t *testing.T) {
	db, mock := newMockDB(t)
	since := testNow.Add(-24 * time.Hour)
	mock.ExpectQuery(`SELECT count\(\*\) FROM "carrier_messages" WHERE workspace_id = \$1 AND run_kind = \$2 AND created_at >= \$3`).
		WithArgs("ws1", "test", since).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(7))
	got, err := NewMessageRepository(db).CountRunKindSince(context.Background(), "ws1", carriermessaging.RunTest, since)
	if err != nil || got != 7 {
		t.Fatalf("got %d %v", got, err)
	}
	expectationsMet(t, mock)
}

func TestLatestDeliveredTo(t *testing.T) {
	cases := []struct {
		name string
		rows *sqlmock.Rows
		want bool
	}{
		{"found", messageRows(messageRow{channel: "rcs", status: "read", step: "rcs", runKind: "campaign", class: "basic", content: `{}`, fallback: `{}`}), true},
		{"none", sqlmock.NewRows(messageColumns), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newMockDB(t)
			since := testNow.Add(-7 * 24 * time.Hour)
			mock.ExpectQuery(`SELECT \* FROM "carrier_messages" WHERE workspace_id = \$1 AND phone = \$2 AND parent_message_id IS NULL AND status IN \(\$3,\$4\) AND delivered_at >= \$5 ORDER BY delivered_at DESC LIMIT \$6`).
				WithArgs("ws1", "5511999990000", "delivered", "read", since, 1).
				WillReturnRows(tc.rows)
			got, err := NewMessageRepository(db).LatestDeliveredTo(context.Background(), "ws1", "5511999990000", since)
			if err != nil || (got != nil) != tc.want {
				t.Fatalf("got %+v %v", got, err)
			}
			expectationsMet(t, mock)
		})
	}
}

func TestLatestSMSToLooksAcrossWorkspaces(t *testing.T) {
	cases := []struct {
		name string
		rows *sqlmock.Rows
		want bool
	}{
		{"found", messageRows(messageRow{channel: "sms", status: "delivered", step: "sms", runKind: "campaign", content: `{}`, fallback: `{}`}), true},
		{"none", sqlmock.NewRows(messageColumns), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newMockDB(t)
			since := testNow.Add(-30 * 24 * time.Hour)
			mock.ExpectQuery(`SELECT \* FROM "carrier_messages" WHERE phone = \$1 AND step = \$2 AND created_at >= \$3 ORDER BY created_at DESC LIMIT \$4`).
				WithArgs("5511999990000", "sms", since, 1).
				WillReturnRows(tc.rows)
			got, err := NewMessageRepository(db).LatestSMSTo(context.Background(), "5511999990000", since)
			if err != nil || (got != nil) != tc.want {
				t.Fatalf("got %+v %v", got, err)
			}
			expectationsMet(t, mock)
		})
	}
}

func TestRecordOperator(t *testing.T) {
	cases := []struct {
		name     string
		affected int64
		want     error
	}{
		{"recorded", 1, nil},
		{"unknown reference", 0, carriermessaging.ErrMessageNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newMockDB(t)
			mock.ExpectExec(`UPDATE "carrier_messages" SET "operator"=\$1,"updated_at"=\$2 WHERE reference = \$3`).
				WithArgs("vivo", sqlmock.AnyArg(), "ref1").WillReturnResult(sqlmock.NewResult(0, tc.affected))
			if err := NewMessageRepository(db).RecordOperator(context.Background(), "ref1", " vivo "); !errors.Is(err, tc.want) {
				t.Fatalf("got %v", err)
			}
			expectationsMet(t, mock)
		})
	}
}

func TestMessageQueriesReturnRepositoryErrors(t *testing.T) {
	boom := errors.New("boom")
	cases := []struct {
		name string
		run  func(carriermessaging.MessageRepository) error
	}{
		{"find", func(r carriermessaging.MessageRepository) error {
			_, err := r.FindByID(context.Background(), "m1")
			return err
		}},
		{"overdue", func(r carriermessaging.MessageRepository) error {
			_, err := r.ListOverdue(context.Background(), testNow, 1)
			return err
		}},
		{"marketing", func(r carriermessaging.MessageRepository) error {
			_, err := r.CountMarketingSince(context.Background(), "ws1", "p", testNow)
			return err
		}},
		{"run kind", func(r carriermessaging.MessageRepository) error {
			_, err := r.CountRunKindSince(context.Background(), "ws1", carriermessaging.RunTest, testNow)
			return err
		}},
		{"latest sms", func(r carriermessaging.MessageRepository) error {
			_, err := r.LatestSMSTo(context.Background(), "p", testNow)
			return err
		}},
		{"latest", func(r carriermessaging.MessageRepository) error {
			_, err := r.LatestDeliveredTo(context.Background(), "ws1", "p", testNow)
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newMockDB(t)
			mock.ExpectQuery(`.*`).WillReturnError(boom)
			if err := tc.run(NewMessageRepository(db)); !errors.Is(err, boom) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func ptr[T any](v T) *T { return &v }
