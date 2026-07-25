package template_usecase

import (
	"vozko/domain/shared"
	"vozko/domain/whatsapp/template"
)

type listTemplatesUseCase struct {
	templateRepo template.Repository
}

func NewListTemplatesUseCase(templateRepo template.Repository) template.ListUseCase {
	return &listTemplatesUseCase{templateRepo: templateRepo}
}

func (uc *listTemplatesUseCase) Execute(input template.ListInput) (*shared.PaginatedResult[*template.Template], error) {
	return uc.templateRepo.List(input)
}
