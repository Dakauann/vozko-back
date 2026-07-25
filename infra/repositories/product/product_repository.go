package product_repository

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"vozko/domain/category"
	"vozko/domain/inventory"
	domainmedia "vozko/domain/media"
	"vozko/domain/product"
	"vozko/domain/shared"
	"vozko/infra/database/schema"

	"gorm.io/gorm"
)

type repository struct {
	db           *gorm.DB
	stockService inventory.VariantStockService
}

func NewRepository(db *gorm.DB, stockService inventory.VariantStockService) product.ProductRepository {
	return &repository{db: db, stockService: stockService}
}

func derefFloat64(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func ptrFloat64(v float64) *float64 {
	return &v
}

func ptrInt(v int) *int {
	return &v
}

func (r *repository) Create(p *product.Product) error {
	if err := r.db.Transaction(func(tx *gorm.DB) error {
		tagsJSON, _ := json.Marshal(p.Tags)
		dbProduct := &schema.Product{
			Name:        p.Name,
			Description: p.Description,
			ShopID:      p.ShopID,
			Tags:        string(tagsJSON),
		}
		if err := tx.Create(dbProduct).Error; err != nil {
			return err
		}
		p.ID = dbProduct.ID

		for i := range p.Variants {
			variant := &p.Variants[i]
			dbVariant := &schema.Variant{
				Name:           variant.Name,
				ProductID:      dbProduct.ID,
				SKU:            variant.SKU,
				RetailPrice:    derefFloat64(variant.RetailPrice),
				WholesalePrice: derefFloat64(variant.WholesalePrice),
				Cost:           derefFloat64(variant.Cost),
				Inventory:      variant.Inventory,
				CategoryID:     variant.CategoryID,
				Announced:      variant.Announced,

				MinQuantityForWholesale: derefInt(variant.MinQuantityForWholesale),
				Weight:                  derefFloat64(variant.WeightKg),
				Height:                  derefFloat64(variant.HeightCm),
				Width:                   derefFloat64(variant.WidthCm),
				Depth:                   derefFloat64(variant.DepthCm),
				IsDefault:               variant.IsDefault,
			}
			if err := tx.Create(dbVariant).Error; err != nil {
				if strings.Contains(err.Error(), "uni_variants_sku") || strings.Contains(err.Error(), "duplicate") {
					return product.ErrVariantSKUDuplicate
				}
				return err
			}
			variant.ID = dbVariant.ID
			variant.ProductID = dbProduct.ID
			variant.BaseInventory = dbVariant.Inventory
			variant.LaunchedStock = 0
			variant.ReservedStock = 0
			variant.SoldStock = 0
			variant.Inventory = dbVariant.Inventory
			variant.Announced = dbVariant.Announced
			variant.MinQuantityForWholesale = ptrInt(dbVariant.MinQuantityForWholesale)

			for _, mediaID := range variant.MediaIDs {
				exists, err := r.MediaExists(mediaID)
				if err != nil {
					return err
				}
				if !exists {
					return product.ErrMediaNotFound
				}

				dbVariantMedia := &schema.VariantMedia{
					VariantID: dbVariant.ID,
					MediaID:   mediaID,
				}
				if err := tx.Create(dbVariantMedia).Error; err != nil {
					return err
				}
			}

			for j := range variant.Options {
				option := &variant.Options[j]
				var dbOptionType schema.OptionType
				if err := tx.Where("name = ?", option.OptionType).FirstOrCreate(&dbOptionType, schema.OptionType{
					Name: option.OptionType,
				}).Error; err != nil {
					return err
				}

				var dbOptionValue schema.OptionValue
				if err := tx.Where("option_type_id = ? AND value = ?", dbOptionType.ID, option.OptionValue).FirstOrCreate(&dbOptionValue, schema.OptionValue{
					OptionTypeID: dbOptionType.ID,
					Value:        option.OptionValue,
				}).Error; err != nil {
					return err
				}

				dbVariantOption := &schema.VariantOption{
					VariantID:     dbVariant.ID,
					OptionValueID: dbOptionValue.ID,
				}
				if err := tx.Create(dbVariantOption).Error; err != nil {
					return err
				}

				option.ID = dbVariantOption.ID
				option.VariantID = dbVariantOption.VariantID
				option.OptionValueID = dbVariantOption.OptionValueID
			}
		}

		return nil
	}); err != nil {
		return err
	}

	created, err := r.FindByID(p.ID)
	if err != nil {
		return err
	}

	*p = *created
	return nil
}

func (r *repository) Update(productId string, p *product.Product) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := r.updateProduct(tx, productId, p); err != nil {
			return err
		}

		for i := range p.Variants {
			if err := r.updateVariant(tx, productId, &p.Variants[i]); err != nil {
				return err
			}
		}

		return nil
	})
}

