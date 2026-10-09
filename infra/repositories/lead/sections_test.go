package lead

import (
	"context"
	"database/sql/driver"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/crmfilter"
	"vozko/domain/lead"
	infracrmfilter "vozko/infra/repositories/crmfilter"
)

var sectionDay = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

func sectionQuery() lead.SectionQuery {
	return lead.SectionQuery{WorkspaceID: pageWorkspace, Today: sectionDay}
}

func valueOf(t *testing.T, v driver.Valuer) driver.Value {
	t.Helper()
	out, err := v.Value()
	if err != nil {
		t.Fatal(err)
	}
	return out
}

const sectionMembers = `SELECT leads\.id FROM leads WHERE leads\.workspace_id = \$\d+ AND leads\.deleted_at IS NULL`

func TestReadSummary_CountsEveryTileInOnePass(t *testing.T) {
	db, mock, _ := newMockDB(t)
	r := &repository{db: db, agg: newAggregateCache(nil)}
	pinning := valueOf(t, infracrmfilter.PinningPrecisions())

	expectMapSession(mock)
	mock.ExpectQuery(`^SELECT COUNT\(\*\) AS total, COUNT\(la\.lead_id\) AS with_address, `+
		`COUNT\(\*\) FILTER \(WHERE la\.latitude IS NOT NULL AND la\.geo_precision = ANY\(\$1\)\) AS on_map, `+
		`COUNT\(\*\) FILTER \(WHERE la\.latitude IS NOT NULL AND NOT COALESCE\(la\.geo_precision = ANY\(\$2\), false\)\) AS approximate, `+
		`COUNT\(\*\) FILTER \(WHERE la\.lead_id IS NOT NULL AND la\.latitude IS NULL AND la\.geo_status = ANY\(\$3\)\) AS not_found, `+
		`COUNT\(\*\) FILTER \(WHERE la\.lead_id IS NOT NULL AND la\.latitude IS NULL AND la\.geo_status = \$4\) AS quota_exceeded, `+
		`COUNT\(\*\) FILTER \(WHERE la\.lead_id IS NOT NULL AND la\.latitude IS NULL AND la\.geo_status = \$5\) AS refused, `+
		`COUNT\(\*\) FILTER \(WHERE la\.lead_id IS NOT NULL AND la\.latitude IS NULL AND NOT COALESCE\(la\.geo_status = ANY\(\$6\), false\)\) AS pending, `+
		`COUNT\(\*\) FILTER \(WHERE leads\.birth_date IS NOT NULL AND .*\) AS birthdays_today, `+
		`COUNT\(\*\) FILTER \(WHERE leads\.blocked\) AS blocked, `+
		`COUNT\(\*\) FILTER \(WHERE leads\.id IN \(SELECT lmw_s\.lead_id FROM lead_message_windows lmw_s WHERE lmw_s\.last_message_at > NOW\(\) - INTERVAL '24 hours'\)\) AS window_open `+
		`FROM leads LEFT JOIN lead_addresses la ON la\.lead_id = leads\.id AND la\.is_primary AND la\.workspace_id = leads\.workspace_id `+
		`WHERE leads\.workspace_id = \$11 AND leads\.deleted_at IS NULL$`).
		WithArgs(pinning, pinning, valueOf(t, infracrmfilter.GeoStatusArray(lead.UnlocatedGeoStatuses())), "quota_exceeded", "refused",
			valueOf(t, infracrmfilter.GeoStatusArray(append(lead.UnlocatedGeoStatuses(), lead.GeoQuotaExceeded, lead.GeoRefused))),
			int64(10), int64(8), int64(10), int64(8), pageWorkspace).
		WillReturnRows(sqlmock.NewRows([]string{"total", "with_address", "on_map", "approximate", "not_found", "quota_exceeded", "refused", "pending", "birthdays_today", "blocked", "window_open"}).
			AddRow(100, 40, 10, 5, 4, 1, 9, 6, 2, 3, 7))
	mock.ExpectCommit()

	got, err := r.ReadSummary(context.Background(), sectionQuery())
	if err != nil {
		t.Fatalf("ReadSummary() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	want := lead.SummarySection{Total: 100, WithAddress: 40, OnMap: 10, Approximate: 5, WithoutAddress: 60, NotFound: 4, QuotaExceeded: 1, Refused: 9, Pending: 6, BirthdaysToday: 2, Blocked: 3, WindowOpen: 7}
	if *got != want {
		t.Fatalf("ReadSummary() = %+v, want %+v", *got, want)
	}
}

func TestReadSummary_NeedsTheWorkspaceDay(t *testing.T) {
	q := sectionQuery()
	q.Today = time.Time{}
	_, err := newNilRepo().ReadSummary(context.Background(), q)
	if !errors.Is(err, crmfilter.ErrBirthdayClockMissing) || errors.Is(err, lead.ErrLeadFilterInvalid) {
		t.Fatalf("ReadSummary() without the day = %v, want ErrBirthdayClockMissing as a server fault", err)
	}
	birthdays := lead.ListLeadsInput{WorkspaceID: q.WorkspaceID, Filter: crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{
		{Field: crmfilter.FieldBirthday, Operator: crmfilter.OpEquals, Values: []string{crmfilter.BirthdayToday}},
	}}}}}
	if _, err := newNilRepo().compile(birthdays); !errors.Is(err, crmfilter.ErrBirthdayClockMissing) || errors.Is(err, lead.ErrLeadFilterInvalid) {
		t.Fatalf("compile() without the day = %v, want ErrBirthdayClockMissing as a server fault", err)
	}
}

