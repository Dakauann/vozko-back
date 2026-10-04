package copilottools

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"time"

	"vozko/domain/advertising"
	"vozko/domain/copilot"
	"vozko/domain/tools"
	"vozko/domain/workspace"
	adsuc "vozko/usecases/advertising"
)

const (
	PreviewAdCreative = "ad_creative"
	maxAdRowsShown    = 30
)

type AdAccountLister interface {
	List(ctx context.Context, workspaceID string) ([]*advertising.AdAccount, error)
}

type AdReporter interface {
	Report(ctx context.Context, q adsuc.ReportQuery) (*adsuc.Report, error)
}

type AdManager interface {
	CheckStatus(ctx context.Context, workspaceID, metaID string, on bool) (*advertising.Object, error)
	SetStatus(ctx context.Context, workspaceID, metaID string, on bool) (*advertising.Object, error)
	CheckBudget(ctx context.Context, workspaceID, metaID string, amount int64) (*advertising.Object, *advertising.AdAccount, error)
	SetBudget(ctx context.Context, workspaceID, metaID string, amount int64) (*advertising.Object, error)
	CheckCopy(ctx context.Context, workspaceID, metaID string, req advertising.CopyRequest) (*advertising.Object, error)
	Copy(ctx context.Context, workspaceID, metaID string, req advertising.CopyRequest) (string, error)
	CheckLifecycle(ctx context.Context, workspaceID, metaID string, action advertising.Lifecycle) (*advertising.Object, error)
	Lifecycle(ctx context.Context, workspaceID, metaID string, action advertising.Lifecycle) (*advertising.Object, error)
}

type AdAssets interface {
	Pages(ctx context.Context, workspaceID, accountID string) ([]adsuc.PromotablePage, error)
	Locations(ctx context.Context, workspaceID, accountID, query string) ([]advertising.RemoteLocation, error)
	NameLocations(ctx context.Context, workspaceID, accountID string, locations []advertising.GeoLocation) ([]advertising.GeoLocation, error)
}

type AdPublisher interface {
	Check(ctx context.Context, workspaceID string, draft advertising.AdDraft) (*adsuc.Preflight, error)
	Preflight(ctx context.Context, workspaceID string, draft advertising.AdDraft) (*adsuc.Preflight, error)
	Publish(ctx context.Context, in adsuc.PublishInput) (*advertising.PublishJob, error)
}

type AdsDeps struct {
	Accounts  AdAccountLister
	Reports   AdReporter
	Manage    AdManager
	Assets    AdAssets
	Publish   AdPublisher
	Live      AdLive
	Drafts    AdDrafts
	Readiness AdReadiness
	Targeting AdTargeting
	Forms     AdForms
	Editor    AdEditor
	Bulk      AdBulk
	Sources   AdCreativeSources
}

func adsMeta(action workspace.Action, mutating bool) copilot.Meta {
	return copilot.Meta{Mutating: mutating, Resource: workspace.ResourceAds, Action: action}
}

func (d AdsDeps) account(ctx context.Context, cc copilot.Context, ref string) (*advertising.AdAccount, error) {
	accounts, err := d.Accounts.List(ctx, cc.WorkspaceID)
	if err != nil {
		return nil, err
	}
	account, err := advertising.ResolveAccount(accounts, ref)
	if errors.Is(err, advertising.ErrAccountNotFound) || errors.Is(err, advertising.ErrAmbiguousAccount) {
		return nil, fmt.Errorf("%w: ad_account_id %q não identifica uma conta; contas conectadas: %s", errInvalidArgs, ref, accountChoices(accounts))
	}
	return account, err
}

func (d AdsDeps) namedDraft(ctx context.Context, cc copilot.Context, draft advertising.AdDraft) (advertising.AdDraft, error) {
	named, err := d.Assets.NameLocations(ctx, cc.WorkspaceID, draft.AdAccountID, draft.AdSet.Targeting.Locations)
	if err != nil {
		return draft, err
	}
	draft.AdSet.Targeting.Locations = named
	return draft, nil
}

func accountChoices(accounts []*advertising.AdAccount) string {
	if len(accounts) == 0 {
		return "nenhuma; conecte uma conta de anúncios primeiro"
	}
	choices := make([]string, 0, len(accounts))
	for _, a := range accounts {
		choices = append(choices, fmt.Sprintf("%s (ad_account_id %s, %s)", a.Name, a.ID, a.Currency))
	}
	return strings.Join(choices, "; ")
}

func metaID(raw string) (string, error) {
	id := strings.TrimSpace(raw)
	if _, err := strconv.ParseUint(id, 10, 64); err != nil {
		return "", fmt.Errorf("%w: meta_id %q não existe; use o meta_id exato de ads_results", errInvalidArgs, raw)
	}
	return id, nil
}

func moneyText(currency string, micros int64) string {
	return currency + " " + strings.Replace(strconv.FormatFloat(advertising.MicrosToAmount(micros), 'f', 2, 64), ".", ",", 1)
}

func optionalAmount(micros *int64) interface{} {
	if micros == nil {
		return nil
	}
	return advertising.MicrosToAmount(*micros)
}

func budgetText(currency string, b *advertising.Budget) string {
	if b == nil {
		return "sem orçamento próprio"
	}
	micros, err := advertising.MinorToMicros(currency, b.Amount)
	if err != nil {
		return "orçamento desconhecido"
	}
	if b.Kind == advertising.BudgetLifetime {
		return moneyText(currency, micros) + " no total"
	}
	return moneyText(currency, micros) + " por dia"
}