func (r *repository) updateProduct(tx *gorm.DB, productId string, p *product.Product) error {
	tagsJSON, _ := json.Marshal(p.Tags)
	updates := map[string]interface{}{
		"name":        p.Name,
		"description": p.Description,
		"shop_id":     p.ShopID,
		"tags":        string(tagsJSON),
	}

	return tx.Model(&schema.Product{}).
		Where("id = ?", productId).
		Updates(updates).Error
}

func (r *repository) updateVariant(tx *gorm.DB, productId string, v *product.Variant) error {
	updates := map[string]interface{}{
		"name":                       v.Name,
		"sku":                        v.SKU,
		"retail_price":               v.RetailPrice,
		"wholesale_price":            v.WholesalePrice,
		"cost":                       v.Cost,
		"announced":                  v.Announced,
		"min_quantity_for_wholesale": v.MinQuantityForWholesale,
		"weight":                     v.WeightKg,
		"height":                     v.HeightCm,
		"width":                      v.WidthCm,
		"depth":                      v.DepthCm,
		"is_default":                 v.IsDefault,
		"category_id":                v.CategoryID,
	}

	result := tx.Model(&schema.Variant{}).
		Where("id = ? AND product_id = ?", v.ID, productId).
		Updates(updates)

	if result.Error != nil {
		if strings.Contains(result.Error.Error(), "uni_variants_sku") || strings.Contains(result.Error.Error(), "duplicate") {
			return product.ErrVariantSKUDuplicate
		}
		return result.Error
	}

	if result.RowsAffected == 0 {
		return product.ErrVariantNotFound
	}

	if len(v.MediaIDs) > 0 {
		if err := r.updateVariantMedia(tx, v.ID, v.MediaIDs); err != nil {
			return err
		}
	}

	if len(v.Options) > 0 {
		if err := r.updateVariantOptions(tx, v.ID, v.Options); err != nil {
			return err
		}
	}

	return nil
}

func (r *repository) updateVariantOptions(tx *gorm.DB, variantId string, options []product.VariantOption) error {
	if err := tx.Where("variant_id = ?", variantId).Delete(&schema.VariantOption{}).Error; err != nil {
		return err
	}

	for i := range options {
		opt := &options[i]

		optionValueID, err := r.resolveOptionValueID(tx, opt)
		if err != nil {
			return err
		}
		if optionValueID == "" {
			continue
		}

		dbOption := &schema.VariantOption{
			VariantID:     variantId,
			OptionValueID: optionValueID,
		}
		if err := tx.Create(dbOption).Error; err != nil {
			return err
		}
	}

	return nil
}

func (r *repository) updateVariantMedia(tx *gorm.DB, variantId string, mediaIDs []string) error {
	if err := tx.Where("variant_id = ?", variantId).Delete(&schema.VariantMedia{}).Error; err != nil {
		return err
	}

	for _, mediaID := range mediaIDs {
		exists, err := r.MediaExists(mediaID)
		if err != nil {
			return err
		}
		if !exists {
			return product.ErrMediaNotFound
		}

		dbVariantMedia := &schema.VariantMedia{
			VariantID: variantId,
			MediaID:   mediaID,
		}
		if err := tx.Create(dbVariantMedia).Error; err != nil {
			return err
		}
	}

	return nil
}

func (r *repository) resolveOptionValueID(tx *gorm.DB, opt *product.VariantOption) (string, error) {
	if opt.OptionType != "" && opt.OptionValue != "" {
		var dbOptionType schema.OptionType
		if err := tx.Where("name = ?", opt.OptionType).FirstOrCreate(&dbOptionType, schema.OptionType{
			Name: opt.OptionType,
		}).Error; err != nil {
			return "", err
		}

		var dbOptionValue schema.OptionValue
		if err := tx.Where("option_type_id = ? AND value = ?", dbOptionType.ID, opt.OptionValue).FirstOrCreate(&dbOptionValue, schema.OptionValue{
			OptionTypeID: dbOptionType.ID,
			Value:        opt.OptionValue,
		}).Error; err != nil {
			return "", err
		}

		return dbOptionValue.ID, nil
	}

	if opt.OptionValueID != "" {
		return opt.OptionValueID, nil
	}

	return "", nil
}

