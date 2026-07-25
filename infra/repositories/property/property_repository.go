package property_repository

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"vozko/domain/property"
	"vozko/domain/shared"
	"vozko/infra/database/schema"

	"gorm.io/gorm"
)

type repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) property.PropertyRepository {
	return &repository{db: db}
}

func (r *repository) Create(p *property.Property) error {
	amenitiesJSON, _ := json.Marshal(p.Amenities)
	imagesJSON, _ := json.Marshal(p.Images)

	dbProperty := &schema.Property{
		ShopID:      p.ShopID,
		Name:        p.Name,
		Description: p.Description,
		Type:        string(p.Type),
		Status:      string(p.Status),
		Condition:   string(p.Condition),

		Latitude:     p.Location.Latitude,
		Longitude:    p.Location.Longitude,
		Address:      p.Location.Address,
		City:         p.Location.City,
		State:        p.Location.State,
		ZipCode:      p.Location.ZipCode,
		Neighborhood: p.Location.Neighborhood,

		TotalArea: p.TotalArea,
		BuiltArea: p.BuiltArea,
		LotSize:   p.LotSize,

		Suites:      p.Suites,
		Bedrooms:    p.Bedrooms,
		Bathrooms:   p.Bathrooms,
		Floors:      p.Floors,
		LivingRooms: p.LivingRooms,

		Price:              p.Price,
		Currency:           p.Currency,
		PricePerSqMeter:    p.PricePerSqMeter,
		HOAFees:            p.HOAFees,
		PropertyTax:        p.PropertyTax,
		FinancingAvailable: p.FinancingAvailable,

		Amenities: string(amenitiesJSON),

		YearBuilt:          p.YearBuilt,
		RenovatedYear:      p.RenovatedYear,
		ConstructionStatus: p.ConstructionStatus,
		RegistrationNumber: p.RegistrationNumber,
		OwnershipType:      p.OwnershipType,
		Images:             string(imagesJSON),
		VirtualTourURL:     p.VirtualTourURL,
		EnergyRating:       p.EnergyRating,
		IsSolar:            p.IsSolar,

		CreatedBy: p.CreatedBy,
	}

	if err := r.db.Create(dbProperty).Error; err != nil {
		return err
	}

	p.ID = dbProperty.ID
	p.CreatedAt = dbProperty.CreatedAt
	p.UpdatedAt = dbProperty.UpdatedAt

	return nil
}

func (r *repository) Update(propertyId string, p *property.Property) error {
	amenitiesJSON, _ := json.Marshal(p.Amenities)
	imagesJSON, _ := json.Marshal(p.Images)

	updates := map[string]interface{}{
		"name":        p.Name,
		"description": p.Description,
		"type":        string(p.Type),
		"status":      string(p.Status),
		"condition":   string(p.Condition),

		"latitude":     p.Location.Latitude,
		"longitude":    p.Location.Longitude,
		"address":      p.Location.Address,
		"city":         p.Location.City,
		"state":        p.Location.State,
		"zip_code":     p.Location.ZipCode,
		"neighborhood": p.Location.Neighborhood,

		"total_area": p.TotalArea,
		"built_area": p.BuiltArea,
		"lot_size":   p.LotSize,

		"suites":       p.Suites,
		"bedrooms":     p.Bedrooms,
		"bathrooms":    p.Bathrooms,
		"floors":       p.Floors,
		"living_rooms": p.LivingRooms,

		"price":               p.Price,
		"currency":            p.Currency,
		"price_per_sq_meter":  p.PricePerSqMeter,
		"hoa_fees":            p.HOAFees,
		"property_tax":        p.PropertyTax,
		"financing_available": p.FinancingAvailable,

		"amenities": string(amenitiesJSON),

		"year_built":          p.YearBuilt,
		"renovated_year":      p.RenovatedYear,
		"construction_status": p.ConstructionStatus,
		"registration_number": p.RegistrationNumber,
		"ownership_type":      p.OwnershipType,
		"images":              string(imagesJSON),
		"virtual_tour_url":    p.VirtualTourURL,
		"energy_rating":       p.EnergyRating,
		"is_solar":            p.IsSolar,
	}

	if err := r.db.Model(&schema.Property{}).
		Where("id = ?", propertyId).
		Updates(updates).Error; err != nil {
		return err
	}

	updated, err := r.FindByID(propertyId)
	if err != nil {
		return err
	}

	*p = *updated
	return nil
}

