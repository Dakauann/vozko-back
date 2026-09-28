package schema

import "time"

type MetaDataDeletionRequest struct {
	Code            string     `gorm:"primaryKey;size:64"`
	App             string     `gorm:"size:16;not null;index"`
	AppScopedUserID string     `gorm:"size:64;not null;index"`
	Status          string     `gorm:"size:16;not null"`
	Detail          string     `gorm:"type:text"`
	RequestedAt     time.Time  `gorm:"type:timestamptz;not null"`
	CompletedAt     *time.Time `gorm:"type:timestamptz"`
}

func (MetaDataDeletionRequest) TableName() string { return "meta_data_deletion_requests" }
