package whatsappform_repository

import (
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/shared"
	"vozko/domain/whatsappform"
)

func responseRowColumns() []string {
	return []string{"id", "workspace_id", "form_id", "version_id", "send_id", "origin", "match", "inbound_message_id", "conversation_id",
		"entry_id", "entry_type", "lead_id", "business_phone_id", "contact_number", "answers", "raw", "apply_status", "report", "submitted_at", "created_at"}
}

const sampleAnswersJSON = `{"values":{"f_color":{"key":"f_color","kind":"single_choice_radio","optionIds":["o_red"]}}}`

func addResponseRow(rows *sqlmock.Rows, id, workspaceID string, submitted time.Time) *sqlmock.Rows {
	return rows.AddRow(id, workspaceID, formID, versionID, sendID, "inbox", "token", "wamid.in", "", entryID, "whatsapp", leadID, phoneID,
		"5511999990000", sampleAnswersJSON, `{"f_color":"o_red"}`, "applied", `{"changed":["name"]}`, submitted, at)
}

func campaignRow(id, workspaceID string, submitted time.Time) *sqlmock.Rows {
	rows := sqlmock.NewRows(append(responseRowColumns(), "campaign_id"))
	return rows.AddRow(id, workspaceID, formID, versionID, sendID, "inbox", "token", "wamid.in", "", entryID, "whatsapp", leadID, phoneID,
		"5511999990000", sampleAnswersJSON, `{"f_color":"o_red"}`, "applied", `{"changed":["name"]}`, submitted, at, campID)
}

func sampleResponse() whatsappform.Response {
	return whatsappform.Response{
		ID: respID, WorkspaceID: wsID, FormID: formID, VersionID: versionID, SendID: sendID, Origin: whatsappform.OriginInbox,
		Match: whatsappform.MatchByToken, InboundMessageID: "wamid.in", EntryID: entryID, EntryType: "whatsapp", LeadID: leadID,
		BusinessPhoneID: phoneID, ContactNumber: "5511999990000",
		Answers:     whatsappform.Answers{Values: map[string]whatsappform.Answer{"f_color": {Key: "f_color", Kind: whatsappform.FieldRadio, OptionIDs: []string{"o_red"}}}},
		Raw:         []byte(`{"f_color":"o_red"}`),
		ApplyStatus: whatsappform.ApplyPending, SubmittedAt: at, CreatedAt: at,
	}
}

func expectResponseInsert(mock sqlmock.Sqlmock) *sqlmock.ExpectedQuery {
	return mock.ExpectQuery(exactly(insertResponseSQL)).
		WithArgs(respID, wsID, formID, versionID, sendID, "inbox", "token", "wamid.in", "", entryID, "whatsapp", leadID, phoneID, "5511999990000",
			jsonArg{sampleAnswersJSON}, jsonArg{`{"f_color":"o_red"}`}, "pending", jsonArg{`{}`}, at, at)
}

func TestResponseInsertIsIdempotentOnTheInboundMessage(t *testing.T) {
	t.Run("a new answer is stored", func(t *testing.T) {
		db, mock := newMockDB(t)
		expectResponseInsert(mock).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(respID))
		got, created, err := NewResponseStore(db).Insert(ctx, sampleResponse())
		if err != nil || !created || got.ID != respID {
			t.Fatalf("got %+v, %v, %v", got, created, err)
		}
		expectationsMet(t, mock)
	})
	t.Run("a redelivered answer returns the stored one", func(t *testing.T) {
		db, mock := newMockDB(t)
		expectResponseInsert(mock).WillReturnRows(sqlmock.NewRows([]string{"id"}))
		mock.ExpectQuery(exactly(responseByInboundSQL)).WithArgs("wamid.in").
			WillReturnRows(addResponseRow(sqlmock.NewRows(responseRowColumns()), nextID, wsID, at))
		got, created, err := NewResponseStore(db).Insert(ctx, sampleResponse())
		if err != nil || created || got.ID != nextID || got.ApplyStatus != whatsappform.ApplyApplied || len(got.Report.Changed) != 1 {
			t.Fatalf("got %+v, %v, %v", got, created, err)
		}
		expectationsMet(t, mock)
	})
	t.Run("an inbound message of another workspace is refused", func(t *testing.T) {
		db, mock := newMockDB(t)
		expectResponseInsert(mock).WillReturnRows(sqlmock.NewRows([]string{"id"}))
		mock.ExpectQuery(exactly(responseByInboundSQL)).WillReturnRows(addResponseRow(sqlmock.NewRows(responseRowColumns()), nextID, otherWsID, at))
		if _, _, err := NewResponseStore(db).Insert(ctx, sampleResponse()); err == nil {
			t.Fatal("expected a refusal")
		}
		expectationsMet(t, mock)
	})
}

func TestResponseInsertNeedsTheInboundMessage(t *testing.T) {
	db, mock := newMockDB(t)
	r := sampleResponse()
	r.InboundMessageID = ""
	if _, _, err := NewResponseStore(db).Insert(ctx, r); !errors.Is(err, whatsappform.ErrResponseInvalid) {
		t.Fatalf("err = %v", err)
	}
	expectationsMet(t, mock)
}

