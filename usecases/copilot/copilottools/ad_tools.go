package copilottools

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"

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
}

type AdPublisher interface {
	Preflight(ctx context.Context, workspaceID string, draft advertising.AdDraft) (*adsuc.Preflight, error)
	Publish(ctx context.Context, in adsuc.PublishInput) (*advertising.PublishJob, error)
}

type AdImages interface {
	Check(req advertising.ImageRequest) error
	Generate(ctx context.Context, req advertising.ImageRequest) (*adsuc.GeneratedCreative, error)
}

type AdsDeps struct {
	Accounts AdAccountLister
	Reports  AdReporter
	Manage   AdManager
	Assets   AdAssets
	Publish  AdPublisher
	Images   AdImages
	Live     AdLive
}

func adsMeta(action workspace.Action, mutating bool) copilot.Meta {
	return copilot.Meta{Mutating: mutating, Resource: workspace.ResourceAds, Action: action}
}

func (d AdsDeps) account(ctx context.Context, cc copilot.Context, id string) (*advertising.AdAccount, error) {
	accountID, err := knownID(id, "ad_account_id", "list_ad_accounts")
	if err != nil {
		return nil, err
	}
	accounts, err := d.Accounts.List(ctx, cc.WorkspaceID)
	if err != nil {
		return nil, err
	}
	for _, a := range accounts {
		if a.ID == accountID {
			return a, nil
		}
	}
	return nil, fmt.Errorf("%w: ad_account_id desconhecido; use o id exato de list_ad_accounts", errInvalidArgs)
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

func adsFailure(tool string, err error) copilot.Result {
	var invalid *advertising.ValidationError
	switch {
	case errors.Is(err, errInvalidArgs):
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	case errors.As(err, &invalid):
		return copilot.Result{Status: copilot.StatusError, Message: "o anúncio tem campos a corrigir: " + invalid.Error()}
	case errors.Is(err, advertising.ErrNoFundingSource):
		return copilot.Result{Status: copilot.StatusError, Message: "a conta de anúncios não tem meio de pagamento; o usuário precisa configurar no Gerenciador de Anúncios da Meta"}
	case errors.Is(err, advertising.ErrAccountNeedsReconnect), errors.Is(err, advertising.ErrMissingScopes), advertising.Classify(err) == advertising.FailureReauth:
		return copilot.Result{Status: copilot.StatusError, Message: "a conta de anúncios precisa ser reconectada na tela Anúncios"}
	case errors.Is(err, advertising.ErrBudgetChangeTooSoon):
		return copilot.Result{Status: copilot.StatusError, Message: "a Meta só permite 4 mudanças de orçamento por hora neste item; tente mais tarde"}
	case errors.Is(err, advertising.ErrObjectNotFound), errors.Is(err, advertising.ErrAccountNotFound):
		return copilot.Result{Status: copilot.StatusError, Message: "item não encontrado; use os ids exatos de list_ad_accounts e ads_results"}
	case advertising.Classify(err) == advertising.FailureRejected:
		return copilot.Result{Status: copilot.StatusError, Message: "a Meta recusou: " + advertising.Explain(err)}
	}
	log.Printf("[copilot] %s failed: %v", tool, err)
	return copilot.Result{Status: copilot.StatusError, Message: "falha ao falar com a Meta; tente de novo em instantes"}
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
	AdAccountID string   `json:"ad_account_id" req:"true" id:"true" desc:"ad_account_id de list_ad_accounts"`
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
	AdAccountID string `json:"ad_account_id" req:"true" id:"true" desc:"ad_account_id de list_ad_accounts"`
}

type listAdPagesTool struct{ deps AdsDeps }

func NewListAdPagesTool(deps AdsDeps) copilot.Tool { return &listAdPagesTool{deps: deps} }

func (t *listAdPagesTool) Meta() copilot.Meta { return adsMeta(workspace.ActionCreate, false) }

func (t *listAdPagesTool) Definition() tools.Definition {
	return definition("list_ad_pages",
		"Lista as páginas do Facebook que podem anunciar por uma conta, o Instagram de cada uma e os números de WhatsApp do workspace "+
			"vinculados a cada página. Um anúncio para WhatsApp só pode usar um desses números.",
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
			"page_id": p.Page.PageID, "name": p.Page.Name, "can_advertise": p.Page.CanAdvertise,
			"instagram_user_id": p.Page.InstagramUserID, "instagram_username": p.Page.InstagramUsername,
			"whatsapp_numbers": numbers,
		})
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"pages": out}}
}

