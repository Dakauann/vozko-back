package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Product struct {
	ID          string         `gorm:"primaryKey;type:uuid"`
	ShopID      int64          `gorm:"not null;index"`
	Shop        *Shop          `gorm:"foreignKey:ShopID;constraint:OnDelete:RESTRICT"`
	Name        string         `gorm:"not null;size:255"`
	Description string         `gorm:"type:text"`
	Tags        string         `gorm:"type:text"`
	CreatedAt   time.Time      `gorm:"autoCreateTime"`
	UpdatedAt   time.Time      `gorm:"autoUpdateTime"`
	Variants    []Variant      `gorm:"foreignKey:ProductID;constraint:OnDelete:CASCADE"`
	DeletedAt   gorm.DeletedAt `gorm:"index"`
}

func (Product) TableName() string {
	return "products"
}

func (p *Product) BeforeCreate(tx *gorm.DB) error {
	if p.ID == "" {
		p.ID = uuid.New().String()
	}
	return nil
}

type OptionType struct {
	ID     string        `gorm:"primaryKey;type:uuid"`
	Name   string        `gorm:"not null;unique;size:100"`
	Values []OptionValue `gorm:"foreignKey:OptionTypeID;constraint:OnDelete:CASCADE"`
}

func (OptionType) TableName() string {
	return "option_types"
}

func (o *OptionType) BeforeCreate(tx *gorm.DB) error {
	if o.ID == "" {
		o.ID = uuid.New().String()
	}
	return nil
}

type OptionValue struct {
	ID           string     `gorm:"primaryKey;type:uuid"`
	OptionTypeID string     `gorm:"not null;type:uuid"`
	Value        string     `gorm:"not null;size:100"`
	OptionType   OptionType `gorm:"foreignKey:OptionTypeID"`
}

func (OptionValue) TableName() string {
	return "option_values"
}

func (o *OptionValue) BeforeCreate(tx *gorm.DB) error {
	if o.ID == "" {
		o.ID = uuid.New().String()
	}
	return nil
}

type Variant struct {
	ID                      string          `gorm:"primaryKey;type:uuid"`
	Name                    string          `gorm:"not null;size:255"`
	ProductID               string          `gorm:"not null;type:uuid;index"`
	SKU                     string          `gorm:"unique;size:50"`
	RetailPrice             float64         `gorm:"not null"`
	WholesalePrice          float64         `gorm:"not null"`
	Cost                    float64         `gorm:"default:0.00"`
	Inventory               int             `gorm:"default:0"`
	CategoryID              *string         `gorm:"type:uuid;index"`
	Announced               bool            `gorm:"default:true"`
	MinQuantityForWholesale int             `gorm:"default:0"`
	Weight                  float64         `gorm:"default:0.00"`
	Height                  float64         `gorm:"default:0.00"`
	Width                   float64         `gorm:"default:0.00"`
	Depth                   float64         `gorm:"default:0.00"`
	IsDefault               bool            `gorm:"default:false"`
	CreatedAt               time.Time       `gorm:"autoCreateTime"`
	UpdatedAt               time.Time       `gorm:"autoUpdateTime"`
	Product                 Product         `gorm:"foreignKey:ProductID"`
	Options                 []VariantOption `gorm:"foreignKey:VariantID;constraint:OnDelete:CASCADE"`
	Medias                  []VariantMedia  `gorm:"foreignKey:VariantID;constraint:OnDelete:CASCADE"`
	Category                *Category       `gorm:"foreignKey:CategoryID;references:ID;constraint:OnDelete:RESTRICT"`
}

func (Variant) TableName() string {
	return "variants"
}

func (v *Variant) BeforeCreate(tx *gorm.DB) error {
	if v.ID == "" {
		v.ID = uuid.New().String()
	}
	return nil
}

type VariantOption struct {
	ID            string      `gorm:"primaryKey;type:uuid"`
	VariantID     string      `gorm:"not null;type:uuid;index"`
	OptionValueID string      `gorm:"not null;type:uuid;index"`
	OptionType    string      `gorm:"type:varchar(255)"`
	OptionValue   string      `gorm:"type:varchar(255)"`
	Variant       Variant     `gorm:"foreignKey:VariantID"`
	OptionValueDB OptionValue `gorm:"foreignKey:OptionValueID"`
}

func (VariantOption) TableName() string {
	return "variant_options"
}

func (vo *VariantOption) BeforeCreate(tx *gorm.DB) error {
	if vo.ID == "" {
		vo.ID = uuid.New().String()
	}
	return nil
}

type VariantMedia struct {
	VariantID string `gorm:"primaryKey;type:uuid"`
	MediaID   string `gorm:"primaryKey;type:uuid"`
	Variant   Variant
	Media     Media `gorm:"foreignKey:MediaID"`
}

func (VariantMedia) TableName() string {
	return "variant_medias"
}