func TestResponseSaveApplyAndDeleteAreScopedToTheWorkspace(t *testing.T) {
	cases := []struct {
		name     string
		sql      string
		args     []driver.Value
		call     func(*ResponseStore) error
		affected int64
		want     error
	}{
		{"apply saved", saveApplySQL, []driver.Value{"partial", jsonArg{`{"refused":["email"]}`}, wsID, respID}, func(s *ResponseStore) error {
			return s.SaveApply(ctx, wsID, respID, whatsappform.ApplyPartial, whatsappform.ApplyReport{Refused: []string{"email"}})
		}, 1, nil},
		{"apply absent", saveApplySQL, []driver.Value{"partial", jsonArg{`{}`}, wsID, respID}, func(s *ResponseStore) error {
			return s.SaveApply(ctx, wsID, respID, whatsappform.ApplyPartial, whatsappform.ApplyReport{})
		}, 0, whatsappform.ErrResponseNotFound},
		{"deleted", deleteResponseSQL, []driver.Value{wsID, respID}, func(s *ResponseStore) error { return s.Delete(ctx, wsID, respID) }, 1, nil},
		{"delete absent", deleteResponseSQL, []driver.Value{wsID, respID}, func(s *ResponseStore) error { return s.Delete(ctx, wsID, respID) }, 0, whatsappform.ErrResponseNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newMockDB(t)
			mock.ExpectExec(exactly(tc.sql)).WithArgs(tc.args...).WillReturnResult(sqlmock.NewResult(0, tc.affected))
			err := tc.call(NewResponseStore(db))
			if !errors.Is(err, tc.want) || (tc.want == nil && err != nil) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			expectationsMet(t, mock)
		})
	}
}

func TestResponseGetAppliesTheReadersScope(t *testing.T) {
	from := "SELECT " + responseColumns + ", wc.id::text AS campaign_id FROM whatsapp_form_responses r" + joinsFor("r") + " WHERE r.workspace_id = ? AND r.id = ?"
	cases := []struct {
		name  string
		scope whatsappform.ReadScope
		sql   string
		args  []driver.Value
	}{
		{"unrestricted", whatsappform.ReadScope{Resolved: true}, from, []driver.Value{wsID, respID}},
		{"restricted to a department", whatsappform.ReadScope{Resolved: true, Restrict: true, DepartmentIDs: []string{deptID}},
			from + " AND wc.department_id = ANY(?::uuid[])", []driver.Value{wsID, respID, "{\"" + deptID + "\"}"}},
		{"restricted without departments", whatsappform.ReadScope{Resolved: true, Restrict: true}, from + " AND 1 = 0", []driver.Value{wsID, respID}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newMockDB(t)
			mock.ExpectQuery(exactly(tc.sql)).WithArgs(tc.args...).WillReturnRows(campaignRow(respID, wsID, at))
			got, err := NewResponseStore(db).Get(ctx, wsID, respID, tc.scope)
			if err != nil {
				t.Fatal(err)
			}
			if got.ID != respID || got.CampaignID != campID || got.Origin != whatsappform.OriginInbox || got.Match != whatsappform.MatchByToken ||
				got.Answers.Values["f_color"].OptionIDs[0] != "o_red" || string(got.Raw) != `{"f_color":"o_red"}` {
				t.Fatalf("got %+v", got)
			}
			expectationsMet(t, mock)
		})
	}
}

func TestResponseGetRefusalsAndMisses(t *testing.T) {
	cases := []struct {
		name       string
		responseID string
		scope      whatsappform.ReadScope
		query      bool
		want       error
	}{
		{"an unresolved scope is forbidden", respID, whatsappform.ReadScope{}, false, whatsappform.ErrForbidden},
		{"no row", respID, whatsappform.ReadScope{Resolved: true}, true, whatsappform.ErrResponseNotFound},
		{"not a uuid", "r-1", whatsappform.ReadScope{Resolved: true}, false, whatsappform.ErrResponseNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newMockDB(t)
			if tc.query {
				mock.ExpectQuery(`FROM whatsapp_form_responses r`).WillReturnRows(sqlmock.NewRows(responseRowColumns()))
			}
			if _, err := NewResponseStore(db).Get(ctx, wsID, tc.responseID, tc.scope); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			expectationsMet(t, mock)
		})
	}
}