func (r *repository) Delete(productId string) error {
	return r.db.Delete(&schema.Product{}, "id = ?", productId).Error
}

func (r *repository) FindByID(productID string) (*product.Product, error) {
	var dbProduct schema.Product
	if err := r.db.Preload("Variants", func(db *gorm.DB) *gorm.DB {
		return db.Preload("Options.OptionValueDB.OptionType").Preload("Category")
	}).
		Preload("Shop").
		Where("id = ?", productID).
		First(&dbProduct).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, product.ErrProductNotFound
		}
		return nil, err
	}

	products, err := r.mapProducts([]schema.Product{dbProduct})
	if err != nil {
		return nil, err
	}
	if len(products) == 0 {
		return nil, product.ErrProductNotFound
	}

	return products[0], nil
}

func (r *repository) List(input product.ListProductsInput) (*shared.PaginatedResult[*product.Product], error) {
	pagination := shared.NormalizePagination(input.Options.Pagination)

	countQuery := r.db.Model(&schema.Product{}).Select("products.id").Distinct()
	countQuery = r.applyProductSearch(countQuery, input.Search)
	countQuery = r.applyProductFilters(countQuery, input.Options.Filters)

	var total int64
	if err := countQuery.Count(&total).Error; err != nil {
		return nil, err
	}

	dataQuery := r.db.Model(&schema.Product{}).
		Preload("Variants", func(db *gorm.DB) *gorm.DB {
			return db.Preload("Options.OptionValueDB.OptionType").Preload("Category")
		}).
		Preload("Shop")

	dataQuery = r.applyProductSearch(dataQuery, input.Search)
	dataQuery = r.applyProductFilters(dataQuery, input.Options.Filters)
	dataQuery = dataQuery.Order("products.id")
	dataQuery = r.applyProductSorts(dataQuery, input.Options.Sorts)

	var dbProducts []schema.Product
	if err := dataQuery.
		Offset(pagination.Offset()).
		Limit(pagination.PageSize).
		Find(&dbProducts).Error; err != nil {
		return nil, err
	}

	products, err := r.mapProducts(dbProducts)
	if err != nil {
		return nil, err
	}

	return shared.NewPaginatedResult(products, pagination, total), nil
}

func (r *repository) FindByIDs(productIDs []string) ([]*product.Product, error) {
	if len(productIDs) == 0 {
		return []*product.Product{}, nil
	}

	var dbProducts []schema.Product
	if err := r.db.Preload("Variants", func(db *gorm.DB) *gorm.DB {
		return db.Preload("Options.OptionValueDB.OptionType").Preload("Category")
	}).
		Preload("Shop").
		Where("id IN ?", productIDs).
		Find(&dbProducts).Error; err != nil {
		return nil, err
	}

	return r.mapProducts(dbProducts)
}

func (r *repository) Search(input product.SearchProductsInput) (*shared.PaginatedResult[*product.Product], error) {
	return r.List(product.ListProductsInput{
		Search:  input.Query,
		Options: input.Options,
	})
}

func (r *repository) applyProductSearch(db *gorm.DB, search string) *gorm.DB {
	trimmed := strings.TrimSpace(search)
	if trimmed == "" {
		return db
	}
	words := strings.Fields(strings.ToLower(trimmed))
	if len(words) == 0 {
		return db
	}

	query := db.Joins("LEFT JOIN variants ON variants.product_id = products.id")

	for _, word := range words {
		pattern := "%" + word + "%"
		query = query.Where("(LOWER(products.name) LIKE ? OR LOWER(products.description) LIKE ? OR LOWER(products.tags) LIKE ? OR LOWER(variants.sku) LIKE ?)", pattern, pattern, pattern, pattern)
	}

	return query
}