func metaIDs(raw []string) ([]string, error) {
	ids := make([]string, 0, len(raw))
	for _, item := range raw {
		id, err := metaID(item)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func adsFailure(tool string, err error) copilot.Result {
	if message := adsMessage(err); message != "" {
		return copilot.Result{Status: copilot.StatusError, Message: message}
	}
	log.Printf("[copilot] %s failed: %v", tool, err)
	return copilot.Result{Status: copilot.StatusError, Message: "falha ao falar com a Meta; tente de novo em instantes"}
}

func adsValidation(tool string, err error) error {
	if err == nil || errors.Is(err, errInvalidArgs) {
		return err
	}
	return fmt.Errorf("%w: %s", errInvalidArgs, adsFailure(tool, err).Message)
}

func adsMessage(err error) string {
	var invalid *advertising.ValidationError
	switch {
	case errors.Is(err, errInvalidArgs):
		return err.Error()
	case errors.As(err, &invalid):
		return "campos a corrigir: " + issuesText(invalid)
	case errors.Is(err, advertising.ErrNoFundingSource), errors.Is(err, advertising.ErrAccountNotActive), errors.Is(err, advertising.ErrAccountReadOnly):
		return "a conta de anúncios ainda não pode publicar (" + err.Error() + "); ofereça save_ad_draft para deixar o anúncio pronto e chame ad_account_readiness para mandar o link exato da Meta que resolve"
	case errors.Is(err, advertising.ErrDraftPublishing):
		return "esse rascunho já está sendo publicado"
	case errors.Is(err, advertising.ErrDraftChanged):
		return "o rascunho mudou depois da leitura; leia de novo com get_ad_draft e use a version nova"
	case errors.Is(err, advertising.ErrDraftNotFound):
		return "rascunho não encontrado; use o draft_id exato de list_ad_drafts"
	case errors.Is(err, advertising.ErrAccountNeedsReconnect), errors.Is(err, advertising.ErrMissingScopes), advertising.Classify(err) == advertising.FailureReauth:
		return "a conta de anúncios precisa ser reconectada na tela Anúncios"
	case errors.Is(err, advertising.ErrBudgetChangeTooSoon):
		return "a Meta só permite 4 mudanças de orçamento por hora neste item; tente mais tarde"
	case errors.Is(err, advertising.ErrObjectNotFound), errors.Is(err, advertising.ErrAccountNotFound):
		return "item não encontrado; use os ids exatos de list_ad_accounts e ads_results"
	case errors.Is(err, advertising.ErrAccountAdminRequired):
		return "só quem é administrador da conta de anúncios na Meta pode mudar o limite de gasto"
	case errors.Is(err, advertising.ErrNothingToChange):
		return "nada muda com esse pedido; o item já está assim"
	case errors.Is(err, advertising.ErrEditNotForLevel):
		return "essa mudança não vale para esse nível; textos mudam só em anúncios"
	case errors.Is(err, advertising.ErrObjectLocked):
		return "itens arquivados ou excluídos não mudam mais"
	case errors.Is(err, advertising.ErrNoBudget):
		return "esse item não tem orçamento próprio; o orçamento fica na campanha ou nos conjuntos"
	case errors.Is(err, advertising.ErrReportNotFound):
		return "relatório não encontrado; use o report_id exato de list_ad_reports"
	case errors.Is(err, advertising.ErrInvalidRange):
		return "período inválido; use YYYY-MM-DD em since e until, com no máximo 37 meses"
	case errors.Is(err, advertising.ErrReportExportTooLarge):
		return "a exportação passou de 10 MB; diminua o período ou os detalhamentos"
	case errors.Is(err, advertising.ErrAudienceTermsNotAccepted):
		return "a conta de anúncios ainda não aceitou os termos de públicos personalizados da Meta, e só o usuário pode aceitar, na Meta; chame ad_account_readiness para mostrar o cartão que leva até lá"
	case errors.Is(err, advertising.ErrAudienceNotFound):
		return "público não encontrado; use o audience_id exato de list_ad_audiences"
	case errors.Is(err, advertising.ErrSavedAudienceNotFound):
		return "público salvo não encontrado; use o saved_audience_id exato de list_ad_audiences"
	case errors.Is(err, advertising.ErrNoCustomersMatched):
		return "nenhum contato do filtro tem telefone que a Meta consiga reconhecer"
	case errors.Is(err, advertising.ErrRuleNotFound):
		return "regra não encontrada; use o rule_id exato de list_ad_rules"
	case errors.Is(err, advertising.ErrBusinessPhoneNotFound):
		return "número oficial não encontrado; use o business_phone_id exato de list_business_phones"
	case advertising.Classify(err) == advertising.FailureRejected:
		return "a Meta recusou: " + advertising.Explain(err)
	}
	return ""
}

func issuesText(invalid *advertising.ValidationError) string {
	parts := make([]string, 0, len(invalid.Issues))
	for _, issue := range invalid.Issues {
		if issue.Code == advertising.CodeUnknownLocation {
			parts = append(parts, "locations (a Meta não conhece esse local; use o campo location exato de search_ad_locations, nunca monte um)")
			continue
		}
		parts = append(parts, issue.Field+" ("+issue.Code+")")
	}
	return strings.Join(parts, ", ")
}

type listAdAccountsTool struct{ deps AdsDeps }

func NewListAdAccountsTool(deps AdsDeps) copilot.Tool { return &listAdAccountsTool{deps: deps} }

func (t *listAdAccountsTool) Meta() copilot.Meta { return adsMeta(workspace.ActionRead, false) }

func (t *listAdAccountsTool) Definition() tools.Definition {
	return definition("list_ad_accounts",
		"Lista as contas de anúncios da Meta conectadas: moeda, fuso, se podem gastar (meio de pagamento e status) e a última sincronização.",
		struct{}{})
}

func (t *listAdAccountsTool) Execute(ctx context.Context, cc copilot.Context, _ map[string]interface{}) copilot.Result {
	accounts, err := t.deps.Accounts.List(ctx, cc.WorkspaceID)
	if err != nil {
		return adsFailure("list_ad_accounts", err)
	}
	out := make([]map[string]interface{}, 0, len(accounts))
	for _, a := range accounts {
		row := map[string]interface{}{
			"ad_account_id": a.ID, "name": a.Name, "currency": a.Currency, "timezone": a.Timezone,
			"meta_status": a.MetaStatus.Key(), "connection": string(a.Connection), "can_spend": a.CanSpend() == nil,
		}
		if err := a.CanSpend(); err != nil {
			row["why_cannot_spend"] = err.Error()
		}
		if a.LastSyncedAt != nil {
			row["last_synced_at"] = a.LastSyncedAt.Format("2006-01-02 15:04")
		}
		out = append(out, row)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"accounts": out}}
}

type adsResultsArgs struct {
	AdAccountID string   `json:"ad_account_id" req:"true" desc:"ad_account_id de list_ad_accounts (também aceita o id da Meta ou o nome exato da conta)"`
	Level       string   `json:"level" enum:"campaign,adset,ad" desc:"campaign (padrão), adset ou ad"`
	Since       string   `json:"since" desc:"primeiro dia YYYY-MM-DD; sem período, os últimos 30 dias"`
	Until       string   `json:"until" desc:"último dia YYYY-MM-DD"`
	CampaignIDs []string `json:"campaign_ids" desc:"meta_id de campanhas para filtrar"`
}

type adsResultsTool struct{ deps AdsDeps }

func NewAdsResultsTool(deps AdsDeps) copilot.Tool { return &adsResultsTool{deps: deps} }

func (t *adsResultsTool) Meta() copilot.Meta { return adsMeta(workspace.ActionRead, false) }

func (t *adsResultsTool) Definition() tools.Definition {
	return definition("ads_results",
		"Resultados das campanhas, conjuntos ou anúncios de uma conta: gasto, resultados e custo por resultado (da Meta), "+
			"conversas, leads, vendas, receita e ROAS (do CRM), status de veiculação, orçamento e problemas de revisão. "+
			"Valores em unidades da moeda da conta. Traz o meta_id de cada item para ligar, desligar, mudar orçamento, duplicar, arquivar ou excluir.",
		adsResultsArgs{})
}

func (t *adsResultsTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a adsResultsArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	account, err := t.deps.account(ctx, cc, a.AdAccountID)
	if err != nil {
		return adsFailure("ads_results", err)
	}
	dates, err := adsRange(a.Since, a.Until)
	if err != nil {
		return adsFailure("ads_results", err)
	}
	q := adsuc.ReportQuery{WorkspaceID: cc.WorkspaceID, AccountID: account.ID, Level: advertising.Level(a.Level), CampaignIDs: a.CampaignIDs, Range: dates}
	report, err := t.deps.Reports.Report(ctx, q)
	if err != nil {
		return adsFailure("ads_results", err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: reportData(report)}
}

func reportData(r *adsuc.Report) map[string]interface{} {
	rows := append([]adsuc.ReportRow(nil), r.Rows...)
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Metrics.SpendMicros > rows[j].Metrics.SpendMicros })
	shown := rows
	if len(shown) > maxAdRowsShown {
		shown = shown[:maxAdRowsShown]
	}
	currency := r.Account.Currency
	items := make([]map[string]interface{}, 0, len(shown))
	for _, row := range shown {
		o := row.Object
		item := map[string]interface{}{
			"meta_id": o.MetaID, "name": o.Name, "level": string(o.Level), "on": o.IsOn(),
			"delivery": string(o.Delivery(r.Range.Until)), "spend": advertising.MicrosToAmount(row.Metrics.SpendMicros),
			"results": row.Metrics.Results, "result_type": row.Metrics.ResultAction,
			"cost_per_result":   optionalAmount(row.Metrics.CostPerResult()),
			"crm_conversations": row.Outcome.Conversations, "crm_leads": row.Outcome.Leads, "won_deals": row.Outcome.WonDeals,
			"cost_per_lead": optionalAmount(row.Outcome.CostPerLead), "roas": row.Outcome.ROAS,
		}
		if b := o.Budget(); b != nil {
			item["budget"] = budgetText(currency, b)
		}
		if len(o.Issues) > 0 {
			item["issues"] = o.Issues
		}
		if o.Level == advertising.LevelAd && o.Creative != nil {
			item["headline"], item["primary_text"] = o.Creative.Title, o.Creative.Body
		}
		items = append(items, item)
	}
	return map[string]interface{}{
		"account": r.Account.Name, "currency": currency,
		"since": r.Range.Since.Format(advertising.DayLayout), "until": r.Range.Until.Format(advertising.DayLayout),
		"total_spend": advertising.MicrosToAmount(r.Totals.SpendMicros), "total_results": r.Totals.Results,
		"total_crm_conversations": r.Outcome.Conversations, "total_cost_per_lead": optionalAmount(r.Outcome.CostPerLead),
		"total_roas": r.Outcome.ROAS, "rows": items, "rows_total": len(r.Rows),
	}
}

