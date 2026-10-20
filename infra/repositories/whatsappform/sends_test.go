package whatsappform_repository

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/shared"
	"vozko/domain/whatsappform"
)

const (
	entryID = "0b0e7c1a-0000-4000-8000-0000000000e9"
	leadID  = "0b0e7c1a-0000-4000-8000-0000000000f9"
	token   = "vf1_AAAAAAAAAAAAAAAAAAAAAAAAAA"
)

func sendRowColumns() []string {
	return []string{"id", "workspace_id", "form_id", "version_id", "publication_id", "flow_token", "origin", "origin_ref", "entry_id", "entry_type",
		"conversation_id", "lead_id", "business_phone_id", "contact_number", "message_id", "template_id", "workflow_run_id", "sent_by",
		"status", "failure_reason", "created_at", "sent_at", "responded_at"}
}

func addSendRow(rows *sqlmock.Rows, id string, created time.Time) *sqlmock.Rows {
	return rows.AddRow(id, wsID, formID, versionID, pubID, token, "inbox", "", entryID, "whatsapp", "", nil, phoneID, "5511999990000",
		"wamid.1", nil, nil, userID, "sent", "", created, at, nil)
}

func sampleSend() whatsappform.Send {
	return whatsappform.Send{
		ID: sendID, WorkspaceID: wsID, FormID: formID, VersionID: versionID, PublicationID: pubID, Token: token,
		Origin: whatsappform.OriginInbox, EntryID: entryID, EntryType: "whatsapp", BusinessPhoneID: phoneID,
		ContactNumber: "5511999990000", SentBy: userID, Status: whatsappform.SendPrepared, CreatedAt: at,
	}
}

func TestSendInsertWritesEveryColumn(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectExec(exactly(insertSendSQL)).
		WithArgs(sendID, wsID, formID, versionID, pubID, token, "inbox", "", entryID, "whatsapp", "", nil, phoneID, "5511999990000",
			nil, nil, nil, userID, "prepared", "", at, nil, nil).
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := NewSendStore(db).Insert(ctx, sampleSend()); err != nil {
		t.Fatal(err)
	}
	expectationsMet(t, mock)
}

func TestSendInsertNeedsItsOwnToken(t *testing.T) {
	db, mock := newMockDB(t)
	s := sampleSend()
	s.Token = " "
	if err := NewSendStore(db).Insert(ctx, s); !errors.Is(err, whatsappform.ErrTokenRequired) {
		t.Fatalf("err = %v", err)
	}
	expectationsMet(t, mock)
}

func TestSendUpdateWritesTheDeliveryColumns(t *testing.T) {
	cases := []struct {
		name     string
		affected int64
		want     error
	}{
		{"updated", 1, nil},
		{"absent", 0, whatsappform.ErrSendNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newMockDB(t)
			s := sampleSend()
			if err := s.MarkSent("wamid.1", at); err != nil {
				t.Fatal(err)
			}
			mock.ExpectExec(exactly(updateSendSQL)).WithArgs("sent", "wamid.1", "", at, nil, wsID, sendID).
				WillReturnResult(sqlmock.NewResult(0, tc.affected))
			err := NewSendStore(db).Update(ctx, s)
			if !errors.Is(err, tc.want) || (tc.want == nil && err != nil) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			expectationsMet(t, mock)
		})
	}
}

func TestSendFinds(t *testing.T) {
	cases := []struct {
		name string
		sql  string
		arg  string
		call func(*SendStore, string) (*whatsappform.Send, error)
	}{
		{"by token", findSendByTokenSQL, token, func(s *SendStore, v string) (*whatsappform.Send, error) { return s.FindByToken(ctx, v) }},
		{"by message id", findSendByMessageSQL, "wamid.1", func(s *SendStore, v string) (*whatsappform.Send, error) { return s.FindByMessageID(ctx, v) }},
	}
	for _, tc := range cases {
		t.Run(tc.name+" found", func(t *testing.T) {
			db, mock := newMockDB(t)
			mock.ExpectQuery(exactly(tc.sql)).WithArgs(tc.arg).WillReturnRows(addSendRow(sqlmock.NewRows(sendRowColumns()), sendID, at))
			got, err := tc.call(NewSendStore(db), tc.arg)
			if err != nil {
				t.Fatal(err)
			}
			if got.ID != sendID || got.Token != token || got.MessageID != "wamid.1" || got.Status != whatsappform.SendSent || got.SentAt == nil || got.RespondedAt != nil || got.LeadID != "" {
				t.Fatalf("got %+v", got)
			}
			expectationsMet(t, mock)
		})
		t.Run(tc.name+" absent", func(t *testing.T) {
			db, mock := newMockDB(t)
			mock.ExpectQuery(exactly(tc.sql)).WillReturnRows(sqlmock.NewRows(sendRowColumns()))
			if _, err := tc.call(NewSendStore(db), tc.arg); !errors.Is(err, whatsappform.ErrSendNotFound) {
				t.Fatalf("err = %v", err)
			}
			expectationsMet(t, mock)
		})
		t.Run(tc.name+" blank", func(t *testing.T) {
			db, mock := newMockDB(t)
			if _, err := tc.call(NewSendStore(db), " "); !errors.Is(err, whatsappform.ErrSendNotFound) {
				t.Fatalf("err = %v", err)
			}
			expectationsMet(t, mock)
		})
	}
}

