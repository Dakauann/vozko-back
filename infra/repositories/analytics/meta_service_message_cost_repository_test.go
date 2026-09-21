package analytics_repository

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	analytics_domain "vozko/domain/analytics"
	"vozko/domain/conversation"
	"vozko/domain/shared"
)

func newMetaCostDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock, *sql.DB) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	mock.MatchExpectationsInOrder(false)
	db, err := gorm.Open(
		postgres.New(postgres.Config{
			Conn:                 sqlDB,
			PreferSimpleProtocol: true,
			WithoutReturning:     true,
		}),
		&gorm.Config{SkipDefaultTransaction: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	return db, mock, sqlDB
}

func metaCostRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"workspace_id", "workspace_name", "providers",
		"service_messages", "meta_confirmed", "meta_answered", "net_billable_sends", "ratio_sort",
		"total_items", "total_service_messages", "total_meta_confirmed", "total_meta_answered", "total_net_billable_sends", "total_unattributed",
	})
}

func metaCostInput() analytics_domain.MetaServiceMessageCostInput {
	return analytics_domain.MetaServiceMessageCostInput{
		StartDate: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		Provider:  analytics_domain.ServiceMessageProviderMeta,
		Page:      1,
		PageSize:  20,
		SortBy:    analytics_domain.SortMetaServiceMessageCostRatio,
		SortOrder: shared.SortDesc,
	}
}

