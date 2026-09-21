package pipeline_repository

import (
	"fmt"
	"strings"

	"gorm.io/gorm"

	"vozko/domain/pipeline"
	"vozko/infra/database/schema"
)

type occupancy struct {
	db *gorm.DB
}

func NewOccupancy(db *gorm.DB) pipeline.Occupancy {
	return &occupancy{db: db}
}

var pipelineBindings = []struct {
	table string
	kind  string
}{
	{"whatsapp_campaigns", "campaign"},
	{"unofficial_whatsapp_campaigns", "campaign"},
	{"instagram_accounts", "channel"},
	{"telegram_accounts", "channel"},
	{"unofficial_whatsapp_instances", "channel"},
}

func (o *occupancy) Usage(workspaceID, pipelineID string) (pipeline.Usage, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	pipelineID = strings.TrimSpace(pipelineID)
	if workspaceID == "" || pipelineID == "" {
		return pipeline.Usage{}, fmt.Errorf("pipeline usage: workspace and funnel are required")
	}

	var usage pipeline.Usage

	if err := o.db.Model(&schema.EntryStage{}).
		Where("workspace_id = ? AND deleted_at IS NULL", workspaceID).
		Where("stage_id IN (?)", o.db.Model(&schema.Stage{}).
			Select("id").
			Where("workspace_id = ? AND pipeline_id = ? AND deleted_at IS NULL", workspaceID, pipelineID)).
		Count(&usage.Entries).Error; err != nil {
		return pipeline.Usage{}, fmt.Errorf("count conversations on funnel %s: %w", pipelineID, err)
	}

	for _, b := range pipelineBindings {
		var n int64
		if err := o.db.Table(b.table).
			Where("workspace_id = ? AND pipeline_id = ? AND deleted_at IS NULL", workspaceID, pipelineID).
			Count(&n).Error; err != nil {
			return pipeline.Usage{}, fmt.Errorf("count %s on funnel %s: %w", b.table, pipelineID, err)
		}
		switch b.kind {
		case "campaign":
			usage.Campaigns += n
		case "channel":
			usage.Channels += n
		}
	}

	if err := o.db.Model(&schema.Opportunity{}).
		Where("workspace_id = ? AND pipeline_id = ? AND deleted_at IS NULL", workspaceID, pipelineID).
		Count(&usage.Opportunities).Error; err != nil {
		return pipeline.Usage{}, fmt.Errorf("count opportunities on funnel %s: %w", pipelineID, err)
	}

	return usage, nil
}

func (o *occupancy) Vacate(workspaceID, pipelineID, intoPipelineID string) (int64, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	pipelineID = strings.TrimSpace(pipelineID)
	intoPipelineID = strings.TrimSpace(intoPipelineID)
	if workspaceID == "" || pipelineID == "" {
		return 0, fmt.Errorf("vacate funnel: workspace and funnel are required")
	}

	var moved int64
	err := o.db.Transaction(func(tx *gorm.DB) error {
		if intoPipelineID != "" {
			landing, err := entryStageOf(tx, workspaceID, intoPipelineID)
			if err != nil {
				return err
			}
			res := tx.Model(&schema.EntryStage{}).
				Where("workspace_id = ? AND deleted_at IS NULL", workspaceID).
				Where("stage_id IN (?)", tx.Model(&schema.Stage{}).
					Select("id").
					Where("workspace_id = ? AND pipeline_id = ? AND deleted_at IS NULL", workspaceID, pipelineID)).
				Update("stage_id", landing)
			if res.Error != nil {
				return fmt.Errorf("move conversations to funnel %s: %w", intoPipelineID, res.Error)
			}
			moved = res.RowsAffected
		}

		if err := tx.Where("workspace_id = ? AND pipeline_id = ?", workspaceID, pipelineID).
			Delete(&schema.Stage{}).Error; err != nil {
			return fmt.Errorf("remove columns of funnel %s: %w", pipelineID, err)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return moved, nil
}

func entryStageOf(tx *gorm.DB, workspaceID, pipelineID string) (string, error) {
	var stage schema.Stage
	err := tx.Select("id").
		Where("workspace_id = ? AND pipeline_id = ? AND deleted_at IS NULL", workspaceID, pipelineID).
		Order("is_initial DESC, position ASC, created_at ASC").
		First(&stage).Error
	if err != nil {
		return "", fmt.Errorf("funnel %s has no column to receive conversations: %w", pipelineID, err)
	}
	return stage.ID, nil
}
