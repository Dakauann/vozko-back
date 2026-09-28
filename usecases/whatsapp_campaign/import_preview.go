package whatsapp_campaign_usecase

import (
	"context"
	"fmt"

	"vozko/domain/balance"
	"vozko/domain/campaign"
	"vozko/domain/lead"
	"vozko/domain/media"
	"vozko/domain/sheet"
	tmpl "vozko/domain/whatsapp/template"
	wc "vozko/domain/whatsapp_campaign"
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
	result, err := campaign.ReadImport(sheet.Parse(file.Data), req.Mapping, template.ParameterCount(), officialNumber)
	if err != nil {
		return nil, err
	}
	if result.TotalRows > wc.MaxCampaignPhoneNumbers {
		return nil, wc.ErrCampaignPhoneNumbersTooMany
	}
	preview := &wc.ImportPreview{ImportResult: *result}
	return preview, uc.price(req.WorkspaceID, template, preview)
}

func officialNumber(raw string) string {
	return lead.NormalizeNumber(lead.NormalizeRawNumber(raw))
}

func (uc *importPreview) price(workspaceID string, template *tmpl.Template, preview *wc.ImportPreview) error {
	category, err := template.BillingCategory()
	if err != nil {
		return fmt.Errorf("template category: %w", err)
	}
	unit, err := uc.deps.Prices.GetTemplateCostMicros(workspaceID, category)
	if err != nil {
		return fmt.Errorf("template price: %w", err)
	}
	balance, err := uc.deps.Balance.GetBalance(workspaceID)
	if err != nil {
		return fmt.Errorf("balance: %w", err)
	}
	preview.UnitCostMicros = unit
	preview.CostMicros = unit * int64(preview.ValidRows)
	preview.BalanceMicros = balance
	preview.Affordable = unit > 0 && balance >= preview.CostMicros
	return nil
}
