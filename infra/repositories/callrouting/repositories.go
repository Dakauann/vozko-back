package callrouting_repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vozko/domain/callrouting"
	"vozko/infra/database/schema"
)

type QueueRepository struct{ db *gorm.DB }

var _ callrouting.QueueRepository = (*QueueRepository)(nil)

func NewQueueRepository(db *gorm.DB) *QueueRepository { return &QueueRepository{db: db} }

func (r *QueueRepository) Create(ctx context.Context, queue *callrouting.Queue) error {
	record, err := queueToSchema(queue)
	if err != nil {
		return err
	}
	if err := r.db.WithContext(ctx).Create(record).Error; err != nil {
		return err
	}
	queue.ID, queue.CreatedAt, queue.UpdatedAt = record.ID, record.CreatedAt, record.UpdatedAt
	return nil
}

func (r *QueueRepository) Update(ctx context.Context, queue *callrouting.Queue) error {
	record, err := queueToSchema(queue)
	if err != nil {
		return err
	}
	result := r.db.WithContext(ctx).Model(&schema.CallQueue{}).
		Where("id = ? AND workspace_id = ?", queue.ID, queue.WorkspaceID).
		Updates(map[string]any{
			"name":             record.Name,
			"strategy":         record.Strategy,
			"department_id":    record.DepartmentID,
			"member_user_ids":  record.MemberUserIDs,
			"ring_seconds":     record.RingSeconds,
			"max_wait_seconds": record.MaxWaitSeconds,
			"wrap_up_seconds":  record.WrapUpSeconds,
			"hold_preset_id":   record.HoldPresetID,
			"hold_media_id":    record.HoldMediaID,
			"updated_at":       time.Now(),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return callrouting.ErrQueueNotFound
	}
	return nil
}

func (r *QueueRepository) Delete(ctx context.Context, workspaceID, id string) error {
	result := r.db.WithContext(ctx).Delete(&schema.CallQueue{}, "id = ? AND workspace_id = ?", id, workspaceID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return callrouting.ErrQueueNotFound
	}
	return nil
}

func (r *QueueRepository) FindInWorkspace(ctx context.Context, workspaceID, id string) (*callrouting.Queue, error) {
	var record schema.CallQueue
	err := r.db.WithContext(ctx).Where("id = ? AND workspace_id = ?", id, workspaceID).First(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, callrouting.ErrQueueNotFound
	}
	if err != nil {
		return nil, err
	}
	return queueToDomain(&record)
}

func (r *QueueRepository) ListByWorkspace(ctx context.Context, workspaceID string) ([]*callrouting.Queue, error) {
	var records []schema.CallQueue
	if err := r.db.WithContext(ctx).Where("workspace_id = ?", workspaceID).Order("name ASC").Find(&records).Error; err != nil {
		return nil, err
	}
	queues := make([]*callrouting.Queue, 0, len(records))
	for i := range records {
		queue, err := queueToDomain(&records[i])
		if err != nil {
			return nil, err
		}
		queues = append(queues, queue)
	}
	return queues, nil
}

func queueToSchema(queue *callrouting.Queue) (*schema.CallQueue, error) {
	members, err := json.Marshal(append([]string{}, queue.MemberUserIDs...))
	if err != nil {
		return nil, err
	}
	return &schema.CallQueue{
		ID:             queue.ID,
		WorkspaceID:    queue.WorkspaceID,
		Name:           queue.Name,
		Strategy:       string(queue.Strategy),
		DepartmentID:   queue.DepartmentID,
		MemberUserIDs:  datatypes.JSON(members),
		RingSeconds:    queue.RingSeconds,
		MaxWaitSeconds: queue.MaxWaitSeconds,
		WrapUpSeconds:  queue.WrapUpSeconds,
		HoldPresetID:   queue.HoldMusic.PresetID,
		HoldMediaID:    queue.HoldMusic.MediaID,
	}, nil
}

func queueToDomain(record *schema.CallQueue) (*callrouting.Queue, error) {
	var members []string
	if len(record.MemberUserIDs) > 0 {
		if err := json.Unmarshal(record.MemberUserIDs, &members); err != nil {
			return nil, err
		}
	}
	return &callrouting.Queue{
		ID:             record.ID,
		WorkspaceID:    record.WorkspaceID,
		Name:           record.Name,
		Strategy:       callrouting.Strategy(record.Strategy),
		DepartmentID:   record.DepartmentID,
		MemberUserIDs:  members,
		RingSeconds:    record.RingSeconds,
		MaxWaitSeconds: record.MaxWaitSeconds,
		WrapUpSeconds:  record.WrapUpSeconds,
		HoldMusic:      callrouting.HoldMusicRef{PresetID: record.HoldPresetID, MediaID: record.HoldMediaID},
		CreatedAt:      record.CreatedAt,
		UpdatedAt:      record.UpdatedAt,
	}, nil
}

type TransferLog struct{ db *gorm.DB }

var _ callrouting.TransferLog = (*TransferLog)(nil)

func NewTransferLog(db *gorm.DB) *TransferLog { return &TransferLog{db: db} }

func (l *TransferLog) Record(ctx context.Context, record callrouting.TransferRecord) error {
	return l.db.WithContext(ctx).Create(&schema.CallTransfer{
		ID:            record.ID,
		WorkspaceID:   record.WorkspaceID,
		CallID:        record.CallID,
		FromUserID:    record.FromUserID,
		TargetKind:    string(record.Target.Kind),
		TargetQueueID: record.Target.QueueID,
		TargetUserID:  record.Target.UserID,
		Notes:         record.Notes,
		Outcome:       string(record.Outcome),
		CreatedAt:     record.CreatedAt,
	}).Error
}

func (l *TransferLog) Finish(ctx context.Context, workspaceID, id string, outcome callrouting.TransferOutcome, answeredBy string, at time.Time) error {
	return l.db.WithContext(ctx).Model(&schema.CallTransfer{}).
		Where("id = ? AND workspace_id = ?", id, workspaceID).
		Updates(map[string]any{"outcome": string(outcome), "answered_by": answeredBy, "finished_at": at}).Error
}

var _ callrouting.TransferHistory = (*TransferLog)(nil)

func (l *TransferLog) ForCalls(ctx context.Context, workspaceID string, callIDs []string) ([]callrouting.TransferRecord, error) {
	if len(callIDs) == 0 {
		return nil, nil
	}
	var rows []schema.CallTransfer
	err := l.db.WithContext(ctx).
		Where("workspace_id = ? AND call_id IN ?", workspaceID, callIDs).
		Order("created_at ASC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	records := make([]callrouting.TransferRecord, 0, len(rows))
	for _, row := range rows {
		record := callrouting.TransferRecord{
			ID: row.ID, WorkspaceID: row.WorkspaceID, CallID: row.CallID, FromUserID: row.FromUserID,
			Target:  callrouting.TransferTarget{Kind: callrouting.TargetKind(row.TargetKind), QueueID: row.TargetQueueID, UserID: row.TargetUserID},
			Notes:   row.Notes,
			Outcome: callrouting.TransferOutcome(row.Outcome), AnsweredBy: row.AnsweredBy, CreatedAt: row.CreatedAt,
		}
		if row.FinishedAt != nil {
			record.FinishedAt = *row.FinishedAt
		}
		records = append(records, record)
	}
	return records, nil
}

var _ callrouting.QueueHistory = (*TransferLog)(nil)

func (l *TransferLog) QueueOutcomes(ctx context.Context, workspaceID string, from, to time.Time) ([]callrouting.QueueOutcome, error) {
	var rows []schema.CallTransfer
	err := l.db.WithContext(ctx).
		Where("workspace_id = ? AND target_kind = ? AND created_at >= ? AND created_at < ?", workspaceID, string(callrouting.TargetQueue), from, to).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	outcomes := make([]callrouting.QueueOutcome, 0, len(rows))
	for _, row := range rows {
		var wait time.Duration
		if row.FinishedAt != nil {
			wait = row.FinishedAt.Sub(row.CreatedAt)
		}
		outcomes = append(outcomes, callrouting.QueueOutcome{QueueID: row.TargetQueueID, Outcome: callrouting.TransferOutcome(row.Outcome), Wait: wait})
	}
	return outcomes, nil
}

type SettingsRepository struct{ db *gorm.DB }

var _ callrouting.SettingsRepository = (*SettingsRepository)(nil)

func NewSettingsRepository(db *gorm.DB) *SettingsRepository { return &SettingsRepository{db: db} }

func (r *SettingsRepository) Get(ctx context.Context, workspaceID string) (callrouting.Settings, error) {
	var record schema.CallRoutingSettings
	err := r.db.WithContext(ctx).Where("workspace_id = ?", workspaceID).First(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return callrouting.DefaultSettings(workspaceID), nil
	}
	if err != nil {
		return callrouting.Settings{}, err
	}
	return callrouting.Settings{
		WorkspaceID: workspaceID,
		HoldMusic:   callrouting.HoldMusicRef{PresetID: record.HoldPresetID, MediaID: record.HoldMediaID},
	}, nil
}

func (r *SettingsRepository) Save(ctx context.Context, settings callrouting.Settings) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "workspace_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"hold_preset_id", "hold_media_id", "updated_at"}),
	}).Create(&schema.CallRoutingSettings{
		WorkspaceID:  settings.WorkspaceID,
		HoldPresetID: settings.HoldMusic.PresetID,
		HoldMediaID:  settings.HoldMusic.MediaID,
	}).Error
}
