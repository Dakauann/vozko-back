package dealautomation_repository

import (
	"errors"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vozko/domain/conversation"
	"vozko/domain/dealautomation"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
)

type repository struct {
	db *gorm.DB
}

func New(db *gorm.DB) dealautomation.Repository {
	return &repository{db: db}
}

func (r *repository) scope(workspaceID string, channel dealautomation.Channel, containerID string) *gorm.DB {
	return r.db.Where("workspace_id = ? AND entry_type = ? AND container_kind = ? AND container_id = ?",
		workspaceID, string(channel.EntryType), string(channel.Kind), containerID)
}

func (r *repository) Find(workspaceID string, channel dealautomation.Channel, containerID string) (*dealautomation.Setting, error) {
	var row schema.DealAutomation
	err := r.scope(workspaceID, channel, containerID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &dealautomation.Setting{
		WorkspaceID: row.WorkspaceID,
		Channel:     dealautomation.Channel{EntryType: shared.EntryType(row.EntryType), Kind: conversation.ContainerKind(row.ContainerKind)},
		ContainerID: row.ContainerID,
		PipelineID:  row.PipelineID,
		UpdatedBy:   row.UpdatedBy,
		UpdatedAt:   row.UpdatedAt,
	}, nil
}

func (r *repository) Save(setting dealautomation.Setting) error {
	row := schema.DealAutomation{
		ID:            uuid.New().String(),
		WorkspaceID:   setting.WorkspaceID,
		EntryType:     string(setting.Channel.EntryType),
		ContainerKind: string(setting.Channel.Kind),
		ContainerID:   setting.ContainerID,
		PipelineID:    setting.PipelineID,
		UpdatedBy:     setting.UpdatedBy,
		UpdatedAt:     setting.UpdatedAt,
	}
	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "workspace_id"}, {Name: "entry_type"}, {Name: "container_kind"}, {Name: "container_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"pipeline_id", "updated_by", "updated_at"}),
	}).Create(&row).Error
}

func (r *repository) Delete(workspaceID string, channel dealautomation.Channel, containerID string) error {
	return r.scope(workspaceID, channel, containerID).Delete(&schema.DealAutomation{}).Error
}
