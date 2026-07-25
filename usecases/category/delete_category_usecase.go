package category_usecase

import "vozko/domain/category"

type deleteCategoryUseCase struct {
	repo category.Repository
}

func NewDeleteCategoryUseCase(repo category.Repository) category.DeleteCategoryUseCase {
	return &deleteCategoryUseCase{repo: repo}
}

func (uc *deleteCategoryUseCase) Execute(id string) error {
	existing, err := uc.repo.FindByID(id)
	if err != nil {
		return err
	}

	hasChildren, err := uc.repo.HasChildren(existing.ID)
	if err != nil {
		return err
	}
	if hasChildren {
		return category.ErrCategoryHasChildren
	}

	inUse, err := uc.repo.HasLinkedProducts(existing.ID)
	if err != nil {
		return err
	}
	if inUse {
		return category.ErrCategoryInUse
	}

	return uc.repo.Delete(existing.ID)
}
