package lead

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/gorm"

	"vozko/domain/cache"
	"vozko/domain/geocoding"
	"vozko/domain/lead"
	"vozko/infra/database"
	"vozko/infra/database/schema"
)

var (
	errGeocodeClaimIncomplete = errors.New("geocode queue: a claim needs a token, a lease, a limit and a workspace turn")
	errGeocodeSettleToken     = errors.New("geocode queue: a write-back needs the claim token")
	errGeocodeClaimCursor     = errors.New("geocode queue: the turn cursor is not a workspace id")
	errGeocodeWakeBatch       = errors.New("geocode queue: waking addresses needs a batch size")
)

const (
	zeroUUID        = "00000000-0000-0000-0000-000000000000"
	geocodeWakeRows = 5000
)

var (
	geocodeClaimSQL = "WITH RECURSIVE queued(workspace_id) AS (" +
		"(SELECT a.workspace_id FROM lead_addresses a WHERE " + database.GeocodeQueuedSQL("a") + " ORDER BY a.workspace_id LIMIT 1)" +
		" UNION ALL" +
		" SELECT (SELECT a.workspace_id FROM lead_addresses a WHERE " + database.GeocodeQueuedSQL("a") +
		" AND a.workspace_id > queued.workspace_id ORDER BY a.workspace_id LIMIT 1)" +
		" FROM queued WHERE queued.workspace_id IS NOT NULL)," +
		" picked AS (SELECT due.id FROM (SELECT workspace_id, workspace_id <= ?::uuid AS wrapped FROM queued" +
		" WHERE workspace_id IS NOT NULL ORDER BY 2, 1) turns CROSS JOIN LATERAL (" +
		"SELECT a.id FROM lead_addresses a WHERE a.workspace_id = turns.workspace_id AND " + database.GeocodeQueuedSQL("a") +
		" AND a.geo_next_at <= ? AND EXISTS (SELECT 1 FROM leads l WHERE l.id = a.lead_id AND l.deleted_at IS NULL)" +
		" ORDER BY a.geo_next_at, a.id LIMIT ? FOR UPDATE OF a SKIP LOCKED) due" +
		" ORDER BY turns.wrapped, turns.workspace_id LIMIT ?)" +
		" UPDATE lead_addresses a SET geo_next_at = ?, geo_attempts = a.geo_attempts + 1, geo_claim = ?" +
		" FROM picked WHERE a.id = picked.id RETURNING a.*"

	geocodeLockLeadsSQL = "SELECT id::text FROM leads WHERE id = ANY(?::uuid[]) ORDER BY id FOR UPDATE"

	geocodeSettleSQL = "UPDATE lead_addresses a SET latitude = NULLIF(v.latitude, '')::float8, longitude = NULLIF(v.longitude, '')::float8," +
		" geo_precision = NULLIF(v.precision, ''), geo_source = NULLIF(v.source, ''), geo_provider = NULLIF(v.provider, '')," +
		" geocoded_at = NULLIF(v.geocoded_at, '')::timestamptz, geo_status = v.status, geo_next_at = NULLIF(v.next_at, '')::timestamptz," +
		" geo_claim = NULL, updated_at = ?" +
		" FROM unnest(?::uuid[], ?::text[], ?::text[], ?::text[], ?::text[], ?::text[], ?::text[], ?::text[], ?::text[], ?::text[], ?::bool[])" +
		" AS v(id, fingerprint, latitude, longitude, precision, source, provider, geocoded_at, status, next_at, changed)" +
		" WHERE a.id = v.id AND a.geo_claim = ?::uuid AND a.fingerprint = v.fingerprint" +
		" RETURNING a.lead_id::text AS lead_id, a.workspace_id::text AS workspace_id, v.changed AS changed"

	geocodeBumpLeadsSQL = "UPDATE leads SET version = version + 1 WHERE id = ANY(?::uuid[]) AND deleted_at IS NULL" +
		" RETURNING id::text AS id, workspace_id::text AS workspace_id, version"

	geocodeReleaseSQL = "UPDATE lead_addresses SET geo_claim = NULL WHERE geo_claim = ?::uuid AND id = ANY(?::uuid[])"

	geocodeArmSQL = "UPDATE lead_addresses SET geo_next_at = ? WHERE " + database.GeocodeQueuedSQL("") + " AND geo_next_at IS NULL"

	geocodeBacklogSQL = "SELECT geo_status AS status, COUNT(*) AS n FROM lead_addresses WHERE " + database.GeocodeQueuedSQL("") +
		" GROUP BY geo_status"

	geocodeWakeSQL = "WITH batch AS (SELECT a.id FROM lead_addresses a WHERE a.id > ?::uuid AND a.geo_status = ? AND a.geo_next_at > ?" +
		" ORDER BY a.id LIMIT ?)" +
		" UPDATE lead_addresses a SET geo_next_at = ? FROM batch WHERE a.id = batch.id RETURNING a.id::text AS id"
)

