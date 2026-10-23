package carriermessaging_repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/gorm"

	"vozko/domain/carriermessaging"
)

var conversationColumns = []string{
	"id", "workspace_id", "sender_id", "contact_id", "conversation_status", "close_source", "close_reason",
	"close_outcome", "closed_at", "automation_enabled", "last_message_at", "last_customer_message_at",
	"last_agent_message_at", "awaiting_reply_since", "conversation_opened_at", "created_at", "updated_at", "deleted_at",
}

func conversationRows() *sqlmock.Rows {
	return sqlmock.NewRows(conversationColumns).AddRow(
		"cv1", "ws1", "s1", "ct1", "open", "", "", "", nil, true, testNow, testNow, nil, nil, testNow, testNow, testNow, nil)
}

func TestConversationFindOrCreateExisting(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "rcs_conversations" WHERE \(workspace_id = \$1 AND sender_id = \$2 AND contact_id = \$3\) AND "rcs_conversations"."deleted_at" IS NULL`).
		WithArgs("ws1", "s1", "ct1", 1).WillReturnRows(conversationRows())
	got, created, err := NewRCSConversationRepository(db).FindOrCreate(context.Background(), "ws1", "s1", "ct1")
	if err != nil || created || got.ID != "cv1" || got.Channel != carriermessaging.ChannelRCS || got.ConversationOpenedAt == nil || got.AutomationEnabled == nil || !*got.AutomationEnabled {
		t.Fatalf("got %+v %v %v", got, created, err)
	}
	expectationsMet(t, mock)
}

func TestConversationFindOrCreateReportsWhetherThisCallCreatedIt(t *testing.T) {
	cases := []struct {
		name     string
		affected int64
		want     bool
	}{
		{"won the insert", 1, true},
		{"lost the race", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newMockDB(t)
			mock.ExpectQuery(`SELECT \* FROM "rcs_conversations"`).WillReturnRows(sqlmock.NewRows(conversationColumns))
			mock.ExpectExec(`INSERT INTO "rcs_conversations" .* ON CONFLICT \("workspace_id","sender_id","contact_id"\) WHERE deleted_at IS NULL DO NOTHING`).
				WillReturnResult(sqlmock.NewResult(0, tc.affected))
			mock.ExpectQuery(`SELECT \* FROM "rcs_conversations"`).WillReturnRows(conversationRows())
			got, created, err := NewRCSConversationRepository(db).FindOrCreate(context.Background(), "ws1", "s1", "ct1")
			if err != nil || created != tc.want || got.ID != "cv1" {
				t.Fatalf("got %+v %v %v", got, created, err)
			}
			expectationsMet(t, mock)
		})
	}
}

func TestConversationFindByID(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "rcs_conversations" WHERE id = \$1`).WithArgs("cv1", 1).WillReturnRows(conversationRows())
	if got, err := NewRCSConversationRepository(db).FindByID(context.Background(), "cv1"); err != nil || got.SenderID != "s1" {
		t.Fatalf("got %+v %v", got, err)
	}
	mock.ExpectQuery(`SELECT \* FROM "rcs_conversations"`).WillReturnRows(sqlmock.NewRows(conversationColumns))
	if _, err := NewRCSConversationRepository(db).FindByID(context.Background(), "nope"); !errors.Is(err, carriermessaging.ErrConversationNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestConversationFindByIDs(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "rcs_conversations" WHERE id IN \(\$1\)`).WithArgs("cv1").WillReturnRows(conversationRows())
	got, err := NewRCSConversationRepository(db).FindByIDs(context.Background(), []string{"cv1"})
	if err != nil || len(got) != 1 {
		t.Fatalf("got %v %v", got, err)
	}
}

func TestConversationRecordClocksUseTheSharedMonotonicTouch(t *testing.T) {
	cases := []struct {
		name string
		side string
		run  func(carriermessaging.ConversationRepository) error
	}{
		{"inbound", "last_customer_message_at", func(r carriermessaging.ConversationRepository) error {
			return r.RecordInbound(context.Background(), "cv1", testNow)
		}},
		{"outbound", "last_agent_message_at", func(r carriermessaging.ConversationRepository) error {
			return r.RecordOutbound(context.Background(), "cv1", testNow)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newMockDB(t)
			mock.ExpectExec(`UPDATE "rcs_conversations" SET .*"` + tc.side + `"=GREATEST\(COALESCE\(` + tc.side + `, .*"last_message_at"=GREATEST`).
				WillReturnResult(sqlmock.NewResult(0, 1))
			if err := tc.run(NewRCSConversationRepository(db)); err != nil {
				t.Fatal(err)
			}
			expectationsMet(t, mock)

			db, mock = newMockDB(t)
			mock.ExpectExec(`UPDATE "rcs_conversations"`).WillReturnResult(sqlmock.NewResult(0, 0))
			if err := tc.run(NewRCSConversationRepository(db)); !errors.Is(err, carriermessaging.ErrConversationNotFound) {
				t.Fatalf("missing conversation: %v", err)
			}
		})
	}
}

func TestOpenBilledConversationOnlyOutsideTheWindow(t *testing.T) {
	cases := []struct {
		name     string
		affected int64
		exists   *int
		want     bool
		wantErr  error
	}{
		{"opens", 1, nil, true, nil},
		{"window still open", 0, ptr(1), false, nil},
		{"missing", 0, ptr(0), false, carriermessaging.ErrConversationNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newMockDB(t)
			mock.ExpectExec(`UPDATE "rcs_conversations" SET "conversation_opened_at"=\$1,"updated_at"=\$2 WHERE \(id = \$3 AND \(conversation_opened_at IS NULL OR conversation_opened_at <= \$4\)\) AND "rcs_conversations"."deleted_at" IS NULL`).
				WithArgs(testNow, sqlmock.AnyArg(), "cv1", testNow.Add(-24*time.Hour)).
				WillReturnResult(sqlmock.NewResult(0, tc.affected))
			if tc.exists != nil {
				mock.ExpectQuery(`SELECT count\(\*\) FROM "rcs_conversations" WHERE id = \$1`).
					WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(*tc.exists))
			}
			got, err := NewRCSConversationRepository(db).OpenBilledConversation(context.Background(), "cv1", testNow)
			if got != tc.want || !errors.Is(err, tc.wantErr) {
				t.Fatalf("got %v %v", got, err)
			}
			expectationsMet(t, mock)
		})
	}
}

func TestSMSConversationsLiveInTheirOwnTable(t *testing.T) {
	cases := []struct {
		name  string
		setup func(sqlmock.Sqlmock)
		run   func(carriermessaging.ConversationRepository) error
	}{
		{"find maps the sms channel", func(m sqlmock.Sqlmock) {
			m.ExpectQuery(`SELECT \* FROM "sms_conversations" WHERE id = \$1 AND "sms_conversations"."deleted_at" IS NULL`).WithArgs("cv1", 1).WillReturnRows(conversationRows())
		}, func(r carriermessaging.ConversationRepository) error {
			got, err := r.FindByID(context.Background(), "cv1")
			if err == nil && got.Channel != carriermessaging.ChannelSMS {
				return errors.New("channel not mapped")
			}
			return err
		}},
		{"find or create inserts into sms conversations", func(m sqlmock.Sqlmock) {
			m.ExpectQuery(`SELECT \* FROM "sms_conversations"`).WillReturnRows(sqlmock.NewRows(conversationColumns))
			m.ExpectExec(`INSERT INTO "sms_conversations" .* ON CONFLICT \("workspace_id","sender_id","contact_id"\) WHERE deleted_at IS NULL DO NOTHING`).WillReturnResult(sqlmock.NewResult(0, 1))
			m.ExpectQuery(`SELECT \* FROM "sms_conversations"`).WillReturnRows(conversationRows())
		}, func(r carriermessaging.ConversationRepository) error {
			_, created, err := r.FindOrCreate(context.Background(), "ws1", "route1", "ct1")
			if err == nil && !created {
				return errors.New("not created")
			}
			return err
		}},
		{"find by ids", func(m sqlmock.Sqlmock) {
			m.ExpectQuery(`SELECT \* FROM "sms_conversations" WHERE id IN \(\$1\) AND "sms_conversations"."deleted_at" IS NULL`).WillReturnRows(conversationRows())
		}, func(r carriermessaging.ConversationRepository) error {
			got, err := r.FindByIDs(context.Background(), []string{"cv1"})
			if err == nil && (len(got) != 1 || got[0].Channel != carriermessaging.ChannelSMS) {
				return errors.New("ids not mapped")
			}
			return err
		}},
		{"billed window", func(m sqlmock.Sqlmock) {
			m.ExpectExec(`UPDATE "sms_conversations" SET "conversation_opened_at"`).WillReturnResult(sqlmock.NewResult(0, 1))
		}, func(r carriermessaging.ConversationRepository) error {
			_, err := r.OpenBilledConversation(context.Background(), "cv1", testNow)
			return err
		}},
		{"clocks", func(m sqlmock.Sqlmock) {
			m.ExpectExec(`UPDATE "sms_conversations" SET .*"last_customer_message_at"=GREATEST`).WillReturnResult(sqlmock.NewResult(0, 1))
		}, func(r carriermessaging.ConversationRepository) error {
			return r.RecordInbound(context.Background(), "cv1", testNow)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newMockDB(t)
			tc.setup(mock)
			if err := tc.run(NewSMSConversationRepository(db)); err != nil {
				t.Fatal(err)
			}
			expectationsMet(t, mock)
		})
	}
}

func TestConversationDepartmentComesFromItsSenderOrRoute(t *testing.T) {
	cases := []struct {
		name  string
		repo  func(*gorm.DB) *ConversationRepository
		query string
		dept  any
		want  string
	}{
		{"rcs", NewRCSConversationRepository, `SELECT "c"\."department_id" FROM rcs_conversations cv JOIN rcs_senders c ON c\.id = cv\.sender_id WHERE cv\.id = \$1 LIMIT \$2`, "d1", "d1"},
		{"sms", NewSMSConversationRepository, `SELECT "c"\."department_id" FROM sms_conversations cv JOIN sms_routes c ON c\.id = cv\.sender_id WHERE cv\.id = \$1 LIMIT \$2`, nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newMockDB(t)
			mock.ExpectQuery(tc.query).WithArgs("cv1", 1).WillReturnRows(sqlmock.NewRows([]string{"department_id"}).AddRow(tc.dept))
			got, err := tc.repo(db).DepartmentIDForEntry(context.Background(), "cv1")
			if err != nil || got != tc.want {
				t.Fatalf("got %q %v", got, err)
			}
			expectationsMet(t, mock)
		})
	}
}