type adAccountArgs struct {
	AdAccountID string `json:"ad_account_id" req:"true" desc:"ad_account_id de list_ad_accounts (também aceita o id da Meta ou o nome exato da conta)"`
}

type listAdPagesTool struct{ deps AdsDeps }

func NewListAdPagesTool(deps AdsDeps) copilot.Tool { return &listAdPagesTool{deps: deps} }

func (t *listAdPagesTool) Meta() copilot.Meta { return adsMeta(workspace.ActionCreate, false) }

func (t *listAdPagesTool) Definition() tools.Definition {
	return definition("list_ad_pages",
		"Lista as páginas do Facebook que podem anunciar por uma conta, o Instagram de cada uma, os números de WhatsApp do workspace "+
			"vinculados a cada página e se a página aceitou os termos de cadastros da Meta (lead_terms_accepted, exigido para formulários). "+
			"Um anúncio para WhatsApp só pode usar um desses números.",
		adAccountArgs{})
}

func (t *listAdPagesTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a adAccountArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	account, err := t.deps.account(ctx, cc, a.AdAccountID)
	if err != nil {
		return adsFailure("list_ad_pages", err)
	}
	pages, err := t.deps.Assets.Pages(ctx, cc.WorkspaceID, account.ID)
	if err != nil {
		return adsFailure("list_ad_pages", err)
	}
	out := make([]map[string]interface{}, 0, len(pages))
	for _, p := range pages {
		numbers := make([]map[string]string, 0, len(p.Numbers))
		for _, n := range p.Numbers {
			numbers = append(numbers, map[string]string{"whatsapp_number": n.Number, "label": n.Label, "kind": string(n.Kind)})
		}
		out = append(out, map[string]interface{}{
			"page_id": p.Page.PageID, "name": p.Page.Name, "can_advertise": p.Page.CanAdvertise, "lead_terms_accepted": p.Page.LeadTermsAccepted,
			"instagram_user_id": p.Page.InstagramUserID, "instagram_username": p.Page.InstagramUsername,
			"whatsapp_numbers": numbers,
		})
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"pages": out}}
}

type searchAdLocationsArgs struct {
	AdAccountID string `json:"ad_account_id" req:"true" desc:"ad_account_id de list_ad_accounts (também aceita o id da Meta ou o nome exato da conta)"`
	Query       string `json:"query" req:"true" desc:"nome do país, estado ou cidade"`
}

type searchAdLocationsTool struct{ deps AdsDeps }

