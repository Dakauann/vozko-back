package opportunity_repository

import (
	"context"
	"database/sql/driver"
	"errors"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/opportunity"
	"vozko/domain/shared"
	crmfiltersql "vozko/infra/repositories/crmfilter"
)

func placeholdersIn(sql string) int {
	return strings.Count(sql, "?")
}

func TestTheDealScopeConditionIsTheBoardRule(t *testing.T) {
	cases := []struct {
		name  string
		scope opportunity.DealScope
		sql   string
		args  int
	}{
		{"no restriction", opportunity.DealScope{}, "", 0},
		{"restricted to nothing", opportunity.DealScope{Restrict: true}, "1 = 0", 0},
		{"own deals only", opportunity.DealScope{Restrict: true, AssigneeOverride: "u-1"}, "(d.owner_id = ?)", 1},
		{"own deals and the department's", opportunity.DealScope{Restrict: true, AssigneeOverride: "u-1", DepartmentIDs: []string{"dep-1"}}, "(d.owner_id = ? OR EXISTS (SELECT 1 FROM workspace_department_members wdm JOIN workspace_members wm ON wm.id = wdm.member_id WHERE wm.user_id = d.owner_id AND wdm.department_id = ANY(?::uuid[])))", 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sql, args := DealScopeCondition("d", tc.scope)
			if sql != tc.sql || len(args) != tc.args || placeholdersIn(sql) != len(args) {
				t.Fatalf("condition = %q with %d args", sql, len(args))
			}
		})
	}
}

func TestTheDealsOfALeadAreItsOwnAndTheOnesLinkedToItsConversations(t *testing.T) {
	membership := "d.id IN (SELECT od.id FROM opportunities od WHERE od.workspace_id = ? AND od.lead_id = ? AND od.deleted_at IS NULL" +
		" UNION SELECT oc.opportunity_id FROM opportunity_conversations oc JOIN " + crmfiltersql.LeadEntriesSource() +
		" ON lead_entries.entry_id = oc.entry_id AND lead_entries.entry_type = oc.entry_type WHERE lead_entries.lead_id = ?)"
	cases := []struct {
		name  string
		scope opportunity.DealScope
		sql   string
		args  []interface{}
	}{
		{"every deal of the lead", opportunity.DealScope{}, "d.workspace_id = ? AND d.deleted_at IS NULL AND " + membership,
			[]interface{}{"ws-1", "ws-1", "lead-1", "lead-1"}},
		{"inside the viewer's scope", opportunity.DealScope{Restrict: true, AssigneeOverride: "u-1"},
			"d.workspace_id = ? AND d.deleted_at IS NULL AND " + membership + " AND (d.owner_id = ?)",
			[]interface{}{"ws-1", "ws-1", "lead-1", "lead-1", "u-1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sql, args := LeadDealsCondition("d", "ws-1", "lead-1", tc.scope)
			if sql != tc.sql {
				t.Fatalf("condition = %q\nwant %q", sql, tc.sql)
			}
			if placeholdersIn(sql) != len(args) || !reflect.DeepEqual(args, tc.args) {
				t.Fatalf("args = %v for %d placeholders", args, placeholdersIn(sql))
			}
		})
	}
}

