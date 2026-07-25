package cart_repository

import (
	"time"

	"vozko/domain/cart"
	"vozko/domain/inventory"
	domainmedia "vozko/domain/media"
	"vozko/infra/database/schema"

	"gorm.io/gorm"
)

type variantMediaRow struct {
	VariantID   string
	MediaID     string
	WorkspaceID string
	MediaURL    string
	PreviewURL  string
	CreatedAt   time.Time
	Type        string
}

type repository struct {
	db           *gorm.DB
	stockService inventory.VariantStockService
}

func NewRepository(db *gorm.DB, stockService inventory.VariantStockService) cart.CartRepository {
	return &repository{db: db, stockService: stockService}
}

func (r *repository) GetCartByUserID(userID string) (*cart.Cart, error) {
	var dbCart schema.Cart
	if err := r.db.Preload("Items.Options").Where("user_id = ?", userID).First(&dbCart).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}

	return r.mapToCart(&dbCart)
}

func (r *repository) CreateCart(cart *cart.Cart) error {
	dbCart := &schema.Cart{
		ID:     cart.ID,
		UserID: cart.UserID,
	}
	return r.db.Create(dbCart).Error
}

func (r *repository) UpdateCart(cart *cart.Cart) error {
	dbCart := &schema.Cart{
		ID:     cart.ID,
		UserID: cart.UserID,
	}
	return r.db.Save(dbCart).Error
}

func (r *repository) AddItemToCart(userID string, item *cart.CartItem) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var dbCart schema.Cart
		if err := tx.Where("user_id = ?", userID).First(&dbCart).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				dbCart = schema.Cart{
					UserID: userID,
				}
				if err := tx.Create(&dbCart).Error; err != nil {
					return err
				}
			} else {
				return err
			}
		}

		var dbVariant schema.Variant
		if err := tx.Where("id = ?", item.VariantID).First(&dbVariant).Error; err != nil {
			return err
		}

		dbItem := &schema.CartItem{
			CartID:     dbCart.ID,
			ProductID:  item.ProductID,
			VariantID:  item.VariantID,
			Quantity:   item.Quantity,
			UnitPrice:  item.UnitPrice,
			TotalPrice: item.TotalPrice,
		}

		if err := tx.Create(dbItem).Error; err != nil {
			return err
		}

		for _, option := range item.SelectedOptions {
			dbOption := &schema.CartItemOption{
				CartItemID:  dbItem.ID,
				OptionType:  option.OptionType,
				OptionValue: option.OptionValue,
			}
			if err := tx.Create(dbOption).Error; err != nil {
				return err
			}
		}

		return nil
	})
}

func (r *repository) UpdateCartItem(userID string, itemID string, quantity int, unitPrice float64) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var dbCart schema.Cart
		if err := tx.Where("user_id = ?", userID).First(&dbCart).Error; err != nil {
			return err
		}

		var dbItem schema.CartItem
		if err := tx.Where("id = ? AND cart_id = ?", itemID, dbCart.ID).First(&dbItem).Error; err != nil {
			return err
		}

		dbItem.Quantity = quantity
		dbItem.UnitPrice = unitPrice
		dbItem.TotalPrice = unitPrice * float64(quantity)

		return tx.Save(&dbItem).Error
	})
}

func (r *repository) RemoveCartItem(userID string, itemID string) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var dbCart schema.Cart
		if err := tx.Where("user_id = ?", userID).First(&dbCart).Error; err != nil {
			return err
		}

		return tx.Where("id = ? AND cart_id = ?", itemID, dbCart.ID).
			Delete(&schema.CartItem{}).Error
	})
}

func (r *repository) ClearCart(userID string) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var dbCart schema.Cart
		if err := tx.Where("user_id = ?", userID).First(&dbCart).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return nil
			}
			return err
		}

		return tx.Where("cart_id = ?", dbCart.ID).Delete(&schema.CartItem{}).Error
	})
}

func (r *repository) RemoveCartItems(userID string, itemIDs []string) error {
	if len(itemIDs) == 0 {
		return nil
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		var dbCart schema.Cart
		if err := tx.Where("user_id = ?", userID).First(&dbCart).Error; err != nil {
			return err
		}

		return tx.Where("id IN ? AND cart_id = ?", itemIDs, dbCart.ID).
			Delete(&schema.CartItem{}).Error
	})
}

func (r *repository) GetCartItemByID(userID string, itemID string) (*cart.CartItem, error) {
	var dbItem schema.CartItem
	if err := r.db.Preload("Options").
		Joins("JOIN carts ON cart_items.cart_id = carts.id").
		Where("carts.user_id = ? AND cart_items.id = ?", userID, itemID).
		First(&dbItem).Error; err != nil {
		return nil, err
	}

	return r.mapToCartItem(&dbItem)
}

