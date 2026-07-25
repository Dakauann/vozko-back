package property

import (
	"errors"
	"time"
)

var (
	ErrPropertyShopNotFound     = errors.New("property shop not found")
	ErrPropertyShopUnauthorized = errors.New("unauthorized to manage properties for this shop")
)

type PropertyType string

const (
	PropertyTypeHouse      PropertyType = "HOUSE"
	PropertyTypeApartment  PropertyType = "APARTMENT"
	PropertyTypeCommercial PropertyType = "COMMERCIAL"
	PropertyTypeLand       PropertyType = "LAND"
	PropertyTypeCondo      PropertyType = "CONDO"
	PropertyTypePentHouse  PropertyType = "PENTHOUSE"
)

type PropertyCondition string

const (
	ConditionNew         PropertyCondition = "NEW"
	ConditionExcellent   PropertyCondition = "EXCELLENT"
	ConditionGood        PropertyCondition = "GOOD"
	ConditionFair        PropertyCondition = "FAIR"
	ConditionNeedsRepair PropertyCondition = "NEEDS_REPAIR"
)

type PropertyStatus string

const (
	StatusAvailable PropertyStatus = "AVAILABLE"
	StatusSold      PropertyStatus = "SOLD"
	StatusRented    PropertyStatus = "RENTED"
	StatusOffer     PropertyStatus = "OFFER"
)

type GeoLocation struct {
	Latitude     float64 `json:"latitude"`
	Longitude    float64 `json:"longitude"`
	Address      string  `json:"address"`
	City         string  `json:"city"`
	State        string  `json:"state"`
	ZipCode      string  `json:"zip_code"`
	Neighborhood string  `json:"neighborhood"`
}

type PropertyAmenities struct {
	HasPool         bool     `json:"has_pool"`
	HasGarden       bool     `json:"has_garden"`
	HasGarage       bool     `json:"has_garage"`
	HasAC           bool     `json:"has_ac"`
	HasFireplace    bool     `json:"has_fireplace"`
	HasSecuritySys  bool     `json:"has_security_system"`
	HasGym          bool     `json:"has_gym"`
	HasConcierge    bool     `json:"has_concierge"`
	HasBalcony      bool     `json:"has_balcony"`
	HasLaundry      bool     `json:"has_laundry"`
	HasPatio        bool     `json:"has_patio"`
	HasElevator     bool     `json:"has_elevator"`
	ParkingSpaces   int      `json:"parking_spaces"`
	CustomAmenities []string `json:"custom_amenities"`
}

type Property struct {
	ID          string `json:"id"`
	ShopID      int64  `json:"shopId"`
	Name        string `json:"name"`
	Description string `json:"description"`

	Type      PropertyType      `json:"type"`
	Status    PropertyStatus    `json:"status"`
	Condition PropertyCondition `json:"condition"`

	Location GeoLocation `json:"location"`

	TotalArea float64 `json:"total_area"`
	BuiltArea float64 `json:"built_area"`
	LotSize   float64 `json:"lot_size"`

	Suites      int `json:"suites"`
	Bedrooms    int `json:"bedrooms"`
	Bathrooms   int `json:"bathrooms"`
	Floors      int `json:"floors"`
	LivingRooms int `json:"living_rooms"`

	Price              float64 `json:"price"`
	Currency           string  `json:"currency"`
	PricePerSqMeter    float64 `json:"price_per_sq_meter"`
	HOAFees            float64 `json:"hoa_fees"`
	PropertyTax        float64 `json:"property_tax"`
	FinancingAvailable bool    `json:"financing_available"`

	Amenities PropertyAmenities `json:"amenities"`

	YearBuilt          int    `json:"year_built"`
	RenovatedYear      *int   `json:"renovated_year,omitempty"`
	ConstructionStatus string `json:"construction_status"`

	RegistrationNumber string `json:"registration_number"`
	OwnershipType      string `json:"ownership_type"`

	Images         []string `json:"images"`
	VirtualTourURL *string  `json:"virtual_tour_url,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	CreatedBy string    `json:"created_by"`

	EnergyRating *string `json:"energy_rating,omitempty"`
	IsSolar      bool    `json:"is_solar"`
}

func (p *Property) Validate() error {
	if p.Name == "" {
		return errors.New("property name is required")
	}
	if p.Type == "" {
		return errors.New("property type is required")
	}
	if p.Location.Address == "" || p.Location.City == "" {
		return errors.New("location is required")
	}
	if p.Price <= 0 {
		return errors.New("price must be greater than 0")
	}
	if p.TotalArea <= 0 {
		return errors.New("total area is required")
	}
	return nil
}

func (p *Property) CalculatePricePerSqMeter() {
	if p.TotalArea > 0 {
		p.PricePerSqMeter = p.Price / p.TotalArea
	}
}