func (r *repository) applyProductFilters(db *gorm.DB, filters []shared.Filter) *gorm.DB {
	query := db

	for _, filter := range filters {
		field := strings.ToLower(filter.Field)
		switch field {
		case "shopid":
			if len(filter.Values) == 0 {
				continue
			}
			shopID, err := strconv.ParseInt(filter.Values[0], 10, 64)
			if err != nil {
				continue
			}
			query = query.Where("products.shop_id = ?", shopID)
		case "tag", "tags":
			for _, value := range filter.Values {
				trimmed := strings.TrimSpace(strings.ToLower(value))
				if trimmed == "" {
					continue
				}
				pattern := "%\"" + trimmed + "\"%"
				query = query.Where("LOWER(products.tags) LIKE ?", pattern)
			}
		case "sku":
			if len(filter.Values) == 0 {
				continue
			}
			value := strings.TrimSpace(strings.ToLower(filter.Values[0]))
			if value == "" {
				continue
			}
			query = query.Joins("LEFT JOIN variants ON variants.product_id = products.id")
			if filter.Operator == shared.FilterOpLike {
				query = query.Where("LOWER(variants.sku) LIKE ?", "%"+value+"%")
			} else {
				query = query.Where("LOWER(variants.sku) = ?", value)
			}
		case "createdat":
			if len(filter.Values) == 0 {
				continue
			}
			timeValue, err := time.Parse(time.RFC3339, filter.Values[0])
			if err != nil {
				continue
			}
			switch filter.Operator {
			case shared.FilterOpGte:
				query = query.Where("products.created_at >= ?", timeValue)
			case shared.FilterOpLte:
				query = query.Where("products.created_at <= ?", timeValue)
			default:
				query = query.Where("products.created_at = ?", timeValue)
			}
		case "updatedat":
			if len(filter.Values) == 0 {
				continue
			}
			timeValue, err := time.Parse(time.RFC3339, filter.Values[0])
			if err != nil {
				continue
			}
			switch filter.Operator {
			case shared.FilterOpGte:
				query = query.Where("products.updated_at >= ?", timeValue)
			case shared.FilterOpLte:
				query = query.Where("products.updated_at <= ?", timeValue)
			default:
				query = query.Where("products.updated_at = ?", timeValue)
			}
		case "announced":
			if len(filter.Values) == 0 {
				continue
			}
			flag, err := strconv.ParseBool(filter.Values[0])
			if err != nil {
				continue
			}
			query = query.Joins("LEFT JOIN variants ON variants.product_id = products.id").
				Where("variants.announced = ?", flag)
		case "minprice":
			if len(filter.Values) == 0 {
				continue
			}
			amount, err := strconv.ParseFloat(filter.Values[0], 64)
			if err != nil {
				continue
			}
			query = query.Joins("LEFT JOIN variants ON variants.product_id = products.id").
				Where("variants.retail_price >= ?", amount)
		case "maxprice":
			if len(filter.Values) == 0 {
				continue
			}
			amount, err := strconv.ParseFloat(filter.Values[0], 64)
			if err != nil {
				continue
			}
			query = query.Joins("LEFT JOIN variants ON variants.product_id = products.id").
				Where("variants.retail_price <= ?", amount)
		case "category", "categoryid":
			if len(filter.Values) == 0 {
				continue
			}
			value := strings.TrimSpace(filter.Values[0])
			if value == "" {
				continue
			}
			query = query.Joins("LEFT JOIN variants ON variants.product_id = products.id").
				Where(`variants.category_id IN (
					WITH RECURSIVE category_tree AS (
						SELECT id FROM categories WHERE id = ?
						UNION ALL
						SELECT c.id FROM categories c
						INNER JOIN category_tree ct ON c.parent_id = ct.id
					)
					SELECT id FROM category_tree
				)`, value)
		}
	}

	return query
}

func (r *repository) applyProductSorts(db *gorm.DB, sorts []shared.Sort) *gorm.DB {
	query := db
	if len(sorts) == 0 {
		return query.Order("products.created_at DESC")
	}

	for _, sort := range sorts {
		direction := "ASC"
		if strings.ToLower(string(sort.Direction)) == string(shared.SortDesc) {
			direction = "DESC"
		}

		switch strings.ToLower(sort.Field) {
		case "name":
			query = query.Order("products.name " + direction)
		case "createdat":
			query = query.Order("products.created_at " + direction)
		case "updatedat":
			query = query.Order("products.updated_at " + direction)
		case "price":
			query = query.Order("(SELECT MIN(variants.retail_price) FROM variants WHERE variants.product_id = products.id) " + direction)
		default:
			continue
		}
	}

	return query
}

