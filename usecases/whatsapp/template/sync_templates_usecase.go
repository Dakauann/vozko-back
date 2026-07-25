package template_usecase

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"

	"vozko/domain/conversation"
	"vozko/domain/whatsapp/template"
)

type syncTemplatesUseCase struct {
	templateRepo  template.Repository
	clientFactory template.WhatsAppClientFactory
}

func NewSyncTemplatesUseCase(templateRepo template.Repository, clientFactory template.WhatsAppClientFactory) template.SyncTemplatesUseCase {
	return &syncTemplatesUseCase{
		templateRepo:  templateRepo,
		clientFactory: clientFactory,
	}
}

func (uc *syncTemplatesUseCase) Execute(input template.SyncTemplatesInput) ([]*template.Template, error) {
	if input.BusinessPhoneID == "" {
		return nil, errors.New("businessPhoneId is required")
	}

	pageSize := input.PageSize
	if pageSize <= 0 {
		pageSize = 100
	}

	wabaID, err := uc.clientFactory.WABAIdForPhone(input.BusinessPhoneID)
	if err != nil {
		return nil, err
	}

	client, err := uc.clientFactory.ClientForPhone(input.BusinessPhoneID)
	if err != nil {
		return nil, err
	}

	synced := make([]*template.Template, 0, pageSize)
	after := ""

	for {
		output, err := client.ListTemplates(context.Background(), conversation.ListTemplatesInput{
			Limit: pageSize,
			After: after,
		})
		if err != nil {
			return nil, err
		}

		for _, apiTemplate := range output.Templates {
			paramFormat := template.ParameterFormat(apiTemplate.ParameterFormat)
			if paramFormat == "" {
				paramFormat = template.ParameterFormatPositional
			}

			domainTemplate := &template.Template{
				ExternalID:      apiTemplate.ID,
				WABAId:          wabaID,
				Name:            apiTemplate.Name,
				Language:        apiTemplate.Language,
				Category:        template.TemplateCategory(strings.ToUpper(strings.TrimSpace(apiTemplate.Category))),
				Status:          template.TemplateStatus(strings.ToUpper(strings.TrimSpace(apiTemplate.Status))),
				ParameterFormat: paramFormat,
				Components:      convertComponents(apiTemplate.Components),
			}

			if apiTemplate.ParameterFormat == "" {
				domainTemplate.ParameterFormat = domainTemplate.GetEffectiveParameterFormat()
			}

			existing, err := uc.templateRepo.FindByExternalIDAndWABA(apiTemplate.ID, wabaID)
			if err == nil && existing != nil {
				domainTemplate.ID = existing.ID
				domainTemplate.HeaderMediaURL = existing.HeaderMediaURL
				domainTemplate.HeaderMediaID = existing.HeaderMediaID
				if err := uc.templateRepo.Update(existing.ID, domainTemplate); err != nil {
					continue
				}
			} else {
				domainTemplate.ID = uuid.New().String()
				if err := uc.templateRepo.Create(domainTemplate); err != nil {
					continue
				}
			}

			synced = append(synced, domainTemplate)
		}

		if !output.HasMore || strings.TrimSpace(output.NextAfter) == "" {
			break
		}
		after = strings.TrimSpace(output.NextAfter)
	}

	return synced, nil
}

func convertComponents(apiComponents []conversation.TemplateComponent) []template.TemplateComponent {
	components := make([]template.TemplateComponent, 0, len(apiComponents))
	for _, c := range apiComponents {
		comp := template.TemplateComponent{
			Type:   c.Type,
			Format: c.Format,
			Text:   c.Text,
		}
		for _, b := range c.Buttons {
			comp.Buttons = append(comp.Buttons, template.TemplateButton{
				Type:        b.Type,
				Text:        b.Text,
				URL:         b.URL,
				PhoneNumber: b.PhoneNumber,
				Example:     b.Example,
			})
		}
		if c.Example != nil {
			comp.Example = &template.TemplateExample{
				HeaderText:   c.Example.HeaderText,
				HeaderHandle: c.Example.HeaderHandle,
				BodyText:     c.Example.BodyText,
			}
			for _, np := range c.Example.BodyTextNamed {
				comp.Example.BodyTextNamed = append(comp.Example.BodyTextNamed, template.NamedParamExample{
					ParamName: np.ParamName,
					Example:   np.Example,
				})
			}
			for _, np := range c.Example.HeaderTextNamed {
				comp.Example.HeaderTextNamed = append(comp.Example.HeaderTextNamed, template.NamedParamExample{
					ParamName: np.ParamName,
					Example:   np.Example,
				})
			}
		}
		components = append(components, comp)
	}
	return components
}
