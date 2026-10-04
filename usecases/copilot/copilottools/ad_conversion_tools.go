package copilottools

import (
	"context"
	"strings"

	"vozko/domain/advertising"
	"vozko/domain/copilot"
	"vozko/domain/tools"
	"vozko/domain/workspace"
)

type AdConversions interface {
	Settings(ctx context.Context, workspaceID string) (*advertising.ConversionSettings, error)
	CheckSave(ctx context.Context, workspaceID string, s advertising.ConversionSettings) (*advertising.Pixel, error)
	Save(ctx context.Context, workspaceID string, s advertising.ConversionSettings) (*advertising.ConversionSettings, error)
	CheckConnectDataset(ctx context.Context, workspaceID, businessPhoneID string) (*advertising.AdAccount, error)
	ConnectDataset(ctx context.Context, workspaceID, businessPhoneID string) (*advertising.ConversionSettings, error)
	Recent(ctx context.Context, workspaceID string) ([]advertising.ConversionRecord, error)
}

type AdPixels interface {
	Pixels(ctx context.Context, workspaceID, accountID string) ([]advertising.Pixel, error)
	CreatePixel(ctx context.Context, workspaceID, accountID, name string) (*advertising.Pixel, error)
}

const (
	leadEventText     = "oportunidade criada no CRM envia " + advertising.EventNameLead
	purchaseEventText = "oportunidade ganha no CRM envia " + advertising.EventNamePurchase + " com o valor"
)

func conversionData(s *advertising.ConversionSettings) map[string]interface{} {
	events := []string{}
	if s.SendLeads {
		events = append(events, leadEventText)
	}
	if s.SendPurchases {
		events = append(events, purchaseEventText)
	}
	return map[string]interface{}{
		"ad_account_id": s.AdAccountID, "enabled": s.Enabled, "whatsapp_dataset_connected": s.DatasetID != "",
		"pixel_id": s.PixelID, "send_leads": s.SendLeads, "send_purchases": s.SendPurchases, "events": events,
	}
}

type getAdConversionSettingsTool struct{ adGrowth }

func (t *getAdConversionSettingsTool) Meta() copilot.Meta {
	return adsMeta(workspace.ActionRead, false)
}

func (t *getAdConversionSettingsTool) Definition() tools.Definition {
	return definition("get_ad_conversion_settings",
		"Mostra como o Vozko avisa a Meta das conversões do CRM: se está ligado, a conta de anúncios, se o WhatsApp oficial está ligado ao "+
			"conjunto de dados da Meta, o pixel e quais eventos saem (oportunidade criada envia LeadSubmitted, oportunidade ganha envia Purchase).",
		struct{}{})
}

