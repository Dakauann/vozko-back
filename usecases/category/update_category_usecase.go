package category_usecase

import (
	"errors"
	"strings"

	"vozko/domain/category"
)

type updateCategoryUseCase struct {
	repo category.Repository
}

func NewUpdateCategoryUseCase(repo category.Repository) category.UpdateCategoryUseCase {
	return &updateCategoryUseCase{repo: repo}
}

func (uc *updateCategoryUseCase) Execute(id string, input category.UpdateCategoryInput) (*category.Category, error) {
	if strings.TrimSpace(id) == "" {
		return nil, category.ErrCategoryNotFound
	}

	existing, err := uc.repo.FindByID(id)
	if err != nil {
		return nil, err
	}

	updated := *existing

	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if name == "" {
			return nil, category.ErrCategoryNameRequired
		}
		updated.Name = name
	}

	if input.Slug != nil {
		slug := strings.TrimSpace(*input.Slug)
		if slug == "" {
			slug = updated.Name
		}
		slug = category.NormalizeSlug(slug)
		if slug == "" {
			return nil, category.ErrCategorySlugInvalid
		}

		excludeID := &id
		exists, err := uc.repo.SlugExists(slug, excludeID)
		if err != nil {
			return nil, err
		}
		if exists {
			return nil, category.ErrCategorySlugExists
		}

		updated.Slug = slug
	}

	if input.Description != nil {
		updated.Description = strings.TrimSpace(*input.Description)
	}

	if input.ClearParent {
		updated.ParentID = nil
	}

	if input.ParentID != nil {
		parentValue := strings.TrimSpace(*input.ParentID)
		if parentValue == "" {
			updated.ParentID = nil
		} else {
			if parentValue == id {
				return nil, category.ErrCategoryCycle
			}
			parent, err := uc.repo.FindByID(parentValue)
			if err != nil {
				if errors.Is(err, category.ErrCategoryNotFound) {
					return nil, category.ErrCategoryParentNotFound
				}
				return nil, err
			}

			if parent.ParentID != nil {
				current := parent.ParentID
				for current != nil {
					if *current == id {
						return nil, category.ErrCategoryCycle
					}
					ancestor, err := uc.repo.FindByID(*current)
					if err != nil {
						if errors.Is(err, category.ErrCategoryNotFound) {
							break
						}
						return nil, err
					}
					if ancestor.ParentID == nil {
						break
					}
					current = ancestor.ParentID
				}
			}

			updated.ParentID = &parentValue
		}
	}

	if err := uc.repo.Update(&updated); err != nil {
		return nil, err
	}

	return uc.repo.FindByID(id)
}