func (r *repository) mapProducts(dbProducts []schema.Product) ([]*product.Product, error) {
	if len(dbProducts) == 0 {
		return []*product.Product{}, nil
	}

	variantIDs := make([]string, 0, len(dbProducts)*2)
	for _, dbProduct := range dbProducts {
		for _, dbVariant := range dbProduct.Variants {
			variantIDs = append(variantIDs, dbVariant.ID)
		}
	}

	stockComponents := map[string]inventory.VariantStockSnapshot{}
	if len(variantIDs) > 0 && r.stockService != nil {
		var err error
		stockComponents, err = r.stockService.GetSnapshots(variantIDs)
		if err != nil {
			return nil, err
		}
	}

	var variantMedia []struct {
		VariantID   string
		MediaID     string
		WorkspaceID string
		MediaURL    string
		PreviewURL  string
		CreatedAt   time.Time
		Type        string
	}
	if len(variantIDs) > 0 {
		if err := r.db.Table("variant_medias").
			Select("variant_medias.variant_id, variant_medias.media_id, medias.workspace_id, medias.url as media_url, medias.preview_url as preview_url, medias.created_at as created_at, medias.type as type").
			Joins("JOIN medias ON variant_medias.media_id = medias.id").
			Where("variant_medias.variant_id IN ?", variantIDs).
			Scan(&variantMedia).Error; err != nil {
			return nil, err
		}
	}

	mediaIDsMap := make(map[string][]string)
	mediaMap := make(map[string][]domainmedia.Media)
	previewMap := make(map[string][]string)
	for _, vm := range variantMedia {
		mediaIDsMap[vm.VariantID] = append(mediaIDsMap[vm.VariantID], vm.MediaID)
		m := domainmedia.Media{
			ID:          vm.MediaID,
			WorkspaceID: vm.WorkspaceID,
			URL:         vm.MediaURL,
			PreviewURL:  vm.PreviewURL,
			CreatedAt:   vm.CreatedAt,
			Type:        domainmedia.MediaType(vm.Type),
		}
		mediaMap[vm.VariantID] = append(mediaMap[vm.VariantID], m)
		if vm.PreviewURL != "" {
			previewMap[vm.VariantID] = append(previewMap[vm.VariantID], vm.PreviewURL)
		}
	}

	products := make([]*product.Product, len(dbProducts))
	for i, dbProduct := range dbProducts {
		var tags []string
		if dbProduct.Tags != "" {
			_ = json.Unmarshal([]byte(dbProduct.Tags), &tags)
		}

		isFromOfficialShop := false
		if dbProduct.Shop != nil {
			isFromOfficialShop = dbProduct.Shop.IsOfficial
		}

		variants := make([]product.Variant, len(dbProduct.Variants))
		for j, dbVariant := range dbProduct.Variants {
			options := make([]product.VariantOption, len(dbVariant.Options))
			for k, dbOption := range dbVariant.Options {
				options[k] = product.VariantOption{
					ID:            dbOption.ID,
					VariantID:     dbOption.VariantID,
					OptionValueID: dbOption.OptionValueID,
					OptionType:    dbOption.OptionValueDB.OptionType.Name,
					OptionValue:   dbOption.OptionValueDB.Value,
				}
			}

			mediaIDs := mediaIDsMap[dbVariant.ID]
			mediaObjs := mediaMap[dbVariant.ID]
			mediaURLs := make([]string, 0, len(mediaObjs))
			for _, mm := range mediaObjs {
				mediaURLs = append(mediaURLs, mm.URL)
			}
			components := stockComponents[dbVariant.ID]

			var categoryID *string
			if dbVariant.CategoryID != nil {
				idCopy := *dbVariant.CategoryID
				categoryID = &idCopy
			}

			var categoryData *category.Category
			if dbVariant.Category != nil {
				categoryData = &category.Category{
					ID:          dbVariant.Category.ID,
					Name:        dbVariant.Category.Name,
					Slug:        dbVariant.Category.Slug,
					Description: dbVariant.Category.Description,
					ParentID:    dbVariant.Category.ParentID,
					CreatedAt:   dbVariant.Category.CreatedAt,
					UpdatedAt:   dbVariant.Category.UpdatedAt,
				}
			}

			baseInventory := dbVariant.Inventory
			if components.BaseInventory != 0 {
				baseInventory = components.BaseInventory
			}

			available := baseInventory + components.Launched - components.Sold - components.Reserved
			if available < 0 {
				available = 0
			}

			variants[j] = product.Variant{
				Name:           dbVariant.Name,
				ID:             dbVariant.ID,
				ProductID:      dbVariant.ProductID,
				SKU:            dbVariant.SKU,
				RetailPrice:    ptrFloat64(dbVariant.RetailPrice),
				WholesalePrice: ptrFloat64(dbVariant.WholesalePrice),
				Cost:           ptrFloat64(dbVariant.Cost),
				Inventory:      available,
				BaseInventory:  baseInventory,
				LaunchedStock:  components.Launched,
				ReservedStock:  components.Reserved,
				SoldStock:      components.Sold,

				Announced: dbVariant.Announced,

				MinQuantityForWholesale: ptrInt(dbVariant.MinQuantityForWholesale),
				WeightKg:                ptrFloat64(dbVariant.Weight),
				HeightCm:                ptrFloat64(dbVariant.Height),
				WidthCm:                 ptrFloat64(dbVariant.Width),
				DepthCm:                 ptrFloat64(dbVariant.Depth),
				MediaIDs:                mediaIDs,

				Medias:     mediaObjs,
				IsDefault:  dbVariant.IsDefault,
				CreatedAt:  dbVariant.CreatedAt,
				UpdatedAt:  dbVariant.UpdatedAt,
				Options:    options,
				CategoryID: categoryID,
				Category:   categoryData,
			}
		}

		products[i] = &product.Product{
			ID:                        dbProduct.ID,
			ShopID:                    dbProduct.ShopID,
			Name:                      dbProduct.Name,
			Description:               dbProduct.Description,
			Tags:                      tags,
			IsFromOfficialShop:        isFromOfficialShop,
			CreatedAt:                 dbProduct.CreatedAt,
			UpdatedAt:                 dbProduct.UpdatedAt,
			Variants:                  variants,
		}
	}

	return products, nil
}