func NewSearchAdLocationsTool(deps AdsDeps) copilot.Tool { return &searchAdLocationsTool{deps: deps} }

func (t *searchAdLocationsTool) Meta() copilot.Meta { return adsMeta(workspace.ActionCreate, false) }

func (t *searchAdLocationsTool) Definition() tools.Definition {
	return definition("search_ad_locations",
		"Busca países, estados e cidades para o público de um anúncio. Use o campo location devolvido em create_ad.",
		searchAdLocationsArgs{})
}

func (t *searchAdLocationsTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a searchAdLocationsArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	account, err := t.deps.account(ctx, cc, a.AdAccountID)
	if err != nil {
		return adsFailure("search_ad_locations", err)
	}
	found, err := t.deps.Assets.Locations(ctx, cc.WorkspaceID, account.ID, a.Query)
	if err != nil {
		return adsFailure("search_ad_locations", err)
	}
	out := make([]map[string]string, 0, len(found))
	for _, l := range found {
		out = append(out, map[string]string{"location": string(l.Kind) + ":" + l.Key, "name": l.Name, "region": l.Region, "country": l.Country})
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"locations": out}}
}

type adDraftArgs struct {
	AdAccountID             string       `json:"ad_account_id" req:"true" desc:"ad_account_id de list_ad_accounts (também aceita o id da Meta ou o nome exato da conta)"`
	CampaignName            string       `json:"campaign_name" req:"true" desc:"nome da campanha"`
	Objective               string       `json:"objective" req:"true" enum:"OUTCOME_AWARENESS,OUTCOME_TRAFFIC,OUTCOME_ENGAGEMENT,OUTCOME_LEADS,OUTCOME_SALES,OUTCOME_APP_PROMOTION" desc:"objetivo da campanha: OUTCOME_AWARENESS (reconhecimento), OUTCOME_TRAFFIC (tráfego para site ou conversa), OUTCOME_ENGAGEMENT (conversas e engajamento com uma publicação), OUTCOME_LEADS (cadastros), OUTCOME_SALES (vendas, inclusive pelo catálogo), OUTCOME_APP_PROMOTION (instalações de app)"`
	Destination             string       `json:"destination" req:"true" enum:"WHATSAPP,MESSENGER,INSTAGRAM_DIRECT,WEBSITE,ON_AD,NONE,APP,CATALOG,ON_POST" desc:"para onde a pessoa vai: WHATSAPP, MESSENGER, INSTAGRAM_DIRECT, WEBSITE (site em link), ON_AD (formulário instantâneo, só com OUTCOME_LEADS), NONE (só alcance, com OUTCOME_AWARENESS), APP (loja do app, com OUTCOME_APP_PROMOTION), CATALOG (produtos do catálogo, com OUTCOME_SALES) ou ON_POST (engajamento com uma publicação, com OUTCOME_ENGAGEMENT e format EXISTING_POST)"`
	Goal                    string       `json:"goal" desc:"meta de desempenho; vazio usa a recomendada para o objetivo e o destino"`
	PageID                  string       `json:"page_id" req:"true" desc:"page_id exato de list_ad_pages"`
	WhatsAppNumber          string       `json:"whatsapp_number" desc:"whatsapp_number exato de list_ad_pages (obrigatório para WHATSAPP)"`
	InstagramUserID         string       `json:"instagram_user_id" desc:"instagram_user_id da página (obrigatório para INSTAGRAM_DIRECT e para publicações do Instagram)"`
	AppID                   string       `json:"app_id" desc:"app_id exato de list_ad_apps (obrigatório para APP)"`
	AppStoreURL             string       `json:"app_store_url" desc:"um dos store_urls do app em list_ad_apps (obrigatório para APP)"`
	CatalogID               string       `json:"catalog_id" desc:"catalog_id exato de list_ad_catalogs (obrigatório para CATALOG)"`
	ProductSetID            string       `json:"product_set_id" desc:"product_set_id exato do mesmo catálogo em list_ad_catalogs (obrigatório para CATALOG)"`
	Link                    string       `json:"link" desc:"endereço https do site (obrigatório para WEBSITE e CATALOG), ex.: https://vozkoia.com"`
	DisplayLink             string       `json:"display_link" desc:"link curto mostrado no anúncio, opcional, ex.: vozkoia.com"`
	CallToAction            string       `json:"call_to_action" desc:"botão para WEBSITE, ON_AD, APP ou CATALOG, ex.: LEARN_MORE, SIGN_UP, CONTACT_US, SHOP_NOW, BOOK_NOW, INSTALL_MOBILE_APP; vazio usa o padrão"`
	LeadFormID              string       `json:"lead_form_id" desc:"lead_form_id de list_lead_forms (obrigatório para ON_AD)"`
	PixelID                 string       `json:"pixel_id" desc:"pixel da conta, só quando a meta for conversões no site"`
	PixelEvent              string       `json:"pixel_event" desc:"evento do pixel, ex.: LEAD, PURCHASE, COMPLETE_REGISTRATION"`
	Budget                  float64      `json:"budget" desc:"valor do orçamento na moeda da conta, diário ou total conforme budget_kind; 30 significa 30 reais em uma conta BRL"`
	DailyBudget             float64      `json:"daily_budget" desc:"o mesmo que budget com budget_kind daily; use um dos dois"`
	BudgetKind              string       `json:"budget_kind" enum:"daily,lifetime" desc:"daily (padrão) gasta até o valor por dia; lifetime gasta o valor no total e exige end_date"`
	BudgetLevel             string       `json:"budget_level" enum:"adset,campaign" desc:"adset (padrão) põe o orçamento no conjunto; campaign usa o orçamento Advantage da campanha, que a Meta distribui sozinha"`
	BidStrategy             string       `json:"bid_strategy" enum:"LOWEST_COST_WITHOUT_CAP,LOWEST_COST_WITH_BID_CAP,COST_CAP,LOWEST_COST_WITH_MIN_ROAS" desc:"estratégia de lance; vazio usa o menor custo (recomendado para iniciantes)"`
	BidAmount               float64      `json:"bid_amount" desc:"valor do lance ou do custo máximo na moeda da conta, para LOWEST_COST_WITH_BID_CAP e COST_CAP"`
	ROAS                    float64      `json:"roas" desc:"retorno mínimo sobre o gasto para LOWEST_COST_WITH_MIN_ROAS, ex.: 2 significa 2 vezes o gasto (só com a meta VALUE)"`
	StartDate               string       `json:"start_date" desc:"primeiro dia de veiculação YYYY-MM-DD no fuso da conta; vazio começa ao publicar"`
	EndDate                 string       `json:"end_date" desc:"último dia de veiculação YYYY-MM-DD no fuso da conta; vazio roda até ser desligado"`
	Locations               []string     `json:"locations" req:"true" desc:"location de search_ad_locations, ex.: country:BR"`
	AgeMin                  int          `json:"age_min" desc:"idade mínima, 13 a 65 (padrão 18)"`
	AgeMax                  int          `json:"age_max" desc:"idade máxima, 13 a 65, 65 significa 65+ (padrão 65)"`
	Genders                 []string     `json:"genders" desc:"male e ou female; vazio para todos"`
	Interests               []string     `json:"interests" desc:"interest de search_ad_interests; vazio deixa a Meta encontrar o público"`
	CustomAudiences         []string     `json:"custom_audiences" desc:"ids exatos de públicos personalizados ou semelhantes da conta para incluir"`
	ExcludedCustomAudiences []string     `json:"excluded_custom_audiences" desc:"ids exatos de públicos personalizados da conta para excluir"`
	Placements              []string     `json:"placements" desc:"vazio ou automatic deixa a Meta escolher (recomendado); ou uma lista entre facebook, instagram, messenger e audience_network"`
	SpecialCategory         string       `json:"special_category" enum:"NONE,HOUSING,EMPLOYMENT,FINANCIAL_PRODUCTS_SERVICES" desc:"categoria especial obrigatória para imóveis, vagas de emprego e crédito; NONE nos demais"`
	Format                  string       `json:"format" req:"true" enum:"IMAGE,VIDEO,CAROUSEL,FLEXIBLE,EXISTING_POST,CATALOG" desc:"IMAGE (uma imagem), VIDEO (um vídeo), CAROUSEL (2 a 10 cartões em cards), FLEXIBLE (várias mídias e variações de texto que a Meta combina), EXISTING_POST (uma publicação da página, de list_page_posts) ou CATALOG (produtos do catálogo, com CATALOG)"`
	PrimaryText             string       `json:"primary_text" desc:"texto principal do anúncio (obrigatório, exceto em FLEXIBLE e EXISTING_POST)"`
	Headline                string       `json:"headline" desc:"título curto (até 40 caracteres é o ideal)"`
	Description             string       `json:"description" desc:"descrição curta opcional"`
	MediaID                 string       `json:"media_id" id:"true" desc:"media_id de generate_image ou de uma imagem ou vídeo anexado, do mesmo tipo de format (IMAGE e VIDEO)"`
	Cards                   []adCardArgs `json:"cards" desc:"cartões do CAROUSEL, de 2 a 10, na ordem"`
	MediaIDs                []string     `json:"media_ids" id:"true" desc:"imagens do FLEXIBLE: media_id de generate_image ou de imagens anexadas"`
	VideoIDs                []string     `json:"video_ids" id:"true" desc:"vídeos do FLEXIBLE: media_id de vídeos anexados"`
	Texts                   []string     `json:"texts" desc:"FLEXIBLE: de 1 a 5 variações do texto principal"`
	Headlines               []string     `json:"headlines" desc:"FLEXIBLE: até 5 variações do título"`
	Descriptions            []string     `json:"descriptions" desc:"FLEXIBLE: até 5 variações da descrição"`
	PostID                  string       `json:"post_id" desc:"post_id exato de list_page_posts (EXISTING_POST)"`
	PostPlatform            string       `json:"post_platform" enum:"facebook,instagram" desc:"de onde vem a publicação de EXISTING_POST: facebook (padrão) ou instagram"`
	Greeting                string       `json:"greeting" desc:"mensagem que já vem escrita para o cliente enviar (WhatsApp, Messenger, Instagram)"`
	IceBreakers             []string     `json:"ice_breakers" desc:"até 3 perguntas prontas para o cliente tocar (WhatsApp, Messenger, Instagram)"`
	KeepPaused              bool         `json:"keep_paused" desc:"true publica desligado, para o usuário ligar depois"`
}

