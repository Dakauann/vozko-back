package lead

import (
	"gorm.io/gorm"

	"vozko/domain/lead"
)

type NumberDirectory struct{ repo *repository }

func NewNumberDirectory(db *gorm.DB) *NumberDirectory {
	return &NumberDirectory{repo: &repository{db: db}}
}

func (d *NumberDirectory) FindByNumbers(workspaceID string, numbers []string) ([]*lead.Lead, error) {
	return d.repo.FindByNumbers(workspaceID, numbers)
}
