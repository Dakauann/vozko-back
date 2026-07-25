package template_usecase

import (
	"vozko/domain/whatsapp/template"
)

type getTemplateUseCase struct {
	templateRepo template.Repository
}

func NewGetTemplateUseCase(templateRepo template.Repository) template.GetUseCase {
	return &getTemplateUseCase{templateRepo: templateRepo}
}

func (uc *getTemplateUseCase) Execute(templateID string) (*template.Template, error) {
	if templateID == "" {
		return nil, template.ErrTemplateNotFound
	}
	return uc.templateRepo.FindByID(templateID)
}
