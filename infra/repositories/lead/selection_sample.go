package lead

import (
	"context"
	"fmt"
	"strconv"

	"gorm.io/gorm"

	"vozko/domain/geo"
	"vozko/domain/lead"
	"vozko/domain/leadmap"
	infracrmfilter "vozko/infra/repositories/crmfilter"
)

type sampleRow struct {
	Point     pointRow `gorm:"embedded"`
	Positions int
	Located   int
}

func (s *selectionQuery) members() (string, []interface{}) {
	if s.order != "" {
		return s.chosen()
	}
	return "SELECT leads.id FROM leads WHERE " + s.where, append([]interface{}{}, s.args...)
}

func (s *selectionQuery) sampleSQL(workspaceID string, limit int) (string, []interface{}) {
	class, classArgs := placementClass("a")
	members, memberArgs := s.members()
	sql := "SELECT a.latitude AS lat, a.longitude AS lng, " + class + ", COUNT(*) AS people," +
		" min(array_position(?::text[], a.geo_precision::text)) AS precision_rank," +
		" (array_agg(a.lead_id::text ORDER BY a.lead_id))[1:" + strconv.Itoa(leadmap.MaxLeadIDsPerPoint) + "] AS lead_ids," +
		" NULL::text AS value, COUNT(*) OVER () AS positions, (SUM(COUNT(*)) OVER ())::bigint AS located" +
		" FROM lead_addresses a WHERE a.workspace_id = ? AND a.is_primary AND a.latitude IS NOT NULL AND a.longitude IS NOT NULL" +
		" AND a.lead_id IN (" + members + ")" +
		" GROUP BY 1, 2, 3 ORDER BY md5(a.latitude::text || ',' || a.longitude::text), 3 LIMIT ?"
	args := append(append([]interface{}{}, classArgs...), infracrmfilter.PrecisionArray(geo.PrecisionsBestFirst()), workspaceID)
	args = append(args, memberArgs...)
	return sql, append(args, limit)
}

func (r *repository) SelectionSample(ctx context.Context, sq lead.SelectionQuery, limit int) (leadmap.Sample, error) {
	if err := leadmap.CheckSampleLimit(limit); err != nil {
		return leadmap.Sample{}, err
	}
	s, err := r.selection(sq)
	if err != nil {
		return leadmap.Sample{}, err
	}
	sql, args := s.sampleSQL(sq.WorkspaceID, limit)
	var rows []sampleRow
	if err := r.inSelectionSession(ctx, s, func(tx *gorm.DB) error { return tx.Raw(sql, args...).Scan(&rows).Error }); err != nil {
		return leadmap.Sample{}, fmt.Errorf("sample lead selection positions: %w", err)
	}
	points := make([]pointRow, 0, len(rows))
	sample := leadmap.Sample{}
	for _, row := range rows {
		points = append(points, row.Point)
		sample.Positions, sample.Located = row.Positions, row.Located
	}
	layer, err := pointLayer(points)
	if err != nil {
		return leadmap.Sample{}, err
	}
	sample.Points = layer.Points
	return sample, nil
}

var _ leadmap.SampleReader = (*repository)(nil)