func (r *repository) GetCartItemsByIDs(userID string, itemIDs []string) ([]cart.CartItem, error) {
	if len(itemIDs) == 0 {
		return nil, nil
	}
	var dbItems []schema.CartItem
	if err := r.db.Preload("Options").
		Joins("JOIN carts ON cart_items.cart_id = carts.id").
		Where("carts.user_id = ? AND cart_items.id IN ?", userID, itemIDs).
		Find(&dbItems).Error; err != nil {
		return nil, err
	}

	result := make([]cart.CartItem, 0, len(dbItems))
	for i := range dbItems {
		item, err := r.mapToCartItem(&dbItems[i])
		if err != nil {
			return nil, err
		}
		result = append(result, *item)
	}
	return result, nil
}

func (r *repository) GetCartItemByVariantAndOptions(userID string, variantID string, options []cart.SelectedOption) (*cart.CartItem, error) {
	var dbItems []schema.CartItem
	if err := r.db.Preload("Options").
		Joins("JOIN carts ON cart_items.cart_id = carts.id").
		Where("carts.user_id = ? AND cart_items.variant_id = ?", userID, variantID).
		Find(&dbItems).Error; err != nil {
		return nil, err
	}

	for i := range dbItems {
		if cartItemOptionsMatch(dbItems[i].Options, options) {
			return r.mapToCartItem(&dbItems[i])
		}
	}

	return nil, nil
}

func (r *repository) GetCartItemByVariant(userID string, variantID string) (*cart.CartItem, error) {
	var dbItem schema.CartItem
	if err := r.db.Preload("Options").
		Joins("JOIN carts ON cart_items.cart_id = carts.id").
		Where("carts.user_id = ? AND cart_items.variant_id = ?", userID, variantID).
		First(&dbItem).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}

	return r.mapToCartItem(&dbItem)
}

func cartItemOptionsMatch(dbOptions []schema.CartItemOption, selected []cart.SelectedOption) bool {
	if len(dbOptions) != len(selected) {
		return len(dbOptions) == 0 && len(selected) == 0
	}

	if len(dbOptions) == 0 {
		return true
	}

	optionMap := make(map[string]string, len(dbOptions))
	for _, opt := range dbOptions {
		optionMap[opt.OptionType] = opt.OptionValue
	}

	for _, sel := range selected {
		if value, ok := optionMap[sel.OptionType]; !ok || value != sel.OptionValue {
			return false
		}
	}

	return true
}

func (r *repository) mapToCart(dbCart *schema.Cart) (*cart.Cart, error) {

	variantIDs := make([]string, 0, len(dbCart.Items))
	variantSet := make(map[string]struct{}, len(dbCart.Items))
	productIDs := make([]string, 0, len(dbCart.Items))
	productSet := make(map[string]struct{}, len(dbCart.Items))
	for _, it := range dbCart.Items {
		if it.VariantID != "" {
			if _, ok := variantSet[it.VariantID]; !ok {
				variantSet[it.VariantID] = struct{}{}
				variantIDs = append(variantIDs, it.VariantID)
			}
		}
		if it.ProductID != "" {
			if _, ok := productSet[it.ProductID]; !ok {
				productSet[it.ProductID] = struct{}{}
				productIDs = append(productIDs, it.ProductID)
			}
		}
	}

	mediaMap := make(map[string][]variantMediaRow)
	if len(variantIDs) > 0 {
		var variantImages []variantMediaRow
		if err := r.db.Table("variant_medias").
			Select("variant_medias.variant_id, variant_medias.media_id, medias.workspace_id, medias.url AS media_url, medias.preview_url AS preview_url, medias.created_at, medias.type").
			Joins("JOIN medias ON variant_medias.media_id = medias.id").
			Where("variant_medias.variant_id IN ?", variantIDs).
			Scan(&variantImages).Error; err != nil {
			return nil, err
		}

		for _, mi := range variantImages {
			mediaMap[mi.VariantID] = append(mediaMap[mi.VariantID], mi)
		}
	}

	productMap := make(map[string]*schema.Product)
	if len(productIDs) > 0 {
		var dbProducts []schema.Product
		if err := r.db.Preload("Variants.Options.OptionValueDB.OptionType").
			Where("id IN ?", productIDs).
			Find(&dbProducts).Error; err != nil {
			return nil, err
		}
		for i := range dbProducts {
			productMap[dbProducts[i].ID] = &dbProducts[i]
		}
	}

	stockMap := map[string]inventory.VariantStockSnapshot{}
	if len(variantIDs) > 0 && r.stockService != nil {
		stockData, err := r.stockService.GetSnapshots(variantIDs)
		if err != nil {
			return nil, err
		}
		stockMap = stockData
	}

	items := make([]cart.CartItem, len(dbCart.Items))
	for i, dbItem := range dbCart.Items {
		cartItem, err := r.mapToCartItemFull(&dbItem, mediaMap, productMap, stockMap)
		if err != nil {
			return nil, err
		}
		items[i] = *cartItem
	}

	return &cart.Cart{
		ID:        dbCart.ID,
		UserID:    dbCart.UserID,
		Items:     items,
		CreatedAt: dbCart.CreatedAt,
		UpdatedAt: dbCart.UpdatedAt,
	}, nil
}

