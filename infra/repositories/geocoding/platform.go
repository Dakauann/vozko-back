package geocoding_repository

import (
	"context"
	"slices"
	"time"

	"github.com/lib/pq"
	"gorm.io/gorm"

	"vozko/domain/crmfilter"
	"vozko/domain/geocoding"
	"vozko/domain/leadmap"
	"vozko/infra/database"
	infracrmfilter "vozko/infra/repositories/crmfilter"
)

const (
	coverageAddressAlias     = "la"
	coverageChunk            = 5
	coverageStatementTimeout = "10s"

	directoryEligibleSQL = "w.deleted_at IS NULL AND (EXISTS (SELECT 1 FROM leads l WHERE l.workspace_id = w.id AND l.deleted_at IS NULL)" +
		" OR EXISTS (SELECT 1 FROM geocoding_settings s WHERE s.workspace_id = w.id)" +
		" OR EXISTS (SELECT 1 FROM geocoding_usage_months m WHERE m.workspace_id = w.id))"

	directorySearchSQL = " AND w.name ILIKE ?"
)

var coveragePlacements = []crmfilter.GeoPlacement{
	crmfilter.PlacementOnMap, crmfilter.PlacementApproximate, crmfilter.PlacementNotFound,
	crmfilter.PlacementQuotaExceeded, crmfilter.PlacementRefused, crmfilter.PlacementPending,
}

var coverageCounts, coverageCountArgs = infracrmfilter.GeoPlacementCountsSQL(coverageAddressAlias, coveragePlacements...)

var coverageSQL = "SELECT leads.workspace_id::text AS workspace_id, " + infracrmfilter.LeadAddressSummarySelect(coverageAddressAlias) + coverageCounts +
	" FROM leads " + infracrmfilter.LeadPrimaryAddressJoin(coverageAddressAlias) +
	" WHERE leads.workspace_id = ANY(?::uuid[]) AND leads.deleted_at IS NULL GROUP BY leads.workspace_id"

var coverageSessionSettings = database.ReadSessionSettings("32MB", coverageStatementTimeout)

func coverageArgs(ids pq.StringArray) []interface{} {
	return append(append([]interface{}{}, coverageCountArgs...), ids)
}

func directoryWhere(search bool) string {
	if search {
		return directoryEligibleSQL + directorySearchSQL
	}
	return directoryEligibleSQL
}

func directoryCountSQL(search bool) string {
	return "SELECT COUNT(*) FROM workspaces w WHERE " + directoryWhere(search)
}

func directoryPageSQL(search bool) string {
	return "SELECT w.id::text AS id, w.name FROM workspaces w" +
		" LEFT JOIN geocoding_usage u ON u.workspace_id = w.id AND u.cycle_start = ?" +
		" WHERE " + directoryWhere(search) +
		" ORDER BY COALESCE(u.requests, 0) DESC, lower(w.name), w.id LIMIT ? OFFSET ?"
}

type PlatformReader struct {
	db *gorm.DB
}

var (
	_ geocoding.PlatformDirectory = (*PlatformReader)(nil)
	_ geocoding.CoverageReader    = (*PlatformReader)(nil)
)

func NewPlatformReader(db *gorm.DB) *PlatformReader {
	return &PlatformReader{db: db}
}

func (r *PlatformReader) GeocodingWorkspaces(ctx context.Context, query geocoding.PlatformQuery, cycleStart time.Time) ([]geocoding.PlatformWorkspace, int64, error) {
	query = query.Normalize()
	search := query.Search != ""
	var filter []interface{}
	if search {
		filter = append(filter, database.LikeContains(query.Search))
	}
	db := r.db.WithContext(ctx)
	var total int64
	if err := db.Raw(directoryCountSQL(search), filter...).Scan(&total).Error; err != nil {
		return nil, 0, err
	}
	if total == 0 || int64(query.Offset()) >= total {
		return []geocoding.PlatformWorkspace{}, total, nil
	}
	args := append(append([]interface{}{cycleStart}, filter...), query.PageSize, query.Offset())
	var rows []geocoding.PlatformWorkspace
	if err := db.Raw(directoryPageSQL(search), args...).Scan(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

type coverageRow struct {
	WorkspaceID string
	infracrmfilter.LeadAddressSummaryRow
}

func (r *PlatformReader) Coverage(ctx context.Context, workspaceIDs []string) (map[string]leadmap.Summary, error) {
	out := make(map[string]leadmap.Summary, len(workspaceIDs))
	if len(workspaceIDs) == 0 {
		return out, nil
	}
	err := database.InReadSession(ctx, r.db, coverageSessionSettings, func(tx *gorm.DB) error {
		for chunk := range slices.Chunk(workspaceIDs, coverageChunk) {
			var rows []coverageRow
			if err := tx.Raw(coverageSQL, coverageArgs(pq.StringArray(chunk))...).Scan(&rows).Error; err != nil {
				return err
			}
			for _, row := range rows {
				out[row.WorkspaceID] = row.Summary()
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
