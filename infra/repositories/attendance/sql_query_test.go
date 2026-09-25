package attendance_repository

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"vozko/domain/attendance"
)

var (
	timezonePlaceholder  = regexp.MustCompile(`AT TIME ZONE\s*$`)
	idPlaceholder        = regexp.MustCompile(`(workspace_id|department_id|assigned_user_id|campaign_id|account_id|instance_id|ig_account_id|lead_id|entry_id)\s*(=|IN)\s*\(?$`)
	timestampPlaceholder = regexp.MustCompile(`(created_at|closed_at|close_date|last_message_at|enabled_at|period_start)\s*(>=|<=|<|>|=)\s*$`)
)

func describeArg(arg interface{}) string {
	switch arg.(type) {
	case time.Time, *time.Time:
		return "timestamp"
	case string:
		return "text"
	default:
		return "other"
	}
}

func assertArgsMatchTheirPlaceholders(t *testing.T, where, sql string, args []interface{}) {
	t.Helper()

	placeholders := strings.Count(sql, "?")
	if placeholders != len(args) {
		t.Fatalf("%s has %d placeholders but %d arguments", where, placeholders, len(args))
	}

	index := 0
	for position, char := range sql {
		if char != '?' {
			continue
		}
		preceding := sql[:position]
		if len(preceding) > 120 {
			preceding = preceding[len(preceding)-120:]
		}
		preceding = strings.TrimRight(preceding, " \t\n")

		want := ""
		switch {
		case timezonePlaceholder.MatchString(preceding):
			want = "text"
		case idPlaceholder.MatchString(preceding):
			want = "text"
		case timestampPlaceholder.MatchString(preceding):
			want = "timestamp"
		}

		if want != "" {
			if got := describeArg(args[index]); got != want {
				t.Fatalf(
					"%s argument %d is a %s but its placeholder follows %q, which wants a %s",
					where, index, got, strings.TrimSpace(lastLineOf(preceding)), want,
				)
			}
		}
		index++
	}
}

func lastLineOf(text string) string {
	lines := strings.Split(text, "\n")
	return lines[len(lines)-1]
}

func trendWindowForTest() (time.Time, time.Time, *time.Location) {
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		panic(err)
	}
	from := time.Date(2025, 9, 1, 0, 0, 0, 0, loc)
	to := time.Date(2026, 10, 1, 0, 0, 0, 0, loc)
	return from, to, loc
}

func TestTrendQueryArgumentsLineUpWithTheirPlaceholders(t *testing.T) {
	from, to, loc := trendWindowForTest()

	for _, filter := range []attendance.OverviewFilter{
		{},
		overviewFilterForTest(),
		{Channel: "whatsapp"},
		{DepartmentID: "dept-1"},
	} {
		sources := selectedChannelSources(filter.Channel)
		sql, args := trendQuery("5a018104-560d-4627-aba7-0ee0895fcf50", filter, sources, from, to, loc).build()
		assertArgsMatchTheirPlaceholders(t, "trend query", sql, args)
	}
}

func TestTrendQueryPassesTheTimezoneToItsOwnPlaceholder(t *testing.T) {
	from, to, loc := trendWindowForTest()
	sources := selectedChannelSources("")

	sql, args := trendQuery("ws-1", attendance.OverviewFilter{}, sources, from, to, loc).build()
	if !strings.Contains(sql, "AT TIME ZONE ?") {
		t.Fatalf("trend query lost its timezone placeholder")
	}
	if len(args) == 0 {
		t.Fatalf("trend query produced no arguments")
	}
	last, ok := args[len(args)-1].(string)
	if !ok || last != loc.String() {
		t.Fatalf("last trend argument = %v, want the timezone %q that its trailing placeholder needs", args[len(args)-1], loc.String())
	}
	if first, ok := args[0].(string); !ok || first != "ws-1" {
		t.Fatalf("first trend argument = %v, want the workspace id its leading placeholder needs", args[0])
	}
}

func TestTrendUnbucketedQueryArgumentsLineUp(t *testing.T) {
	from, to, _ := trendWindowForTest()
	sources := selectedChannelSources("")

	sql, args := trendUnbucketedQuery("ws-1", overviewFilterForTest(), sources, from, to).build()
	assertArgsMatchTheirPlaceholders(t, "trend unbucketed query", sql, args)
}

func TestSQLQueryKeepsTextAndArgumentsInStep(t *testing.T) {
	query := newSQLQuery()
	query.add("SELECT ? , ?", "a", "b")

	inner := newSQLQuery()
	inner.add(" FROM t WHERE x = ?", "c")
	query.addQuery(inner)
	query.add(" AND y = ?", "d")

	sql, args := query.build()
	if strings.Count(sql, "?") != len(args) {
		t.Fatalf("builder produced %d placeholders for %d args", strings.Count(sql, "?"), len(args))
	}
	if args[0] != "a" || args[1] != "b" || args[2] != "c" || args[3] != "d" {
		t.Fatalf("builder reordered the arguments: %v", args)
	}
}