func (a adDraftArgs) route() (advertising.Objective, advertising.Destination, advertising.OptimizationGoal, error) {
	objective, destination := advertising.Objective(a.Objective), advertising.Destination(a.Destination)
	goal := advertising.OptimizationGoal(strings.TrimSpace(a.Goal))
	if goal == "" {
		recommended, ok := objective.DefaultGoal(destination)
		if !ok {
			return "", "", "", fmt.Errorf("%w: o objetivo %s não leva para %s", errInvalidArgs, a.Objective, a.Destination)
		}
		goal = recommended
	}
	if !objective.Allows(destination, goal) {
		return "", "", "", fmt.Errorf("%w: o objetivo %s com destino %s não aceita a meta %s", errInvalidArgs, a.Objective, a.Destination, goal)
	}
	return objective, destination, goal, nil
}

func parseTargetRefs(raw []string) ([]advertising.TargetRef, error) {
	out := make([]advertising.TargetRef, 0, len(raw))
	for _, item := range raw {
		id, name, ok := strings.Cut(strings.TrimSpace(item), ":")
		if !ok || id == "" {
			return nil, fmt.Errorf("%w: interest %q inválido; use o campo interest de search_ad_interests", errInvalidArgs, item)
		}
		out = append(out, advertising.TargetRef{ID: id, Name: name})
	}
	return out, nil
}

func parseLocations(raw []string) ([]advertising.GeoLocation, error) {
	out := make([]advertising.GeoLocation, 0, len(raw))
	for _, item := range raw {
		kind, key, ok := strings.Cut(strings.TrimSpace(item), ":")
		if !ok || key == "" {
			return nil, fmt.Errorf("%w: location %q inválida; use o campo location de search_ad_locations", errInvalidArgs, item)
		}
		out = append(out, advertising.GeoLocation{Kind: advertising.LocationKind(kind), Key: key})
	}
	return out, nil
}

func genders(raw []string) []int {
	var out []int
	for _, g := range raw {
		switch strings.ToLower(strings.TrimSpace(g)) {
		case "male":
			out = append(out, advertising.GenderMale)
		case "female":
			out = append(out, advertising.GenderFemale)
		default:
			out = append(out, 0)
		}
	}
	return out
}

