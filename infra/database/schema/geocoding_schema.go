package schema

import "time"

type GeoCEPPoint struct {
	ZipCode      string    `gorm:"primaryKey;type:varchar(8)"`
	Latitude     float64   `gorm:"type:double precision;not null"`
	Longitude    float64   `gorm:"type:double precision;not null"`
	SpreadM      float64   `gorm:"type:double precision;not null"`
	AddressCount int64     `gorm:"not null"`
	SampleCount  int64     `gorm:"not null;default:0"`
	CityCode     string    `gorm:"type:varchar(7);not null"`
	BuiltAt      time.Time `gorm:"type:timestamptz;not null"`
}

func (GeoCEPPoint) TableName() string { return "geo_cep_points" }

type GeoCity struct {
	CityCode  string    `gorm:"primaryKey;type:varchar(7)"`
	Name      string    `gorm:"type:varchar(100);not null"`
	NameKey   string    `gorm:"type:text;not null;index:idx_geo_cities_state_name_key,priority:2"`
	State     string    `gorm:"type:varchar(2);not null;index:idx_geo_cities_state_name_key,priority:1"`
	Latitude  float64   `gorm:"type:double precision;not null"`
	Longitude float64   `gorm:"type:double precision;not null"`
	BuiltAt   time.Time `gorm:"type:timestamptz;not null"`
	GeoBounds
	AddressCount int64 `gorm:"not null;default:0"`
}

func (GeoCity) TableName() string { return "geo_cities" }

type GeoBounds struct {
	BoundsSouth *float64 `gorm:"column:bounds_south;type:double precision"`
	BoundsWest  *float64 `gorm:"column:bounds_west;type:double precision"`
	BoundsNorth *float64 `gorm:"column:bounds_north;type:double precision"`
	BoundsEast  *float64 `gorm:"column:bounds_east;type:double precision"`
}

type GeoCEPStreet struct {
	ZipCode      string    `gorm:"primaryKey;type:varchar(8)"`
	CityCode     string    `gorm:"primaryKey;type:varchar(7)"`
	StreetKey    string    `gorm:"primaryKey;type:text"`
	DistrictKey  string    `gorm:"primaryKey;type:text"`
	State        string    `gorm:"type:varchar(2);not null"`
	Street       string    `gorm:"type:varchar(200);not null"`
	District     string    `gorm:"type:varchar(200);not null"`
	AddressCount int64     `gorm:"not null"`
	BuiltAt      time.Time `gorm:"type:timestamptz;not null"`
}

func (GeoCEPStreet) TableName() string { return "geo_cep_streets" }

type GeoDistrictPoint struct {
	CityCode    string    `gorm:"primaryKey;type:varchar(7)"`
	DistrictKey string    `gorm:"primaryKey;type:text"`
	Name        string    `gorm:"type:varchar(200);not null"`
	Latitude    float64   `gorm:"type:double precision;not null"`
	Longitude   float64   `gorm:"type:double precision;not null"`
	SpreadM     float64   `gorm:"type:double precision;not null"`
	SampleCount int64     `gorm:"not null"`
	Source      string    `gorm:"type:varchar(16);not null"`
	BuiltAt     time.Time `gorm:"type:timestamptz;not null"`
	GeoBounds
}

func (GeoDistrictPoint) TableName() string { return "geo_district_points" }

type GeoReferenceLoad struct {
	State      string    `gorm:"primaryKey;type:varchar(2)"`
	Rows       int64     `gorm:"not null"`
	CEPs       int64     `gorm:"column:ceps;not null"`
	StreetCEPs int64     `gorm:"column:street_ceps;not null"`
	Districts  int64     `gorm:"not null"`
	Cities     int64     `gorm:"not null"`
	Source     string    `gorm:"type:varchar(64);not null"`
	BuiltAt    time.Time `gorm:"type:timestamptz;not null"`
}

func (GeoReferenceLoad) TableName() string { return "geo_reference_loads" }
