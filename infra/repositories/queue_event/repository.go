package queue_event_repository

import (
	"math"
	"time"

	"gorm.io/gorm"

	qe "vozko/domain/queue_event"
	"vozko/infra/database/schema"
)

type repository struct {
	db *gorm.DB
}

func New(db *gorm.DB) qe.Repository {
	return &repository{db: db}
}

func (r *repository) Create(e *qe.Event) error {
	if e.OccurredAt.IsZero() {
		e.OccurredAt = time.Now().UTC()
	}
	rec := schema.QueueEvent{
		ID:          e.ID,
		WorkspaceID: e.WorkspaceID,
		TransferID:  e.TransferID,
		CallID:      e.CallID,
		TargetKind:  e.TargetKind,
		TargetID:    e.TargetID,
		Type:        e.Type,
		Position:    e.Position,
		WaitedMS:    e.WaitedMS,
		OccurredAt:  e.OccurredAt,
	}
	if err := r.db.Create(&rec).Error; err != nil {
		return err
	}
	e.ID = rec.ID
	e.CreatedAt = rec.CreatedAt
	return nil
}

func (r *repository) Stats(workspaceID string, from, to *time.Time) (*qe.Stats, error) {
	return r.StatsWithSL(workspaceID, from, to, 20)
}

func (r *repository) StatsWithSL(workspaceID string, from, to *time.Time, slSeconds int) (*qe.Stats, error) {
	if slSeconds <= 0 {
		slSeconds = 20
	}
	type row struct {
		Type string
		Cnt  int64
	}
	q := r.db.Model(&schema.QueueEvent{}).
		Select("type, COUNT(*) AS cnt").
		Where("workspace_id = ?", workspaceID)
	if from != nil {
		q = q.Where("occurred_at >= ?", *from)
	}
	if to != nil {
		q = q.Where("occurred_at <= ?", *to)
	}
	var rows []row
	if err := q.Group("type").Scan(&rows).Error; err != nil {
		return nil, err
	}
	st := &qe.Stats{}
	for _, rw := range rows {
		switch rw.Type {
		case "enqueued":
			st.Enqueued = rw.Cnt
		case "connected":
			st.Connected = rw.Cnt
		case "abandoned":
			st.Abandoned = rw.Cnt
		case "overflow":
			st.Overflow = rw.Cnt
		case "queue_full":
			st.QueueFull = rw.Cnt
		case "cancelled":
			st.Cancelled = rw.Cnt
		}
	}
	var avg *float64
	avgQ := r.db.Model(&schema.QueueEvent{}).
		Select("AVG(waited_ms)").
		Where("workspace_id = ? AND type = ?", workspaceID, "connected")
	if from != nil {
		avgQ = avgQ.Where("occurred_at >= ?", *from)
	}
	if to != nil {
		avgQ = avgQ.Where("occurred_at <= ?", *to)
	}
	_ = avgQ.Scan(&avg).Error
	if avg != nil {
		st.AvgASAMs = math.Round(*avg*100) / 100
	}
	var maxW *float64
	maxQ := r.db.Model(&schema.QueueEvent{}).
		Select("MAX(waited_ms)").
		Where("workspace_id = ? AND type IN ?", workspaceID, []string{"connected", "abandoned"})
	if from != nil {
		maxQ = maxQ.Where("occurred_at >= ?", *from)
	}
	if to != nil {
		maxQ = maxQ.Where("occurred_at <= ?", *to)
	}
	_ = maxQ.Scan(&maxW).Error
	if maxW != nil {
		st.MaxWaitMS = *maxW
	}
	// Service level on queue: connected within threshold.
	slMS := int64(slSeconds) * 1000
	var within int64
	slQ := r.db.Model(&schema.QueueEvent{}).
		Where("workspace_id = ? AND type = ? AND waited_ms <= ?", workspaceID, "connected", slMS)
	if from != nil {
		slQ = slQ.Where("occurred_at >= ?", *from)
	}
	if to != nil {
		slQ = slQ.Where("occurred_at <= ?", *to)
	}
	_ = slQ.Count(&within).Error
	st.ConnectedWithinSL = within
	if st.Connected > 0 {
		st.ServiceLevelPct = math.Round(float64(within)/float64(st.Connected)*10000) / 100
	}
	if st.Enqueued > 0 {
		st.AbandonRate = math.Round(float64(st.Abandoned)/float64(st.Enqueued)*10000) / 100
	}
	return st, nil
}

// Sink implements dialer queue EventSink asynchronously.
type Sink struct {
	repo qe.Repository
}

func NewSink(repo qe.Repository) *Sink {
	return &Sink{repo: repo}
}

// QueueEvent is the dialer queue.EventSink method (duck-typed; package cannot import usecases).
// Callers adapt dialer.queue.Event → domain via container wiring.
func (s *Sink) Persist(workspaceID, transferID, callID, targetKind, targetID, typ string, position int, waitedMS int64, at time.Time) {
	if s == nil || s.repo == nil || workspaceID == "" {
		return
	}
	go func() {
		_ = s.repo.Create(&qe.Event{
			WorkspaceID: workspaceID,
			TransferID:  transferID,
			CallID:      callID,
			TargetKind:  targetKind,
			TargetID:    targetID,
			Type:        typ,
			Position:    position,
			WaitedMS:    waitedMS,
			OccurredAt:  at,
		})
	}()
}
