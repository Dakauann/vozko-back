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
	// Approximate: sum duration of intervals overlapping the window, by state.
	// Uses LEAST/GREATEST for clip; ended_at null treated as now.
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
	// Postgres: epoch ms of clipped interval length.
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
			// on_call time also counts as available for occupancy denominator
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

// LastSeen implements the presence read the roulette's last_seen mode needs.
//
// COALESCE(ended_at, started_at) — not NOW() — is the load-bearing detail. An
// open interval means one of two things: the user is connected right now, in
// which case the caller's live connected-set overlay already reports them as
// online and this value is never consulted; or a replica died without
// unregistering, in which case the row will stay open forever and reading it as
// "present now" would pin a departed user to the head of the ring for good.
// started_at is the last moment we can prove they were there.
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
	// The state filter is a no-op today (offline never creates a row) and is
	// spelled out so a future state cannot silently start counting as presence.
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
