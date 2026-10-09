package opportunity_repository

import (
	"gorm.io/gorm"

	"vozko/domain/opportunity"
	actor_repository "vozko/infra/repositories/actor"
)

func NewOwnerDirectory(db *gorm.DB) opportunity.OwnerDirectory {
	return actor_repository.NewOwnerDirectory(db)
}
