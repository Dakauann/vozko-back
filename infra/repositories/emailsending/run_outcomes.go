package emailsending_repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	es "vozko/domain/emailsending"
)

const runOutcomeSQL = `INSERT INTO email_run_outcomes AS o (run_kind, run_id, delivered, hard_bounces, complaints, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT (run_kind, run_id) DO UPDATE SET delivered = o.delivered + EXCLUDED.delivered, hard_bounces = o.hard_bounces + EXCLUDED.hard_bounces,
  complaints = o.complaints + EXCLUDED.complaints, updated_at = EXCLUDED.updated_at`

func countRunOutcome(tx *gorm.DB, m es.Message, effect es.EventEffect, at time.Time) error {
	if m.RunKind != es.RunCampaign || m.RunID == "" || !effect.CountsTowardRunOutcome() {
		return nil
	}
	return tx.Exec(runOutcomeSQL, string(m.RunKind), m.RunID, flag(effect.Delivered), flag(effect.HardBounced), flag(effect.Complained), at).Error
}

func flag(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

type RunOutcomes struct {
	db *gorm.DB
}

var _ es.RunOutcomeReader = (*RunOutcomes)(nil)

func NewRunOutcomes(db *gorm.DB) *RunOutcomes {
	return &RunOutcomes{db: db}
}

func (r *RunOutcomes) RunOutcome(ctx context.Context, kind es.RunKind, runID string) (es.RunOutcome, error) {
	var row struct {
		Delivered   int64
		HardBounces int64
		Complaints  int64
	}
	err := r.db.WithContext(ctx).Raw("SELECT delivered, hard_bounces, complaints FROM email_run_outcomes WHERE run_kind = ? AND run_id = ?", string(kind), runID).Scan(&row).Error
	if err != nil {
		return es.RunOutcome{}, err
	}
	return es.RunOutcome{Delivered: row.Delivered, HardBounces: row.HardBounces, Complaints: row.Complaints}, nil
}
