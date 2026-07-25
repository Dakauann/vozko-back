package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type MediaType string

const (
	MediaTypeVslVideo     MediaType = "vsl_video"
	MediaTypeProductImage MediaType = "image"
	MediaTypeProductVideo MediaType = "video"
	MediaTypeDocumentPdf  MediaType = "document_pdf"
	MediaTypeDocumentDoc  MediaType = "document_doc"
	MediaTypeHtml5        MediaType = "html5"
)

type Media struct {
	ID          string         `gorm:"primaryKey;type:uuid"`
	WorkspaceID string         `gorm:"not null;type:uuid;index"`
	Description string         `gorm:"size:1000"`
	URL         string         `gorm:"not null;size:500"`
	PreviewURL  string         `gorm:"size:500"`
	CreatedAt   time.Time      `gorm:"autoCreateTime"`
	Type        MediaType      `gorm:"not null;size:50"`
	DeletedAt   gorm.DeletedAt `gorm:"index"`
}

func (Media) TableName() string {
	return "medias"
}

func (i *Media) BeforeCreate(tx *gorm.DB) error {
	if i.ID == "" {
		i.ID = uuid.New().String()
	}
	return nil
}
