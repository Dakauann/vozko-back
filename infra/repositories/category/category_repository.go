package category_repository

import (
	"errors"
	"strings"

	"vozko/domain/category"
	"vozko/domain/shared"
	"vozko/infra/database/schema"

	"gorm.io/gorm"
)

type repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) category.Repository {
	return &repository{db: db}
}

func (r *repository) Create(cat *category.Category) error {
	dbCat := schema.Category{
		ID:          cat.ID,
		Name:        cat.Name,
		Slug:        cat.Slug,
		Description: cat.Description,
		ParentID:    cat.ParentID,
	}
	if err := r.db.Create(&dbCat).Error; err != nil {
		return err
	}
	cat.CreatedAt = dbCat.CreatedAt
	cat.UpdatedAt = dbCat.UpdatedAt
	return nil
}

func (r *repository) Update(cat *category.Category) error {
	update := map[string]interface{}{
		"name":        cat.Name,
		"slug":        cat.Slug,
		"description": cat.Description,
		"parent_id":   cat.ParentID,
	}
	return r.db.Model(&schema.Category{}).Where("id = ?", cat.ID).Updates(update).Error
}

func (r *repository) Delete(id string) error {
	return r.db.Delete(&schema.Category{}, "id = ?", id).Error
}

func (r *repository) FindByID(id string) (*category.Category, error) {
	var dbCat schema.Category
	if err := r.db.Preload("Parent").Preload("Children").
		Where("id = ?", id).First(&dbCat).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, category.ErrCategoryNotFound
		}
		return nil, err
	}
	return mapToDomain(&dbCat), nil
}

func (r *repository) List(input category.ListCategoriesInput) (*shared.PaginatedResult[*category.Category], error) {
	pagination := shared.NormalizePagination(input.Options.Pagination)

	countQuery := r.db.Model(&schema.Category{}).Select("categories.id")
	countQuery = r.applyFilters(countQuery, input)

	var total int64
	if err := countQuery.Count(&total).Error; err != nil {
		return nil, err
	}

	dataQuery := r.db.Model(&schema.Category{}).
		Preload("Parent").
		Preload("Children").
		Offset(pagination.Offset()).
		Limit(pagination.PageSize)

	dataQuery = r.applyFilters(dataQuery, input)
	hasSearch := strings.TrimSpace(input.Search) != ""
	dataQuery = r.applySorts(dataQuery, input.Options.Sorts, hasSearch)

	var dbCategories []schema.Category
	if err := dataQuery.Find(&dbCategories).Error; err != nil {
		return nil, err
	}

	items := make([]*category.Category, len(dbCategories))
	for i := range dbCategories {
		items[i] = mapToDomain(&dbCategories[i])
	}

	return shared.NewPaginatedResult(items, pagination, total), nil
}

func (r *repository) Exists(id string) (bool, error) {
	var count int64
	if err := r.db.Model(&schema.Category{}).Where("id = ?", id).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *repository) SlugExists(slug string, excludeID *string) (bool, error) {
	query := r.db.Model(&schema.Category{}).Where("LOWER(slug) = ?", strings.ToLower(slug))
	if excludeID != nil && *excludeID != "" {
		query = query.Where("id <> ?", *excludeID)
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *repository) HasChildren(id string) (bool, error) {
	var count int64
	if err := r.db.Model(&schema.Category{}).Where("parent_id = ?", id).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *repository) HasLinkedProducts(id string) (bool, error) {
	var count int64
	if err := r.db.Model(&schema.Variant{}).Where("category_id = ?", id).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *repository) GetDescendantIDs(id string) ([]string, error) {
	var descendantIDs []string

	query := `
		WITH RECURSIVE category_tree AS (
			SELECT id
			FROM categories
			WHERE id = ?
			
			UNION ALL
			
			SELECT c.id
			FROM categories c
			INNER JOIN category_tree ct ON c.parent_id = ct.id
		)
		SELECT id FROM category_tree
	`

	if err := r.db.Raw(query, id).Pluck("id", &descendantIDs).Error; err != nil {
		return nil, err
	}

	return descendantIDs, nil
}

func (r *repository) applyFilters(db *gorm.DB, input category.ListCategoriesInput) *gorm.DB {
	query := db

	if trimmed := strings.TrimSpace(input.Search); trimmed != "" {
		like := "%" + strings.ToLower(trimmed) + "%"
		query = query.Where("LOWER(name) LIKE ? OR LOWER(slug) LIKE ? OR LOWER(description) LIKE ?", like, like, like)
	}

	if input.ParentID != nil {
		if trimmed := strings.TrimSpace(*input.ParentID); trimmed != "" {
			query = query.Where("parent_id = ?", trimmed)
		} else {
			query = query.Where("parent_id IS NULL")
		}
	}

	return query
}

func (r *repository) applySorts(db *gorm.DB, sorts []shared.Sort, hasSearch bool) *gorm.DB {
	if len(sorts) == 0 {
		if hasSearch {
			return db.Order("name ASC")
		}
		return db.Order("created_at DESC")
	}

	query := db
	for _, sort := range sorts {
		direction := "ASC"
		if strings.EqualFold(string(sort.Direction), string(shared.SortDesc)) {
			direction = "DESC"
		}
		switch strings.ToLower(sort.Field) {
		case "name":
			query = query.Order("name " + direction)
		case "slug":
			query = query.Order("slug " + direction)
		case "createdat":
			query = query.Order("created_at " + direction)
		case "updatedat":
			query = query.Order("updated_at " + direction)
		default:
			continue
		}
	}

	return query
}

func mapToDomain(dbCat *schema.Category) *category.Category {
	if dbCat == nil {
		return nil
	}

	var parent *category.Category
	if dbCat.Parent != nil {
		parent = &category.Category{
			ID:          dbCat.Parent.ID,
			Name:        dbCat.Parent.Name,
			Slug:        dbCat.Parent.Slug,
			Description: dbCat.Parent.Description,
			ParentID:    dbCat.Parent.ParentID,
			CreatedAt:   dbCat.Parent.CreatedAt,
			UpdatedAt:   dbCat.Parent.UpdatedAt,
		}
	}

	children := make([]category.Category, len(dbCat.Children))
	for i := range dbCat.Children {
		childPtr := mapToDomain(&dbCat.Children[i])
		if childPtr != nil {
			children[i] = *childPtr
		}
	}

	return &category.Category{
		ID:          dbCat.ID,
		Name:        dbCat.Name,
		Slug:        dbCat.Slug,
		Description: dbCat.Description,
		ParentID:    dbCat.ParentID,
		CreatedAt:   dbCat.CreatedAt,
		UpdatedAt:   dbCat.UpdatedAt,
		Parent:      parent,
		Children:    children,
	}
}
