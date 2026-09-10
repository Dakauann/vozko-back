package attendance_repository

import (
	"gorm.io/gorm"

	"vozko/domain/attendance"
)

// overviewStageTalliesTX reads where the scoped conversations are sitting.
//
// It is ONE more pass over msgTmp, the per-entry aggregate the overview has
// already materialised. That table is the whole reason this page stopped
// re-joining conversation_messages five times, so the stage panel reads it too:
// no second scope derivation, no second message scan.
//
//	msgTmp ──join──▶ entry_stages (one live row per entry)
//	                 ──join──▶ stages ──left join──▶ pipelines
//
// Driving the assignment lookup FROM msgTmp is what keeps this cheap: the scope
// is already narrowed to the period, so entry_stages is probed through
// idx_et_entry_type_ws (entry_id, entry_type, workspace_id) instead of scanning
// every stage assignment the workspace has ever made.
//
// The DISTINCT ON is not defensive decoration. AssignStage soft-deletes the
// previous row and inserts a new one, so a conversation can carry more than one
// row that looks live, and a plain join would count it in two stages at once.
// GetBatchEntryStages already resolves the same ambiguity when reading a single
// entry; this picks the most recent assignment for the same reason.
//
// Column types were verified against production rather than read off the gorm
// tags, which disagree: entry_stages.stage_id and stages.id are both text (the
// type:uuid tag on the former is not what the column is), every channel's entry
// id is uuid, and stages.pipeline_id / pipelines.id are uuid. Only the nullable
// funnel id needs a cast.
//
// The shaping (grouping, ordering, percentages) is deliberately NOT here. It
// lives in domain/attendance/stage_distribution.go, where it is unit-tested
// without a database.
func overviewStageTalliesTX(tx *gorm.DB, workspaceID, msgTmp string) ([]attendance.StageTally, error) {
	sql := `
		WITH live_stage AS (
			SELECT DISTINCT ON (es.entry_id, es.entry_type)
				es.entry_id,
				es.entry_type,
				es.stage_id,
				es.created_at AS staged_at
			FROM ` + msgTmp + ` m
			JOIN entry_stages es
				ON es.entry_id = m.entry_id
				AND es.entry_type = m.entry_type
			WHERE es.workspace_id = ?::uuid
			  AND es.deleted_at IS NULL
			ORDER BY es.entry_id, es.entry_type, es.created_at DESC
		)
		SELECT
			COALESCE(p.id::text, '')            AS funnel_id,
			COALESCE(p.name, '')                AS funnel_name,
			COALESCE(p.is_default, FALSE)       AS funnel_is_default,
			s.id                                AS stage_id,
			COALESCE(s.name, '')                AS stage_name,
			COALESCE(s.color, '')               AS color,
			COALESCE(s.position, 0)             AS position,
			COALESCE(s.is_won, FALSE)           AS is_won,
			COALESCE(s.is_lost, FALSE)          AS is_lost,
			s.rot_days                          AS rot_days,
			COUNT(*) FILTER (WHERE m.total_msgs > 0)  AS engaged,
			COUNT(*) FILTER (WHERE m.total_msgs = 0)  AS shell,
			COUNT(*) FILTER (WHERE m.total_msgs > 0 AND m.status_bucket = 'finished') AS finished,
			COUNT(*) FILTER (WHERE m.total_msgs > 0 AND m.status_bucket = 'ongoing')  AS ongoing,
			COUNT(*) FILTER (WHERE m.total_msgs > 0 AND m.status_bucket = 'pending')  AS pending,
			AVG(EXTRACT(EPOCH FROM (NOW() - ls.staged_at)) / 86400.0) FILTER (
				WHERE m.total_msgs > 0 AND m.status_bucket <> 'finished'
			) AS avg_open_days,
			MAX(EXTRACT(EPOCH FROM (NOW() - ls.staged_at)) / 86400.0) FILTER (
				WHERE m.total_msgs > 0 AND m.status_bucket <> 'finished'
			) AS oldest_open_days,
			COUNT(*) FILTER (
				WHERE m.total_msgs > 0
				  AND m.status_bucket <> 'finished'
				  AND ls.staged_at < NOW() - (INTERVAL '1 day' * COALESCE(s.rot_days, ?)::float8)
			) AS stuck
		FROM ` + msgTmp + ` m
		JOIN live_stage ls
			ON ls.entry_id = m.entry_id
			AND ls.entry_type = m.entry_type
		JOIN stages s
			ON s.id = ls.stage_id
			AND s.deleted_at IS NULL
		LEFT JOIN pipelines p
			ON p.id = s.pipeline_id
			AND p.deleted_at IS NULL
		GROUP BY p.id, p.name, p.is_default,
			s.id, s.name, s.color, s.position, s.is_won, s.is_lost, s.rot_days
	`

	// Dwell and the stuck count measure OPEN engaged conversations against NOW,
	// not against the period end. "9,4 days in Documentação" is a fact about this
	// moment, and the panel says so; measuring to the period end would report a
	// backlog that has since cleared as though it were still sitting there.
	type row struct {
		FunnelID        string   `gorm:"column:funnel_id"`
		FunnelName      string   `gorm:"column:funnel_name"`
		FunnelIsDefault bool     `gorm:"column:funnel_is_default"`
		StageID         string   `gorm:"column:stage_id"`
		StageName       string   `gorm:"column:stage_name"`
		Color           string   `gorm:"column:color"`
		Position        int      `gorm:"column:position"`
		IsWon           bool     `gorm:"column:is_won"`
		IsLost          bool     `gorm:"column:is_lost"`
		RotDays         *int     `gorm:"column:rot_days"`
		Engaged         int64    `gorm:"column:engaged"`
		Shell           int64    `gorm:"column:shell"`
		Finished        int64    `gorm:"column:finished"`
		Ongoing         int64    `gorm:"column:ongoing"`
		Pending         int64    `gorm:"column:pending"`
		AvgOpenDays     *float64 `gorm:"column:avg_open_days"`
		OldestOpenDays  *float64 `gorm:"column:oldest_open_days"`
		Stuck           int64    `gorm:"column:stuck"`
	}
	var rows []row
	if err := tx.Raw(sql, workspaceID, attendance.DefaultStageStuckDays).Scan(&rows).Error; err != nil {
		return nil, err
	}

	out := make([]attendance.StageTally, 0, len(rows))
	for _, r := range rows {
		out = append(out, attendance.StageTally{
			FunnelID:        r.FunnelID,
			FunnelName:      r.FunnelName,
			FunnelIsDefault: r.FunnelIsDefault,
			StageID:         r.StageID,
			StageName:       r.StageName,
			Color:           r.Color,
			Position:        r.Position,
			IsWon:           r.IsWon,
			IsLost:          r.IsLost,
			RotDays:         r.RotDays,
			Engaged:         r.Engaged,
			Shell:           r.Shell,
			Finished:        r.Finished,
			Ongoing:         r.Ongoing,
			Pending:         r.Pending,
			AvgOpenDays:     r.AvgOpenDays,
			OldestOpenDays:  r.OldestOpenDays,
			Stuck:           r.Stuck,
		})
	}
	return out, nil
}