type createAdTool struct{ deps AdsDeps }

func NewCreateAdTool(deps AdsDeps) copilot.Tool { return &createAdTool{deps: deps} }

func (t *createAdTool) Meta() copilot.Meta { return adsMeta(workspace.ActionCreate, true) }

func (t *createAdTool) Definition() tools.Definition {
	return adDraftDefinition("create_ad",
		"Cria e publica um anúncio na Meta (campanha, conjunto e anúncio) com qualquer destino: WhatsApp, Messenger, Instagram Direct, um site, "+
			"um formulário instantâneo, só alcance, a loja de um app (list_ad_apps), os produtos de um catálogo (list_ad_catalogs) ou engajamento com "+
			"uma publicação. Formatos: imagem, vídeo, carrossel, flexível, publicação existente (list_page_posts) e catálogo. Orçamento diário ou total, "+
			"no conjunto ou na campanha, posicionamentos automáticos ou escolhidos, lance e públicos personalizados são opcionais. Tudo é criado desligado e só é ligado no fim, a menos que keep_paused seja true; depois a Meta "+
			"revisa o anúncio antes de veicular. Exige a conta pronta para gastar: se não estiver, use save_ad_draft. "+
			"Cobra a taxa por anúncio publicado do saldo; o gasto com a Meta sai da conta de anúncios. Só depois da aprovação do usuário.",
		adDraftArgs{})
}

func (t *createAdTool) preflight(ctx context.Context, cc copilot.Context, args map[string]interface{}) (*adsuc.Preflight, error) {
	a, err := validateArgs[adDraftArgs](nil, cc, args)
	if err != nil {
		return nil, err
	}
	account, err := t.deps.account(ctx, cc, a.AdAccountID)
	if err != nil {
		return nil, err
	}
	draft, err := a.draft(account)
	if err != nil {
		return nil, err
	}
	if draft, err = t.deps.namedDraft(ctx, cc, draft); err != nil {
		return nil, err
	}
	return t.deps.Publish.Preflight(ctx, cc.WorkspaceID, draft)
}

func (t *createAdTool) Validate(ctx context.Context, cc copilot.Context, args map[string]interface{}) error {
	_, err := t.preflight(ctx, cc, args)
	return adsValidation("create_ad", err)
}

func (t *createAdTool) Describe(ctx context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	pre, err := t.preflight(ctx, cc, args)
	if err != nil {
		return []copilot.Field{{Key: "ad", Value: "anúncio com campos a corrigir"}}
	}
	return draftFields(pre)
}

var objectiveNames = map[advertising.Objective]string{
	advertising.ObjectiveAwareness:    "Reconhecimento",
	advertising.ObjectiveTraffic:      "Tráfego",
	advertising.ObjectiveEngagement:   "Engajamento",
	advertising.ObjectiveLeads:        "Leads",
	advertising.ObjectiveSales:        "Vendas",
	advertising.ObjectiveAppPromotion: "Promoção do app",
}

func destinationText(pre *adsuc.Preflight) string {
	d := pre.Draft
	switch d.AdSet.Destination {
	case advertising.DestinationWhatsApp:
		return "WhatsApp " + d.AdSet.WhatsAppNumber
	case advertising.DestinationMessenger:
		return "Messenger da página"
	case advertising.DestinationInstagramDirect:
		return "Instagram Direct @" + pre.Page.InstagramUsername
	case advertising.DestinationWebsite:
		return "Site " + d.Ads[0].Creative.Link
	case advertising.DestinationInstantForm:
		return "Formulário instantâneo"
	case advertising.DestinationApp:
		return "App na loja " + d.AdSet.AppStoreURL
	case advertising.DestinationCatalog:
		return "Produtos do catálogo " + d.AdSet.CatalogID + ", conjunto " + d.AdSet.ProductSetID
	case advertising.DestinationOnPost:
		return "Engajamento com a publicação"
	}
	return "Mostrar para o maior número de pessoas"
}

var formatNames = map[advertising.CreativeFormat]string{
	advertising.FormatImage:        "Imagem",
	advertising.FormatVideo:        "Vídeo",
	advertising.FormatCarousel:     "Carrossel",
	advertising.FormatFlexible:     "Flexível",
	advertising.FormatExistingPost: "Publicação existente",
	advertising.FormatCatalog:      "Catálogo de produtos",
	advertising.FormatCollection:   "Coleção",
}

func formatText(d advertising.AdDraft) string {
	if len(d.Ads) != 1 {
		return strconv.Itoa(len(d.Ads)) + " anúncios"
	}
	c := d.Ads[0].Creative
	name := formatNames[c.Format]
	switch c.Format {
	case advertising.FormatCarousel:
		return name + " com " + strconv.Itoa(len(c.Cards)) + " cartões"
	case advertising.FormatFlexible:
		return name + " com " + strconv.Itoa(len(c.Medias)) + " mídias e " + strconv.Itoa(len(c.Texts)) + " textos"
	case advertising.FormatExistingPost:
		if c.InstagramMediaID != "" {
			return name + " do Instagram"
		}
		return name + " do Facebook"
	}
	return name
}

func draftBudget(d advertising.AdDraft) (*advertising.Budget, advertising.Bid, string) {
	if d.CampaignBudget() {
		return d.Campaign.Budget, d.Campaign.Bid, "na campanha (orçamento Advantage, a Meta distribui entre os conjuntos)"
	}
	return d.AdSet.Budget, d.AdSet.Bid, "no conjunto de anúncios"
}

func scheduleFields(s advertising.AdSetDraft, loc *time.Location) []copilot.Field {
	var fields []copilot.Field
	if s.StartAt != nil {
		fields = append(fields, copilot.Field{Key: "starts", Value: "a partir de " + s.StartAt.In(loc).Format("02/01/2006")})
	}
	if s.EndAt != nil {
		fields = append(fields, copilot.Field{Key: "ends", Value: "até " + lastDay(*s.EndAt, loc).Format("02/01/2006")})
	}
	return fields
}

func audienceText(t advertising.Targeting) string {
	parts := []string{}
	if n := len(t.CustomAudiences); n > 0 {
		parts = append(parts, "públicos incluídos: "+strconv.Itoa(n))
	}
	if n := len(t.ExcludedCustomAudiences); n > 0 {
		parts = append(parts, "públicos excluídos: "+strconv.Itoa(n))
	}
	return strings.Join(parts, ", ")
}

