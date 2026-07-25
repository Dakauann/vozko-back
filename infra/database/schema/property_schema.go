package schema

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Property struct {
	ID          string `gorm:"primaryKey;type:uuid"`
	ShopID      int64  `gorm:"not null;index"`
	Name        string `gorm:"not null;size:255"`
	Description string `gorm:"type:text"`
	Type        string `gorm:"not null;size:50"`
	Status      string `gorm:"not null;size:50"`
	Condition   string `gorm:"size:50"`

	Latitude     float64 `gorm:"type:decimal(10,8);index"`
	Longitude    float64 `gorm:"type:decimal(11,8);index"`
	Address      string  `gorm:"type:text"`
	City         string  `gorm:"size:100;index"`
	State        string  `gorm:"size:2;index"`
	ZipCode      string  `gorm:"size:10"`
	Neighborhood string  `gorm:"size:100"`

	TotalArea float64 `gorm:"type:decimal(10,2)"`
	BuiltArea float64 `gorm:"type:decimal(10,2)"`
	LotSize   float64 `gorm:"type:decimal(10,2)"`

	Suites      int `gorm:"default:0"`
	Bedrooms    int `gorm:"default:0"`
	Bathrooms   int `gorm:"default:0"`
	Floors      int `gorm:"default:0"`
	LivingRooms int `gorm:"default:0"`

	Price              float64 `gorm:"not null;index"`
	Currency           string  `gorm:"size:3;default:'BRL'"`
	PricePerSqMeter    float64 `gorm:"type:decimal(10,2)"`
	HOAFees            float64 `gorm:"type:decimal(10,2);default:0"`
	PropertyTax        float64 `gorm:"type:decimal(10,2);default:0"`
	FinancingAvailable bool    `gorm:"default:false"`

	Amenities string `gorm:"type:text"`

	YearBuilt          int     `gorm:"default:0"`
	RenovatedYear      *int    `gorm:"default:null"`
	ConstructionStatus string  `gorm:"size:50"`
	RegistrationNumber string  `gorm:"size:100"`
	OwnershipType      string  `gorm:"size:50"`
	Images             string  `gorm:"type:text"`
	VirtualTourURL     *string `gorm:"size:500"`
	EnergyRating       *string `gorm:"size:10"`
	IsSolar            bool    `gorm:"default:false"`

	CreatedAt time.Time      `gorm:"autoCreateTime"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime"`
	CreatedBy string         `gorm:"type:uuid"`
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

func (Property) TableName() string {
	return "properties"
}

func (p *Property) BeforeCreate(tx *gorm.DB) error {
	if p.ID == "" {
		p.ID = uuid.New().String()
	}
	return nil
}