type searchAdLocationsArgs struct {
	AdAccountID string `json:"ad_account_id" req:"true" id:"true" desc:"ad_account_id de list_ad_accounts"`
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

type generateAdImageArgs struct {
	Prompt string `json:"prompt" req:"true" desc:"descrição da imagem do anúncio: produto, cena, estilo; evite texto na imagem"`
	Aspect string `json:"aspect" req:"true" enum:"square,portrait,story" desc:"square (feed 1:1), portrait (feed 4:5) ou story (9:16)"`
}

func (a generateAdImageArgs) request(cc copilot.Context) advertising.ImageRequest {
	return advertising.ImageRequest{WorkspaceID: cc.WorkspaceID, Prompt: a.Prompt, Aspect: advertising.Aspect(a.Aspect)}
}

type generateAdImageTool struct{ deps AdsDeps }

func NewGenerateAdImageTool(deps AdsDeps) copilot.Tool { return &generateAdImageTool{deps: deps} }

func (t *generateAdImageTool) Meta() copilot.Meta { return adsMeta(workspace.ActionCreate, true) }

func (t *generateAdImageTool) Definition() tools.Definition {
	return definition("generate_ad_image",
		"Gera uma imagem para anúncio com IA, salva na biblioteca de mídia e devolve o media_id para create_ad. "+
			"É cobrada como uso de IA, por isso só depois da aprovação do usuário.",
		generateAdImageArgs{})
}

func (t *generateAdImageTool) Validate(_ context.Context, cc copilot.Context, args map[string]interface{}) error {
	a, err := validateArgs[generateAdImageArgs](nil, cc, args)
	if err != nil {
		return err
	}
	if err := t.deps.Images.Check(a.request(cc)); err != nil {
		return fmt.Errorf("%w: %v", errInvalidArgs, err)
	}
	return nil
}

func (t *generateAdImageTool) Describe(_ context.Context, _ copilot.Context, args map[string]interface{}) []copilot.Field {
	var a generateAdImageArgs
	bindArgs(args, &a)
	return []copilot.Field{
		{Key: "image", Value: a.Prompt},
		{Key: "format", Value: a.Aspect},
		{Key: "cost", Value: "cobrado do saldo como uso de IA"},
	}
}

func (t *generateAdImageTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a generateAdImageArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	image, err := t.deps.Images.Generate(ctx, a.request(cc))
	if err != nil {
		log.Printf("[copilot] generate_ad_image failed: %v", err)
		return copilot.Result{Status: copilot.StatusError, Message: "não foi possível gerar a imagem; tente outra descrição"}
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"media_id": image.Media.ID, "model": image.Model}}
}