func TestDealsOfLeadPinsItsQuery(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	reader := NewLeadDealReader(db)
	before := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	cursorID := "6f1c2d3e-4b5a-4c6d-8e7f-9a0b1c2d3e4f"

	mock.ExpectQuery(`^SELECT o\.\* FROM opportunities o WHERE o\.workspace_id = \$1 AND o\.deleted_at IS NULL AND o\.id IN \(SELECT od\.id FROM opportunities od .*\) AND \(o\.owner_id = \$5\) AND \(o\.created_at, o\.id\) < \(\$6, \$7::uuid\) ORDER BY o\.created_at DESC, o\.id DESC LIMIT \$8$`).
		WithArgs("ws-1", "ws-1", "lead-1", "lead-1", "u-1", before, cursorID, 11).
		WillReturnRows(sqlmock.NewRows([]string{"id", "workspace_id", "lead_id", "pipeline_id", "stage_id", "owner_id", "owner_kind", "title", "status", "created_at"}).
			AddRow("d-1", "ws-1", "lead-1", "p-1", "s-1", "u-1", "human", "Matrícula", "open", before.Add(-time.Hour)))

	deals, err := reader.DealsOfLead(context.Background(), opportunity.LeadDealsQuery{
		WorkspaceID: "ws-1", LeadID: "lead-1", Limit: 11,
		Scope:  opportunity.DealScope{Restrict: true, AssigneeOverride: "u-1"},
		Before: &shared.Keyset{At: before, ID: cursorID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(deals) != 1 || deals[0].ID != "d-1" || deals[0].OwnerID != "u-1" || deals[0].LeadID != "lead-1" {
		t.Fatalf("deals = %+v", deals)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDealsOfLeadRefusesWhatItCannotAnswerWithoutQuerying(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	reader := NewLeadDealReader(db)
	cases := map[string]opportunity.LeadDealsQuery{
		"no workspace":     {LeadID: "lead-1", Limit: 10},
		"no lead":          {WorkspaceID: "ws-1", Limit: 10},
		"no limit":         {WorkspaceID: "ws-1", LeadID: "lead-1"},
		"a foreign cursor": {WorkspaceID: "ws-1", LeadID: "lead-1", Limit: 10, Before: &shared.Keyset{At: time.Now(), ID: "deal:1"}},
	}
	for name, q := range cases {
		if _, err := reader.DealsOfLead(context.Background(), q); !errors.Is(err, ErrLeadDealsQueryInvalid) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTheFirstPageOfDealsHasNoCursorBound(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectQuery(`^SELECT o\.\* FROM opportunities o WHERE .* ORDER BY o\.created_at DESC, o\.id DESC LIMIT \$5$`).
		WithArgs("ws-1", "ws-1", "lead-1", "lead-1", 31).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	if _, err := NewLeadDealReader(db).DealsOfLead(context.Background(), opportunity.LeadDealsQuery{WorkspaceID: "ws-1", LeadID: "lead-1", Limit: 31}); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	if regexp.MustCompile(`created_at, o\.id\) <`).MatchString(dealsOfLeadSQL("o.id IN (x)", false)) {
		t.Fatal("a first page must not carry a keyset bound")
	}
}

func numberedPlaceholders(sql string) string {
	var out strings.Builder
	n := 0
	for _, r := range sql {
		if r == '?' {
			n++
			out.WriteString("$" + strconv.Itoa(n))
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
}

func TestCountDealsOfLeadCountsInsideTheViewersScope(t *testing.T) {
	scope := opportunity.DealScope{Restrict: true, AssigneeOverride: "u-1"}
	condition, args := LeadDealsCondition(oppAlias, "ws-1", "lead-1", scope)
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectQuery("^" + regexp.QuoteMeta(numberedPlaceholders(countDealsOfLeadSQL(condition))) + "$").
		WithArgs(toDriverValues(args)...).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))

	got, err := NewLeadDealCounter(db).CountDealsOfLead(context.Background(), "ws-1", "lead-1", scope)
	if err != nil || got != 3 {
		t.Fatalf("count = %d, %v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func toDriverValues(args []interface{}) []driver.Value {
	out := make([]driver.Value, 0, len(args))
	for _, arg := range args {
		out = append(out, arg)
	}
	return out
}

func TestTheDealCountBindsEveryPlaceholderOfEachScope(t *testing.T) {
	scopes := map[string]opportunity.DealScope{
		"unrestricted":  {},
		"assignee":      {Restrict: true, AssigneeOverride: "u-1"},
		"department":    {Restrict: true, DepartmentIDs: []string{"dep-1", "dep-2"}},
		"assignee+dept": {Restrict: true, AssigneeOverride: "u-1", DepartmentIDs: []string{"dep-1"}},
	}
	for name, scope := range scopes {
		t.Run(name, func(t *testing.T) {
			condition, args := LeadDealsCondition(oppAlias, "ws-1", "lead-1", scope)
			sql := countDealsOfLeadSQL(condition)
			if placeholdersIn(sql) != len(args) {
				t.Fatalf("%d placeholders for %d args in %q", placeholdersIn(sql), len(args), sql)
			}
			if !strings.HasPrefix(sql, "SELECT count(*) FROM opportunities "+oppAlias+" WHERE "+condition) || strings.Contains(sql, "LIMIT") {
				t.Fatalf("count statement = %q", sql)
			}
		})
	}
}

func TestCountDealsOfLeadRefusesWithoutAWorkspaceOrALead(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	counter := NewLeadDealCounter(db)
	for name, ids := range map[string][2]string{"no workspace": {" ", "lead-1"}, "no lead": {"ws-1", ""}} {
		if _, err := counter.CountDealsOfLead(context.Background(), ids[0], ids[1], opportunity.DealScope{}); !errors.Is(err, ErrLeadDealsQueryInvalid) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
