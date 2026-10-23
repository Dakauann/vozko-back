package carriermessaging_repository

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/carriermessaging"
	"vozko/domain/rcs"
)

var senderColumns = []string{
	"id", "workspace_id", "brand_name", "description", "logo_url", "banner_url", "color", "use_case", "sector",
	"gambling_authorization_url", "contact_name", "contact_email", "contact_phone", "website_url", "privacy_url",
	"terms_url", "cnpj", "billing_category", "status", "status_reason", "paused_by", "pause_reason", "provider",
	"provider_profile_id", "provider_request_id", "submitted_at", "launched_at", "paused_at", "created_at",
	"updated_at", "deleted_at",
	"agent_id", "workflow_id", "enable_agent_responses", "enable_workflow", "enable_analysis", "enable_auto_staging", "enable_auto_memory", "automation_disclosure",
}

type senderRow struct {
	status, billing, useCase, sector, pausedBy, pauseReason string
}

func defaultSenderRow() senderRow {
	return senderRow{status: "launched", billing: "conversational", useCase: "promotional", sector: "retail"}
}

func (s senderRow) values() []driver.Value {
	return []driver.Value{
		"s1", "ws1", "Loja", "Vozko envia promoções em nome de Loja.", "https://l/logo.png", "https://l/banner.png", "#FFFFFF",
		s.useCase, s.sector, "", "Ana", "ana@loja.com.br", "+5511999990000", "https://loja.com.br", "https://loja.com.br/p",
		"https://loja.com.br/t", "12345678000190", s.billing, s.status, "", s.pausedBy, s.pauseReason, "cm",
		"prof1", "req1", testNow, testNow, nil, testNow, testNow, nil,
		"agent1", nil, true, false, true, false, false, "Sou um assistente",
	}
}

func senderRows(rows ...senderRow) *sqlmock.Rows {
	out := sqlmock.NewRows(senderColumns)
	for _, r := range rows {
		out.AddRow(r.values()...)
	}
	return out
}

func TestSenderCreateWritesBackIDAndTimes(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectExec(`INSERT INTO "rcs_senders"`).WillReturnResult(sqlmock.NewResult(0, 1))
	s := &rcs.Sender{WorkspaceID: "ws1", BrandName: "Loja", Status: rcs.SenderDraft}
	if err := NewSenderRepository(db).Create(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if s.ID == "" || s.CreatedAt.IsZero() || s.UpdatedAt.IsZero() {
		t.Fatalf("got %+v", s)
	}
	expectationsMet(t, mock)
}

func TestSenderCreateRefusesAnUnknownStatus(t *testing.T) {
	db, mock := newMockDB(t)
	s := &rcs.Sender{WorkspaceID: "ws1", Status: "live"}
	if err := NewSenderRepository(db).Create(context.Background(), s); !errors.Is(err, errUnknownStoredValue) {
		t.Fatalf("got %v", err)
	}
	expectationsMet(t, mock)
}

func TestSenderUpdateWritesEveryFieldScopedToTheWorkspace(t *testing.T) {
	cases := []struct {
		name     string
		affected int64
		want     error
	}{
		{"updated", 1, nil},
		{"missing", 0, rcs.ErrSenderNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newMockDB(t)
			mock.ExpectExec(`UPDATE "rcs_senders" SET .*"paused_by"=.* WHERE workspace_id = \$\d+ AND "rcs_senders"."deleted_at" IS NULL AND "id" = \$\d+`).
				WillReturnResult(sqlmock.NewResult(0, tc.affected))
			s := &rcs.Sender{ID: "s1", WorkspaceID: "ws1", Status: rcs.SenderLaunched}
			if err := NewSenderRepository(db).Update(context.Background(), s); !errors.Is(err, tc.want) {
				t.Fatalf("got %v", err)
			}
			expectationsMet(t, mock)
		})
	}
}

func TestSenderFinders(t *testing.T) {
	cases := []struct {
		name  string
		query string
		args  []driver.Value
		run   func(carriermessaging.SenderRepository) (*rcs.Sender, error)
	}{
		{"by id", `SELECT \* FROM "rcs_senders" WHERE \(id = \$1 AND workspace_id = \$2\) AND "rcs_senders"."deleted_at" IS NULL`, []driver.Value{"s1", "ws1", 1},
			func(r carriermessaging.SenderRepository) (*rcs.Sender, error) {
				return r.FindByID(context.Background(), "ws1", "s1")
			}},
		{"unscoped", `SELECT \* FROM "rcs_senders" WHERE id = \$1 AND "rcs_senders"."deleted_at" IS NULL`, []driver.Value{"s1", 1},
			func(r carriermessaging.SenderRepository) (*rcs.Sender, error) {
				return r.FindByIDUnscoped(context.Background(), "s1")
			}},
		{"by profile", `SELECT \* FROM "rcs_senders" WHERE \(provider = \$1 AND provider_profile_id = \$2\) AND "rcs_senders"."deleted_at" IS NULL`, []driver.Value{"cm", "prof1", 1},
			func(r carriermessaging.SenderRepository) (*rcs.Sender, error) {
				return r.FindByProfileID(context.Background(), "cm", "prof1")
			}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newMockDB(t)
			mock.ExpectQuery(tc.query).WithArgs(tc.args...).WillReturnRows(senderRows(defaultSenderRow()))
			got, err := tc.run(NewSenderRepository(db))
			if err != nil || got.ID != "s1" || got.Status != rcs.SenderLaunched || got.BillingCategory != rcs.BillingConversational || got.CNPJ != "12345678000190" ||
				got.Automation.AgentID == nil || *got.Automation.AgentID != "agent1" || !got.Automation.EnableAgentResponses || !got.Automation.EnableAnalysis || got.Automation.Disclosure != "Sou um assistente" {
				t.Fatalf("got %+v %v", got, err)
			}
			expectationsMet(t, mock)

			db, mock = newMockDB(t)
			mock.ExpectQuery(tc.query).WillReturnRows(sqlmock.NewRows(senderColumns))
			if _, err := tc.run(NewSenderRepository(db)); !errors.Is(err, rcs.ErrSenderNotFound) {
				t.Fatalf("missing sender: %v", err)
			}
		})
	}
}

