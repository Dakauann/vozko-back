package category_usecase

import "vozko/domain/category"

type getCategoryUseCase struct {
	repo category.Repository
}

func NewGetCategoryUseCase(repo category.Repository) category.GetCategoryUseCase {
	return &getCategoryUseCase{repo: repo}
}

func (uc *getCategoryUseCase) Execute(id string) (*category.Category, error) {
	return uc.repo.FindByID(id)
}
