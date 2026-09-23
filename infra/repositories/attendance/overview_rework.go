package attendance_repository

import (
	"gorm.io/gorm"

	"vozko/domain/attendance"
	"vozko/domain/conversation_event"
)

const unassignedReworkActor = ""

func overviewReworkTX(
	tx *gorm.DB,
	workspaceID, msgTmp string,
	filter attendance.OverviewFilter,
) (attendance.OverviewRework, error) {
	type reworkRow struct {
		ActorID     string `gorm:"column:actor_id"`
		ActorKind   string `gorm:"column:actor_kind"`
		DisplayName string `gorm:"column:display_name"`
		Finished    int64  `gorm:"column:finished"`
		Reopened    int64  `gorm:"column:reopened"`
		Templates   int64  `gorm:"column:templates"`
		CostMicros  int64  `gorm:"column:cost_micros"`
	}

	query := reworkQuery(workspaceID, msgTmp)
	sql, args := query.build()

	var rows []reworkRow
	if err := tx.Raw(sql, args...).Scan(&rows).Error; err != nil {
		return attendance.OverviewRework{}, err
	}

	currency, err := workspaceBillingCurrency(tx, workspaceID)
	if err != nil {
		return attendance.OverviewRework{}, err
	}

	tallies := make([]attendance.ReworkTally, 0, len(rows))
	var unassigned attendance.ReworkTally
	for _, row := range rows {
		tally := attendance.ReworkTally{
			ActorID:     row.ActorID,
			ActorKind:   row.ActorKind,
			DisplayName: row.DisplayName,
			Finished:    row.Finished,
			Reopened:    row.Reopened,
			Templates:   row.Templates,
			CostMicros:  row.CostMicros,
		}
		if row.ActorID == unassignedReworkActor {
			unassigned = tally
			continue
		}
		tallies = append(tallies, tally)
	}

	_ = filter
	return attendance.BuildRework(tallies, unassigned, currency), nil
}

func reworkQuery(workspaceID, msgTmp string) *sqlQuery {
	query := newSQLQuery()
	query.add(`
		WITH reopened AS (
			SELECT ev.entry_id::text AS entry_id, ev.entry_type, MIN(ev.created_at) AS reopened_at
			FROM conversation_events ev
			WHERE ev.workspace_id = ?::uuid
			  AND ev.event_type = ?
			GROUP BY ev.entry_id, ev.entry_type
		),
		closes AS (
			SELECT m.entry_id,
				m.entry_type,
				m.assigned_user_id,
				CASE
					WHEN m.close_source = 'ai' THEN 'ai'
					WHEN m.close_source = 'system' THEN 'system'
					ELSE 'human'
				END AS actor_kind,
				r.reopened_at
			FROM `+msgTmp+` m
			LEFT JOIN reopened r
				ON r.entry_id = m.entry_id::text AND r.entry_type = m.entry_type
			WHERE m.total_msgs > 0 AND m.status_bucket = 'finished'
		),
		rework_cost AS (
			SELECT c.entry_id,
				COUNT(ts.id)::bigint AS templates,
				COALESCE(SUM(ts.charged_micros), 0)::bigint AS cost_micros
			FROM closes c
			JOIN whatsapp_template_sends ts
				ON ts.entry_id = c.entry_id
				AND ts.deleted_at IS NULL
				AND ts.refunded_at IS NULL
				AND ts.charged_micros > 0
				AND COALESCE(ts.charged_at, ts.sent_at, ts.created_at) >= c.reopened_at
			WHERE c.reopened_at IS NOT NULL
			GROUP BY c.entry_id
		)
		SELECT c.assigned_user_id AS actor_id,
			MAX(c.actor_kind) AS actor_kind,
			COALESCE(NULLIF(MAX(u.username), ''), NULLIF(MAX(u.email), ''), c.assigned_user_id) AS display_name,
			COUNT(*)::bigint AS finished,
			COUNT(*) FILTER (WHERE c.reopened_at IS NOT NULL)::bigint AS reopened,
			COALESCE(SUM(rc.templates), 0)::bigint AS templates,
			COALESCE(SUM(rc.cost_micros), 0)::bigint AS cost_micros
		FROM closes c
		LEFT JOIN users u ON u.id::text = c.assigned_user_id
		LEFT JOIN rework_cost rc ON rc.entry_id = c.entry_id
		GROUP BY c.assigned_user_id
	`, workspaceID, string(conversation_event.EventReopened))
	return query
}

func workspaceBillingCurrency(tx *gorm.DB, workspaceID string) (string, error) {
	var currency string
	err := tx.Raw(
		`SELECT currency FROM balances WHERE workspace_id = ?::uuid LIMIT 1`,
		workspaceID,
	).Scan(&currency).Error
	if err != nil {
		return "", err
	}
	return currency, nil
}