func (r *repository) CreateOptionType(ot *product.OptionType) error {
	dbOptionType := &schema.OptionType{
		Name: ot.Name,
	}
	if err := r.db.Create(dbOptionType).Error; err != nil {
		return err
	}
	ot.ID = dbOptionType.ID
	return nil
}

func (r *repository) CreateOptionValue(ov *product.OptionValue) error {
	dbOptionValue := &schema.OptionValue{
		OptionTypeID: ov.OptionTypeID,
		Value:        ov.Value,
	}
	if err := r.db.Create(dbOptionValue).Error; err != nil {
		return err
	}
	ov.ID = dbOptionValue.ID
	return nil
}

func (r *repository) ListOptionTypes() ([]*product.OptionType, error) {
	var dbOptionTypes []schema.OptionType
	if err := r.db.Preload("Values").Find(&dbOptionTypes).Error; err != nil {
		return nil, err
	}

	optionTypes := make([]*product.OptionType, len(dbOptionTypes))
	for i, dbOT := range dbOptionTypes {
		values := make([]product.OptionValue, len(dbOT.Values))
		for j, dbVal := range dbOT.Values {
			values[j] = product.OptionValue{
				ID:           dbVal.ID,
				OptionTypeID: dbVal.OptionTypeID,
				Value:        dbVal.Value,
			}
		}
		optionTypes[i] = &product.OptionType{
			ID:     dbOT.ID,
			Name:   dbOT.Name,
			Values: values,
		}
	}

	return optionTypes, nil
}

func (r *repository) ListOptionValues(optionTypeID string) ([]*product.OptionValue, error) {
	var dbOptionValues []schema.OptionValue
	if err := r.db.Where("option_type_id = ?", optionTypeID).Find(&dbOptionValues).Error; err != nil {
		return nil, err
	}

	optionValues := make([]*product.OptionValue, len(dbOptionValues))
	for i, dbOV := range dbOptionValues {
		optionValues[i] = &product.OptionValue{
			ID:           dbOV.ID,
			OptionTypeID: dbOV.OptionTypeID,
			Value:        dbOV.Value,
		}
	}

	return optionValues, nil
}

func (r *repository) OptionValueExists(optionValueID string) (bool, error) {
	var count int64
	if err := r.db.Model(&schema.OptionValue{}).Where("id = ?", optionValueID).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *repository) FindOrCreateOptionValue(optionType, optionValue string) (string, error) {
	var dbOptionType schema.OptionType
	if err := r.db.Where("name = ?", optionType).FirstOrCreate(&dbOptionType, schema.OptionType{
		Name: optionType,
	}).Error; err != nil {
		return "", err
	}

	var dbOptionValue schema.OptionValue
	if err := r.db.Where("option_type_id = ? AND value = ?", dbOptionType.ID, optionValue).FirstOrCreate(&dbOptionValue, schema.OptionValue{
		OptionTypeID: dbOptionType.ID,
		Value:        optionValue,
	}).Error; err != nil {
		return "", err
	}

	return dbOptionValue.ID, nil
}

func (r *repository) MediaExists(mediaID string) (bool, error) {
	var count int64
	if err := r.db.Model(&schema.Media{}).Where("id = ?", mediaID).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}
