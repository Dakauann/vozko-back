package lead

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/address"
	"vozko/domain/geo"
	"vozko/domain/geocoding"
	"vozko/domain/lead"
	"vozko/infra/database"
)

func TestGeocodeClaimSQLLeasesBeforeAnyProviderCall(t *testing.T) {
	sql := geocodeClaimSQL
	for _, want := range []string{
		"WITH RECURSIVE queued(workspace_id) AS",
		"ORDER BY a.workspace_id LIMIT 1",
		"a.workspace_id > queued.workspace_id",
		"workspace_id <= ?::uuid AS wrapped",
		"CROSS JOIN LATERAL",
		"AND a.geo_next_at <= ? AND EXISTS (SELECT 1 FROM leads l WHERE l.id = a.lead_id AND l.deleted_at IS NULL) ORDER BY a.geo_next_at, a.id LIMIT ? FOR UPDATE OF a SKIP LOCKED",
		"ORDER BY turns.wrapped, turns.workspace_id LIMIT ?",
		"SET geo_next_at = ?, geo_attempts = a.geo_attempts + 1, geo_claim = ?",
		"RETURNING a.*",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("claim SQL misses %q:\n%s", want, sql)
		}
	}
	if got := strings.Count(sql, database.GeocodeQueuedSQL("a")); got != 3 {
		t.Fatalf("the queued predicate appears %d times, want 3 so every branch matches the partial index", got)
	}
	if got := strings.Count(sql, "geo_next_at <= ?"); got != 1 {
		t.Fatalf("the due filter appears %d times, want it only inside the per-workspace turn so the skip-scan stays a pure index skip", got)
	}
	if got := strings.Count(sql, "?"); got != 6 {
		t.Fatalf("claim SQL has %d placeholders, want 6", got)
	}
}

