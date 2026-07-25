package schema

import (
	"time"

	"gorm.io/gorm"
)

type Shop struct {
	ID            int64          `gorm:"primaryKey;autoIncrement"`
	UserID        string         `gorm:"not null;type:uuid;index"`
	Name          string         `gorm:"not null;size:255"`
	Brand         string         `gorm:"size:255"`
	LogoMediaID   *string        `gorm:"type:uuid"`
	BannerMediaID *string        `gorm:"type:uuid"`
	CreatedAt     time.Time      `gorm:"autoCreateTime"`
	UpdatedAt     time.Time      `gorm:"autoUpdateTime"`
	DeletedAt     gorm.DeletedAt `gorm:"index"`
	IsOfficial    bool           `gorm:"default:false"`
	// The former brand-named official-store flag was removed from code;
	// its DB column (never written by any code path) is left orphaned on
	// purpose so no data is dropped.
}

func (Shop) TableName() string {
	return "shops"
}