func (r *repository) Delete(propertyId string) error {
	return r.db.Delete(&schema.Property{}, "id = ?", propertyId).Error
}

func (r *repository) FindByID(propertyID string) (*property.Property, error) {
	var dbProperty schema.Property
	if err := r.db.Preload("Category").First(&dbProperty, "id = ?", propertyID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("property not found")
		}
		return nil, err
	}

	return r.mapProperty(&dbProperty)
}

func (r *repository) List(input property.ListPropertiesInput) (*shared.PaginatedResult[*property.Property], error) {
	pagination := shared.NormalizePagination(input.Options.Pagination)

	query := r.db.Model(&schema.Property{}).Preload("Category")

	if input.Search != "" {
		query = r.applyPropertySearch(query, input.Search)
	}

	if len(input.Options.Filters) > 0 {
		query = r.applyPropertyFilters(query, input.Options.Filters)
	}

	if input.Latitude != nil && input.Longitude != nil {
		query = r.applyLocationFilter(query, *input.Latitude, *input.Longitude)
	}

	query = r.applyPropertySorts(query, input.Options.Sorts, input.Latitude, input.Longitude)

	var total int64
	countQuery := query.Session(&gorm.Session{})
	if err := countQuery.Count(&total).Error; err != nil {
		return nil, err
	}

	var dbProperties []schema.Property
	if err := query.
		Limit(pagination.PageSize).
		Offset(pagination.Offset()).
		Find(&dbProperties).Error; err != nil {
		return nil, err
	}

	properties, err := r.mapProperties(dbProperties)
	if err != nil {
		return nil, err
	}

	return shared.NewPaginatedResult(properties, pagination, total), nil
}

func (r *repository) FindByIDs(propertyIDs []string) ([]*property.Property, error) {
	if len(propertyIDs) == 0 {
		return []*property.Property{}, nil
	}

	var dbProperties []schema.Property
	if err := r.db.Preload("Category").
		Where("id IN ?", propertyIDs).
		Find(&dbProperties).Error; err != nil {
		return nil, err
	}

	return r.mapProperties(dbProperties)
}

func (r *repository) Search(input property.SearchPropertiesInput) (*shared.PaginatedResult[*property.Property], error) {
	return r.List(property.ListPropertiesInput{
		Search:  input.Query,
		Options: input.Options,
	})
}

func (r *repository) applyPropertySearch(db *gorm.DB, search string) *gorm.DB {
	trimmed := strings.TrimSpace(search)
	if trimmed == "" {
		return db
	}

	words := strings.Fields(strings.ToLower(trimmed))
	if len(words) == 0 {
		return db
	}

	for _, word := range words {
		pattern := "%" + word + "%"
		db = db.Where("(LOWER(name) LIKE ? OR LOWER(description) LIKE ? OR LOWER(address) LIKE ? OR LOWER(city) LIKE ? OR LOWER(neighborhood) LIKE ?)",
			pattern, pattern, pattern, pattern, pattern)
	}

	return db
}

