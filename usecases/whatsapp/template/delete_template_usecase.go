package template_usecase

import (
	"context"

	"vozko/domain/conversation"
	"vozko/domain/whatsapp/template"
)

type deleteTemplateUseCase struct {
	clientFactory template.WhatsAppClientFactory
	templateRepo  template.Repository
}

func NewDeleteTemplateUseCase(clientFactory template.WhatsAppClientFactory, templateRepo template.Repository) template.DeleteTemplateUseCase {
	return &deleteTemplateUseCase{
		clientFactory: clientFactory,
		templateRepo:  templateRepo,
	}
}

func (uc *deleteTemplateUseCase) Execute(input template.DeleteTemplateInput) error {
	if input.TemplateID == "" {
		return template.ErrExternalIDRequired
	}

	tmpl, err := uc.templateRepo.FindByID(input.TemplateID)
	if err != nil {
		return err
	}

	client, err := uc.clientFactory.ClientForWABA(tmpl.WABAId)
	if err != nil {
		return err
	}

	err = client.DeleteTemplate(context.Background(), conversation.DeleteTemplateInput{
		Name:       tmpl.Name,
		TemplateID: tmpl.ExternalID,
	})
	if err != nil {
		return err
	}

	return uc.templateRepo.Delete(tmpl.ID)
}
