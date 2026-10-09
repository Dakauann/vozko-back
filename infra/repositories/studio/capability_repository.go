package studio_repository

import (
	"context"
	"encoding/json"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vozko/domain/studio"
	"vozko/infra/database/schema"
)

var updatedCapabilityColumns = []string{
	"kind", "backend", "gpu_vendor", "gpu_renderer", "webgpu", "decode", "encode_video", "encode_audio", "pixel_ratio", "cores", "memory_gb", "user_agent",
	"frames", "slow_frames", "stalls", "context_losses", "decode_fallbacks", "worker_failures", "browser_exports", "export_failures", "updated_at",
}

type capabilityRepository struct {
	db *gorm.DB
}

var _ studio.CapabilityRepository = (*capabilityRepository)(nil)

func NewCapabilities(db *gorm.DB) studio.CapabilityRepository {
	return &capabilityRepository{db: db}
}

func (r *capabilityRepository) Upsert(ctx context.Context, report studio.CapabilityReport) error {
	record, err := capabilityRecord(report)
	if err != nil {
		return err
	}
	result := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "session_id"}},
		Where:     clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: "studio_capability_reports.workspace_id = excluded.workspace_id AND studio_capability_reports.user_id = excluded.user_id"}}},
		DoUpdates: clause.AssignmentColumns(updatedCapabilityColumns),
	}).Create(record)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return studio.ErrSessionTaken
	}
	return nil
}

func capabilityRecord(report studio.CapabilityReport) (*schema.StudioCapabilityReport, error) {
	failures := report.Usage.ExportFailures
	if failures == nil {
		failures = map[studio.ExportFailure]int64{}
	}
	encoded, err := json.Marshal(failures)
	if err != nil {
		return nil, err
	}
	c, u := report.Capabilities, report.Usage
	return &schema.StudioCapabilityReport{
		SessionID: report.SessionID, WorkspaceID: report.WorkspaceID, UserID: report.UserID, Kind: string(report.Kind), UserAgent: report.UserAgent,
		Backend: string(c.Backend), GPUVendor: c.GPUVendor, GPURenderer: c.GPURenderer, WebGPU: c.WebGPU, Decode: c.Decode, EncodeVideo: c.EncodeVideo, EncodeAudio: c.EncodeAudio,
		PixelRatio: c.PixelRatio, Cores: c.Cores, MemoryGB: c.MemoryGB,
		Frames: u.Frames, SlowFrames: u.SlowFrames, Stalls: u.Stalls, ContextLosses: u.ContextLosses, DecodeFallbacks: u.DecodeFallbacks, WorkerFailures: u.WorkerFailures,
		BrowserExports: u.BrowserExports, ExportFailures: encoded,
	}, nil
}
