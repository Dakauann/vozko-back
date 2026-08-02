package shop_repository

import (
	"errors"
	"strings"

	"gorm.io/gorm"

	"vozko/domain/shared"
	"vozko/domain/shop"
	"vozko/infra/database/schema"
)

type repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) shop.Repository {
	return &repository{db: db}
}

func (r *repository) Create(s *shop.Shop) error {
	dbShop := &schema.Shop{
		UserID:        s.UserID,
		Name:          s.Name,
		Brand:         s.Brand,
		LogoMediaID:   stringToPtr(s.LogoMediaID),
		BannerMediaID: stringToPtr(s.BannerMediaID),
	}

	if err := r.db.Create(dbShop).Error; err != nil {
		return err
	}

	s.ID = dbShop.ID
	s.CreatedAt = dbShop.CreatedAt
	s.UpdatedAt = dbShop.UpdatedAt

	return nil
}

func (r *repository) Update(s *shop.Shop) error {
	update := map[string]interface{}{
		"name":            s.Name,
		"brand":           s.Brand,
		"logo_media_id":   stringToPtr(s.LogoMediaID),
		"banner_media_id": stringToPtr(s.BannerMediaID),
	}

	return r.db.Model(&schema.Shop{}).Where("id = ?", s.ID).Updates(update).Error
}

func (r *repository) Delete(id int64) error {
	return r.db.Delete(&schema.Shop{}, "id = ?", id).Error
}

func (r *repository) FindByID(id int64) (*shop.Shop, error) {
	var dbShop schema.Shop
	if err := r.db.Where("id = ?", id).First(&dbShop).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, shop.ErrShopNotFound
		}
		return nil, err
	}

	return mapToDomain(&dbShop), nil
}

func (r *repository) FindByUserID(userID string) (*shop.Shop, error) {
	var dbShop schema.Shop
	if err := r.db.Where("user_id = ?", userID).First(&dbShop).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, shop.ErrShopNotFound
		}
		return nil, err
	}

	return mapToDomain(&dbShop), nil
}

func (r *repository) List(input shop.ListShopsInput) (*shared.PaginatedResult[*shop.Shop], error) {
	page := input.Page
	if page < 1 {
		page = 1
	}
	pageSize := input.PageSize
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}

	offset := (page - 1) * pageSize

	countQuery := r.db.Model(&schema.Shop{})
	countQuery = r.applyFilters(countQuery, input)

	var total int64
	if err := countQuery.Count(&total).Error; err != nil {
		return nil, err
	}

	dataQuery := r.db.Model(&schema.Shop{}).
		Offset(offset).
		Limit(pageSize).
		Order("created_at DESC")

	dataQuery = r.applyFilters(dataQuery, input)

	var dbShops []schema.Shop
	if err := dataQuery.Find(&dbShops).Error; err != nil {
		return nil, err
	}

	items := make([]*shop.Shop, len(dbShops))
	for i := range dbShops {
		items[i] = mapToDomain(&dbShops[i])
	}

	pagination := shared.Pagination{
		Page:     page,
		PageSize: pageSize,
	}

	return shared.NewPaginatedResult(items, pagination, total), nil
}

func (r *repository) Exists(id int64) (bool, error) {
	var count int64
	err := r.db.Model(&schema.Shop{}).Where("id = ?", id).Count(&count).Error
	return count > 0, err
}

func (r *repository) UserHasShop(userID string) (bool, error) {
	var count int64
	err := r.db.Model(&schema.Shop{}).Where("user_id = ?", userID).Count(&count).Error
	return count > 0, err
}

func (r *repository) CountByUserID(userID string) (int64, error) {
	var count int64
	err := r.db.Model(&schema.Shop{}).Where("user_id = ?", userID).Count(&count).Error
	return count, err
}

func (r *repository) applyFilters(query *gorm.DB, input shop.ListShopsInput) *gorm.DB {
	if input.UserID != nil && *input.UserID != "" {
		query = query.Where("user_id = ?", *input.UserID)
	}

	if input.Search != nil && *input.Search != "" {
		searchTerm := "%" + strings.ToLower(*input.Search) + "%"
		query = query.Where("LOWER(name) LIKE ? OR LOWER(brand) LIKE ?", searchTerm, searchTerm)
	}

	return query
}

func mapToDomain(dbShop *schema.Shop) *shop.Shop {
	return &shop.Shop{
		ID:            dbShop.ID,
		UserID:        dbShop.UserID,
		Name:          dbShop.Name,
		Brand:         dbShop.Brand,
		LogoMediaID:   ptrToString(dbShop.LogoMediaID),
		BannerMediaID: ptrToString(dbShop.BannerMediaID),
		CreatedAt:     dbShop.CreatedAt,
		UpdatedAt:     dbShop.UpdatedAt,
		IsOfficial:    dbShop.IsOfficial,
	}
}

func stringToPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func ptrToString(s *string) string {
	if s != nil {
		return *s
	}
	return ""
}