func (t *getAdConversionSettingsTool) Execute(ctx context.Context, cc copilot.Context, _ map[string]interface{}) copilot.Result {
	s, err := t.deps.Conversions.Settings(ctx, cc.WorkspaceID)
	if err != nil {
		return growthFailure("get_ad_conversion_settings", "", err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: conversionData(s)}
}

type saveAdConversionSettingsArgs struct {
	AdAccountID   string `json:"ad_account_id" req:"true" desc:"ad_account_id de list_ad_accounts (também aceita o id da Meta ou o nome exato da conta) que recebe as conversões"`
	Enabled       *bool  `json:"enabled" desc:"true liga o envio, false desliga; omita para manter"`
	SendLeads     *bool  `json:"send_leads" desc:"oportunidade criada no CRM envia LeadSubmitted; omita para manter"`
	SendPurchases *bool  `json:"send_purchases" desc:"oportunidade ganha no CRM envia Purchase com o valor; omita para manter"`
	PixelID       string `json:"pixel_id" desc:"pixel_id de list_ad_pixels, para conversões de quem não veio por mensagem; omita para manter"`
}

type conversionPlan struct {
	account  *advertising.AdAccount
	settings advertising.ConversionSettings
	pixel    string
}

type saveAdConversionSettingsTool struct{ adGrowth }

func (t *saveAdConversionSettingsTool) Meta() copilot.Meta {
	return adsMeta(workspace.ActionUpdate, true)
}

func (t *saveAdConversionSettingsTool) Definition() tools.Definition {
	return definition("save_ad_conversion_settings",
		"Muda o envio de conversões do CRM para a Meta: liga ou desliga, escolhe a conta, o pixel e quais eventos saem "+
			"(oportunidade criada envia LeadSubmitted, oportunidade ganha envia Purchase). Os dados de contato saem criptografados. "+
			"Para ligar, a conta precisa de um conjunto de dados do WhatsApp (connect_ad_dataset) ou de um pixel. Só depois da aprovação do usuário.",
		saveAdConversionSettingsArgs{})
}

func (t *saveAdConversionSettingsTool) plan(ctx context.Context, cc copilot.Context, args map[string]interface{}) (*conversionPlan, error) {
	a, err := validateArgs[saveAdConversionSettingsArgs](nil, cc, args)
	if err != nil {
		return nil, err
	}
	account, err := t.ads.account(ctx, cc, a.AdAccountID)
	if err != nil {
		return nil, err
	}
	current, err := t.deps.Conversions.Settings(ctx, cc.WorkspaceID)
	if err != nil {
		return nil, err
	}
	s := *current
	s.AdAccountID = account.ID
	overwrite(&s.Enabled, a.Enabled)
	overwrite(&s.SendLeads, a.SendLeads)
	overwrite(&s.SendPurchases, a.SendPurchases)
	if pixel := strings.TrimSpace(a.PixelID); pixel != "" {
		s.PixelID = pixel
	}
	pixel, err := t.deps.Conversions.CheckSave(ctx, cc.WorkspaceID, s)
	if err != nil {
		return nil, err
	}
	plan := &conversionPlan{account: account, settings: s}
	if pixel != nil {
		plan.pixel = pixel.Name
	}
	return plan, nil
}

func overwrite(target *bool, value *bool) {
	if value != nil {
		*target = *value
	}
}

func (t *saveAdConversionSettingsTool) Validate(ctx context.Context, cc copilot.Context, args map[string]interface{}) error {
	_, err := t.plan(ctx, cc, args)
	return adsValidation("save_ad_conversion_settings", err)
}

func onOff(on bool) string {
	if on {
		return "ligado"
	}
	return "desligado"
}

func (t *saveAdConversionSettingsTool) Describe(ctx context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	p, err := t.plan(ctx, cc, args)
	if err != nil {
		return []copilot.Field{{Key: "conversions", Value: "configuração com campos a corrigir"}}
	}
	fields := []copilot.Field{
		{Key: "account", Value: p.account.Name},
		{Key: "sending", Value: onOff(p.settings.Enabled)},
		{Key: "leads", Value: leadEventText + ": " + onOff(p.settings.SendLeads)},
		{Key: "purchases", Value: purchaseEventText + ": " + onOff(p.settings.SendPurchases)},
	}
	if p.pixel != "" {
		fields = append(fields, copilot.Field{Key: "pixel", Value: p.pixel})
	}
	return fields
}

func (t *saveAdConversionSettingsTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	p, err := t.plan(ctx, cc, args)
	if err != nil {
		return growthFailure("save_ad_conversion_settings", "", err)
	}
	saved, err := t.deps.Conversions.Save(ctx, cc.WorkspaceID, p.settings)
	if err != nil {
		return growthFailure("save_ad_conversion_settings", p.account.ID, err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: conversionData(saved)}
}

type connectAdDatasetArgs struct {
	BusinessPhoneID string `json:"business_phone_id" req:"true" id:"true" desc:"business_phone_id exato de list_business_phones (número oficial do WhatsApp)"`
}

type connectAdDatasetTool struct{ adGrowth }

func (t *connectAdDatasetTool) Meta() copilot.Meta { return adsMeta(workspace.ActionUpdate, true) }

func (t *connectAdDatasetTool) Definition() tools.Definition {
	return definition("connect_ad_dataset",
		"Liga o número oficial do WhatsApp ao conjunto de dados da Meta da conta escolhida em save_ad_conversion_settings, para a Meta saber "+
			"quais conversas de anúncio viraram oportunidades e vendas. Salve a conta antes. Só depois da aprovação do usuário.",
		connectAdDatasetArgs{})
}

func (t *connectAdDatasetTool) plan(ctx context.Context, cc copilot.Context, args map[string]interface{}) (*advertising.AdAccount, string, error) {
	a, err := validateArgs[connectAdDatasetArgs](nil, cc, args)
	if err != nil {
		return nil, "", err
	}
	phoneID, err := knownID(a.BusinessPhoneID, "business_phone_id", "list_business_phones")
	if err != nil {
		return nil, "", err
	}
	account, err := t.deps.Conversions.CheckConnectDataset(ctx, cc.WorkspaceID, phoneID)
	if err != nil {
		return nil, "", err
	}
	return account, phoneID, nil
}

func (t *connectAdDatasetTool) Validate(ctx context.Context, cc copilot.Context, args map[string]interface{}) error {
	_, _, err := t.plan(ctx, cc, args)
	return adsValidation("connect_ad_dataset", err)
}

func (t *connectAdDatasetTool) Describe(ctx context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	account, _, err := t.plan(ctx, cc, args)
	if err != nil {
		return []copilot.Field{{Key: "dataset", Value: "ligação com campos a corrigir"}}
	}
	return []copilot.Field{{Key: "account", Value: account.Name}, {Key: "dataset", Value: "número oficial do WhatsApp ligado ao conjunto de dados da Meta"}}
}

func (t *connectAdDatasetTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	account, phoneID, err := t.plan(ctx, cc, args)
	if err != nil {
		return growthFailure("connect_ad_dataset", "", err)
	}
	saved, err := t.deps.Conversions.ConnectDataset(ctx, cc.WorkspaceID, phoneID)
	if err != nil {
		return growthFailure("connect_ad_dataset", account.ID, err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: conversionData(saved)}
}

type listAdPixelsTool struct{ adGrowth }

func (t *listAdPixelsTool) Meta() copilot.Meta { return adsMeta(workspace.ActionRead, false) }

func (t *listAdPixelsTool) Definition() tools.Definition {
	return definition("list_ad_pixels",
		"Lista os pixels da conta de anúncios: nome, quando receberam o último evento e se estão disponíveis. Use o pixel_id em save_ad_conversion_settings, create_ad e save_ad_draft.",
		adAccountArgs{})
}

func (t *listAdPixelsTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a adAccountArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	account, err := t.ads.account(ctx, cc, a.AdAccountID)
	if err != nil {
		return growthFailure("list_ad_pixels", "", err)
	}
	pixels, err := t.deps.Pixels.Pixels(ctx, cc.WorkspaceID, account.ID)
	if err != nil {
		return growthFailure("list_ad_pixels", account.ID, err)
	}
	out := make([]map[string]interface{}, 0, len(pixels))
	for _, p := range pixels {
		row := map[string]interface{}{"pixel_id": p.MetaID, "name": p.Name, "available": !p.Unavailable}
		if p.LastFiredTime != nil {
			row["last_event_at"] = p.LastFiredTime.Format("2006-01-02 15:04")
		}
		out = append(out, row)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"pixels": out}}
}

type createAdPixelArgs struct {
	AdAccountID string `json:"ad_account_id" req:"true" desc:"ad_account_id de list_ad_accounts (também aceita o id da Meta ou o nome exato da conta)"`
	Name        string `json:"name" req:"true" desc:"nome do pixel, até 100 caracteres"`
}

type createAdPixelTool struct{ adGrowth }

func (t *createAdPixelTool) Meta() copilot.Meta { return adsMeta(workspace.ActionCreate, true) }

func (t *createAdPixelTool) Definition() tools.Definition {
	return definition("create_ad_pixel",
		"Cria um pixel na conta de anúncios da Meta. O código do pixel ainda precisa ser instalado no site para medir visitas e compras. "+
			"Só depois da aprovação do usuário.",
		createAdPixelArgs{})
}

func (t *createAdPixelTool) plan(ctx context.Context, cc copilot.Context, args map[string]interface{}) (*advertising.AdAccount, string, error) {
	a, err := validateArgs[createAdPixelArgs](nil, cc, args)
	if err != nil {
		return nil, "", err
	}
	account, err := t.ads.account(ctx, cc, a.AdAccountID)
	if err != nil {
		return nil, "", err
	}
	return account, strings.TrimSpace(a.Name), nil
}

func (t *createAdPixelTool) Validate(ctx context.Context, cc copilot.Context, args map[string]interface{}) error {
	_, _, err := t.plan(ctx, cc, args)
	return adsValidation("create_ad_pixel", err)
}

func (t *createAdPixelTool) Describe(ctx context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	account, name, err := t.plan(ctx, cc, args)
	if err != nil {
		return []copilot.Field{{Key: "pixel", Value: "pixel com campos a corrigir"}}
	}
	return []copilot.Field{{Key: "account", Value: account.Name}, {Key: "pixel", Value: name}}
}

func (t *createAdPixelTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	account, name, err := t.plan(ctx, cc, args)
	if err != nil {
		return growthFailure("create_ad_pixel", "", err)
	}
	pixel, err := t.deps.Pixels.CreatePixel(ctx, cc.WorkspaceID, account.ID, name)
	if err != nil {
		return growthFailure("create_ad_pixel", account.ID, err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"pixel_id": pixel.MetaID, "name": pixel.Name}}
}

type recentAdConversionsTool struct{ adGrowth }

func (t *recentAdConversionsTool) Meta() copilot.Meta { return adsMeta(workspace.ActionRead, false) }

func (t *recentAdConversionsTool) Definition() tools.Definition {
	return definition("recent_ad_conversions",
		"Mostra as últimas conversões que o Vozko tentou enviar à Meta: o evento, se foi enviado, pulado ou falhou e o motivo. "+
			"Motivos comuns: not_enabled (envio desligado), event_off (evento desligado), no_ad_identity (o contato não veio de anúncio), "+
			"too_old (mais de 7 dias), no_dataset (sem conjunto de dados nem pixel), value_missing (venda sem valor).",
		struct{}{})
}

func (t *recentAdConversionsTool) Execute(ctx context.Context, cc copilot.Context, _ map[string]interface{}) copilot.Result {
	records, err := t.deps.Conversions.Recent(ctx, cc.WorkspaceID)
	if err != nil {
		return growthFailure("recent_ad_conversions", "", err)
	}
	totals := map[string]int{}
	out := make([]map[string]interface{}, 0, len(records))
	for _, r := range records {
		totals[string(r.Status)]++
		row := map[string]interface{}{"opportunity_id": r.OpportunityID, "event": r.EventName, "status": string(r.Status), "updated_at": r.UpdatedAt.Format("2006-01-02 15:04")}
		if r.Reason != "" {
			row["reason"] = r.Reason
		}
		out = append(out, row)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"totals": totals, "conversions": out}}
}
