package telemetry_dedupe_repository

import (
	"time"

	"gorm.io/gorm"

	"vozko/infra/database/schema"
)

type Repository struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Claim(id, kind string) (bool, error) {
	if r == nil || r.db == nil || id == "" {
		return true, nil
	}
	res := r.db.Exec(
		`INSERT INTO telemetry_dedupe (id, kind, created_at) VALUES (?, ?, ?) ON CONFLICT (id) DO NOTHING`,
		id, kind, time.Now().UTC(),
	)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func (r *Repository) Release(id string) error {
	if r == nil || r.db == nil || id == "" {
		return nil
	}
	return r.db.Where("id = ?", id).Delete(&schema.TelemetryDedupe{}).Error
}