func expectDistinctFacets(mock sqlmock.Sqlmock, campaigns, channels, memories *sqlmock.Rows) {
	mock.ExpectQuery(`^SELECT s\.key AS key, COUNT\(\*\) AS count FROM \(SELECT DISTINCT wce_g\.status AS key, wce_g\.lead_id FROM whatsapp_campaign_entries wce_g `+
		`WHERE wce_g\.deleted_at IS NULL AND wce_g\.campaign_id IN \(SELECT wc_g\.id FROM whatsapp_campaigns wc_g WHERE wc_g\.workspace_id = \$1\) `+
		`AND wce_g\.lead_id IN \(`+sectionMembers+`\)\) s GROUP BY s\.key$`).
		WithArgs(pageWorkspace, pageWorkspace).WillReturnRows(campaigns)
	mock.ExpectQuery(`^SELECT s\.key AS key, COUNT\(\*\) AS count FROM \(SELECT DISTINCT lead_channels\.channel AS key, lead_channels\.lead_id FROM \(.*\) lead_channels `+
		`WHERE lead_channels\.lead_id IN \(`+sectionMembers+`\)\) s GROUP BY s\.key$`).
		WithArgs(pageWorkspace, pageWorkspace, pageWorkspace, pageWorkspace, pageWorkspace, pageWorkspace, pageWorkspace).WillReturnRows(channels)
	mock.ExpectQuery(`^SELECT s\.key AS key, COUNT\(\*\) AS count FROM \(SELECT DISTINCT lm_g\.category AS key, lm_g\.lead_id FROM lead_memories lm_g `+
		`WHERE lm_g\.workspace_id = \$1 AND lm_g\.deleted_at IS NULL AND lm_g\.lead_id IN \(`+sectionMembers+`\)\) s GROUP BY s\.key$`).
		WithArgs(pageWorkspace, pageWorkspace).WillReturnRows(memories)
}

func counts() *sqlmock.Rows { return sqlmock.NewRows([]string{"key", "count"}) }

func leadColumnRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"skip_source", "skip_owner", "source", "owner_id", "owner_kind", "classification", "count"})
}

func TestReadFacets_FailsWhenAnyFacetFails(t *testing.T) {
	db, mock, _ := newMockDB(t)
	r := &repository{db: db, agg: newAggregateCache(nil)}
	expectMapSession(mock)
	mock.ExpectQuery(`FROM whatsapp_campaign_entries wce_g`).WillReturnRows(counts().AddRow("SENT", 3))
	mock.ExpectQuery(`lead_channels`).WillReturnError(errors.New("canceling statement due to statement timeout"))
	mock.ExpectRollback()

	if _, err := r.ReadFacets(context.Background(), sectionQuery()); err == nil {
		t.Fatal("a facet that failed must fail the section, never answer an empty chart")
	}
}

