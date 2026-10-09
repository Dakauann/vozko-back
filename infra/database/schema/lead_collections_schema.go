package schema

import "time"

type LeadPhone struct {
	ID          string    `gorm:"primaryKey;type:uuid"`
	WorkspaceID string    `gorm:"type:uuid;not null"`
	LeadID      string    `gorm:"type:uuid;not null;uniqueIndex:ux_lead_phones_lead_number,priority:1"`
	Lead        *Lead     `gorm:"foreignKey:LeadID;references:ID;constraint:OnDelete:CASCADE"`
	Number      string    `gorm:"type:varchar(20);not null;uniqueIndex:ux_lead_phones_lead_number,priority:2"`
	Label       string    `gorm:"type:varchar(16);not null"`
	Position    int       `gorm:"not null;default:0"`
	CreatedAt   time.Time `gorm:"type:timestamptz;not null"`
}

func (LeadPhone) TableName() string {
	return "lead_phones"
}

type LeadAddress struct {
	ID           string       `gorm:"primaryKey;type:uuid"`
	WorkspaceID  string       `gorm:"type:uuid;not null;index:idx_lead_addresses_workspace_fingerprint,priority:1"`
	LeadID       string       `gorm:"type:uuid;not null;index:idx_lead_addresses_lead"`
	Lead         *Lead        `gorm:"foreignKey:LeadID;references:ID;constraint:OnDelete:CASCADE"`
	Label        string       `gorm:"type:varchar(16);not null"`
	IsPrimary    bool         `gorm:"not null;default:false"`
	Position     int          `gorm:"not null;default:0"`
	ZipCode      OptionalText `gorm:"type:varchar(8)"`
	Street       OptionalText `gorm:"type:varchar(200)"`
	Number       OptionalText `gorm:"type:varchar(20)"`
	Complement   OptionalText `gorm:"type:varchar(100)"`
	District     OptionalText `gorm:"type:varchar(100)"`
	DistrictKey  OptionalText `gorm:"type:text"`
	City         OptionalText `gorm:"type:varchar(100)"`
	CityKey      OptionalText `gorm:"type:text"`
	CityCode     OptionalText `gorm:"type:varchar(7)"`
	State        OptionalText `gorm:"type:varchar(2)"`
	Latitude     *float64     `gorm:"type:double precision"`
	Longitude    *float64     `gorm:"type:double precision"`
	GeoPrecision OptionalText `gorm:"type:varchar(16)"`
	GeoSource    OptionalText `gorm:"type:varchar(16)"`
	GeoProvider  OptionalText `gorm:"type:varchar(32)"`
	GeoStatus    string       `gorm:"type:varchar(24);not null"`
	GeoAttempts  int          `gorm:"not null;default:0"`
	GeoNextAt    *time.Time   `gorm:"type:timestamptz"`
	GeoClaim     OptionalText `gorm:"type:uuid"`
	GeocodedAt   *time.Time   `gorm:"type:timestamptz"`
	ImportID     OptionalText `gorm:"type:uuid"`
	Fingerprint  string       `gorm:"type:varchar(32);not null;index:idx_lead_addresses_workspace_fingerprint,priority:2"`
	CreatedAt    time.Time    `gorm:"type:timestamptz;not null"`
	UpdatedAt    time.Time    `gorm:"type:timestamptz;not null"`
}

func (LeadAddress) TableName() string {
	return "lead_addresses"
}

type LeadRelation struct {
	ID          string       `gorm:"primaryKey;type:uuid;index:idx_lead_relations_lead_created,priority:3;index:idx_lead_relations_other_lead_created,priority:3"`
	WorkspaceID string       `gorm:"type:uuid;not null"`
	LeadID      string       `gorm:"type:uuid;not null;index:idx_lead_relations_lead_created,priority:1;check:chk_lead_relations_not_self,lead_id <> other_lead_id"`
	Lead        *Lead        `gorm:"foreignKey:LeadID;references:ID;constraint:OnDelete:CASCADE"`
	OtherLeadID string       `gorm:"type:uuid;not null;index:idx_lead_relations_other_lead_created,priority:1"`
	OtherLead   *Lead        `gorm:"foreignKey:OtherLeadID;references:ID;constraint:OnDelete:CASCADE"`
	Dimension   string       `gorm:"type:varchar(16);not null"`
	Kind        string       `gorm:"type:varchar(24);not null"`
	CreatedBy   OptionalText `gorm:"type:uuid"`
	CreatedAt   time.Time    `gorm:"type:timestamptz;not null;index:idx_lead_relations_lead_created,priority:2;index:idx_lead_relations_other_lead_created,priority:2"`
}

func (LeadRelation) TableName() string {
	return "lead_relations"
}