type GeocodeQueue struct {
	db        *gorm.DB
	agg       *aggregateCache
	wakeBatch int
}

var _ geocoding.Queue = (*GeocodeQueue)(nil)

func NewGeocodeQueue(db *gorm.DB, state cache.SharedState) *GeocodeQueue {
	return &GeocodeQueue{db: db, agg: newAggregateCache(state), wakeBatch: geocodeWakeRows}
}

type claimedRow struct {
	schema.LeadAddress
}

func (q *GeocodeQueue) Arm(ctx context.Context, now time.Time) (int64, error) {
	result := q.db.WithContext(ctx).Exec(geocodeArmSQL, now)
	return result.RowsAffected, result.Error
}

func (q *GeocodeQueue) Claim(ctx context.Context, req geocoding.ClaimRequest) ([]geocoding.Claim, error) {
	if strings.TrimSpace(req.Token) == "" || req.Lease <= 0 || req.Limit <= 0 || req.WorkspaceTurn <= 0 {
		return nil, errGeocodeClaimIncomplete
	}
	after := zeroUUID
	if cursor := strings.TrimSpace(req.After); cursor != "" {
		if _, err := uuid.Parse(cursor); err != nil {
			return nil, errGeocodeClaimCursor
		}
		after = cursor
	}
	var rows []claimedRow
	err := q.db.WithContext(ctx).Raw(geocodeClaimSQL,
		after, req.Now, req.WorkspaceTurn, req.Limit, req.Now.Add(req.Lease), req.Token,
	).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	claims := make([]geocoding.Claim, 0, len(rows))
	for _, row := range rows {
		claims = append(claims, geocoding.Claim{
			AddressID: row.ID, WorkspaceID: row.WorkspaceID, LeadID: row.LeadID,
			Address: addressOf(row.LeadAddress), Fingerprint: row.Fingerprint, Attempts: row.GeoAttempts,
		})
	}
	return claims, nil
}

type settleColumns struct {
	ids, fingerprints, latitudes, longitudes, precisions, sources, providers, fixedAt, statuses, nextAt pq.StringArray
	changed                                                                                             pq.BoolArray
	leads                                                                                               pq.StringArray
}

func columnsOf(settlements []geocoding.Settlement) settleColumns {
	var c settleColumns
	seen := map[string]bool{}
	for _, s := range settlements {
		a := s.Resolution.Address
		var lat, lng, precision, source, provider, fixedAt, next string
		if a.Fix != nil {
			lat = strconv.FormatFloat(a.Fix.Point.Lat, 'f', -1, 64)
			lng = strconv.FormatFloat(a.Fix.Point.Lng, 'f', -1, 64)
			precision, source, provider = string(a.Fix.Precision), string(a.Fix.Source), a.Fix.Provider
			if !a.Fix.FixedAt.IsZero() {
				fixedAt = a.Fix.FixedAt.UTC().Format(time.RFC3339Nano)
			}
		}
		if s.Resolution.NextAt != nil {
			next = s.Resolution.NextAt.UTC().Format(time.RFC3339Nano)
		}
		c.ids = append(c.ids, s.Claim.AddressID)
		c.fingerprints = append(c.fingerprints, s.Claim.Fingerprint)
		c.latitudes, c.longitudes = append(c.latitudes, lat), append(c.longitudes, lng)
		c.precisions, c.sources, c.providers = append(c.precisions, precision), append(c.sources, source), append(c.providers, provider)
		c.fixedAt, c.statuses, c.nextAt = append(c.fixedAt, fixedAt), append(c.statuses, string(a.GeoStatus)), append(c.nextAt, next)
		c.changed = append(c.changed, s.Resolution.Changed)
		if !seen[s.Claim.LeadID] {
			seen[s.Claim.LeadID] = true
			c.leads = append(c.leads, s.Claim.LeadID)
		}
	}
	return c
}

