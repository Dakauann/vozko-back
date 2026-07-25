package template_usecase

import (
	"context"
	"errors"

	"vozko/domain/whatsapp/template"
)

var (
	ErrTemplateNotInDatabase = errors.New("template not found in database")
	ErrTemplateNoExternalID  = errors.New("template has no external ID, cannot sync")
)

type syncTemplateUseCase struct {
	templateRepo  template.Repository
	clientFactory template.WhatsAppClientFactory
}

func NewSyncTemplateUseCase(templateRepo template.Repository, clientFactory template.WhatsAppClientFactory) template.SyncTemplateUseCase {
	return &syncTemplateUseCase{
		templateRepo:  templateRepo,
		clientFactory: clientFactory,
	}
}

func (uc *syncTemplateUseCase) Execute(input template.SyncTemplateInput) (*template.Template, error) {
	existing, err := uc.templateRepo.FindByID(input.TemplateID)
	if err != nil {
		return nil, ErrTemplateNotInDatabase
	}
	if existing.ExternalID == "" {
		return nil, ErrTemplateNoExternalID
	}
	if existing.WABAId == "" {
		return nil, errors.New("template has no WABA ID — cannot determine which WABA to sync from")
	}

	client, err := uc.clientFactory.ClientForWABA(existing.WABAId)
	if err != nil {
		return nil, err
	}

	apiTemplate, err := client.GetTemplate(context.Background(), existing.ExternalID)
	if err != nil {
		return nil, err
	}

	existing.Name = apiTemplate.Name
	existing.Language = apiTemplate.Language
	existing.Category = template.TemplateCategory(apiTemplate.Category)
	existing.Status = template.TemplateStatus(apiTemplate.Status)
	existing.Components = convertComponents(apiTemplate.Components)

	if apiTemplate.ParameterFormat != "" {
		existing.ParameterFormat = template.ParameterFormat(apiTemplate.ParameterFormat)
	} else {
		existing.ParameterFormat = existing.GetEffectiveParameterFormat()
	}

	if err := uc.templateRepo.Update(existing.ID, existing); err != nil {
		return nil, err
	}

	return existing, nil
}