func TestSQLQueryEmpty(t *testing.T) {
	if !newSQLQuery().empty() {
		t.Fatalf("a fresh query should be empty")
	}
	var nilQuery *sqlQuery
	if !nilQuery.empty() {
		t.Fatalf("a nil query should be empty")
	}
	if newSQLQuery().add("SELECT 1").empty() {
		t.Fatalf("a query with text should not be empty")
	}
}

func TestRevenueQueryArgumentsLineUpWithTheirPlaceholders(t *testing.T) {
	from, to, loc := trendWindowForTest()
	const workspaceID = "5a018104-560d-4627-aba7-0ee0895fcf50"

	cases := map[string]*sqlQuery{
		"revenue tallies":      revenueTalliesQuery(workspaceID, from, to, attendance.RevenueScope{}),
		"revenue unattributed": revenueUnattributedQuery(workspaceID, from, to, attendance.RevenueScope{}),
		"revenue by month":     revenueByMonthQuery(workspaceID, from, to, loc, "", attendance.RevenueScope{}),
		"revenue by month, one owner": revenueByMonthQuery(
			workspaceID, from, to, loc, "9f1d2c3b-4a5e-6f70-8192-a3b4c5d6e7f8", attendance.RevenueScope{}),
	}

	for name, query := range cases {
		sql, args := query.build()
		assertArgsMatchTheirPlaceholders(t, name, sql, args)
	}
}

func TestRevenueByMonthSendsTheTimezoneFirst(t *testing.T) {
	from, to, loc := trendWindowForTest()
	sql, args := revenueByMonthQuery("ws-1", from, to, loc, "", attendance.RevenueScope{}).build()

	if strings.Index(sql, "AT TIME ZONE ?") > strings.Index(sql, "workspace_id = ?") {
		t.Fatalf("the timezone placeholder moved after the workspace placeholder; the argument order no longer matches")
	}
	if first, ok := args[0].(string); !ok || first != loc.String() {
		t.Fatalf("first argument = %v, want the timezone %q", args[0], loc.String())
	}
}

func TestPriorFinishedLeadsUnionArgumentsLineUp(t *testing.T) {
	sql, args := priorFinishedLeadsUnion("5a018104-560d-4627-aba7-0ee0895fcf50", "tmp_msg", overviewFilterForTest())
	assertArgsMatchTheirPlaceholders(t, "prior finished leads union", sql, args)
}

var untypedArithmeticPlaceholder = regexp.MustCompile(`\?\s*(::[a-z]+\s*)?[-+*/]`)

func assertNoUntypedArithmetic(t *testing.T, where, sql string) {
	t.Helper()
	for _, match := range untypedArithmeticPlaceholder.FindAllString(sql, -1) {
		if strings.Contains(match, "::") {
			continue
		}
		t.Fatalf(
			"%s does arithmetic on a bare placeholder (%q); Postgres resolves the untyped parameter to the operand type and the comparison then fails",
			where, strings.TrimSpace(match),
		)
	}
}

func TestBacklogBandQueriesCompareAgainstTimestamps(t *testing.T) {
	now := time.Date(2026, 9, 23, 4, 16, 8, 0, time.UTC)

	cases := map[string]*sqlQuery{
		"backlog age bands":    backlogAgeQuery("tmp_msg", now),
		"backlog tenure bands": backlogTenureQuery("tmp_msg", now),
	}

	for name, query := range cases {
		sql, args := query.build()
		assertArgsMatchTheirPlaceholders(t, name, sql, args)
		assertNoUntypedArithmetic(t, name, sql)
		for i, arg := range args {
			if _, ok := arg.(time.Time); !ok {
				t.Fatalf("%s argument %d = %v, want a precomputed cutoff timestamp", name, i, arg)
			}
		}
	}
}

func TestBacklogAgeCutoffsWalkBackwardsFromNow(t *testing.T) {
	now := time.Date(2026, 9, 23, 4, 16, 8, 0, time.UTC)
	_, args := backlogAgeQuery("tmp_msg", now).build()

	want := []time.Time{
		now.AddDate(0, 0, -1),
		now.AddDate(0, 0, -3),
		now.AddDate(0, 0, -7),
		now.AddDate(0, 0, -30),
	}
	if len(args) != len(want) {
		t.Fatalf("backlog age query produced %d cutoffs, want %d", len(args), len(want))
	}
	for i, expected := range want {
		if got := args[i].(time.Time); !got.Equal(expected) {
			t.Fatalf("backlog age cutoff %d = %v, want %v", i, got, expected)
		}
	}
}