type bumpedLead struct {
	ID          string
	WorkspaceID string
	Version     int64
}

type settledRow struct {
	LeadID      string
	WorkspaceID string
	Changed     bool
}

func (q *GeocodeQueue) Settle(ctx context.Context, token string, now time.Time, settlements []geocoding.Settlement) (geocoding.SettleResult, error) {
	if strings.TrimSpace(token) == "" {
		return geocoding.SettleResult{}, errGeocodeSettleToken
	}
	if len(settlements) == 0 {
		return geocoding.SettleResult{}, nil
	}
	c := columnsOf(settlements)
	var rows []settledRow
	var bumped []bumpedLead
	err := q.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var locked []string
		if err := tx.Raw(geocodeLockLeadsSQL, c.leads).Scan(&locked).Error; err != nil {
			return err
		}
		if err := tx.Raw(geocodeSettleSQL, now,
			c.ids, c.fingerprints, c.latitudes, c.longitudes, c.precisions, c.sources, c.providers, c.fixedAt, c.statuses, c.nextAt, c.changed,
			token,
		).Scan(&rows).Error; err != nil {
			return err
		}
		changed := changedLeads(rows)
		if len(changed) == 0 {
			return nil
		}
		return tx.Raw(geocodeBumpLeadsSQL, changed).Scan(&bumped).Error
	})
	if err != nil {
		return geocoding.SettleResult{}, err
	}
	result := geocoding.SettleResult{Written: len(rows), Stale: len(settlements) - len(rows), Leads: len(bumped)}
	seen := map[string]bool{}
	for _, row := range rows {
		if row.Changed && !seen[row.WorkspaceID] {
			seen[row.WorkspaceID] = true
			result.Workspaces = append(result.Workspaces, row.WorkspaceID)
		}
	}
	for _, b := range bumped {
		result.Changed = append(result.Changed, lead.Change{WorkspaceID: b.WorkspaceID, LeadID: b.ID, Version: b.Version, Fields: []string{lead.FieldAddresses}})
	}
	return result, nil
}

func changedLeads(rows []settledRow) pq.StringArray {
	var out pq.StringArray
	seen := map[string]bool{}
	for _, row := range rows {
		if row.Changed && !seen[row.LeadID] {
			seen[row.LeadID] = true
			out = append(out, row.LeadID)
		}
	}
	return out
}

func (q *GeocodeQueue) Release(ctx context.Context, token string, addressIDs []string) error {
	if strings.TrimSpace(token) == "" {
		return errGeocodeSettleToken
	}
	if len(addressIDs) == 0 {
		return nil
	}
	return q.db.WithContext(ctx).Exec(geocodeReleaseSQL, token, pq.StringArray(addressIDs)).Error
}

func (q *GeocodeQueue) RefreshAggregates(workspaceIDs []string) {
	for _, ws := range workspaceIDs {
		q.agg.bump(ws)
	}
}

func (q *GeocodeQueue) WakeUnavailable(ctx context.Context, now time.Time) (int64, error) {
	if q.wakeBatch <= 0 {
		return 0, errGeocodeWakeBatch
	}
	var woken int64
	cursor := zeroUUID
	for {
		var ids []string
		if err := q.db.WithContext(ctx).Raw(geocodeWakeSQL, cursor, string(lead.GeoUnavailable), now, q.wakeBatch, now).Scan(&ids).Error; err != nil {
			return woken, err
		}
		woken += int64(len(ids))
		for _, id := range ids {
			if id > cursor {
				cursor = id
			}
		}
		if len(ids) < q.wakeBatch {
			return woken, nil
		}
	}
}

func (q *GeocodeQueue) Backlog(ctx context.Context) (map[lead.GeoStatus]int64, error) {
	var rows []struct {
		Status string
		N      int64
	}
	if err := q.db.WithContext(ctx).Raw(geocodeBacklogSQL).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[lead.GeoStatus]int64, len(rows))
	for _, row := range rows {
		out[lead.GeoStatus(row.Status)] = row.N
	}
	return out, nil
}