func TestGeocodeClaimStartsAfterTheCursor(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	after := "7d0f5c8e-3c2a-4a7f-9a54-0c4f3e2a1b9d"
	mock.ExpectQuery(exact(geocodeClaimSQL)).
		WithArgs(after, now, 100, 500, now.Add(geocoding.Lease), "token-1").
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	req := geocoding.ClaimRequest{Token: "token-1", Now: now, Lease: geocoding.Lease, Limit: 500, WorkspaceTurn: 100, After: after}
	if _, err := NewGeocodeQueue(db, nil).Claim(context.Background(), req); err != nil {
		t.Fatalf("Claim() err = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	req.After = "not-a-workspace"
	if _, err := NewGeocodeQueue(db, nil).Claim(context.Background(), req); err == nil {
		t.Fatal("Claim() with a cursor that is not a workspace id must refuse")
	}
}

func TestGeocodeClaimBindsTheLeaseAndToken(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	q := NewGeocodeQueue(db, nil)
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	lat, lng := -23.56, -46.65
	rows := sqlmock.NewRows([]string{"id", "workspace_id", "lead_id", "label", "is_primary", "zip_code", "street", "number", "district", "city", "state", "city_code",
		"latitude", "longitude", "geo_precision", "geo_source", "geo_provider", "geo_status", "geo_attempts", "fingerprint", "created_at"}).
		AddRow("a-1", "ws-1", "lead-1", "home", true, "01310100", "Avenida Paulista", "1000", "Bela Vista", "São Paulo", "SP", "",
			lat, lng, "district", "reference", "", "unavailable", 2, "fp-1", now)
	mock.ExpectQuery(exact(geocodeClaimSQL)).
		WithArgs(zeroUUID, now, 100, 500, now.Add(geocoding.Lease), "token-1").
		WillReturnRows(rows)
	claims, err := q.Claim(context.Background(), geocoding.ClaimRequest{Token: "token-1", Now: now, Lease: geocoding.Lease, Limit: 500, WorkspaceTurn: 100})
	if err != nil {
		t.Fatalf("Claim() err = %v", err)
	}
	if len(claims) != 1 {
		t.Fatalf("Claim() = %+v, want one claim", claims)
	}
	c := claims[0]
	if c.AddressID != "a-1" || c.WorkspaceID != "ws-1" || c.LeadID != "lead-1" || c.Fingerprint != "fp-1" || c.Attempts != 2 {
		t.Fatalf("claim = %+v", c)
	}
	if c.Address.Postal.District != "Bela Vista" || c.Address.Fix == nil || c.Address.Fix.Precision != geo.PrecisionDistrict || c.Address.GeoStatus != lead.GeoUnavailable {
		t.Fatalf("claimed address = %+v", c.Address)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestGeocodeClaimRefusesAnIncompleteRequest(t *testing.T) {
	q := NewGeocodeQueue(nil, nil)
	for name, req := range map[string]geocoding.ClaimRequest{
		"token": {Now: time.Now(), Lease: time.Minute, Limit: 1, WorkspaceTurn: 1},
		"lease": {Token: "t", Now: time.Now(), Limit: 1, WorkspaceTurn: 1},
		"limit": {Token: "t", Now: time.Now(), Lease: time.Minute, WorkspaceTurn: 1},
		"turn":  {Token: "t", Now: time.Now(), Lease: time.Minute, Limit: 1},
	} {
		if _, err := q.Claim(context.Background(), req); err == nil {
			t.Fatalf("Claim() without %s must refuse", name)
		}
	}
}

func TestGeocodeSettleWritesUnderTheClaimAndBumpsChangedLeads(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	q := NewGeocodeQueue(db, nil)
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	next := now.Add(time.Hour)
	fix := geo.Fix{Point: geo.Point{Lat: -23.56, Lng: -46.65}, Precision: geo.PrecisionStreet, Source: geo.SourceReference, FixedAt: now}
	settled := []geocoding.Settlement{
		{
			Claim:      geocoding.Claim{AddressID: "a-1", WorkspaceID: "ws-1", LeadID: "lead-1", Fingerprint: "fp-1"},
			Resolution: geocoding.Resolution{Address: lead.Address{Fix: &fix, GeoStatus: lead.GeoLocated, Postal: address.Postal{City: "x"}}, Changed: true},
		},
		{
			Claim:      geocoding.Claim{AddressID: "a-2", WorkspaceID: "ws-1", LeadID: "lead-2", Fingerprint: "fp-2"},
			Resolution: geocoding.Resolution{Address: lead.Address{GeoStatus: lead.GeoUnavailable}, NextAt: &next, Changed: false},
		},
	}
	mock.ExpectBegin()
	mock.ExpectQuery(exact(geocodeLockLeadsSQL)).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("lead-1").AddRow("lead-2"))
	mock.ExpectQuery(exact(geocodeSettleSQL)).
		WithArgs(now, sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), "token-1").
		WillReturnRows(sqlmock.NewRows([]string{"lead_id", "workspace_id", "changed"}).AddRow("lead-1", "ws-1", true).AddRow("lead-2", "ws-1", false))
	mock.ExpectQuery(exact(geocodeBumpLeadsSQL)).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "workspace_id", "version"}).AddRow("lead-1", "ws-1", 4))
	mock.ExpectCommit()
	got, err := q.Settle(context.Background(), "token-1", now, settled)
	if err != nil {
		t.Fatalf("Settle() err = %v", err)
	}
	if got.Written != 2 || got.Stale != 0 || got.Leads != 1 {
		t.Fatalf("Settle() = %+v, want 2 written, 0 stale and one lead bumped", got)
	}
	if len(got.Workspaces) != 1 || got.Workspaces[0] != "ws-1" {
		t.Fatalf("workspaces = %+v, want the workspace whose positions changed", got.Workspaces)
	}
	wantChange := lead.Change{WorkspaceID: "ws-1", LeadID: "lead-1", Version: 4, Fields: []string{lead.FieldAddresses}}
	if len(got.Changed) != 1 || got.Changed[0].LeadID != wantChange.LeadID || got.Changed[0].Version != 4 || got.Changed[0].Fields[0] != lead.FieldAddresses {
		t.Fatalf("changed = %+v, want %+v so the lead can be announced", got.Changed, wantChange)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestGeocodeSettleSQLGuardsTheClaimAndTheFingerprint(t *testing.T) {
	for _, want := range []string{"a.geo_claim = ?::uuid", "a.fingerprint = v.fingerprint", "geo_claim = NULL", "FROM unnest("} {
		if !strings.Contains(geocodeSettleSQL, want) {
			t.Fatalf("settle SQL misses %q:\n%s", want, geocodeSettleSQL)
		}
	}
	if got := strings.Count(geocodeSettleSQL, "?"); got != 13 {
		t.Fatalf("settle SQL has %d placeholders, want 13 (arrays bound once each)", got)
	}
	if !strings.Contains(geocodeBumpLeadsSQL, "version = version + 1") || !strings.Contains(geocodeBumpLeadsSQL, "RETURNING") || strings.Count(geocodeBumpLeadsSQL, "?") != 1 {
		t.Fatalf("bump SQL = %q, want one array and a version bump", geocodeBumpLeadsSQL)
	}
	if !strings.Contains(geocodeLockLeadsSQL, "ORDER BY id FOR UPDATE") {
		t.Fatalf("lock SQL = %q, want leads locked in id order like every other lead write", geocodeLockLeadsSQL)
	}
}

func TestGeocodeArmAndBacklogUseTheQueuedPredicate(t *testing.T) {
	for name, sql := range map[string]string{"arm": geocodeArmSQL, "backlog": geocodeBacklogSQL, "release": geocodeReleaseSQL} {
		if name != "release" && !strings.Contains(sql, database.GeocodeQueuedSQL("")) {
			t.Fatalf("%s SQL = %q, want the queued predicate", name, sql)
		}
	}
	if !strings.Contains(geocodeReleaseSQL, "geo_claim = ?::uuid") {
		t.Fatalf("release SQL = %q, want only the token's own claims released", geocodeReleaseSQL)
	}
}

func TestGeocodeSettleLeavesTheAggregatesToTheSweepAndRefreshBumpsEachOnce(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	state := newFakeState()
	q := NewGeocodeQueue(db, state)
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	fix := geo.Fix{Point: geo.Point{Lat: -23.56, Lng: -46.65}, Precision: geo.PrecisionStreet, Source: geo.SourceReference, FixedAt: now}
	settled := []geocoding.Settlement{{
		Claim:      geocoding.Claim{AddressID: "a-1", WorkspaceID: "ws-1", LeadID: "lead-1", Fingerprint: "fp-1"},
		Resolution: geocoding.Resolution{Address: lead.Address{Fix: &fix, GeoStatus: lead.GeoLocated}, Changed: true},
	}}
	mock.ExpectBegin()
	mock.ExpectQuery(exact(geocodeLockLeadsSQL)).WithArgs(sqlmock.AnyArg()).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("lead-1"))
	mock.ExpectQuery(exact(geocodeSettleSQL)).WillReturnRows(sqlmock.NewRows([]string{"lead_id", "workspace_id", "changed"}).AddRow("lead-1", "ws-1", true))
	mock.ExpectQuery(exact(geocodeBumpLeadsSQL)).WithArgs(sqlmock.AnyArg()).WillReturnRows(sqlmock.NewRows([]string{"id", "workspace_id", "version"}).AddRow("lead-1", "ws-1", 4))
	mock.ExpectCommit()
	if _, err := q.Settle(context.Background(), "token-1", now, settled); err != nil {
		t.Fatal(err)
	}
	if got := q.agg.generation("ws-1"); got != "0" {
		t.Fatalf("generation after a batch = %s, want untouched until the sweep ends", got)
	}
	q.RefreshAggregates([]string{"ws-1", "ws-2"})
	if q.agg.generation("ws-1") != "1" || q.agg.generation("ws-2") != "1" {
		t.Fatalf("generations = %s, %s, want each workspace bumped once", q.agg.generation("ws-1"), q.agg.generation("ws-2"))
	}
}