func TestNoAttendanceQueryDoesArithmeticOnABarePlaceholder(t *testing.T) {
	from, to, loc := trendWindowForTest()
	now := time.Date(2026, 9, 23, 4, 16, 8, 0, time.UTC)
	sources := selectedChannelSources("")
	filter := overviewFilterForTest()

	queries := map[string]*sqlQuery{
		"trend":                trendQuery("ws-1", filter, sources, from, to, loc),
		"trend unbucketed":     trendUnbucketedQuery("ws-1", filter, sources, from, to),
		"revenue tallies":      revenueTalliesQuery("ws-1", from, to, attendance.RevenueScope{}),
		"revenue unattributed": revenueUnattributedQuery("ws-1", from, to, attendance.RevenueScope{}),
		"revenue by month":     revenueByMonthQuery("ws-1", from, to, loc, "", attendance.RevenueScope{}),
		"revenue by month, one owner": revenueByMonthQuery(
			"ws-1", from, to, loc, "9f1d2c3b-4a5e-6f70-8192-a3b4c5d6e7f8", attendance.RevenueScope{}),
		"backlog age":    backlogAgeQuery("tmp_msg", now),
		"backlog tenure": backlogTenureQuery("tmp_msg", now),
	}

	for name, query := range queries {
		sql, _ := query.build()
		assertNoUntypedArithmetic(t, name, sql)
	}

	union, _ := priorFinishedLeadsUnion("ws-1", "tmp_msg", filter)
	assertNoUntypedArithmetic(t, "prior finished leads union", union)
}

func TestRevenueCreditsTheOwnerAsAnActor(t *testing.T) {
	from, to, _ := trendWindowForTest()
	sql, _ := revenueTalliesQuery("ws-1", from, to, attendance.RevenueScope{}).build()
	if !strings.Contains(sql, actorIDSQL("owner_id", "owner_kind")) {
		t.Fatalf("revenue tallies do not read the owner's kind, so an AI deal would be credited to a person:\n%s", sql)
	}
	if !strings.Contains(sql, "won_without_value") {
		t.Fatalf("revenue tallies do not count wins without a value:\n%s", sql)
	}
}

func TestUnscopedRevenueDoesNotRequireAConversation(t *testing.T) {
	from, to, loc := trendWindowForTest()
	for name, query := range map[string]*sqlQuery{
		"tallies":      revenueTalliesQuery("ws-1", from, to, attendance.RevenueScope{}),
		"unattributed": revenueUnattributedQuery("ws-1", from, to, attendance.RevenueScope{}),
		"by month":     revenueByMonthQuery("ws-1", from, to, loc, "", attendance.RevenueScope{}),
	} {
		sql, _ := query.build()
		if strings.Contains(sql, "opportunity_conversations") {
			t.Fatalf("%s without a filter only counts deals with a conversation:\n%s", name, sql)
		}
	}
}

func TestScopedRevenueCountsDealsThroughTheirConversations(t *testing.T) {
	from, to, loc := trendWindowForTest()
	scope := attendance.RevenueScope{CampaignID: "c1", CampaignType: "whatsapp", DepartmentID: "d1"}
	for name, query := range map[string]*sqlQuery{
		"tallies":      revenueTalliesQuery("ws-1", from, to, scope),
		"unattributed": revenueUnattributedQuery("ws-1", from, to, scope),
		"by month":     revenueByMonthQuery("ws-1", from, to, loc, "ai:agent-1", scope),
	} {
		sql, args := query.build()
		assertArgsMatchTheirPlaceholders(t, name, sql, args)
		assertColumnsExist(t, name, sql)
		if !strings.Contains(sql, "EXISTS (") || !strings.Contains(sql, "oc.opportunity_id = opportunities.id") {
			t.Fatalf("%s is not scoped through the deal's conversations:\n%s", name, sql)
		}
		if strings.Count(sql, "oc.entry_type = '") != 1 {
			t.Fatalf("%s should only look at the campaign's own channel:\n%s", name, sql)
		}
	}
}

func TestRevenueScopedToAnotherChannelsCampaignIsEmpty(t *testing.T) {
	from, to, _ := trendWindowForTest()
	scope := attendance.RevenueScope{CampaignID: "c1", CampaignType: "whatsapp", Channel: "instagram"}
	sql, _ := revenueTalliesQuery("ws-1", from, to, scope).build()
	if !strings.Contains(sql, "AND FALSE") {
		t.Fatalf("a campaign of one channel filtered to another must match nothing:\n%s", sql)
	}
}
