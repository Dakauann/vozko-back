package template_usecase

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"

	"vozko/domain/conversation"
	"vozko/domain/whatsapp/template"
)

type replicateTemplateUseCase struct {
	templateRepo  template.Repository
	clientFactory template.WhatsAppClientFactory
}

func NewReplicateTemplateUseCase(templateRepo template.Repository, clientFactory template.WhatsAppClientFactory) template.ReplicateTemplateUseCase {
	return &replicateTemplateUseCase{
		templateRepo:  templateRepo,
		clientFactory: clientFactory,
	}
}

func (uc *replicateTemplateUseCase) Execute(input template.ReplicateTemplateInput) (*template.CreateTemplateOutput, error) {
	if input.TemplateID == "" {
		return nil, errors.New("templateId is required")
	}
	if input.TargetBusinessPhoneID == "" {
		return nil, errors.New("targetBusinessPhoneId is required")
	}

	source, err := uc.templateRepo.FindByID(input.TemplateID)
	if err != nil {
		return nil, errors.New("source template not found")
	}

	targetWABAId, err := uc.clientFactory.WABAIdForPhone(input.TargetBusinessPhoneID)
	if err != nil {
		return nil, err
	}

	if source.WABAId == targetWABAId {
		return nil, errors.New("target phone belongs to the same WABA as the source template, nothing to replicate")
	}

	existing, _ := uc.templateRepo.FindByNameAndWABA(source.Name, source.Language, targetWABAId)
	if existing != nil {
		return nil, errors.New("template with this name already exists on the target WABA")
	}

	targetClient, err := uc.clientFactory.ClientForPhone(input.TargetBusinessPhoneID)
	if err != nil {
		return nil, err
	}

	apiComponents := template.ToClientComponents(source.Components)

	paramFormat := string(source.ParameterFormat)
	if paramFormat == "" {
		paramFormat = string(template.ParameterFormatPositional)
	}

	apiOutput, err := targetClient.CreateTemplate(context.Background(), conversation.CreateTemplateInput{
		Name:            strings.ToLower(source.Name),
		Language:        source.Language,
		Category:        string(source.Category),
		ParameterFormat: paramFormat,
		Components:      apiComponents,
	})
	if err != nil {
		return nil, err
	}

	tmpl := &template.Template{
		ID:              uuid.New().String(),
		ExternalID:      apiOutput.ID,
		WABAId:          targetWABAId,
		Name:            strings.ToLower(source.Name),
		Language:        source.Language,
		Category:        source.Category,
		Status:          template.TemplateStatus(apiOutput.Status),
		ParameterFormat: source.ParameterFormat,
		Components:      source.Components,
		HeaderMediaURL:  source.HeaderMediaURL,
	}

	if err := uc.templateRepo.Create(tmpl); err != nil {
	}

	return &template.CreateTemplateOutput{
		ID:         tmpl.ID,
		ExternalID: apiOutput.ID,
		Name:       tmpl.Name,
		Status:     tmpl.Status,
	}, nil
}