type createAdArgs struct {
	AdAccountID     string   `json:"ad_account_id" req:"true" id:"true" desc:"ad_account_id de list_ad_accounts"`
	CampaignName    string   `json:"campaign_name" req:"true" desc:"nome da campanha"`
	Destination     string   `json:"destination" req:"true" enum:"WHATSAPP,MESSENGER,INSTAGRAM_DIRECT" desc:"para onde a pessoa vai ao clicar"`
	PageID          string   `json:"page_id" req:"true" desc:"page_id exato de list_ad_pages"`
	WhatsAppNumber  string   `json:"whatsapp_number" desc:"whatsapp_number exato de list_ad_pages (obrigatório para WHATSAPP)"`
	InstagramUserID string   `json:"instagram_user_id" desc:"instagram_user_id da página (obrigatório para INSTAGRAM_DIRECT)"`
	DailyBudget     float64  `json:"daily_budget" req:"true" desc:"orçamento diário na moeda da conta; 30 significa 30 reais em uma conta BRL"`
	Locations       []string `json:"locations" req:"true" desc:"location de search_ad_locations, ex.: country:BR"`
	AgeMin          int      `json:"age_min" desc:"idade mínima, 13 a 65 (padrão 18)"`
	AgeMax          int      `json:"age_max" desc:"idade máxima, 13 a 65, 65 significa 65+ (padrão 65)"`
	Genders         []string `json:"genders" desc:"male e ou female; vazio para todos"`
	SpecialCategory string   `json:"special_category" enum:"NONE,HOUSING,EMPLOYMENT,FINANCIAL_PRODUCTS_SERVICES" desc:"categoria especial obrigatória para imóveis, vagas de emprego e crédito; NONE nos demais"`
	PrimaryText     string   `json:"primary_text" req:"true" desc:"texto principal do anúncio"`
	Headline        string   `json:"headline" desc:"título curto (até 40 caracteres é o ideal)"`
	Description     string   `json:"description" desc:"descrição curta opcional"`
	Format          string   `json:"format" req:"true" enum:"IMAGE,VIDEO" desc:"IMAGE para uma imagem, VIDEO para um vídeo"`
	MediaID         string   `json:"media_id" req:"true" id:"true" desc:"media_id de generate_ad_image ou de uma imagem ou vídeo anexado, do mesmo tipo de format"`
	Greeting        string   `json:"greeting" desc:"mensagem que já vem escrita para o cliente enviar"`
	IceBreakers     []string `json:"ice_breakers" desc:"até 3 perguntas prontas para o cliente tocar"`
}

func (a createAdArgs) draft(account *advertising.AdAccount) (advertising.AdDraft, error) {
	format := advertising.CreativeFormat(a.Format)
	kind, single := format.SingleMediaKind()
	if !single {
		return advertising.AdDraft{}, fmt.Errorf("%w: format deve ser IMAGE ou VIDEO", errInvalidArgs)
	}
	budget, err := advertising.AmountToMinor(account.Currency, a.DailyBudget)
	if err != nil {
		return advertising.AdDraft{}, fmt.Errorf("%w: daily_budget deve ser maior que zero", errInvalidArgs)
	}
	locations, err := parseLocations(a.Locations)
	if err != nil {
		return advertising.AdDraft{}, err
	}
	d := advertising.AdDraft{
		AdAccountID: account.ID,
		Identity:    advertising.Identity{PageID: strings.TrimSpace(a.PageID), InstagramUserID: a.InstagramUserID},
		Campaign: advertising.CampaignDraft{
			Name: a.CampaignName, Objective: advertising.ObjectiveEngagement,
			SpecialCategory: advertising.SpecialCategory(a.SpecialCategory),
		},
		AdSet: advertising.AdSetDraft{
			Destination: advertising.Destination(a.Destination), Goal: advertising.GoalConversations,
			WhatsAppNumber: a.WhatsAppNumber, Budget: &advertising.Budget{Kind: advertising.BudgetDaily, Amount: budget},
			Targeting: advertising.Targeting{Locations: locations, AgeMin: a.AgeMin, AgeMax: a.AgeMax, Genders: genders(a.Genders)},
		},
		Ads: []advertising.AdItem{{Name: a.CampaignName, Creative: advertising.CreativeDraft{
			Format: format, PrimaryText: a.PrimaryText, Headline: a.Headline, Description: a.Description,
			Media:    advertising.MediaRef{Kind: kind, MediaID: a.MediaID},
			Greeting: a.Greeting, IceBreakers: a.IceBreakers,
		}}},
	}
	d.Normalize()
	return d, nil
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
	return definition("create_ad",
		"Cria e publica um anúncio na Meta que leva a pessoa para o WhatsApp, Messenger ou Instagram Direct: campanha, conjunto e anúncio. "+
			"Tudo é criado desligado e só é ligado no fim; depois a Meta revisa o anúncio antes de veicular. "+
			"Cobra a taxa por anúncio publicado do saldo; o gasto com a Meta sai da conta de anúncios. Só depois da aprovação do usuário.",
		createAdArgs{})
}

