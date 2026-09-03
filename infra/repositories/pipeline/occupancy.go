package pipeline_repository

import (
	"fmt"
	"strings"

	"gorm.io/gorm"

	"vozko/domain/pipeline"
	"vozko/infra/database/schema"
)

// occupancy answers what a funnel still holds, and empties it.
//
// It reaches across aggregates on purpose and is the ONLY place allowed to: six
// tables name a pipeline_id, and no single domain package may import all of
// them. Keeping the fan-out here means the pipeline use case depends on one
// port with two methods instead of six repositories.
//
// Every count is a COUNT, never a fetch. The caller wants "how many", and a
// funnel being deleted can hold tens of thousands of conversations.
type occupancy struct {
	db *gorm.DB
}

func NewOccupancy(db *gorm.DB) pipeline.Occupancy {
	return &occupancy{db: db}
}

// pipelineBindings are the tables whose rows ROUTE INTO a funnel: a campaign
// that puts its conversations there, a connected account or number that does the
// same, a deal that lives on it.
//
// They are listed as data rather than written out as six queries because the
// question is identical for each and only the table name moves. A new surface
// that starts naming a pipeline is a row here, and the delete guard covers it
// the same day.
var pipelineBindings = []struct {
	table string
	// kind decides which counter the row lands in, which is what the UI needs to
	// tell the operator WHERE to go and unlink.
	kind string
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

	// Conversations sit on the funnel's STAGES, not on the funnel, so this reads
	// through them. A single subquery rather than "list stages, then count per
	// stage": the second shape costs one round trip per column.
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

	// Opportunities carry pipeline_id directly (a deal IS on a funnel), so they
	// are counted apart from the routing bindings above.
	if err := o.db.Model(&schema.Opportunity{}).
		Where("workspace_id = ? AND pipeline_id = ? AND deleted_at IS NULL", workspaceID, pipelineID).
		Count(&usage.Opportunities).Error; err != nil {
		return pipeline.Usage{}, fmt.Errorf("count opportunities on funnel %s: %w", pipelineID, err)
	}

	return usage, nil
}

// Vacate moves the funnel's conversations onto the destination's entry stage and
// then removes the funnel's own columns.
//
// One transaction, because the two halves are not independently correct: columns
// removed without the move strands every conversation on a stage that no longer
// exists, which is the failure this whole guard was written for.
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

// entryStageOf resolves where an arriving conversation lands on a funnel: the
// column flagged initial, and otherwise the first by position.
//
// The fallback is not defensive padding. A funnel seeded before the initial flag
// existed, or one whose initial column was deleted, still has a leftmost column,
// and that is where an operator reading the board expects an arrival. Refusing
// the move because a flag is missing would strand the conversations instead.
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