// Only what genuinely varies per request may be bound: the period, the
// provider, the search and the page. Everything else is inlined so the partial
// index can serve the query, and this pins the exact argument list so a stray
// bind parameter cannot creep back into the message predicate unnoticed.
func TestMetaServiceMessageCostBindsOnlyWhatVaries(t *testing.T) {
	db, mock, sqlDB := newMetaCostDB(t)
	defer sqlDB.Close()

	input := metaCostInput()

	mock.ExpectBegin()
	mock.ExpectExec(`SET LOCAL jit = off`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`SET LOCAL statement_timeout`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`FROM conversation_messages cm[\s\S]*FROM balance_transactions[\s\S]*FROM exposure_rows`).
		WithArgs(
			// The provider filter appears twice, once for the count and once for
			// the provider list, before the period.
			string(analytics_domain.ServiceMessageProviderMeta),
			string(analytics_domain.ServiceMessageProviderMeta),
			string(analytics_domain.ServiceMessageProviderMeta),
			string(analytics_domain.ServiceMessageProviderMeta),
			input.StartDate, input.EndDate,
			"whatsapp_campaign",
			input.StartDate, input.EndDate,
			20, 0,
		).
		WillReturnRows(metaCostRows())
	mock.ExpectCommit()

	repo := &repository{db: db}
	if _, err := repo.GetMetaServiceMessageCost(input); err != nil {
		t.Fatalf("GetMetaServiceMessageCost() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// The message predicate must carry the domain's constants as SQL literals.
//
// If any of them became a bind parameter the query would still be correct, and
// would silently stop matching idx_cm_service_exposure once Postgres switched
// that prepared statement to a generic plan: a full scan of three million rows,
// in production only, with every test still green. That is exactly the failure
// this test exists to prevent.
func TestServiceMessagePredicateInlinesTheDomainConstants(t *testing.T) {
	for _, mt := range conversation.ServiceMessageTypeStrings() {
		if !strings.Contains(serviceMessagePredicate, "'"+mt+"'") {
			t.Errorf("message type %q is not inlined as a literal in the predicate", mt)
		}
	}
	for _, status := range conversation.BillableDeliveryStatusStrings() {
		if !strings.Contains(serviceMessagePredicate, "'"+status+"'") {
			t.Errorf("delivery status %q is not inlined as a literal in the predicate", status)
		}
	}
	if !strings.Contains(serviceMessagePredicate, "'"+string(shared.EntryTypeWhatsApp)+"'") {
		t.Error("entry type is not inlined; the partial index predicate names it as a constant")
	}
	if !strings.Contains(serviceMessagePredicate, "'"+string(conversation.MessageDirectionOutbound)+"'") {
		t.Error("direction is not inlined; the partial index predicate names it as a constant")
	}
	if strings.Contains(serviceMessagePredicate, "?") {
		t.Error("the message predicate binds a parameter, which defeats the partial index under a generic plan")
	}
}

// An unbounded analytical query over these exact tables took the platform down
// on 2026-09-08, and the server still runs statement_timeout = 0. The ceiling
// is part of the query, not an optional extra, so it is pinned here.
func TestMetaServiceMessageCostRunsUnderAStatementTimeout(t *testing.T) {
	db, mock, sqlDB := newMetaCostDB(t)
	defer sqlDB.Close()

	mock.ExpectBegin()
	mock.ExpectExec(`SET LOCAL jit = off`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`SET LOCAL statement_timeout = '15s'`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`FROM exposure_rows`).WillReturnRows(metaCostRows())
	mock.ExpectCommit()

	repo := &repository{db: db}
	if _, err := repo.GetMetaServiceMessageCost(metaCostInput()); err != nil {
		t.Fatalf("GetMetaServiceMessageCost() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// The provider filter rides a FILTER clause rather than the WHERE, so that one
// scan can answer both "how many match this provider" and "how many could not
// be attributed to any provider". Moving it into the WHERE would lose the
// second answer and hide messages from the report.
func TestProviderNarrowingIsAFilterNotAWhere(t *testing.T) {
	clause, needsArg := providerFilterClause(analytics_domain.ServiceMessageProviderMeta)
	if !needsArg {
		t.Error("a named provider must bind its value rather than inline it")
	}
	if clause != providerExpr+" = ?" {
		t.Errorf("clause = %q, want the provider expression compared to a bind parameter", clause)
	}

	allClause, allNeedsArg := providerFilterClause(analytics_domain.ServiceMessageProviderAll)
	if allNeedsArg || allClause != "TRUE" {
		t.Errorf("all providers = (%q, %v), want an unconditional TRUE with no argument", allClause, allNeedsArg)
	}

	unattrClause, unattrNeedsArg := providerFilterClause(analytics_domain.ServiceMessageProviderUnattributed)
	if unattrNeedsArg || unattrClause != "p.id IS NULL" {
		t.Errorf("unattributed = (%q, %v), want a NULL phone test with no argument", unattrClause, unattrNeedsArg)
	}
}

// The sort field is interpolated into the query, so every branch must return a
// column this query actually has, and nothing may pass through unmapped.
func TestOrderExpressionIsAClosedSet(t *testing.T) {
	allowed := map[string]bool{
		"ratio_sort": true, "service_messages": true,
		"net_billable_sends": true, "workspace_name": true,
	}
	fields := []analytics_domain.MetaServiceMessageCostSortField{
		analytics_domain.SortMetaServiceMessageCostRatio,
		analytics_domain.SortMetaServiceMessageCostServiceMessages,
		analytics_domain.SortMetaServiceMessageCostNetBillableSends,
		analytics_domain.SortMetaServiceMessageCostWorkspaceName,
		analytics_domain.MetaServiceMessageCostSortField("1; DROP TABLE workspaces"),
	}
	for _, f := range fields {
		if got := metaCostOrderExpr(f); !allowed[got] {
			t.Errorf("sort %q produced %q, which is not a column of this query", f, got)
		}
	}
}

// The window columns repeat the same totals on every row, so the report reads
// them once. A page that shows ten workspaces must still report the totals for
// all sixty-nine, or the headline contradicts the table under it.
func TestTotalsComeFromTheWindowNotThePage(t *testing.T) {
	rows := []metaServiceMessageCostRow{
		{WorkspaceID: "a", ServiceMessages: 10, NetBillableSends: 5, TotalItems: 69, TotalServiceMsgs: 319134, TotalNetSends: 690608, TotalUnattrib: 7},
		{WorkspaceID: "b", ServiceMessages: 1, NetBillableSends: 100, TotalItems: 69, TotalServiceMsgs: 319134, TotalNetSends: 690608, TotalUnattrib: 7},
	}

	report := buildMetaServiceMessageCost(metaCostInput(), shared.Pagination{Page: 1, PageSize: 20}, rows)

	if report.Totals.ServiceMessages != 319134 || report.Totals.NetBillableSends != 690608 {
		t.Errorf("totals = %d/%d, want the period totals, not the page's",
			report.Totals.ServiceMessages, report.Totals.NetBillableSends)
	}
	if report.Totals.WorkspacesCovered != 69 {
		t.Errorf("WorkspacesCovered = %d, want 69", report.Totals.WorkspacesCovered)
	}
	if report.Totals.UnattributedServiceMessages != 7 {
		t.Errorf("unattributed = %d, want it surfaced rather than folded into the total", report.Totals.UnattributedServiceMessages)
	}
	if report.Workspaces.TotalItems != 69 {
		t.Errorf("pagination total = %d, want 69 so the page count is right", report.Workspaces.TotalItems)
	}
}

// A workspace that sent service messages and bought nothing has no ratio. Zero
// would read as "sends nothing per send", the exact opposite of the truth, so
// the field stays nil and the UI renders it as its own state.
func TestNoBillableSendsProducesNoRatioRatherThanZero(t *testing.T) {
	rows := []metaServiceMessageCostRow{{WorkspaceID: "a", ServiceMessages: 900, NetBillableSends: 0, TotalItems: 1}}

	report := buildMetaServiceMessageCost(metaCostInput(), shared.Pagination{Page: 1, PageSize: 20}, rows)

	if report.Workspaces.Items[0].Ratio != nil {
		t.Errorf("Ratio = %v, want nil when nothing was bought", *report.Workspaces.Items[0].Ratio)
	}
	if report.Totals.Ratio != nil {
		t.Error("total Ratio should also be nil when the period bought nothing")
	}
}

func TestRatioIsComputedFromTheCountedRows(t *testing.T) {
	rows := []metaServiceMessageCostRow{{WorkspaceID: "a", ServiceMessages: 13288, NetBillableSends: 8414, TotalItems: 1, TotalServiceMsgs: 13288, TotalNetSends: 8414}}

	report := buildMetaServiceMessageCost(metaCostInput(), shared.Pagination{Page: 1, PageSize: 20}, rows)

	got := report.Workspaces.Items[0].Ratio
	if got == nil {
		t.Fatal("Ratio = nil, want a number")
	}
	if *got < 1.57 || *got > 1.58 {
		t.Errorf("Ratio = %v, want about 1,58", *got)
	}
}

// An empty result is a legitimate answer (nothing matched the filters), not an
// error, and it must not read as a page of unknown totals.
func TestEmptyResultReportsZeroTotals(t *testing.T) {
	report := buildMetaServiceMessageCost(metaCostInput(), shared.Pagination{Page: 1, PageSize: 20}, nil)

	if report.Totals.ServiceMessages != 0 || report.Totals.WorkspacesCovered != 0 {
		t.Error("an empty result must report zero, not leftover totals")
	}
	if report.Workspaces == nil || len(report.Workspaces.Items) != 0 {
		t.Error("an empty result must still carry an empty item list")
	}
}

// The provider list is aggregated as a delimited string so it scans on any
// driver without a driver-specific array type. Splitting it must not invent an
// empty provider for a workspace that matched nothing.
func TestProviderListSplitting(t *testing.T) {
	cases := map[string][]string{
		"":                {},
		"meta":            {"meta"},
		"meta,dialog360":  {"meta", "dialog360"},
		" meta , ,  meta": {"meta", "meta"},
	}
	for raw, want := range cases {
		got := splitProviders(raw)
		if len(got) != len(want) {
			t.Errorf("splitProviders(%q) = %v, want %v", raw, got, want)
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("splitProviders(%q)[%d] = %q, want %q", raw, i, got[i], want[i])
			}
		}
	}
}

// Meta's own verdict is counted from columns, not inferred, and a row Meta has
// not spoken about must not be counted as a confirmed zero. NULL is excluded by
// IS TRUE rather than by "= true", which NULL would also fail but less
// obviously, and the free entry point is excluded explicitly because that is
// the one exemption our inference cannot see.
func TestMetaConfirmedPredicateExcludesUnknownAndFreeEntryPoint(t *testing.T) {
	if !strings.Contains(metaConfirmedPredicate, "meta_pricing_billable IS TRUE") {
		t.Error("confirmed count must use IS TRUE so an unspoken NULL is not read as false")
	}
	if !strings.Contains(metaConfirmedPredicate, "'"+conversation.MetaPricingCategoryService+"'") {
		t.Error("confirmed count must name the service category as a literal")
	}
	if !strings.Contains(metaConfirmedPredicate, "'"+conversation.MetaOriginFreeEntryPoint+"'") {
		t.Error("confirmed count must exclude the 72 hour free entry point, which Meta does not charge for")
	}
	if strings.Contains(metaConfirmedPredicate, "?") {
		t.Error("the confirmed predicate binds a parameter, which would defeat index matching")
	}
}

// The confirmed count rides the same aggregate as the inferred one, so it must
// come back on every row and total the same way.
func TestConfirmedCountIsCarriedThrough(t *testing.T) {
	rows := []metaServiceMessageCostRow{
		{WorkspaceID: "a", ServiceMessages: 100, MetaConfirmed: 40, NetBillableSends: 50,
			TotalItems: 1, TotalServiceMsgs: 100, TotalMetaConfirm: 40, TotalNetSends: 50},
	}

	report := buildMetaServiceMessageCost(metaCostInput(), shared.Pagination{Page: 1, PageSize: 20}, rows)

	if report.Workspaces.Items[0].MetaConfirmed != 40 {
		t.Errorf("row MetaConfirmed = %d, want 40", report.Workspaces.Items[0].MetaConfirmed)
	}
	if report.Totals.MetaConfirmed != 40 {
		t.Errorf("total MetaConfirmed = %d, want 40", report.Totals.MetaConfirmed)
	}
	if report.Totals.MetaConfirmed > report.Totals.ServiceMessages {
		t.Error("confirmed can never exceed the inferred count it is a subset of")
	}
}

// The "still an estimate" flag is derived from coverage here, where coverage is
// known. It used to be asserted unconditionally by the usecase, which meant the
// page would have carried an upper bound caveat forever.
func TestInferredFlagFollowsMetaCoverage(t *testing.T) {
	cases := []struct {
		name         string
		serviceMsgs  int64
		metaAnswered int64
		wantInferred bool
	}{
		{"Meta has said nothing yet", 319134, 0, true},
		{"one message short of complete", 100, 99, true},
		{"Meta answered for all of them", 100, 100, false},
		{"an empty period confirms nothing", 0, 0, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rows := []metaServiceMessageCostRow{{
				WorkspaceID:      "a",
				ServiceMessages:  tc.serviceMsgs,
				TotalItems:       1,
				TotalServiceMsgs: tc.serviceMsgs,
				TotalMetaAnswer:  tc.metaAnswered,
			}}
			report := buildMetaServiceMessageCost(metaCostInput(), shared.Pagination{Page: 1, PageSize: 20}, rows)
			if report.InferredOnly != tc.wantInferred {
				t.Errorf("InferredOnly = %v, want %v", report.InferredOnly, tc.wantInferred)
			}
		})
	}
}

// Answered counts every verdict, billable or not. Reusing the confirmed
// predicate would make a period where Meta said "this was free" look like a
// period Meta had not answered at all, and the caveat would never lift.
func TestAnsweredPredicateIsNotTheConfirmedPredicate(t *testing.T) {
	if strings.Contains(metaConfirmedPredicate, "IS NOT NULL") {
		t.Error("the confirmed predicate must test the verdict, not merely its presence")
	}
}
