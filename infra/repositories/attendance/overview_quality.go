package attendance_repository

import (
	"gorm.io/gorm"

	"vozko/domain/attendance"
	"vozko/domain/conversation"
)

func overviewQualityTX(tx *gorm.DB, msgTmp string, policy attendance.QualityPolicy) (attendance.Quality, error) {
	if !policy.Enabled {
		return attendance.UnavailableQuality(attendance.ReasonCaptureDisabled), nil
	}
	if !policy.Measurable() {
		return attendance.UnavailableQuality(attendance.ReasonCaptureNotDated), nil
	}

	durable := policy.DurableCodes
	if len(durable) == 0 {
		durable = []string{""}
	}

	type qualityRow struct {
		ActorID     string `gorm:"column:actor_id"`
		ActorKind   string `gorm:"column:actor_kind"`
		DisplayName string `gorm:"column:display_name"`
		Closes      int64  `gorm:"column:closes"`
		Captured    int64  `gorm:"column:captured"`
		Durable     int64  `gorm:"column:durable"`
	}

	sql := `
		SELECT
			m.assigned_user_id AS actor_id,
			CASE
				WHEN m.close_source = 'ai' THEN 'ai'
				WHEN m.close_source = 'system' THEN 'system'
				ELSE 'human'
			END AS actor_kind,
			COALESCE(NULLIF(u.username, ''), NULLIF(u.email, ''), m.assigned_user_id) AS display_name,
			COUNT(*)::bigint AS closes,
			COUNT(*) FILTER (
				WHERE m.close_outcome <> '' AND LEFT(m.close_outcome, 1) <> ?
			)::bigint AS captured,
			COUNT(*) FILTER (
				WHERE m.close_outcome <> ''
				  AND LEFT(m.close_outcome, 1) <> ?
				  AND m.close_outcome IN (?)
			)::bigint AS durable
		FROM ` + msgTmp + ` m
		LEFT JOIN users u ON u.id::text = m.assigned_user_id
		WHERE m.total_msgs > 0
		  AND m.status_bucket = 'finished'
		  AND m.assigned_user_id <> ''
		  AND m.closed_at IS NOT NULL
		  AND m.closed_at >= ?
		GROUP BY m.assigned_user_id, 2, u.username, u.email
	`
	var rows []qualityRow
	err := tx.Raw(
		sql,
		conversation.ReservedOutcomePrefix,
		conversation.ReservedOutcomePrefix,
		durable,
		*policy.EnabledAt,
	).Scan(&rows).Error
	if err != nil {
		return attendance.Quality{}, err
	}

	notCapturedSQL := `
		SELECT COUNT(*)::bigint
		FROM ` + msgTmp + ` m
		WHERE m.total_msgs > 0
		  AND m.status_bucket = 'finished'
		  AND (m.closed_at IS NULL OR m.closed_at < ?)
	`
	var notCaptured int64
	if err := tx.Raw(notCapturedSQL, *policy.EnabledAt).Scan(&notCaptured).Error; err != nil {
		return attendance.Quality{}, err
	}

	tallies := make([]attendance.QualityTally, 0, len(rows))
	for _, row := range rows {
		tallies = append(tallies, attendance.QualityTally{
			ActorID:     row.ActorID,
			ActorKind:   row.ActorKind,
			DisplayName: row.DisplayName,
			Closes:      row.Closes,
			Captured:    row.Captured,
			Durable:     row.Durable,
		})
	}

	return attendance.BuildQuality(tallies, policy.Threshold, policy.EnabledAt, notCaptured), nil
}