const officialJoinsSQL = " LEFT JOIN whatsapp_campaign_entries wce ON wce.id = %s.entry_id AND %s.entry_type = 'whatsapp'" +
	" LEFT JOIN whatsapp_campaigns wc ON wc.id = wce.campaign_id AND wc.workspace_id = %s.workspace_id"

func TestSendListPagesWithinTheReadersScope(t *testing.T) {
	cursor := shared.Keyset{At: at, ID: sendID}.Encode()
	sendsFrom := "SELECT " + sendColumns + " FROM whatsapp_form_sends s" + joinsFor("s")
	cases := []struct {
		name  string
		query whatsappform.SendQuery
		sql   string
		args  []driver.Value
	}{
		{
			name:  "an unrestricted first page",
			query: whatsappform.SendQuery{WorkspaceID: wsID, FormID: formID, Limit: 2, Scope: whatsappform.ReadScope{Resolved: true}},
			sql:   sendsFrom + " WHERE s.workspace_id = ? AND s.form_id = ? ORDER BY s.created_at DESC, s.id DESC LIMIT ?",
			args:  []driver.Value{wsID, formID, int64(3)},
		},
		{
			name: "a restricted later page",
			query: whatsappform.SendQuery{WorkspaceID: wsID, FormID: formID, Limit: 2, After: cursor,
				Scope: whatsappform.ReadScope{Resolved: true, Restrict: true, DepartmentIDs: []string{deptID}, AssigneeUserID: userID}},
			sql: sendsFrom + " WHERE s.workspace_id = ? AND s.form_id = ?" +
				" AND (wc.department_id = ANY(?::uuid[]) OR EXISTS (SELECT 1 FROM inbox_assignments ia_d WHERE ia_d.entry_id = wce.id AND ia_d.assigned_user_id = ?))" +
				" AND (s.created_at, s.id) < (?, ?::uuid) ORDER BY s.created_at DESC, s.id DESC LIMIT ?",
			args: []driver.Value{wsID, formID, "{\"" + deptID + "\"}", userID, at, sendID, int64(3)},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newMockDB(t)
			later := at.Add(-time.Minute)
			mock.ExpectQuery(exactly(tc.sql)).WithArgs(tc.args...).
				WillReturnRows(addSendRow(addSendRow(addSendRow(sqlmock.NewRows(sendRowColumns()), sendID, at), nextID, later), respID, later.Add(-time.Minute)))
			page, err := NewSendStore(db).List(ctx, tc.query)
			if err != nil {
				t.Fatal(err)
			}
			if len(page.Items) != 2 || page.Next != (shared.Keyset{At: later, ID: nextID}).Encode() {
				t.Fatalf("got %+v", page)
			}
			expectationsMet(t, mock)
		})
	}
}

func TestSendListLastPageHasNoCursor(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(`FROM whatsapp_form_sends s`).WillReturnRows(addSendRow(sqlmock.NewRows(sendRowColumns()), sendID, at))
	page, err := NewSendStore(db).List(ctx, whatsappform.SendQuery{WorkspaceID: wsID, FormID: formID, Scope: whatsappform.ReadScope{Resolved: true}})
	if err != nil || len(page.Items) != 1 || page.Next != "" {
		t.Fatalf("got %+v, %v", page, err)
	}
	expectationsMet(t, mock)
}

func TestSendListRefusesBeforeAnySQL(t *testing.T) {
	cases := []struct {
		name  string
		query whatsappform.SendQuery
		want  error
	}{
		{"an unresolved scope", whatsappform.SendQuery{WorkspaceID: wsID, FormID: formID}, whatsappform.ErrForbidden},
		{"a cursor this server did not give", whatsappform.SendQuery{WorkspaceID: wsID, FormID: formID, After: "nope", Scope: whatsappform.ReadScope{Resolved: true}}, whatsappform.ErrListQueryInvalid},
		{"a page above the maximum", whatsappform.SendQuery{WorkspaceID: wsID, FormID: formID, Limit: whatsappform.MaxPageSize + 1, Scope: whatsappform.ReadScope{Resolved: true}}, whatsappform.ErrListQueryInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newMockDB(t)
			if _, err := NewSendStore(db).List(ctx, tc.query); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			expectationsMet(t, mock)
		})
	}
}

func TestOfficialJoinsReadTheCampaignDepartment(t *testing.T) {
	for _, alias := range []string{"s", "r"} {
		want := fmt.Sprintf(officialJoinsSQL, alias, alias, alias)
		if got := joinsFor(alias); got != want {
			t.Fatalf("joins(%s) = %q, want %q", alias, got, want)
		}
	}
}