func (t *createAdTool) preflight(ctx context.Context, cc copilot.Context, args map[string]interface{}) (*adsuc.Preflight, error) {
	a, err := validateArgs[createAdArgs](nil, cc, args)
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
	return t.deps.Publish.Preflight(ctx, cc.WorkspaceID, draft)
}

func (t *createAdTool) Validate(ctx context.Context, cc copilot.Context, args map[string]interface{}) error {
	if _, err := t.preflight(ctx, cc, args); err != nil {
		if errors.Is(err, errInvalidArgs) {
			return err
		}
		return fmt.Errorf("%w: %s", errInvalidArgs, adsFailure("create_ad", err).Message)
	}
	return nil
}

func (t *createAdTool) Describe(ctx context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	pre, err := t.preflight(ctx, cc, args)
	if err != nil {
		return []copilot.Field{{Key: "ad", Value: "anúncio com campos a corrigir"}}
	}
	d := pre.Draft
	destination := map[advertising.Destination]string{
		advertising.DestinationWhatsApp:        "WhatsApp " + d.AdSet.WhatsAppNumber,
		advertising.DestinationMessenger:       "Messenger da página",
		advertising.DestinationInstagramDirect: "Instagram Direct @" + pre.Page.InstagramUsername,
	}[d.AdSet.Destination]
	return []copilot.Field{
		{Key: "account", Value: pre.Account.Name},
		{Key: "campaign", Value: d.Campaign.Name},
		{Key: "page", Value: pre.Page.Name},
		{Key: "destination", Value: destination},
		{Key: "budget", Value: budgetText(pre.Account.Currency, d.AdSet.Budget)},
		{Key: "fee", Value: moneyText(pre.Fee.Currency, pre.Fee.PriceMicros) + " por anúncio publicado, cobrado do saldo"},
	}
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
	if budget := d.AdSet.Budget; budget != nil && budget.Kind == advertising.BudgetDaily {
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
	data := map[string]interface{}{"publish_status": string(job.Status), "ad_meta_id": job.Progress.Ads[0], "campaign_meta_id": job.CampaignID()}
	if job.ErrorMessage != "" {
		data["message"] = job.ErrorMessage
	}
	status := copilot.StatusOK
	if job.Status == advertising.JobFailed {
		status = copilot.StatusError
	}
	return copilot.Result{Status: status, Data: data}
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
	if _, err := t.check(ctx, cc, args); err != nil {
		if errors.Is(err, errInvalidArgs) {
			return err
		}
		return fmt.Errorf("%w: %s", errInvalidArgs, adsFailure(t.Definition().Name, err).Message)
	}
	return nil
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
		"Muda o valor do orçamento diário ou total de uma campanha ou conjunto na Meta, mantendo o tipo (até 4 mudanças por hora por item). Só depois da aprovação do usuário.",
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
	object, account, err := t.deps.Manage.CheckBudget(ctx, cc.WorkspaceID, id, 1)
	if err != nil {
		return nil, err
	}
	amount, err := advertising.AmountToMinor(account.Currency, a.Amount)
	if err != nil {
		return nil, fmt.Errorf("%w: amount deve ser maior que zero", errInvalidArgs)
	}
	return &budgetPlan{object: object, account: account, amount: amount}, nil
}

func (t *updateAdBudgetTool) Validate(ctx context.Context, cc copilot.Context, args map[string]interface{}) error {
	if _, err := t.plan(ctx, cc, args); err != nil {
		if errors.Is(err, errInvalidArgs) {
			return err
		}
		return fmt.Errorf("%w: %s", errInvalidArgs, adsFailure("update_ad_budget", err).Message)
	}
	return nil
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
		NewGenerateAdImageTool(deps), NewCreateAdTool(deps), NewTurnOnAdTool(deps), NewTurnOffAdTool(deps), NewUpdateAdBudgetTool(deps),
		NewDuplicateAdTool(deps), NewArchiveAdTool(deps), NewDeleteAdTool(deps), NewAdsBreakdownTool(deps),
	}
}