func TestReadFacets_ReadsEveryFacetInOneSessionAndTheLeadColumnsInOneScan(t *testing.T) {
	db, mock, _ := newMockDB(t)
	r := &repository{db: db, agg: newAggregateCache(nil)}
	expectMapSession(mock)
	expectDistinctFacets(mock, counts().AddRow("SENT", 2), counts().AddRow("whatsapp", 3), counts().AddRow("interest", 1))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT GROUPING(l.source) AS skip_source, GROUPING(l.owner_id, l.owner_kind) AS skip_owner, "+
		"l.source AS source, l.owner_id AS owner_id, l.owner_kind AS owner_kind, l.classification AS classification, COUNT(*) AS count "+
		"FROM (SELECT leads.source AS source, leads.owner_id::text AS owner_id, leads.owner_kind AS owner_kind, leads.custom_fields ->> $1::text AS classification "+
		"FROM leads WHERE leads.workspace_id = $2 AND leads.deleted_at IS NULL) l "+
		"GROUP BY GROUPING SETS ((l.source), (l.owner_id, l.owner_kind), (l.classification))")).
		WithArgs("classificacao", pageWorkspace).
		WillReturnRows(leadColumnRows().
			AddRow(0, 1, "import", nil, nil, nil, 9).
			AddRow(0, 1, nil, nil, nil, nil, 4).
			AddRow(1, 0, nil, secondLead, "ai", nil, 1).
			AddRow(1, 0, nil, firstLead, "human", nil, 4).
			AddRow(1, 0, nil, nil, nil, nil, 8).
			AddRow(1, 1, nil, nil, nil, "positivo", 6).
			AddRow(1, 1, nil, nil, nil, nil, 7))
	mock.ExpectCommit()

	q := sectionQuery()
	q.ClassificationKey = "classificacao"
	got, err := r.ReadFacets(context.Background(), q)
	if err != nil {
		t.Fatalf("ReadFacets() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	if got.Sources["import"] != 9 || len(got.Sources) != 1 || got.Channels["whatsapp"] != 3 || got.CampaignStatuses["SENT"] != 2 || got.MemoryCategories["interest"] != 1 {
		t.Fatalf("facets = %+v", got)
	}
	if len(got.Owners) != 2 || got.Owners[0].Owner != firstLead || got.Owners[0].Count != 4 || got.Owners[1].Owner != "ai:"+secondLead || got.OwnersTruncated {
		t.Fatalf("owners = %+v, truncated %v, want the largest first, no row for leads without an owner, nothing cut", got.Owners, got.OwnersTruncated)
	}
	if got.Classification == nil || got.Classification.Key != "classificacao" || len(got.Classification.Values) != 1 || got.Classification.Values["positivo"] != 6 {
		t.Fatalf("classification = %+v", got.Classification)
	}
}

func TestReadFacets_NoClassificationWithoutAVisibleKey(t *testing.T) {
	db, mock, _ := newMockDB(t)
	r := &repository{db: db, agg: newAggregateCache(nil)}
	expectMapSession(mock)
	expectDistinctFacets(mock, counts(), counts(), counts())
	mock.ExpectQuery(regexp.QuoteMeta("l.owner_kind AS owner_kind, NULL::text AS classification, COUNT(*) AS count " +
		"FROM (SELECT leads.source AS source, leads.owner_id::text AS owner_id, leads.owner_kind AS owner_kind FROM leads WHERE leads.workspace_id = $1 AND leads.deleted_at IS NULL) l " +
		"GROUP BY GROUPING SETS ((l.source), (l.owner_id, l.owner_kind))")).
		WithArgs(pageWorkspace).
		WillReturnRows(leadColumnRows())
	mock.ExpectCommit()

	got, err := r.ReadFacets(context.Background(), sectionQuery())
	if err != nil {
		t.Fatalf("ReadFacets() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	if got.Classification != nil {
		t.Fatalf("classification = %+v, want none", got.Classification)
	}
}

func TestFillLeadColumnFacets_SaysWhenTheOwnersWereCut(t *testing.T) {
	ownerRows := func(n int) []leadColumnRow {
		rows := make([]leadColumnRow, 0, n)
		human := "human"
		for i := range n {
			id := "owner-" + string(rune('a'+i%26)) + strings.Repeat("x", i/26)
			rows = append(rows, leadColumnRow{SkipSource: 1, OwnerID: &id, OwnerKind: &human, Count: int64(i%7 + 1)})
		}
		return rows
	}
	for _, tc := range []struct {
		name          string
		owners        int
		wantLen       int
		wantTruncated bool
	}{
		{"every owner fits", lead.MaxFacetOwners, lead.MaxFacetOwners, false},
		{"more owners than the cap", lead.MaxFacetOwners + 5, lead.MaxFacetOwners, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := &lead.FacetsSection{}
			fillLeadColumnFacets(out, ownerRows(tc.owners), "")
			if len(out.Owners) != tc.wantLen || out.OwnersTruncated != tc.wantTruncated {
				t.Fatalf("owners = %d, truncated %v; want %d, %v", len(out.Owners), out.OwnersTruncated, tc.wantLen, tc.wantTruncated)
			}
			if out.Owners[0].Count < out.Owners[len(out.Owners)-1].Count {
				t.Fatalf("owners must come largest first, got %+v", out.Owners)
			}
		})
	}
}