func draftFields(pre *adsuc.Preflight) []copilot.Field {
	d := pre.Draft
	currency := pre.Account.Currency
	budget, bid, level := draftBudget(d)
	fields := []copilot.Field{
		{Key: "account", Value: pre.Account.Name},
		{Key: "campaign", Value: d.Campaign.Name},
		{Key: "objective", Value: objectiveNames[d.Campaign.Objective]},
		{Key: "page", Value: pre.Page.Name},
		{Key: "destination", Value: destinationText(pre)},
		{Key: "format", Value: formatText(d)},
		{Key: "budget", Value: budgetText(currency, budget)},
		{Key: "budget_level", Value: level},
	}
	if bid.Normalized().Strategy != advertising.BidLowestCost {
		fields = append(fields, copilot.Field{Key: "bid", Value: bidText(currency, bid)})
	}
	fields = append(fields, copilot.Field{Key: "placements", Value: placementsText(&d.AdSet.Placements)})
	if audiences := audienceText(d.AdSet.Targeting); audiences != "" {
		fields = append(fields, copilot.Field{Key: "audiences", Value: audiences})
	}
	if loc, err := pre.Account.Location(); err == nil {
		fields = append(fields, scheduleFields(d.AdSet, loc)...)
	}
	if d.KeepPaused {
		fields = append(fields, copilot.Field{Key: "status", Value: "publicado desligado"})
	}
	return fields
}

type PreviewMedia struct {
	URL  string                `json:"url"`
	Kind advertising.MediaKind `json:"kind"`
}

type PreviewCard struct {
	URL         string                `json:"url"`
	Kind        advertising.MediaKind `json:"kind"`
	Headline    string                `json:"headline,omitempty"`
	Description string                `json:"description,omitempty"`
}

type AdCreativePreview struct {
	PageName       string                     `json:"pageName"`
	PagePictureURL string                     `json:"pagePictureUrl,omitempty"`
	AccountName    string                     `json:"accountName"`
	Format         advertising.CreativeFormat `json:"format"`
	PrimaryText    string                     `json:"primaryText"`
	Headline       string                     `json:"headline,omitempty"`
	Description    string                     `json:"description,omitempty"`
	MediaURL       string                     `json:"mediaUrl,omitempty"`
	MediaKind      advertising.MediaKind      `json:"mediaKind,omitempty"`
	Medias         []PreviewMedia             `json:"medias,omitempty"`
	Cards          []PreviewCard              `json:"cards,omitempty"`
	Destination    string                     `json:"destination"`
	CallToAction   advertising.CallToAction   `json:"callToAction"`
	DisplayLink    string                     `json:"displayLink,omitempty"`
	Greeting       string                     `json:"greeting,omitempty"`
	IceBreakers    []string                   `json:"iceBreakers,omitempty"`
	DailyBudget    int64                      `json:"dailyBudget"`
	Currency       string                     `json:"currency"`
	Fee            int64                      `json:"fee"`
	FeeCurrency    string                     `json:"feeCurrency"`
}

func creativePreview(pre *adsuc.Preflight, c advertising.CreativeDraft) AdCreativePreview {
	d := pre.Draft
	out := AdCreativePreview{
		PageName: pre.Page.Name, PagePictureURL: pre.Page.PictureURL, AccountName: pre.Account.Name, Format: c.Format,
		PrimaryText: c.PrimaryText, Headline: c.Headline, Description: c.Description,
		Destination: string(d.AdSet.Destination), CallToAction: c.ResolvedCallToAction(d.AdSet.Destination), DisplayLink: c.DisplayLink,
		Greeting: c.Greeting, IceBreakers: c.IceBreakers,
		Currency: pre.Account.Currency, Fee: pre.Fee.PriceMicros, FeeCurrency: pre.Fee.Currency,
	}
	if c.Media.MediaID != "" {
		out.MediaURL, out.MediaKind = pre.MediaURLs[c.Media.MediaID], c.Media.Kind
	}
	for _, m := range c.Medias {
		out.Medias = append(out.Medias, PreviewMedia{URL: pre.MediaURLs[m.MediaID], Kind: m.Kind})
	}
	for _, card := range c.Cards {
		out.Cards = append(out.Cards, PreviewCard{URL: pre.MediaURLs[card.Media.MediaID], Kind: card.Media.Kind, Headline: card.Headline, Description: card.Description})
	}
	if budget, _, _ := draftBudget(d); budget != nil && budget.Kind == advertising.BudgetDaily {
		out.DailyBudget = budget.Amount
	}
	return out
}

func (t *createAdTool) Preview(ctx context.Context, cc copilot.Context, args map[string]interface{}) *copilot.Preview {
	pre, err := t.preflight(ctx, cc, args)
	if err != nil {
		return nil
	}
	return &copilot.Preview{Kind: PreviewAdCreative, Data: creativePreview(pre, pre.Draft.Ads[0].Creative)}
}

func (t *createAdTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	pre, err := t.preflight(ctx, cc, args)
	if err != nil {
		return adsFailure("create_ad", err)
	}
	job, err := t.deps.Publish.Publish(ctx, adsuc.PublishInput{
		WorkspaceID: cc.WorkspaceID, UserID: cc.UserID, Actor: advertising.ActorAssistant, Draft: pre.Draft,
	})
	if err != nil {
		return adsFailure("create_ad", err)
	}
	return publishResult(job)
}

type adStatusArgs struct {
	MetaID string `json:"meta_id" req:"true" desc:"meta_id exato de ads_results (campanha, conjunto ou anúncio)"`
}

type adStatusTool struct {
	deps AdsDeps
	on   bool
}

func NewTurnOnAdTool(deps AdsDeps) copilot.Tool  { return &adStatusTool{deps: deps, on: true} }
func NewTurnOffAdTool(deps AdsDeps) copilot.Tool { return &adStatusTool{deps: deps, on: false} }

func (t *adStatusTool) Meta() copilot.Meta {
	if t.on {
		return adsMeta(workspace.ActionStart, true)
	}
	return adsMeta(workspace.ActionStop, true)
}

