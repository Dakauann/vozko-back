package whatsapp_campaign_usecase

import (
	"strings"

	"vozko/domain/shared"
	wa_template "vozko/domain/whatsapp/template"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
)

type listCampaignsUseCase struct {
	campaignRepo wc.Repository
	entryRepo    wce.Repository
	templateRepo wa_template.Repository
}

func NewListCampaignsUseCase(
	campaignRepo wc.Repository,
	entryRepo wce.Repository,
	templateRepo wa_template.Repository,
) wc.ListCampaignsUseCase {
	return &listCampaignsUseCase{
		campaignRepo: campaignRepo,
		entryRepo:    entryRepo,
		templateRepo: templateRepo,
	}
}

func (uc *listCampaignsUseCase) Execute(input wc.ListCampaignsInput) (*shared.PaginatedResult[*wc.Campaign], error) {
	result, err := uc.campaignRepo.List(input)
	if err != nil {
		return nil, err
	}

	if result == nil || len(result.Items) == 0 {
		return result, nil
	}

	ids := make([]string, len(result.Items))
	for i, item := range result.Items {
		ids[i] = item.ID
	}

	countsByCampaign, err := uc.entryRepo.CountByStatusForCampaigns(ids)
	if err != nil {
		return nil, err
	}

	for _, item := range result.Items {
		item.Metrics = wc.NewCampaignMetrics(countsByCampaign[item.ID])

		recentEntries, err := uc.entryRepo.ListRecentlyUpdated(item.ID, recentEntriesLimit)
		if err != nil {
			return nil, err
		}
		item.RecentEntries = recentEntries
	}

	uc.enrichTemplateMeta(result.Items)

	return result, nil
}

func (uc *listCampaignsUseCase) enrichTemplateMeta(items []*wc.Campaign) {
	if uc.templateRepo == nil || len(items) == 0 {
		return
	}

	idSet := make(map[string]struct{}, len(items))
	templateIDs := make([]string, 0, len(items))
	for _, item := range items {
		tid := strings.TrimSpace(item.TemplateID)
		if tid == "" {
			continue
		}
		if _, ok := idSet[tid]; ok {
			continue
		}
		idSet[tid] = struct{}{}
		templateIDs = append(templateIDs, tid)
	}
	if len(templateIDs) == 0 {
		return
	}

	listed, err := uc.templateRepo.List(wa_template.ListInput{
		TemplateIDs: templateIDs,
		Options: shared.QueryOptions{
			Pagination: shared.Pagination{Page: 1, PageSize: len(templateIDs)},
		},
	})
	if err != nil || listed == nil {
		return
	}

	byID := make(map[string]*wa_template.Template, len(listed.Items))
	for _, tmpl := range listed.Items {
		if tmpl == nil {
			continue
		}
		byID[tmpl.ID] = tmpl
	}

	for _, item := range items {
		tmpl := byID[strings.TrimSpace(item.TemplateID)]
		if tmpl == nil {
			continue
		}
		item.TemplateName = tmpl.Name
		item.TemplateCategory = string(tmpl.Category)
	}
}