func TestResponseListAppliesEveryFilter(t *testing.T) {
	from, to := at.Add(-48*time.Hour), at
	cursor := shared.Keyset{At: at, ID: respID}.Encode()
	base := "SELECT " + responseColumns + ", wc.id::text AS campaign_id FROM whatsapp_form_responses r" + joinsFor("r")
	cases := []struct {
		name  string
		query whatsappform.ResponseQuery
		sql   string
		args  []driver.Value
	}{
		{
			name:  "no filters",
			query: whatsappform.ResponseQuery{WorkspaceID: wsID, FormID: formID, Limit: 2, Scope: whatsappform.ReadScope{Resolved: true}},
			sql:   base + " WHERE r.workspace_id = ? AND r.form_id = ? ORDER BY r.submitted_at DESC, r.id DESC LIMIT ?",
			args:  []driver.Value{wsID, formID, int64(3)},
		},
		{
			name: "every filter",
			query: whatsappform.ResponseQuery{
				WorkspaceID: wsID, FormID: formID, VersionIDs: []string{versionID}, From: &from, To: &to,
				Origins: []whatsappform.SendOrigin{whatsappform.OriginInbox, whatsappform.OriginCampaign}, PhoneIDs: []string{phoneID},
				CampaignID: campID, Match: whatsappform.MatchFilterMatched, Search: "José", After: cursor, Limit: 2,
				Scope: whatsappform.ReadScope{Resolved: true, Restrict: true, DepartmentIDs: []string{deptID}},
			},
			sql: base + leadJoinSQL + " WHERE r.workspace_id = ? AND r.form_id = ?" +
				" AND r.version_id = ANY(?::uuid[]) AND r.submitted_at >= ? AND r.submitted_at <= ? AND r.origin = ANY(?)" +
				" AND r.business_phone_id = ANY(?::uuid[]) AND wce.campaign_id = ? AND r.match IN ('token', 'context')" +
				" AND (r.contact_number LIKE ? OR vozko_fold(l.name) LIKE vozko_fold(?))" +
				" AND wc.department_id = ANY(?::uuid[])" +
				" AND (r.submitted_at, r.id) < (?, ?::uuid) ORDER BY r.submitted_at DESC, r.id DESC LIMIT ?",
			args: []driver.Value{wsID, formID, "{\"" + versionID + "\"}", from, to, `{"inbox","campaign"}`, "{\"" + phoneID + "\"}", campID,
				"%José%", "%José%", "{\"" + deptID + "\"}", at, respID, int64(3)},
		},
		{
			name:  "unmatched only",
			query: whatsappform.ResponseQuery{WorkspaceID: wsID, FormID: formID, Match: whatsappform.MatchFilterUnmatched, Limit: 2, Scope: whatsappform.ReadScope{Resolved: true}},
			sql:   base + " WHERE r.workspace_id = ? AND r.form_id = ? AND r.match NOT IN ('token', 'context') ORDER BY r.submitted_at DESC, r.id DESC LIMIT ?",
			args:  []driver.Value{wsID, formID, int64(3)},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newMockDB(t)
			rows := sqlmock.NewRows(responseRowColumns())
			addResponseRow(addResponseRow(addResponseRow(rows, respID, wsID, at), nextID, wsID, at.Add(-time.Minute)), sendID, wsID, at.Add(-2*time.Minute))
			mock.ExpectQuery(exactly(tc.sql)).WithArgs(tc.args...).WillReturnRows(rows)
			page, err := NewResponseStore(db).List(ctx, tc.query)
			if err != nil {
				t.Fatal(err)
			}
			if len(page.Items) != 2 || page.Next != (shared.Keyset{At: at.Add(-time.Minute), ID: nextID}).Encode() {
				t.Fatalf("got %+v", page)
			}
			expectationsMet(t, mock)
		})
	}
}

func TestResponseListRefusesOrEmptiesBeforeAnySQL(t *testing.T) {
	resolved := whatsappform.ReadScope{Resolved: true}
	cases := []struct {
		name  string
		query whatsappform.ResponseQuery
		want  error
	}{
		{"an unresolved scope", whatsappform.ResponseQuery{WorkspaceID: wsID, FormID: formID}, whatsappform.ErrForbidden},
		{"a cursor this server did not give", whatsappform.ResponseQuery{WorkspaceID: wsID, FormID: formID, After: "x", Scope: resolved}, whatsappform.ErrListQueryInvalid},
		{"an unknown origin", whatsappform.ResponseQuery{WorkspaceID: wsID, FormID: formID, Origins: []whatsappform.SendOrigin{"fax"}, Scope: resolved}, whatsappform.ErrListQueryInvalid},
		{"a campaign that cannot exist", whatsappform.ResponseQuery{WorkspaceID: wsID, FormID: formID, CampaignID: "c-1", Scope: resolved}, nil},
		{"versions that cannot exist", whatsappform.ResponseQuery{WorkspaceID: wsID, FormID: formID, VersionIDs: []string{"v-1"}, Scope: resolved}, nil},
		{"numbers that cannot exist", whatsappform.ResponseQuery{WorkspaceID: wsID, FormID: formID, PhoneIDs: []string{"p-1"}, Scope: resolved}, nil},
		{"a form that cannot exist", whatsappform.ResponseQuery{WorkspaceID: wsID, FormID: "f-1", Scope: resolved}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newMockDB(t)
			page, err := NewResponseStore(db).List(ctx, tc.query)
			if !errors.Is(err, tc.want) || (tc.want == nil && (err != nil || len(page.Items) != 0)) {
				t.Fatalf("got %+v, %v, want %v", page, err, tc.want)
			}
			expectationsMet(t, mock)
		})
	}
}
