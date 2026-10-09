package lead_memory_repository

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"

	"vozko/infra/database/schema"
)

var ErrMemoryCountInvalid = errors.New("lead memories: a count needs a workspace and a lead")

type LeadCounter struct {
	db *gorm.DB
}

func NewLeadCounter(db *gorm.DB) *LeadCounter {
	return &LeadCounter{db: db}
}

func (c *LeadCounter) CountMemoriesOfLead(ctx context.Context, workspaceID, leadID string) (int, error) {
	workspaceID, leadID = strings.TrimSpace(workspaceID), strings.TrimSpace(leadID)
	if workspaceID == "" || leadID == "" {
		return 0, ErrMemoryCountInvalid
	}
	total, err := countByLead(c.db.WithContext(ctx), workspaceID, leadID)
	return int(total), err
}

func countByLead(db *gorm.DB, workspaceID, leadID string) (int64, error) {
	var total int64
	err := db.Model(&schema.LeadMemory{}).
		Where("workspace_id = ? AND lead_id = ?", workspaceID, leadID).
		Count(&total).Error
	return total, err
}
