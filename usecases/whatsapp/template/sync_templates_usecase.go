package template_usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/domain/conversation"
	"vozko/domain/whatsapp/quality"
	"vozko/domain/whatsapp/template"
)

type syncTemplatesUseCase struct {
	templateRepo  template.Repository
	clientFactory template.WhatsAppClientFactory
	history       quality.History
	changes       quality.Recorder
	now           func() time.Time
}

func NewSyncTemplatesUseCase(templateRepo template.Repository, clientFactory template.WhatsAppClientFactory, history quality.History, changes quality.Recorder) template.SyncTemplatesUseCase {
	return &syncTemplatesUseCase{
		templateRepo:  templateRepo,
		clientFactory: clientFactory,
		history:       history,
		changes:       changes,
		now:           time.Now,
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
				domainTemplate.Quality = existing.Quality
				domainTemplate.RateQuality(apiTemplate.QualityScore)
				if err := uc.templateRepo.Update(existing.ID, domainTemplate); err != nil {
					continue
				}
			} else {
				domainTemplate.ID = uuid.New().String()
				domainTemplate.RateQuality(apiTemplate.QualityScore)
				if err := uc.templateRepo.Create(domainTemplate); err != nil {
					continue
				}
			}

			synced = append(synced, domainTemplate)
		}

		if err := uc.recordQuality(wabaID, output.Templates); err != nil {
			return nil, err
		}

		if !output.HasMore || strings.TrimSpace(output.NextAfter) == "" {
			break
		}
		after = strings.TrimSpace(output.NextAfter)
	}

	return synced, nil
}

func (uc *syncTemplatesUseCase) recordQuality(wabaID string, rated []conversation.Template) error {
	ids := make([]string, 0, len(rated))
	for _, t := range rated {
		if strings.TrimSpace(t.QualityScore) != "" {
			ids = append(ids, t.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	latest, err := uc.history.LatestValues(quality.SubjectTemplate, quality.SignalQuality, ids)
	if err != nil {
		return fmt.Errorf("read template quality of waba %s: %w", wabaID, err)
	}
	now := uc.now()
	var moved []quality.Change
	for _, t := range rated {
		base := quality.Change{Subject: quality.SubjectTemplate, ExternalID: t.ID, WABAID: wabaID, Name: t.Name, Signal: quality.SignalQuality,
			Source: quality.SourcePoll, OccurredAt: quality.OccurredAt(t.QualityScoreAt, now)}
		if change, ok := base.Moved(latest[t.ID], t.QualityScore); ok {
			moved = append(moved, change)
		}
	}
	return uc.changes.Record(moved...)
}

func convertComponents(apiComponents []conversation.TemplateComponent) []template.TemplateComponent {
	return template.FromClientComponents(apiComponents)
}