func TestReadPlaces_TopCitiesAndBairroPairsOfTheFilteredLeads(t *testing.T) {
	db, mock, _ := newMockDB(t)
	r := &repository{db: db, agg: newAggregateCache(nil)}
	membership := `la\.workspace_id = \$1 AND la\.is_primary AND la\.city_key IS NOT NULL AND la\.lead_id IN \(SELECT leads\.id FROM leads WHERE leads\.workspace_id = \$2 AND leads\.deleted_at IS NULL\)`
	expectMapSession(mock)
	mock.ExpectQuery(`^SELECT g\.city_key AS city_key, \(array_agg\(g\.city ORDER BY g\.n DESC, g\.city\)\)\[1\] AS city, \(array_agg\(g\.state ORDER BY g\.n DESC, g\.state\)\)\[1\] AS state, sum\(g\.n\)::bigint AS count FROM \(SELECT la\.city_key, la\.city, la\.state, COUNT\(\*\) AS n FROM lead_addresses la WHERE `+
		membership+` GROUP BY la\.city_key, la\.city, la\.state\) g GROUP BY g\.city_key ORDER BY count DESC, g\.city_key LIMIT \$3$`).
		WithArgs(pageWorkspace, pageWorkspace, lead.MaxPlaceCities).
		WillReturnRows(sqlmock.NewRows([]string{"city_key", "city", "state", "count"}).AddRow("sp:sao paulo", "São Paulo", "SP", 12))
	mock.ExpectQuery(`^SELECT g\.city_key AS city_key, g\.district_key AS district_key, \(array_agg\(g\.district ORDER BY g\.n DESC, g\.district\)\)\[1\] AS district, \(array_agg\(g\.city ORDER BY g\.n DESC, g\.city\)\)\[1\] AS city, \(array_agg\(g\.state ORDER BY g\.n DESC, g\.state\)\)\[1\] AS state, sum\(g\.n\)::bigint AS count FROM \(SELECT la\.city_key, la\.district_key, la\.district, la\.city, la\.state, COUNT\(\*\) AS n FROM lead_addresses la WHERE `+
		membership+` AND la\.district_key IS NOT NULL GROUP BY la\.city_key, la\.district_key, la\.district, la\.city, la\.state\) g GROUP BY g\.city_key, g\.district_key ORDER BY count DESC, g\.city_key, g\.district_key LIMIT \$3$`).
		WithArgs(pageWorkspace, pageWorkspace, lead.MaxPlaceDistricts).
		WillReturnRows(sqlmock.NewRows([]string{"city_key", "district_key", "district", "city", "state", "count"}).AddRow("sp:sao paulo", "centro", "Centro", "São Paulo", "SP", 7))
	mock.ExpectCommit()

	got, err := r.ReadPlaces(context.Background(), sectionQuery())
	if err != nil {
		t.Fatalf("ReadPlaces() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	if len(got.Cities) != 1 || got.Cities[0].Count != 12 {
		t.Fatalf("cities = %+v", got.Cities)
	}
	if len(got.Districts) != 1 || got.Districts[0].Pair != "sp:sao paulo/centro" || got.Districts[0].District != "Centro" {
		t.Fatalf("districts = %+v", got.Districts)
	}
}

func TestSectionQueriesBindOnePlaceholderPerArgument(t *testing.T) {
	q, err := newNilRepo().compile(lead.ListLeadsInput{
		WorkspaceID: pageWorkspace,
		Today:       sectionDay,
		Filter: crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{
			{Field: crmfilter.FieldDistrict, Operator: crmfilter.OpIn, Values: []string{"sp:sao paulo/centro", "sp:sao paulo/se"}},
			{Field: crmfilter.FieldPhoneAny, Operator: crmfilter.OpIn, Values: []string{"5511987654321"}},
		}}}},
	})
	if err != nil {
		t.Fatalf("compile() error = %v", err)
	}
	summary, summaryArgs, err := q.summarySQL()
	if err != nil {
		t.Fatalf("summarySQL() error = %v", err)
	}
	cities, cityArgs := q.citiesSQL()
	districts, districtArgs := q.districtsSQL()
	campaigns, campaignArgs := q.campaignStatusesSQL()
	channels, channelArgs := q.channelsSQL()
	memories, memoryArgs := q.memoryCategoriesSQL()
	columns, columnArgs := q.leadColumnsSQL("classificacao")
	for name, pair := range map[string]struct {
		sql  string
		args []interface{}
	}{
		"summary": {summary, summaryArgs}, "cities": {cities, cityArgs}, "districts": {districts, districtArgs},
		"campaigns": {campaigns, campaignArgs}, "channels": {channels, channelArgs}, "memories": {memories, memoryArgs}, "columns": {columns, columnArgs},
	} {
		if got := strings.Count(pair.sql, "?"); got != len(pair.args) {
			t.Fatalf("%s: %d placeholders for %d args", name, got, len(pair.args))
		}
	}
	if strings.Count(summary, "leads.workspace_id = ?") != 1 {
		t.Fatalf("the summary must evaluate the lead filter once: %s", summary)
	}
}