func (t *adStatusTool) Definition() tools.Definition {
	if t.on {
		return definition("turn_on_ad", "Liga uma campanha, conjunto ou anúncio na Meta, o que volta a gastar o orçamento. Só depois da aprovação do usuário.", adStatusArgs{})
	}
	return definition("turn_off_ad", "Desliga uma campanha, conjunto ou anúncio na Meta. Só depois da aprovação do usuário.", adStatusArgs{})
}

func (t *adStatusTool) check(ctx context.Context, cc copilot.Context, args map[string]interface{}) (*advertising.Object, error) {
	a, err := validateArgs[adStatusArgs](nil, cc, args)
	if err != nil {
		return nil, err
	}
	id, err := metaID(a.MetaID)
	if err != nil {
		return nil, err
	}
	return t.deps.Manage.CheckStatus(ctx, cc.WorkspaceID, id, t.on)
}

func (t *adStatusTool) Validate(ctx context.Context, cc copilot.Context, args map[string]interface{}) error {
	_, err := t.check(ctx, cc, args)
	return adsValidation(t.Definition().Name, err)
}

func (t *adStatusTool) Describe(ctx context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	object, err := t.check(ctx, cc, args)
	if err != nil {
		return []copilot.Field{{Key: "item", Value: "item desconhecido"}}
	}
	change := "desligar"
	if t.on {
		change = "ligar"
	}
	return []copilot.Field{{Key: "item", Value: object.Name}, {Key: "level", Value: levelNames[object.Level]}, {Key: "change", Value: change}}
}

func (t *adStatusTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a adStatusArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	id, err := metaID(a.MetaID)
	if err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	object, err := t.deps.Manage.SetStatus(ctx, cc.WorkspaceID, id, t.on)
	if err != nil {
		return adsFailure(t.Definition().Name, err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"meta_id": object.MetaID, "on": object.IsOn(), "status": string(object.EffectiveStatus)}}
}

type adBudgetArgs struct {
	MetaID string  `json:"meta_id" req:"true" desc:"meta_id exato de ads_results (campanha ou conjunto com orçamento próprio)"`
	Amount float64 `json:"amount" req:"true" desc:"novo valor na moeda da conta, do mesmo tipo do orçamento atual (diário ou total); 50 significa 50 reais em uma conta BRL"`
}

type updateAdBudgetTool struct{ deps AdsDeps }

func NewUpdateAdBudgetTool(deps AdsDeps) copilot.Tool { return &updateAdBudgetTool{deps: deps} }

func (t *updateAdBudgetTool) Meta() copilot.Meta { return adsMeta(workspace.ActionUpdate, true) }

func (t *updateAdBudgetTool) Definition() tools.Definition {
	return definition("update_ad_budget",
		"Muda o valor do orçamento diário ou total de uma campanha ou conjunto na Meta, mantendo o tipo (até 4 mudanças por hora por item). "+
			"Um orçamento diário abaixo do mínimo da Meta é recusado antes da aprovação. Só depois da aprovação do usuário.",
		adBudgetArgs{})
}

type budgetPlan struct {
	object  *advertising.Object
	account *advertising.AdAccount
	amount  int64
}

func (t *updateAdBudgetTool) plan(ctx context.Context, cc copilot.Context, args map[string]interface{}) (*budgetPlan, error) {
	a, err := validateArgs[adBudgetArgs](nil, cc, args)
	if err != nil {
		return nil, err
	}
	id, err := metaID(a.MetaID)
	if err != nil {
		return nil, err
	}
	detail, err := t.deps.Editor.Detail(ctx, cc.WorkspaceID, id)
	if err != nil {
		return nil, err
	}
	owner, err := t.deps.account(ctx, cc, detail.Object.AdAccountID)
	if err != nil {
		return nil, err
	}
	amount, err := advertising.AmountToMinor(owner.Currency, a.Amount)
	if err != nil {
		return nil, fmt.Errorf("%w: amount deve ser maior que zero", errInvalidArgs)
	}
	object, account, err := t.deps.Manage.CheckBudget(ctx, cc.WorkspaceID, id, amount)
	if err != nil {
		return nil, err
	}
	return &budgetPlan{object: object, account: account, amount: amount}, nil
}

func (t *updateAdBudgetTool) Validate(ctx context.Context, cc copilot.Context, args map[string]interface{}) error {
	_, err := t.plan(ctx, cc, args)
	return adsValidation("update_ad_budget", err)
}

func (t *updateAdBudgetTool) Describe(ctx context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	p, err := t.plan(ctx, cc, args)
	if err != nil {
		return []copilot.Field{{Key: "item", Value: "item desconhecido"}}
	}
	current := p.object.Budget()
	return []copilot.Field{
		{Key: "item", Value: p.object.Name},
		{Key: "from", Value: budgetText(p.account.Currency, current)},
		{Key: "to", Value: budgetText(p.account.Currency, &advertising.Budget{Kind: current.Kind, Amount: p.amount})},
	}
}

func (t *updateAdBudgetTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	p, err := t.plan(ctx, cc, args)
	if err != nil {
		return adsFailure("update_ad_budget", err)
	}
	object, err := t.deps.Manage.SetBudget(ctx, cc.WorkspaceID, p.object.MetaID, p.amount)
	if err != nil {
		return adsFailure("update_ad_budget", err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"meta_id": object.MetaID, "budget": budgetText(p.account.Currency, object.Budget())}}
}

func AdsTools(deps AdsDeps) []copilot.Tool {
	return []copilot.Tool{
		NewListAdAccountsTool(deps), NewAdsResultsTool(deps), NewListAdPagesTool(deps), NewSearchAdLocationsTool(deps),
		NewCreateAdTool(deps), NewTurnOnAdTool(deps), NewTurnOffAdTool(deps), NewUpdateAdBudgetTool(deps),
		NewDuplicateAdTool(deps), NewArchiveAdTool(deps), NewDeleteAdTool(deps), NewAdsBreakdownTool(deps),
		NewAdAccountReadinessTool(deps), NewSearchAdInterestsTool(deps), NewEstimateAdAudienceTool(deps), NewListLeadFormsTool(deps), NewCreateLeadFormTool(deps),
		NewSaveAdDraftTool(deps), NewListAdDraftsTool(deps), NewPublishAdDraftTool(deps), NewEditAdTextTool(deps),
		NewListPagePostsTool(deps), NewListAdAppsTool(deps), NewListAdCatalogsTool(deps), NewGetAdDraftTool(deps), NewUpdateAdDraftTool(deps),
	}
}