func (r *repository) mapToCartItem(dbItem *schema.CartItem) (*cart.CartItem, error) {

	mediaMap := make(map[string][]variantMediaRow)

	var variantImages []variantMediaRow
	if err := r.db.Table("variant_medias").
		Select("variant_medias.variant_id, variant_medias.media_id, medias.workspace_id, medias.url AS media_url, medias.preview_url AS preview_url, medias.created_at, medias.type").
		Joins("JOIN medias ON variant_medias.media_id = medias.id").
		Where("variant_medias.variant_id = ?", dbItem.VariantID).
		Scan(&variantImages).Error; err != nil {
		return nil, err
	}

	for _, img := range variantImages {
		mediaMap[dbItem.VariantID] = append(mediaMap[dbItem.VariantID], img)
	}

	return r.mapToCartItemWithMedia(dbItem, mediaMap)
}

func (r *repository) mapToCartItemWithMedia(dbItem *schema.CartItem, mediaMap map[string][]variantMediaRow) (*cart.CartItem, error) {

	productMap := make(map[string]*schema.Product)
	var dbProduct schema.Product
	if err := r.db.Preload("Variants.Options.OptionValueDB.OptionType").
		Where("id = ?", dbItem.ProductID).First(&dbProduct).Error; err == nil {
		productMap[dbProduct.ID] = &dbProduct
	}

	stockMap := map[string]inventory.VariantStockSnapshot{}
	if dbProduct.ID != "" && r.stockService != nil {
		variantIDs := make([]string, len(dbProduct.Variants))
		for i, v := range dbProduct.Variants {
			variantIDs[i] = v.ID
		}
		if len(variantIDs) > 0 {
			stockData, err := r.stockService.GetSnapshots(variantIDs)
			if err != nil {
				return nil, err
			}
			stockMap = stockData
		}
	}

	return r.mapToCartItemFull(dbItem, mediaMap, productMap, stockMap)
}

func (r *repository) mapToCartItemFull(dbItem *schema.CartItem, mediaMap map[string][]variantMediaRow, productMap map[string]*schema.Product, stockMap map[string]inventory.VariantStockSnapshot) (*cart.CartItem, error) {
	var productInfo cart.CartProduct
	var variantInfo cart.CartVariant

	if dbProduct, ok := productMap[dbItem.ProductID]; ok {
		productInfo = cart.CartProduct{
			ID:          dbProduct.ID,
			Name:        dbProduct.Name,
			Description: dbProduct.Description,
		}

		for _, dbVariant := range dbProduct.Variants {
			if dbVariant.ID == dbItem.VariantID {
				components := stockMap[dbVariant.ID]
				baseInventory := dbVariant.Inventory
				if components.BaseInventory != 0 {
					baseInventory = components.BaseInventory
				}
				available := baseInventory + components.Launched - components.Sold - components.Reserved
				if available < 0 {
					available = 0
				}
				variantInfo = cart.CartVariant{
					ID:             dbVariant.ID,
					SKU:            dbVariant.SKU,
					RetailPrice:    dbVariant.RetailPrice,
					WholesalePrice: dbVariant.WholesalePrice,
					Name:           dbVariant.Name,
					Announced:      dbVariant.Announced,

					MinQuantityForWholesale: dbVariant.MinQuantityForWholesale,
					Inventory:               available,
					Options:                 []cart.CartVariantOption{},
				}
				break
			}
		}
	}

	mediaURLs := make([]string, 0)
	mediaObjs := make([]domainmedia.Media, 0)
	if imgs, ok := mediaMap[dbItem.VariantID]; ok {
		for _, img := range imgs {
			if img.MediaURL != "" {
				mediaURLs = append(mediaURLs, img.MediaURL)
			}
			m := domainmedia.Media{
				ID:          img.MediaID,
				WorkspaceID: img.WorkspaceID,
				URL:         img.MediaURL,
				PreviewURL:  img.PreviewURL,
				CreatedAt:   img.CreatedAt,
				Type:        domainmedia.MediaType(img.Type),
			}
			mediaObjs = append(mediaObjs, m)
		}
	}

	productInfo.Medias = mediaObjs

	productInfo.Variant = &variantInfo

	selectedOptions := make([]cart.CartVariantOption, len(dbItem.Options))
	for i, dbOption := range dbItem.Options {
		selectedOptions[i] = cart.CartVariantOption{
			OptionType:  dbOption.OptionType,
			OptionValue: dbOption.OptionValue,
		}
	}

	return &cart.CartItem{
		ID:              dbItem.ID,
		CartID:          dbItem.CartID,
		ProductID:       dbItem.ProductID,
		VariantID:       dbItem.VariantID,
		Quantity:        dbItem.Quantity,
		UnitPrice:       dbItem.UnitPrice,
		TotalPrice:      dbItem.UnitPrice * float64(dbItem.Quantity),
		SelectedOptions: selectedOptions,
		CreatedAt:       dbItem.CreatedAt,
		UpdatedAt:       dbItem.UpdatedAt,
		Product:         &productInfo,
	}, nil
}
