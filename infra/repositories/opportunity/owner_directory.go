package opportunity_repository

import (
	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/actor"
	"vozko/domain/opportunity"
)

type ownerDirectory struct {
	db *gorm.DB
}

func NewOwnerDirectory(db *gorm.DB) opportunity.OwnerDirectory {
	return &ownerDirectory{db: db}
}

func (d *ownerDirectory) Belongs(workspaceID, actorID string) (bool, error) {
	id, kind := actor.Split(actorID)
	if _, err := uuid.Parse(id); err != nil {
		return false, nil
	}
	var query string
	switch kind {
	case actor.KindHuman:
		query = `SELECT EXISTS (SELECT 1 FROM workspace_members WHERE workspace_id = ? AND user_id = ?)`
	case actor.KindAI:
		query = `SELECT EXISTS (SELECT 1 FROM agents WHERE workspace_id = ? AND id = ? AND deleted_at IS NULL)`
	case actor.KindWorkflow:
		query = `SELECT EXISTS (SELECT 1 FROM workflows WHERE workspace_id = ? AND id = ? AND deleted_at IS NULL)`
	default:
		return false, nil
	}
	var belongs bool
	if err := d.db.Raw(query, workspaceID, id).Scan(&belongs).Error; err != nil {
		return false, err
	}
	return belongs, nil
}
