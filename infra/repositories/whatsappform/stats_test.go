package whatsappform_repository

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/shared"
	"vozko/domain/whatsappform"
)

func TestStatsCountsSendsResponsesAndTheMedianWithinTheScope(t *testing.T) {
	from, to := at.Add(-72*time.Hour), at
	restricted := whatsappform.ReadScope{Resolved: true, Restrict: true, DepartmentIDs: []string{deptID}}
	scopeSQL := " AND wc.department_id = ANY(?::uuid[])"
	dept := "{\"" + deptID + "\"}"
	db, mock := newMockDB(t)

	mock.ExpectQuery(exactly("SELECT s.origin, to_char(s.created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD') AS day, s.status, COUNT(*) AS sends,"+
		" COUNT(*) FILTER (WHERE EXISTS (SELECT 1 FROM whatsapp_message_status_events e WHERE e.message_id = s.message_id AND e.status IN ('delivered', 'read'))) AS delivered,"+
		" COUNT(*) FILTER (WHERE EXISTS (SELECT 1 FROM whatsapp_message_status_events e WHERE e.message_id = s.message_id AND e.status = 'read')) AS read_count"+
		" FROM whatsapp_form_sends s"+joinsFor("s")+
		" WHERE s.workspace_id = ? AND s.form_id = ? AND s.created_at >= ? AND s.created_at <= ?"+scopeSQL+" GROUP BY 1, 2, 3")).
		WithArgs(wsID, formID, from, to, dept).
		WillReturnRows(sqlmock.NewRows([]string{"origin", "day", "status", "sends", "delivered", "read_count"}).
			AddRow("inbox", "2026-10-18", "sent", 3, 2, 1).
			AddRow("inbox", "2026-10-18", "responded", 2, 2, 2).
			AddRow("campaign", "2026-10-19", "failed", 1, 0, 0).
			AddRow("campaign", "2026-10-19", "prepared", 1, 0, 0))

	mock.ExpectQuery(exactly("SELECT r.origin, to_char(r.submitted_at AT TIME ZONE 'UTC', 'YYYY-MM-DD') AS day, COUNT(*) AS responses"+
		" FROM whatsapp_form_responses r"+joinsFor("r")+
		" WHERE r.workspace_id = ? AND r.form_id = ? AND r.match IN ('token', 'context') AND r.submitted_at >= ? AND r.submitted_at <= ?"+scopeSQL+
		" GROUP BY 1, 2")).
		WithArgs(wsID, formID, from, to, dept).
		WillReturnRows(sqlmock.NewRows([]string{"origin", "day", "responses"}).
			AddRow("inbox", "2026-10-18", 2).
			AddRow("campaign", "2026-10-20", 1))

	mock.ExpectQuery(exactly("SELECT percentile_cont(0.5) WITHIN GROUP (ORDER BY EXTRACT(EPOCH FROM (r.submitted_at - s.sent_at))) AS median"+
		" FROM whatsapp_form_responses r JOIN whatsapp_form_sends s ON s.id = r.send_id AND s.workspace_id = r.workspace_id"+joinsFor("r")+
		" WHERE r.workspace_id = ? AND r.form_id = ? AND r.match IN ('token', 'context') AND r.submitted_at >= ? AND r.submitted_at <= ?"+scopeSQL+
		" AND s.sent_at IS NOT NULL")).
		WithArgs(wsID, formID, from, to, dept).
		WillReturnRows(sqlmock.NewRows([]string{"median"}).AddRow(90.5))

	got, err := NewResponseStore(db).Stats(ctx, whatsappform.StatsQuery{WorkspaceID: wsID, FormID: formID, From: &from, To: &to, Scope: restricted})
	if err != nil {
		t.Fatal(err)
	}
	median := 90.5
	want := whatsappform.Stats{
		Sends:     map[whatsappform.SendStatus]int64{whatsappform.SendSent: 3, whatsappform.SendResponded: 2, whatsappform.SendFailed: 1, whatsappform.SendPrepared: 1},
		Delivered: 4, Read: 3, Responses: 3, MedianSecondsToRespond: &median,
		ByOrigin: []whatsappform.OriginStat{{Origin: whatsappform.OriginInbox, Sent: 5, Responses: 2}, {Origin: whatsappform.OriginCampaign, Sent: 0, Responses: 1}},
		ByDay:    []whatsappform.DayStat{{Day: "2026-10-18", Sent: 5, Responses: 2}, {Day: "2026-10-19"}, {Day: "2026-10-20", Responses: 1}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	expectationsMet(t, mock)
}

func TestStatsWithoutDataIsEmptyNotNull(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(`FROM whatsapp_form_sends s`).WithArgs(wsID, formID).WillReturnRows(sqlmock.NewRows([]string{"origin", "day", "status", "sends", "delivered", "read_count"}))
	mock.ExpectQuery(`COUNT\(\*\) AS responses`).WithArgs(wsID, formID).WillReturnRows(sqlmock.NewRows([]string{"origin", "day", "responses"}))
	mock.ExpectQuery(`percentile_cont`).WithArgs(wsID, formID).WillReturnRows(sqlmock.NewRows([]string{"median"}).AddRow(nil))
	got, err := NewResponseStore(db).Stats(ctx, whatsappform.StatsQuery{WorkspaceID: wsID, FormID: formID, Scope: whatsappform.ReadScope{Resolved: true}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Sends == nil || got.ByOrigin == nil || got.ByDay == nil || got.MedianSecondsToRespond != nil || got.Responses != 0 {
		t.Fatalf("got %+v", got)
	}
	expectationsMet(t, mock)
}

func TestDistributionCountsTheChosenOptionsOfMatchedResponses(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectQuery(exactly("SELECT o.option_id, COUNT(DISTINCT r.id) AS responses FROM whatsapp_form_responses r"+joinsFor("r")+
		" CROSS JOIN LATERAL (SELECT jsonb_extract_path(r.answers, 'values', ?::text, 'optionIds') AS ids) a"+
		" CROSS JOIN LATERAL jsonb_array_elements_text(CASE WHEN jsonb_typeof(a.ids) = 'array' THEN a.ids ELSE '[]'::jsonb END) AS o(option_id)"+
		" WHERE r.workspace_id = ? AND r.form_id = ? AND r.match IN ('token', 'context') GROUP BY o.option_id")).
		WithArgs("f_color", wsID, formID).
		WillReturnRows(sqlmock.NewRows([]string{"option_id", "responses"}).AddRow("o_red", 3).AddRow("o_blue", 1))
	got, err := NewResponseStore(db).Distribution(ctx, whatsappform.StatsQuery{WorkspaceID: wsID, FormID: formID, Scope: whatsappform.ReadScope{Resolved: true}}, "f_color")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, map[string]int64{"o_red": 3, "o_blue": 1}) {
		t.Fatalf("got %v", got)
	}
	expectationsMet(t, mock)
}

func TestStatsAndDistributionRefuseBeforeAnySQL(t *testing.T) {
	later := at.Add(time.Hour)
	cases := []struct {
		name  string
		query whatsappform.StatsQuery
		key   string
		want  error
	}{
		{"an unresolved scope", whatsappform.StatsQuery{WorkspaceID: wsID, FormID: formID}, "f_color", whatsappform.ErrForbidden},
		{"a range that ends before it starts", whatsappform.StatsQuery{WorkspaceID: wsID, FormID: formID, From: &later, To: &at, Scope: whatsappform.ReadScope{Resolved: true}}, "f_color", whatsappform.ErrListQueryInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newMockDB(t)
			store := NewResponseStore(db)
			if _, err := store.Stats(ctx, tc.query); !errors.Is(err, tc.want) {
				t.Fatalf("stats err = %v, want %v", err, tc.want)
			}
			if _, err := store.Distribution(ctx, tc.query, tc.key); !errors.Is(err, tc.want) {
				t.Fatalf("distribution err = %v, want %v", err, tc.want)
			}
			expectationsMet(t, mock)
		})
	}
	t.Run("a distribution needs a field", func(t *testing.T) {
		db, mock := newMockDB(t)
		if _, err := NewResponseStore(db).Distribution(ctx, whatsappform.StatsQuery{WorkspaceID: wsID, FormID: formID, Scope: whatsappform.ReadScope{Resolved: true}}, " "); !errors.Is(err, whatsappform.ErrListQueryInvalid) {
			t.Fatalf("err = %v", err)
		}
		expectationsMet(t, mock)
	})
}

func TestBuiltQueriesBindOneArgumentPerPlaceholder(t *testing.T) {
	from, to := at.Add(-time.Hour), at
	full := whatsappform.ReadScope{Resolved: true, Restrict: true, DepartmentIDs: []string{deptID}, AssigneeUserID: userID}
	q := whatsappform.StatsQuery{WorkspaceID: wsID, FormID: formID, From: &from, To: &to, Scope: full}
	for name, build := range map[string]func() (string, []interface{}){
		"send stats":     func() (string, []interface{}) { sql, args, _ := sendStatsQuery(q); return sql, args },
		"response stats": func() (string, []interface{}) { sql, args, _ := responseStatsQuery(q); return sql, args },
		"median":         func() (string, []interface{}) { sql, args, _ := medianQuery(q); return sql, args },
		"distribution":   func() (string, []interface{}) { sql, args, _ := distributionQuery(q, "f_x"); return sql, args },
		"send list": func() (string, []interface{}) {
			sql, args, _ := sendListQuery(whatsappform.SendQuery{WorkspaceID: wsID, FormID: formID, Scope: full}, &shared.Keyset{At: at, ID: sendID}, 10)
			return sql, args
		},
		"response list": func() (string, []interface{}) {
			sql, args, _ := responseListQuery(whatsappform.ResponseQuery{
				WorkspaceID: wsID, FormID: formID, VersionIDs: []string{versionID}, From: &from, To: &to, Origins: []whatsappform.SendOrigin{whatsappform.OriginInbox},
				PhoneIDs: []string{phoneID}, CampaignID: campID, Match: whatsappform.MatchFilterMatched, Search: "a?b", Limit: 10, Scope: full,
			}, &shared.Keyset{At: at, ID: respID})
			return sql, args
		},
	} {
		t.Run(name, func(t *testing.T) {
			sql, args := build()
			placeholdersMatch(t, sql, args)
		})
	}
	for name, sql := range map[string]string{
		"insert form": insertFormSQL, "insert version": insertVersionSQL, "insert publication": insertPublicationSQL,
		"save publication": savePublicationSQL, "insert send": insertSendSQL, "update send": updateSendSQL, "insert response": insertResponseSQL,
		"list forms": listFormsSQL + listFormsSearchSQL,
	} {
		t.Run(name, func(t *testing.T) {
			if want := map[string]int{
				"insert form": 11, "insert version": 12, "insert publication": 16, "save publication": 13, "insert send": 23,
				"update send": 7, "insert response": 20, "list forms": 2,
			}[name]; want != strings.Count(sql, "?") {
				t.Fatalf("%s binds %d placeholders, want %d", name, strings.Count(sql, "?"), want)
			}
		})
	}
}
