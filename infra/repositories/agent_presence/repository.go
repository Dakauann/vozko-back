package agent_presence_repository

import (
	"math"
	"time"

	"gorm.io/gorm"

	ap "vozko/domain/agent_presence"
	"vozko/infra/database/schema"
)

type repository struct {
	db *gorm.DB
}

func New(db *gorm.DB) ap.Repository {
	return &repository{db: db}
}

func (r *repository) Transition(workspaceID, userID string, state ap.State, source string, at time.Time) error {
	if workspaceID == "" || userID == "" {
		return nil
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&schema.AgentPresenceInterval{}).
			Where("workspace_id = ? AND user_id = ? AND ended_at IS NULL", workspaceID, userID).
			Update("ended_at", at).Error; err != nil {
			return err
		}
		if state == ap.StateOffline || !state.Valid() {
			return nil
		}
		rec := schema.AgentPresenceInterval{
			WorkspaceID: workspaceID,
			UserID:      userID,
			State:       string(state),
			Source:      source,
			StartedAt:   at,
		}
		return tx.Create(&rec).Error
	})
}

func (r *repository) Occupancy(workspaceID string, from, to *time.Time) ([]ap.OccupancyRow, error) {
	now := time.Now().UTC()
	windowStart := time.Time{}
	windowEnd := now
	if from != nil {
		windowStart = *from
	}
	if to != nil {
		windowEnd = *to
	}

	type row struct {
		UserID string
		State  string
		MS     int64
	}
	sql := `
		SELECT user_id, state,
			GREATEST(0, EXTRACT(EPOCH FROM (
				LEAST(COALESCE(ended_at, ?), ?) - GREATEST(started_at, ?)
			)) * 1000)::bigint AS ms
		FROM agent_presence_intervals
		WHERE workspace_id = ?
		  AND started_at < ?
		  AND (ended_at IS NULL OR ended_at > ?)
	`
	var rows []row
	if err := r.db.Raw(sql, now, windowEnd, windowStart, workspaceID, windowEnd, windowStart).Scan(&rows).Error; err != nil {
		return nil, err
	}
	byUser := map[string]*ap.OccupancyRow{}
	for _, rw := range rows {
		o := byUser[rw.UserID]
		if o == nil {
			o = &ap.OccupancyRow{UserID: rw.UserID}
			byUser[rw.UserID] = o
		}
		switch ap.State(rw.State) {
		case ap.StateOnline:
			o.OnlineMS += rw.MS
		case ap.StateOnCall:
			o.OnCallMS += rw.MS
			o.OnlineMS += rw.MS
		}
	}
	out := make([]ap.OccupancyRow, 0, len(byUser))
	for _, o := range byUser {
		if o.OnlineMS > 0 {
			o.Occupancy = math.Round(float64(o.OnCallMS)/float64(o.OnlineMS)*10000) / 100
		}
		out = append(out, *o)
	}
	return out, nil
}

func (r *repository) LastSeen(workspaceID string, userIDs []string) (map[string]time.Time, error) {
	out := make(map[string]time.Time, len(userIDs))
	if workspaceID == "" || len(userIDs) == 0 {
		return out, nil
	}

	type row struct {
		UserID   string    `gorm:"column:user_id"`
		LastSeen time.Time `gorm:"column:last_seen"`
	}
	var rows []row
	err := r.db.Model(&schema.AgentPresenceInterval{}).
		Select("user_id, MAX(COALESCE(ended_at, started_at)) AS last_seen").
		Where("workspace_id = ? AND user_id IN ? AND state IN ?",
			workspaceID, userIDs, []string{string(ap.StateOnline), string(ap.StateOnCall), string(ap.StateWrapUp)}).
		Group("user_id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, item := range rows {
		if item.UserID == "" || item.LastSeen.IsZero() {
			continue
		}
		out[item.UserID] = item.LastSeen.UTC()
	}
	return out, nil
}
