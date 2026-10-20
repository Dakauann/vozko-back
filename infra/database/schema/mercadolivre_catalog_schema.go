package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type MercadoLivreListing struct {
	ID          string `gorm:"primaryKey;type:uuid;index:idx_ml_listing_account_created,priority:3,sort:desc;index:idx_ml_listing_account_status,priority:4,sort:desc"`
	WorkspaceID string `gorm:"type:uuid;not null;index"`
	AccountID   string `gorm:"type:uuid;not null;uniqueIndex:ux_ml_listing_account_item,priority:1;index:idx_ml_listing_account_created,priority:1;index:idx_ml_listing_account_status,priority:1"`

	ItemID          string `gorm:"size:24;not null;uniqueIndex:ux_ml_listing_account_item,priority:2"`
	Title           string `gorm:"type:text;not null;default:''"`
	Permalink       string `gorm:"type:text;not null;default:''"`
	ThumbnailKey    string `gorm:"type:text;not null;default:''"`
	ThumbnailSource string `gorm:"type:text;not null;default:''"`
	Status          string `gorm:"size:32;not null;default:'';index:idx_ml_listing_account_status,priority:2"`

	SyncedAt *time.Time `gorm:"type:timestamptz"`

	CreatedAt time.Time `gorm:"autoCreateTime;index:idx_ml_listing_account_created,priority:2,sort:desc;index:idx_ml_listing_account_status,priority:3,sort:desc"`
	UpdatedAt time.Time `gorm:"autoUpdateTime"`
}

func (MercadoLivreListing) TableName() string { return "mercadolivre_listings" }

func (l *MercadoLivreListing) BeforeCreate(tx *gorm.DB) error {
	if l.ID == "" {
		l.ID = uuid.New().String()
	}
	return nil
}

type MercadoLivreListingSync struct {
	AccountID string    `gorm:"primaryKey;type:uuid"`
	StartedAt time.Time `gorm:"type:timestamptz;not null"`
}

func (MercadoLivreListingSync) TableName() string { return "mercadolivre_listing_syncs" }

type MercadoLivreQuestion struct {
	ID          string `gorm:"primaryKey;type:uuid;index:idx_ml_q_account_open,priority:4,sort:desc;index:idx_ml_q_account_status,priority:4,sort:desc;index:idx_ml_q_account_item,priority:5,sort:desc"`
	WorkspaceID string `gorm:"type:uuid;not null;index"`
	AccountID   string `gorm:"type:uuid;not null;uniqueIndex:ux_ml_question_account_question,priority:1;index:idx_ml_q_account_open,priority:1;index:idx_ml_q_account_status,priority:1;index:idx_ml_q_account_item,priority:1"`

	QuestionID string `gorm:"size:20;not null;uniqueIndex:ux_ml_question_account_question,priority:2"`
	ItemID     string `gorm:"size:24;not null;index:idx_ml_q_account_item,priority:2"`
	ContactID  string `gorm:"type:uuid;not null;index"`

	Text       string `gorm:"type:text;not null;default:''"`
	Status     string `gorm:"size:24;not null;index:idx_ml_q_account_status,priority:2"`
	Unanswered bool   `gorm:"not null;default:false;index:idx_ml_q_account_open,priority:2,sort:desc;index:idx_ml_q_account_item,priority:3,sort:desc"`

	AnswerText       string     `gorm:"type:text;not null;default:''"`
	AnswerStatus     string     `gorm:"size:16;not null;default:''"`
	AnsweredBy       string     `gorm:"size:24;not null;default:''"`
	AnsweredByUserID *string    `gorm:"type:uuid"`
	RuleID           *string    `gorm:"type:uuid"`
	AnsweredAt       *time.Time `gorm:"type:timestamptz"`

	AskedAt       time.Time `gorm:"type:timestamptz;not null;index:idx_ml_q_account_open,priority:3,sort:desc;index:idx_ml_q_account_status,priority:3,sort:desc;index:idx_ml_q_account_item,priority:4,sort:desc"`
	SuspectedSpam bool      `gorm:"not null;default:false"`
	LastFetchedAt time.Time `gorm:"type:timestamptz;not null"`

	CreatedAt time.Time `gorm:"autoCreateTime"`
	UpdatedAt time.Time `gorm:"autoUpdateTime"`
}

func (MercadoLivreQuestion) TableName() string { return "mercadolivre_questions" }

func (q *MercadoLivreQuestion) BeforeCreate(tx *gorm.DB) error {
	if q.ID == "" {
		q.ID = uuid.New().String()
	}
	return nil
}