func TestGeocodeWakeSQLWalksTheTableInIDBatches(t *testing.T) {
	for _, want := range []string{"a.id > ?::uuid", "a.geo_status = ?", "a.geo_next_at > ?", "ORDER BY a.id LIMIT ?", "SET geo_next_at = ?", "RETURNING a.id::text"} {
		if !strings.Contains(geocodeWakeSQL, want) {
			t.Fatalf("wake SQL misses %q in %s", want, geocodeWakeSQL)
		}
	}
	if got := strings.Count(geocodeWakeSQL, "?"); got != 5 {
		t.Fatalf("wake SQL has %d placeholders, want 5", got)
	}
}

func TestGeocodeWakeUnavailableLoopsUntilABatchComesBackShort(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	first := sqlmock.NewRows([]string{"id"})
	for _, id := range []string{"00000000-0000-0000-0000-000000000003", "00000000-0000-0000-0000-000000000001", "00000000-0000-0000-0000-000000000002"} {
		first.AddRow(id)
	}
	mock.ExpectQuery(exact(geocodeWakeSQL)).WithArgs(zeroUUID, string(lead.GeoUnavailable), now, 3, now).WillReturnRows(first)
	mock.ExpectQuery(exact(geocodeWakeSQL)).WithArgs("00000000-0000-0000-0000-000000000003", string(lead.GeoUnavailable), now, 3, now).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("00000000-0000-0000-0000-000000000009"))
	q := NewGeocodeQueue(db, nil)
	q.wakeBatch = 3
	woken, err := q.WakeUnavailable(context.Background(), now)
	if err != nil || woken != 4 {
		t.Fatalf("WakeUnavailable() = %d, %v, want 4 addresses woken in two batches", woken, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestGeocodeBacklogCountsPerStatus(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectQuery(exact(geocodeBacklogSQL)).
		WillReturnRows(sqlmock.NewRows([]string{"status", "n"}).AddRow("pending", 12).AddRow("quota_exceeded", 3))
	got, err := NewGeocodeQueue(db, nil).Backlog(context.Background())
	if err != nil || got[lead.GeoPending] != 12 || got[lead.GeoQuotaExceeded] != 3 {
		t.Fatalf("Backlog() = %+v, %v", got, err)
	}
}
