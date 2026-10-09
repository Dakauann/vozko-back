package whatsapp_campaign_usecase

import (
	"context"

	"vozko/domain/balance"
	"vozko/domain/campaign"
	"vozko/domain/media"
	"vozko/domain/shared"
	"vozko/domain/sheet"
	tmpl "vozko/domain/whatsapp/template"
	wc "vozko/domain/whatsapp_campaign"
	template_usecase "vozko/usecases/whatsapp/template"
)

type ImportPreviewDeps struct {
	Files     media.ReadMediaUseCase
	Templates tmpl.WorkspaceTemplatesUseCase
	Prices    tmpl.TemplateCostReader
	Balance   balance.BalanceReader
}

type importPreview struct{ deps ImportPreviewDeps }

func NewImportPreviewUseCase(deps ImportPreviewDeps) wc.ImportPreviewUseCase {
	return &importPreview{deps: deps}
}

func (uc *importPreview) Preview(ctx context.Context, req wc.ImportRequest) (*wc.ImportPreview, error) {
	template, err := uc.deps.Templates.Get(req.WorkspaceID, req.TemplateID)
	if err != nil {
		return nil, err
	}
	file, err := uc.deps.Files.Read(ctx, req.WorkspaceID, req.MediaID)
	if err != nil {
		return nil, err
	}
	result, err := campaign.ReadImport(sheet.Parse(file.Data), req.Mapping, template.ParameterCount(), shared.NormalizePhone)
	if err != nil {
		return nil, err
	}
	if result.TotalRows > wc.MaxCampaignPhoneNumbers {
		return nil, wc.ErrCampaignPhoneNumbersTooMany
	}
	preview := &wc.ImportPreview{ImportResult: *result}
	return preview, uc.price(req.WorkspaceID, template, preview)
}

func (uc *importPreview) price(workspaceID string, template *tmpl.Template, preview *wc.ImportPreview) error {
	cost, err := template_usecase.QuoteSend(uc.deps.Prices, uc.deps.Balance, workspaceID, template, int64(preview.ValidRows))
	if err != nil {
		return err
	}
	preview.UnitCostMicros = cost.UnitPriceMicros
	preview.CostMicros = cost.CostMicros
	preview.BalanceMicros = cost.BalanceMicros
	preview.Affordable = cost.Affordable
	return nil
}
