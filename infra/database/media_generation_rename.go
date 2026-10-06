package database

import (
	"fmt"

	"gorm.io/gorm"
)

var mediaGenerationIndexRenames = [][2]string{
	{"idx_image_generation_jobs_active", "idx_media_generation_jobs_active"},
	{"idx_image_generation_jobs_stale", "idx_media_generation_jobs_stale"},
}

func renameImageGenerationToMedia(tx *gorm.DB) error {
	if err := renameTableIfNeeded(tx, "image_generation_jobs", "media_generation_jobs"); err != nil {
		return fmt.Errorf("renaming image_generation_jobs: %w", err)
	}
	for _, r := range mediaGenerationIndexRenames {
		if err := renameIndexIfNeeded(tx, r[0], r[1]); err != nil {
			return fmt.Errorf("renaming index %s: %w", r[0], err)
		}
	}
	return nil
}

func renameIndexIfNeeded(tx *gorm.DB, from, to string) error {
	var pending bool
	if err := tx.Raw(`SELECT to_regclass(?) IS NOT NULL AND to_regclass(?) IS NULL`, "public."+from, "public."+to).Scan(&pending).Error; err != nil {
		return err
	}
	if !pending {
		return nil
	}
	return tx.Exec(fmt.Sprintf("ALTER INDEX %s RENAME TO %s", from, to)).Error
}