func (r *repository) applyPropertyFilters(db *gorm.DB, filters []shared.Filter) *gorm.DB {
	query := db

	for _, filter := range filters {
		field := strings.ToLower(filter.Field)
		switch field {
		case "type":
			if len(filter.Values) > 0 {
				query = query.Where("type = ?", strings.ToUpper(filter.Values[0]))
			}
		case "status":
			if len(filter.Values) > 0 {
				query = query.Where("status = ?", strings.ToUpper(filter.Values[0]))
			}
		case "condition":
			if len(filter.Values) > 0 {
				query = query.Where("condition = ?", strings.ToUpper(filter.Values[0]))
			}
		case "city":
			if len(filter.Values) > 0 {
				value := strings.TrimSpace(filter.Values[0])
				if filter.Operator == shared.FilterOpLike {
					query = query.Where("LOWER(city) LIKE ?", "%"+strings.ToLower(value)+"%")
				} else {
					query = query.Where("LOWER(city) = ?", strings.ToLower(value))
				}
			}
		case "state":
			if len(filter.Values) > 0 {
				query = query.Where("LOWER(state) = ?", strings.ToLower(filter.Values[0]))
			}
		case "neighborhood":
			if len(filter.Values) > 0 {
				value := strings.TrimSpace(filter.Values[0])
				if filter.Operator == shared.FilterOpLike {
					query = query.Where("LOWER(neighborhood) LIKE ?", "%"+strings.ToLower(value)+"%")
				} else {
					query = query.Where("LOWER(neighborhood) = ?", strings.ToLower(value))
				}
			}
		case "minprice":
			if len(filter.Values) > 0 {
				amount, err := strconv.ParseFloat(filter.Values[0], 64)
				if err == nil {
					query = query.Where("price >= ?", amount)
				}
			}
		case "maxprice":
			if len(filter.Values) > 0 {
				amount, err := strconv.ParseFloat(filter.Values[0], 64)
				if err == nil {
					query = query.Where("price <= ?", amount)
				}
			}
		case "minbedrooms":
			if len(filter.Values) > 0 {
				count, err := strconv.Atoi(filter.Values[0])
				if err == nil {
					query = query.Where("bedrooms >= ?", count)
				}
			}
		case "minbathrooms":
			if len(filter.Values) > 0 {
				count, err := strconv.Atoi(filter.Values[0])
				if err == nil {
					query = query.Where("bathrooms >= ?", count)
				}
			}
		case "minarea":
			if len(filter.Values) > 0 {
				area, err := strconv.ParseFloat(filter.Values[0], 64)
				if err == nil {
					query = query.Where("total_area >= ?", area)
				}
			}
		case "maxarea":
			if len(filter.Values) > 0 {
				area, err := strconv.ParseFloat(filter.Values[0], 64)
				if err == nil {
					query = query.Where("total_area <= ?", area)
				}
			}
		case "category", "categoryid":
			if len(filter.Values) > 0 {
				value := strings.TrimSpace(filter.Values[0])
				if value != "" {
					query = query.Where("category_id = ?", value)
				}
			}
		case "createdat":
			if len(filter.Values) > 0 {
				timeValue, err := time.Parse(time.RFC3339, filter.Values[0])
				if err == nil {
					switch filter.Operator {
					case shared.FilterOpGte:
						query = query.Where("created_at >= ?", timeValue)
					case shared.FilterOpLte:
						query = query.Where("created_at <= ?", timeValue)
					default:
						query = query.Where("created_at = ?", timeValue)
					}
				}
			}
		}
	}

	return query
}

func (r *repository) applyLocationFilter(db *gorm.DB, lat, lon float64) *gorm.DB {
	distanceFormula := fmt.Sprintf(
		"(6371 * acos(cos(radians(%f)) * cos(radians(latitude)) * cos(radians(longitude) - radians(%f)) + sin(radians(%f)) * sin(radians(latitude))))",
		lat, lon, lat,
	)

	return db.Select("*, " + distanceFormula + " AS distance")
}

func (r *repository) applyPropertySorts(db *gorm.DB, sorts []shared.Sort, lat, lon *float64) *gorm.DB {
	query := db

	if len(sorts) == 0 {
		return query.Order("created_at DESC")
	}

	for _, sort := range sorts {
		direction := "ASC"
		if strings.ToLower(string(sort.Direction)) == string(shared.SortDesc) {
			direction = "DESC"
		}

		switch strings.ToLower(sort.Field) {
		case "name":
			query = query.Order("name " + direction)
		case "price":
			query = query.Order("price " + direction)
		case "createdat":
			query = query.Order("created_at " + direction)
		case "updatedat":
			query = query.Order("updated_at " + direction)
		case "totalarea":
			query = query.Order("total_area " + direction)
		case "bedrooms":
			query = query.Order("bedrooms " + direction)
		case "distance":
			if lat != nil && lon != nil {
				distanceFormula := fmt.Sprintf(
					"(6371 * acos(cos(radians(%f)) * cos(radians(latitude)) * cos(radians(longitude) - radians(%f)) + sin(radians(%f)) * sin(radians(latitude))))",
					*lat, *lon, *lat,
				)
				query = query.Order(distanceFormula + " " + direction)
			}
		default:
			continue
		}
	}

	return query
}

