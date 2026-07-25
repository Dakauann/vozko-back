package category_usecase

import (
	"strings"

	"github.com/google/uuid"

	"vozko/domain/category"
)

type createCategoryUseCase struct {
	repo category.Repository
}

func NewCreateCategoryUseCase(repo category.Repository) category.CreateCategoryUseCase {
	return &createCategoryUseCase{repo: repo}
}

func (uc *createCategoryUseCase) Execute(input category.CreateCategoryInput) (*category.Category, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, category.ErrCategoryNameRequired
	}

	slug := strings.TrimSpace(input.Slug)
	if slug == "" {
		slug = name
	}
	slug = category.NormalizeSlug(slug)
	if slug == "" {
		return nil, category.ErrCategorySlugInvalid
	}

	exists, err := uc.repo.SlugExists(slug, nil)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, category.ErrCategorySlugExists
	}

	var parentID *string
	if input.ParentID != nil {
		trimmed := strings.TrimSpace(*input.ParentID)
		if trimmed != "" {
			parentExists, err := uc.repo.Exists(trimmed)
			if err != nil {
				return nil, err
			}
			if !parentExists {
				return nil, category.ErrCategoryParentNotFound
			}
			parentID = &trimmed
		}
	}

	cat := &category.Category{
		ID:          uuid.New().String(),
		Name:        name,
		Slug:        slug,
		Description: strings.TrimSpace(input.Description),
		ParentID:    parentID,
	}

	if err := uc.repo.Create(cat); err != nil {
		return nil, err
	}

	created, err := uc.repo.FindByID(cat.ID)
	if err != nil {
		return nil, err
	}

	return created, nil
}
