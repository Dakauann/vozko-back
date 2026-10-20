package database

import (
	"context"

	"gorm.io/gorm"

	"vozko/domain/shared"
)

const maxIDsPerFind = 1000

func FindByIDs[R any](ctx context.Context, db *gorm.DB, ids []string) ([]R, error) {
	unique := shared.UniqueNonEmpty(ids)
	out := make([]R, 0, len(unique))
	for start := 0; start < len(unique); start += maxIDsPerFind {
		end := min(start+maxIDsPerFind, len(unique))
		var chunk []R
		if err := db.WithContext(ctx).Where("id IN ?", unique[start:end]).Find(&chunk).Error; err != nil {
			return nil, err
		}
		out = append(out, chunk...)
	}
	return out, nil
}