func TestSenderReadsFailClosedOnUnknownStoredValues(t *testing.T) {
	cases := []struct {
		name   string
		modify func(*senderRow)
	}{
		{"status", func(r *senderRow) { r.status = "live" }},
		{"billing", func(r *senderRow) { r.billing = "monthly" }},
		{"use case", func(r *senderRow) { r.useCase = "spam" }},
		{"sector", func(r *senderRow) { r.sector = "mining" }},
		{"paused by", func(r *senderRow) { r.pausedBy = "someone" }},
		{"pause reason", func(r *senderRow) { r.pauseReason = "mood" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newMockDB(t)
			row := defaultSenderRow()
			tc.modify(&row)
			mock.ExpectQuery(`SELECT \* FROM "rcs_senders"`).WillReturnRows(senderRows(row))
			if got, err := NewSenderRepository(db).FindByIDUnscoped(context.Background(), "s1"); !errors.Is(err, errUnknownStoredValue) || got != nil {
				t.Fatalf("got %+v %v", got, err)
			}
		})
	}
}

func TestSenderDraftsMayLeaveBrandChoicesEmpty(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "rcs_senders"`).WillReturnRows(senderRows(senderRow{status: "draft"}))
	got, err := NewSenderRepository(db).FindByIDUnscoped(context.Background(), "s1")
	if err != nil || got.Status != rcs.SenderDraft {
		t.Fatalf("got %+v %v", got, err)
	}
}

func TestSenderLists(t *testing.T) {
	paused := rcs.SenderPaused
	cases := []struct {
		name  string
		query string
		args  []driver.Value
		run   func(carriermessaging.SenderRepository) ([]*rcs.Sender, error)
	}{
		{"by workspace", `SELECT \* FROM "rcs_senders" WHERE workspace_id = \$1 AND "rcs_senders"."deleted_at" IS NULL ORDER BY created_at DESC`, []driver.Value{"ws1"},
			func(r carriermessaging.SenderRepository) ([]*rcs.Sender, error) {
				return r.ListByWorkspace(context.Background(), "ws1")
			}},
		{"awaiting provider", `SELECT \* FROM "rcs_senders" WHERE status IN \(\$1,\$2\) AND "rcs_senders"."deleted_at" IS NULL ORDER BY updated_at ASC LIMIT \$3`, []driver.Value{"submitted", "awaiting_confirmation", 20},
			func(r carriermessaging.SenderRepository) ([]*rcs.Sender, error) {
				return r.ListAwaitingProvider(context.Background(), 20)
			}},
		{"all", `SELECT \* FROM "rcs_senders" WHERE "rcs_senders"."deleted_at" IS NULL ORDER BY created_at DESC`, nil,
			func(r carriermessaging.SenderRepository) ([]*rcs.Sender, error) {
				return r.ListAll(context.Background(), nil)
			}},
		{"all by status", `SELECT \* FROM "rcs_senders" WHERE status = \$1 AND "rcs_senders"."deleted_at" IS NULL ORDER BY created_at DESC`, []driver.Value{"paused"},
			func(r carriermessaging.SenderRepository) ([]*rcs.Sender, error) {
				return r.ListAll(context.Background(), &paused)
			}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newMockDB(t)
			q := mock.ExpectQuery(tc.query)
			if tc.args != nil {
				q.WithArgs(tc.args...)
			}
			q.WillReturnRows(senderRows(defaultSenderRow(), defaultSenderRow()))
			got, err := tc.run(NewSenderRepository(db))
			if err != nil || len(got) != 2 {
				t.Fatalf("got %v %v", got, err)
			}
			expectationsMet(t, mock)
		})
	}
}

func TestSenderDeleteIsSoftAndScoped(t *testing.T) {
	cases := []struct {
		name     string
		affected int64
		want     error
	}{
		{"deleted", 1, nil},
		{"missing", 0, rcs.ErrSenderNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newMockDB(t)
			mock.ExpectExec(`UPDATE "rcs_senders" SET "deleted_at"=\$1 WHERE \(id = \$2 AND workspace_id = \$3\) AND "rcs_senders"."deleted_at" IS NULL`).
				WithArgs(sqlmock.AnyArg(), "s1", "ws1").WillReturnResult(sqlmock.NewResult(0, tc.affected))
			if err := NewSenderRepository(db).Delete(context.Background(), "ws1", "s1"); !errors.Is(err, tc.want) {
				t.Fatalf("got %v", err)
			}
			expectationsMet(t, mock)
		})
	}
}