func (r *repository) mapProperty(dbProperty *schema.Property) (*property.Property, error) {
	var amenities property.PropertyAmenities
	if dbProperty.Amenities != "" {
		json.Unmarshal([]byte(dbProperty.Amenities), &amenities)
	}

	var images []string
	if dbProperty.Images != "" {
		json.Unmarshal([]byte(dbProperty.Images), &images)
	}

	p := &property.Property{
		ID:          dbProperty.ID,
		ShopID:      dbProperty.ShopID,
		Name:        dbProperty.Name,
		Description: dbProperty.Description,
		Type:        property.PropertyType(dbProperty.Type),
		Status:      property.PropertyStatus(dbProperty.Status),
		Condition:   property.PropertyCondition(dbProperty.Condition),

		Location: property.GeoLocation{
			Latitude:     dbProperty.Latitude,
			Longitude:    dbProperty.Longitude,
			Address:      dbProperty.Address,
			City:         dbProperty.City,
			State:        dbProperty.State,
			ZipCode:      dbProperty.ZipCode,
			Neighborhood: dbProperty.Neighborhood,
		},

		TotalArea: dbProperty.TotalArea,
		BuiltArea: dbProperty.BuiltArea,
		LotSize:   dbProperty.LotSize,

		Suites:      dbProperty.Suites,
		Bedrooms:    dbProperty.Bedrooms,
		Bathrooms:   dbProperty.Bathrooms,
		Floors:      dbProperty.Floors,
		LivingRooms: dbProperty.LivingRooms,

		Price:              dbProperty.Price,
		Currency:           dbProperty.Currency,
		PricePerSqMeter:    dbProperty.PricePerSqMeter,
		HOAFees:            dbProperty.HOAFees,
		PropertyTax:        dbProperty.PropertyTax,
		FinancingAvailable: dbProperty.FinancingAvailable,

		Amenities: amenities,

		YearBuilt:          dbProperty.YearBuilt,
		RenovatedYear:      dbProperty.RenovatedYear,
		ConstructionStatus: dbProperty.ConstructionStatus,
		RegistrationNumber: dbProperty.RegistrationNumber,
		OwnershipType:      dbProperty.OwnershipType,
		Images:             images,
		VirtualTourURL:     dbProperty.VirtualTourURL,
		EnergyRating:       dbProperty.EnergyRating,
		IsSolar:            dbProperty.IsSolar,

		CreatedAt: dbProperty.CreatedAt,
		UpdatedAt: dbProperty.UpdatedAt,
		CreatedBy: dbProperty.CreatedBy,
	}

	return p, nil
}

func (r *repository) mapProperties(dbProperties []schema.Property) ([]*property.Property, error) {
	if len(dbProperties) == 0 {
		return []*property.Property{}, nil
	}

	properties := make([]*property.Property, len(dbProperties))
	for i, dbProperty := range dbProperties {
		p, err := r.mapProperty(&dbProperty)
		if err != nil {
			return nil, err
		}
		properties[i] = p
	}

	return properties, nil
}

func calculateDistance(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadius = 6371

	dLat := (lat2 - lat1) * math.Pi / 180.0
	dLon := (lon2 - lon1) * math.Pi / 180.0

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*math.Pi/180.0)*math.Cos(lat2*math.Pi/180.0)*
			math.Sin(dLon/2)*math.Sin(dLon/2)

	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	return earthRadius * c
}
