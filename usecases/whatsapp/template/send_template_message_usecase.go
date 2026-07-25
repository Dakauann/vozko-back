package template_usecase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"vozko/domain/balance"
	"vozko/domain/business_metrics"
	"vozko/domain/conversation"
	"vozko/domain/whatsapp/template"

	"github.com/google/uuid"
)

var (
	ErrRecipientRequired    = errors.New("recipient phone number is required")
	ErrTemplateNameRequired = errors.New("template name is required")
)

type sendTemplateMessageUseCase struct {
	clientFactory           template.WhatsAppClientFactory
	templateRepo            template.Repository
	recordMetric            business_metrics.RecordMetricUseCase
	consumeWhatsappTemplate balance.ConsumeWhatsappTemplateUseCase
}

func NewSendTemplateMessageUseCase(clientFactory template.WhatsAppClientFactory, templateRepo template.Repository, recordMetric business_metrics.RecordMetricUseCase, consumeWhatsappTemplate balance.ConsumeWhatsappTemplateUseCase) template.SendTemplateMessageUseCase {
	return &sendTemplateMessageUseCase{clientFactory: clientFactory, templateRepo: templateRepo, recordMetric: recordMetric, consumeWhatsappTemplate: consumeWhatsappTemplate}
}

func (uc *sendTemplateMessageUseCase) Execute(input template.SendTemplateMessageInput) (*template.SendTemplateResult, error) {
	if input.To == "" {
		return nil, ErrRecipientRequired
	}
	if input.TemplateName == "" {
		return nil, ErrTemplateNameRequired
	}
	if input.BusinessPhoneID == "" {
		return nil, errors.New("businessPhoneId is required")
	}

	client, err := uc.clientFactory.ClientForPhone(input.BusinessPhoneID)
	if err != nil {
		return nil, err
	}

	language := input.Language
	if language == "" {
		language = "pt_BR"
	}

	apiInput := conversation.SendTemplateMessageInput{
		To:               input.To,
		TemplateName:     input.TemplateName,
		Language:         language,
		Parameters:       input.TemplateParams,
		HeaderTextParams: input.HeaderParams,
	}

	var templateCategory string
	var resolvedTemplate *template.Template

	if uc.templateRepo != nil {
		wabaID, wabaErr := uc.clientFactory.WABAIdForPhone(input.BusinessPhoneID)
		if wabaErr != nil {
			if input.WorkspaceID != "" {
				return nil, fmt.Errorf("resolve WABA for template billing: %w", wabaErr)
			}
		} else if strings.TrimSpace(wabaID) == "" {
			if input.WorkspaceID != "" {
				return nil, fmt.Errorf("resolve WABA for template billing: %w", template.ErrTemplateCategoryUnavailable)
			}
		} else {
			resolvedTemplate, err = uc.templateRepo.FindByNameAndWABA(input.TemplateName, language, wabaID)
			if err != nil {
				if input.WorkspaceID != "" {
					return nil, fmt.Errorf("template metadata unavailable for billing: %w", err)
				}
			} else if resolvedTemplate != nil {
				bodyParamNames, headerParamNames := resolvedTemplate.GetBodyAndHeaderParameterNames()
				apiInput.ParameterNames = bodyParamNames
				apiInput.IsNamedParameterFormat = resolvedTemplate.IsNamedParameterFormat()
				log.Printf("[send-template-usecase] Template %s: isNamedFormat=%v, bodyParams=%v, headerParams=%v", resolvedTemplate.Name, apiInput.IsNamedParameterFormat, bodyParamNames, headerParamNames)

				if resolvedTemplate.HasMediaHeader() {
					headerMediaID := resolvedTemplate.GetHeaderMediaID()
					if headerMediaID != "" {
						apiInput.HeaderType = strings.ToLower(resolvedTemplate.GetHeaderFormat())
						apiInput.HeaderMediaID = headerMediaID
						log.Printf("[send-template-usecase] Using stored header media ID: %s (type: %s)", headerMediaID, apiInput.HeaderType)
					} else {
						log.Printf("[send-template-usecase] WARNING: Template %s has %s header but no header media ID stored", resolvedTemplate.Name, resolvedTemplate.GetHeaderFormat())
					}
				}

				if len(input.HeaderParams) == 0 && len(headerParamNames) > 0 {
					log.Printf("[send-template-usecase] WARNING: Template %s has %d header text parameter(s) but none were provided", resolvedTemplate.Name, len(headerParamNames))
				}
			}
		}
	}

	if input.WorkspaceID != "" {
		if resolvedTemplate == nil {
			return nil, fmt.Errorf("template metadata unavailable for billing: %w", template.ErrTemplateNotFound)
		}

		templateCategory, err = resolvedTemplate.BillingCategory()
		if err != nil {
			return nil, fmt.Errorf("template metadata unavailable for billing: %w", err)
		}

		refID := input.BusinessPhoneID + ":" + input.To
		_, consumeErr := uc.consumeWhatsappTemplate.Execute(input.WorkspaceID, refID, templateCategory)
		if consumeErr != nil {
			return nil, fmt.Errorf("insufficient balance to send WhatsApp template: %w", consumeErr)
		}
	}

	output, err := client.SendTemplateMessage(context.Background(), apiInput)

	result := &template.SendTemplateResult{}
	if output != nil {
		result.MessageID = output.MessageID
		result.RequestPayload = output.RequestPayload
		result.ResponsePayload = output.ResponsePayload
		result.ResponseStatus = output.ResponseStatus
	}

	if err != nil {

		if input.WorkspaceID != "" {
			refID := input.BusinessPhoneID + ":" + input.To
			if refundErr := uc.consumeWhatsappTemplate.Refund(input.WorkspaceID, refID, templateCategory); refundErr != nil {
				log.Printf("[send-template-usecase] WARNING: failed to refund balance for workspace %s: %v", input.WorkspaceID, refundErr)
			}
		}
		return result, err
	}

	if uc.recordMetric != nil {
		entityID := strings.TrimSpace(output.MessageID)
		if entityID == "" {
			entityID = uuid.NewString()
		}

		if err := uc.recordMetric.Execute(business_metrics.RecordMetricInput{
			EventType:  business_metrics.EventWhatsAppTemplateMessageSent,
			EntityID:   entityID,
			EntityType: business_metrics.EntityTypeMessage,
			Metadata: map[string]string{
				"to":            strings.TrimSpace(input.To),
				"template_name": strings.TrimSpace(input.TemplateName),
				"language":      language,
			},
		}); err != nil {
			log.Printf("failed to record whatsapp_template_message_sent metric: %v", err)
		}
	}

	return result, nil
}
