package copilottools

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"vozko/domain/advertising"
	"vozko/domain/copilot"
	"vozko/domain/tools"
	"vozko/domain/workspace"
	adsuc "vozko/usecases/advertising"
)

type AdBulk interface {
	CheckStatus(ctx context.Context, workspaceID string, metaIDs []string, on bool) ([]adsuc.BulkResult, error)
	SetStatus(ctx context.Context, workspaceID string, metaIDs []string, on bool) ([]adsuc.BulkResult, error)
	CheckEdit(ctx context.Context, workspaceID string, metaIDs []string, change advertising.BulkChange) ([]adsuc.BulkResult, error)
	Edit(ctx context.Context, workspaceID string, metaIDs []string, change advertising.BulkChange) ([]adsuc.BulkResult, error)
	CheckApply(ctx context.Context, workspaceID string, metaIDs []string, edit advertising.ObjectEdit) ([]adsuc.BulkResult, error)
	Apply(ctx context.Context, workspaceID string, metaIDs []string, edit advertising.ObjectEdit) ([]adsuc.BulkResult, error)
}

type AdSpendCap interface {
	CheckSpendCap(ctx context.Context, workspaceID, accountID string, cap *int64) (*advertising.AdAccount, error)
	SetSpendCap(ctx context.Context, workspaceID, accountID string, cap *int64) (*advertising.AdAccount, error)
}

type AdReportRuns interface {
	Run(ctx context.Context, in adsuc.ReportRunInput) (*adsuc.ReportRun, error)
	Export(ctx context.Context, in adsuc.ReportExportInput) (*advertising.ReportExport, error)
}

type AdSavedReports interface {
	List(ctx context.Context, workspaceID string) ([]*advertising.SavedReport, error)
}

type AdManageDeps struct {
	Bulk     AdBulk
	SpendCap AdSpendCap
	Runs     AdReportRuns
	Reports  AdSavedReports
	Now      func() time.Time
}

type adManage struct {
	AdManageDeps
	ads AdsDeps
}

func AdManageTools(deps AdManageDeps, ads AdsDeps) []copilot.Tool {
	if deps.Now == nil {
		deps.Now = time.Now
	}
	m := adManage{AdManageDeps: deps, ads: ads}
	return []copilot.Tool{
		&editAdSetTool{deps: m},
		&bulkStatusTool{deps: m, on: true}, &bulkStatusTool{deps: m, on: false},
		&bulkEditTextTool{deps: m}, &bulkChangeTool{deps: m},
		&setSpendCapTool{deps: m}, &swapAdCreativeTool{deps: m},
		&runAdReportTool{deps: m}, &listAdReportsTool{deps: m}, &runSavedAdReportTool{deps: m}, &exportAdReportTool{deps: m},
	}
}

func minorText(currency string, minor int64) string {
	micros, err := advertising.MinorToMicros(currency, minor)
	if err != nil {
		return "valor desconhecido"
	}
	return moneyText(currency, micros)
}

func lastDayText(end *time.Time, account *advertising.AdAccount) string {
	if end == nil {
		return "sem data de término"
	}
	loc, err := account.Location()
	if err != nil {
		return "data desconhecida"
	}
	return "até " + lastDay(*end, loc).Format("02/01/2006")
}

func changeText(from, to string) string { return from + " → " + to }

type editAdSetArgs struct {
	MetaID      string    `json:"meta_id" req:"true" desc:"meta_id exato de ads_results de um conjunto de anúncios; para orçamento, lance e término também de uma campanha"`
	Locations   []string  `json:"locations" desc:"novas localizações, campo location de search_ad_locations; substitui as atuais"`
	AgeMin      int       `json:"age_min" desc:"nova idade mínima, 13 a 65"`
	AgeMax      int       `json:"age_max" desc:"nova idade máxima, 13 a 65; 65 significa 65+"`
	Genders     *[]string `json:"genders" desc:"male e ou female; [] volta para todos os gêneros"`
	Interests   *[]string `json:"interests" desc:"campo interest de search_ad_interests; substitui os atuais; [] tira todos e deixa a Meta encontrar o público"`
	Placements  []string  `json:"placements" desc:"automatic para posicionamentos automáticos (Advantage+) ou as plataformas escolhidas: facebook, instagram, messenger, audience_network, threads"`
	EndDate     string    `json:"end_date" desc:"novo último dia de veiculação YYYY-MM-DD no fuso da conta"`
	Budget      float64   `json:"budget" desc:"novo valor do orçamento na moeda da conta, do mesmo tipo do atual (diário ou total); 50 significa 50 reais em uma conta BRL"`
	BidStrategy string    `json:"bid_strategy" enum:"LOWEST_COST_WITHOUT_CAP,LOWEST_COST_WITH_BID_CAP,COST_CAP,LOWEST_COST_WITH_MIN_ROAS" desc:"estratégia de lance: LOWEST_COST_WITHOUT_CAP (menor custo, recomendada), LOWEST_COST_WITH_BID_CAP (limite de lance), COST_CAP (limite de custo por resultado) ou LOWEST_COST_WITH_MIN_ROAS (ROAS mínimo, só em conjunto com meta de valor)"`
	BidAmount   float64   `json:"bid_amount" desc:"valor do limite de lance ou de custo na moeda da conta, para LOWEST_COST_WITH_BID_CAP e COST_CAP"`
	ROASFloor   float64   `json:"roas_floor" desc:"ROAS mínimo, ex.: 2.5, para LOWEST_COST_WITH_MIN_ROAS"`
}

func (a editAdSetArgs) edit(current advertising.ObjectDetail, account *advertising.AdAccount) (advertising.ObjectEdit, error) {
	targeting, err := a.targeting(current.Targeting)
	if err != nil {
		return advertising.ObjectEdit{}, err
	}
	endAt, err := adDraftArgs{EndDate: a.EndDate}.endAt(account)
	if err != nil {
		return advertising.ObjectEdit{}, err
	}
	budget, err := a.budget(current.Budget, account)
	if err != nil {
		return advertising.ObjectEdit{}, err
	}
	bid, err := a.bid(current.Bid, account)
	if err != nil {
		return advertising.ObjectEdit{}, err
	}
	placements, err := a.placements(current.Placements)
	if err != nil {
		return advertising.ObjectEdit{}, err
	}
	edit := advertising.ObjectEdit{Targeting: targeting, Placements: placements, EndAt: endAt, Budget: budget, Bid: bid}
	if edit.Empty() {
		return advertising.ObjectEdit{}, fmt.Errorf("%w: diga o que mudar: público, posicionamentos, término, orçamento ou lance", errInvalidArgs)
	}
	return edit, nil
}

func (a editAdSetArgs) targeting(current *advertising.Targeting) (*advertising.Targeting, error) {
	if len(a.Locations) == 0 && a.AgeMin == 0 && a.AgeMax == 0 && a.Genders == nil && a.Interests == nil {
		return nil, nil
	}
	if current == nil {
		return nil, fmt.Errorf("%w: o público só muda em um conjunto de anúncios; use o meta_id de um conjunto de ads_results", errInvalidArgs)
	}
	next := *current
	if len(a.Locations) > 0 {
		locations, err := parseLocations(a.Locations)
		if err != nil {
			return nil, err
		}
		next.Locations = locations
	}
	if a.AgeMin != 0 {
		next.AgeMin = a.AgeMin
	}
	if a.AgeMax != 0 {
		next.AgeMax = a.AgeMax
	}
	if a.Genders != nil {
		next.Genders = genders(*a.Genders)
	}
	if a.Interests != nil {
		interests, err := parseTargetRefs(*a.Interests)
		if err != nil {
			return nil, err
		}
		next.Interests = interests
	}
	return &next, nil
}

func (a editAdSetArgs) placements(current *advertising.Placements) (*advertising.Placements, error) {
	if len(a.Placements) == 0 {
		return nil, nil
	}
	kept := advertising.Placements{}
	if current != nil {
		kept = *current
	}
	next, err := placementsKeepingDevices(a.Placements, kept)
	if err != nil {
		return nil, err
	}
	return &next, nil
}

func (a editAdSetArgs) budget(current *advertising.Budget, account *advertising.AdAccount) (*advertising.Budget, error) {
	if a.Budget == 0 {
		return nil, nil
	}
	amount, err := advertising.AmountToMinor(account.Currency, a.Budget)
	if err != nil {
		return nil, fmt.Errorf("%w: budget deve ser maior que zero", errInvalidArgs)
	}
	kind := advertising.BudgetDaily
	if current != nil {
		kind = current.Kind
	}
	return &advertising.Budget{Kind: kind, Amount: amount}, nil
}

func (a editAdSetArgs) bid(current advertising.Bid, account *advertising.AdAccount) (*advertising.Bid, error) {
	if a.BidStrategy == "" && a.BidAmount == 0 && a.ROASFloor == 0 {
		return nil, nil
	}
	next := current.Normalized()
	if a.BidStrategy != "" && advertising.BidStrategy(a.BidStrategy) != next.Strategy {
		next = advertising.Bid{Strategy: advertising.BidStrategy(a.BidStrategy)}
	}
	if a.BidAmount != 0 {
		amount, err := advertising.AmountToMinor(account.Currency, a.BidAmount)
		if err != nil {
			return nil, fmt.Errorf("%w: bid_amount deve ser maior que zero", errInvalidArgs)
		}
		next.Amount = amount
	}
	if a.ROASFloor != 0 {
		next.ROASFloor = a.ROASFloor
	}
	return &next, nil
}

type editAdSetTool struct{ deps adManage }

func (t *editAdSetTool) Meta() copilot.Meta { return adsMeta(workspace.ActionUpdate, true) }

func (t *editAdSetTool) Definition() tools.Definition {
	return definition("edit_ad_set",
		"Muda um conjunto de anúncios publicado: público (localizações, idade, gênero, interesses), posicionamentos, data de término, "+
			"valor do orçamento e lance. Envie só o que muda: o resto do público continua como está. O tipo de orçamento (diário ou total) "+
			"não muda, a Meta não permite. Para uma campanha vale orçamento, lance e término. Só depois da aprovação do usuário.",
		editAdSetArgs{})
}

type adSetEditPlan struct {
	detail  *advertising.ObjectDetail
	account *advertising.AdAccount
	edit    advertising.ObjectEdit
}

func (t *editAdSetTool) plan(ctx context.Context, cc copilot.Context, args map[string]interface{}) (*adSetEditPlan, error) {
	a, err := validateArgs[editAdSetArgs](nil, cc, args)
	if err != nil {
		return nil, err
	}
	id, err := metaID(a.MetaID)
	if err != nil {
		return nil, err
	}
	detail, err := t.deps.ads.Editor.Detail(ctx, cc.WorkspaceID, id)
	if err != nil {
		return nil, err
	}
	account, err := t.deps.ads.account(ctx, cc, detail.Object.AdAccountID)
	if err != nil {
		return nil, err
	}
	edit, err := a.edit(*detail, account)
	if err != nil {
		return nil, err
	}
	if len(a.Locations) > 0 && edit.Targeting != nil {
		if edit.Targeting.Locations, err = t.deps.ads.Assets.NameLocations(ctx, cc.WorkspaceID, account.ID, edit.Targeting.Locations); err != nil {
			return nil, err
		}
	}
	if _, err := t.deps.ads.Editor.CheckEdit(ctx, cc.WorkspaceID, id, edit); err != nil {
		return nil, err
	}
	return &adSetEditPlan{detail: detail, account: account, edit: edit}, nil
}

func (t *editAdSetTool) Validate(ctx context.Context, cc copilot.Context, args map[string]interface{}) error {
	_, err := t.plan(ctx, cc, args)
	return adsValidation("edit_ad_set", err)
}

func (t *editAdSetTool) Describe(ctx context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	p, err := t.plan(ctx, cc, args)
	if err != nil {
		return []copilot.Field{{Key: "item", Value: "item desconhecido"}}
	}
	return p.fields()
}

func (p *adSetEditPlan) fields() []copilot.Field {
	current, edit, currency := p.detail, p.edit, p.account.Currency
	fields := []copilot.Field{{Key: "item", Value: current.Object.Name}, {Key: "level", Value: levelNames[current.Object.Level]}}
	add := func(key, from, to string) {
		if from != to {
			fields = append(fields, copilot.Field{Key: key, Value: changeText(from, to)})
		}
	}
	if edit.Targeting != nil {
		before := advertising.Targeting{}
		if current.Targeting != nil {
			before = *current.Targeting
		}
		add("locations", locationsText(before.Locations), locationsText(edit.Targeting.Locations))
		add("age", ageText(before), ageText(*edit.Targeting))
		add("genders", gendersText(before.Genders), gendersText(edit.Targeting.Genders))
		add("interests", interestsText(before.Interests), interestsText(edit.Targeting.Interests))
	}
	if edit.Placements != nil {
		add("placements", placementsText(current.Placements), placementsText(edit.Placements))
	}
	if edit.EndAt != nil {
		add("ends", lastDayText(current.Object.EndTime, p.account), lastDayText(edit.EndAt, p.account))
	}
	if edit.Budget != nil {
		add("budget", budgetText(currency, current.Budget), budgetText(currency, edit.Budget))
	}
	if edit.Bid != nil {
		add("bid", bidText(currency, current.Bid), bidText(currency, *edit.Bid))
	}
	return fields
}

func (t *editAdSetTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	p, err := t.plan(ctx, cc, args)
	if err != nil {
		return adsFailure("edit_ad_set", err)
	}
	object, err := t.deps.ads.Editor.Edit(ctx, cc.WorkspaceID, p.detail.Object.MetaID, p.edit)
	if err != nil {
		return adsFailure("edit_ad_set", err)
	}
	changes := map[string]string{}
	for _, f := range p.fields()[2:] {
		changes[f.Key] = f.Value
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"meta_id": object.MetaID, "name": object.Name, "changes": changes}}
}

func locationsText(locations []advertising.GeoLocation) string {
	if len(locations) == 0 {
		return "nenhuma"
	}
	names := make([]string, 0, len(locations))
	for _, l := range locations {
		name := l.Name
		if name == "" {
			name = l.Key
		}
		names = append(names, name)
	}
	return strings.Join(names, ", ")
}

func ageText(t advertising.Targeting) string {
	oldest := strconv.Itoa(t.AgeMax)
	if t.AgeMax == 65 {
		oldest = "65+"
	}
	return strconv.Itoa(t.AgeMin) + " a " + oldest + " anos"
}

func gendersText(values []int) string {
	if len(values) != 1 {
		return "todos"
	}
	if values[0] == advertising.GenderMale {
		return "homens"
	}
	return "mulheres"
}

func interestsText(refs []advertising.TargetRef) string {
	if len(refs) == 0 {
		return "nenhum (a Meta encontra o público)"
	}
	names := make([]string, 0, len(refs))
	for _, r := range refs {
		name := r.Name
		if name == "" {
			name = r.ID
		}
		names = append(names, name)
	}
	return strings.Join(names, ", ")
}

var platformNames = map[string]string{
	advertising.PlatformFacebook:        "Facebook",
	advertising.PlatformInstagram:       "Instagram",
	advertising.PlatformMessenger:       "Messenger",
	advertising.PlatformAudienceNetwork: "Audience Network",
	advertising.PlatformThreads:         "Threads",
}

func placementsText(p *advertising.Placements) string {
	if p == nil || p.Automatic {
		return "automáticos (Advantage+)"
	}
	names := make([]string, 0, len(p.Platforms))
	for _, platform := range p.Platforms {
		name := platformNames[platform]
		if name == "" {
			name = platform
		}
		names = append(names, name)
	}
	return strings.Join(names, ", ")
}

func bidText(currency string, b advertising.Bid) string {
	b = b.Normalized()
	switch b.Strategy {
	case advertising.BidCap:
		return "limite de lance de " + minorText(currency, b.Amount)
	case advertising.BidCostCap:
		return "limite de custo de " + minorText(currency, b.Amount)
	case advertising.BidMinROAS:
		return "ROAS mínimo de " + strings.Replace(strconv.FormatFloat(b.ROASFloor, 'f', -1, 64), ".", ",", 1)
	case advertising.BidLowestCost:
		return "menor custo, sem limite"
	}
	return string(b.Strategy)
}

type spendCapArgs struct {
	AdAccountID string  `json:"ad_account_id" req:"true" desc:"ad_account_id de list_ad_accounts (também aceita o id da Meta ou o nome exato da conta)"`
	Amount      float64 `json:"amount" desc:"novo limite de gasto total da conta na moeda da conta; precisa ser maior que o valor que a conta já gastou"`
	Remove      bool    `json:"remove" desc:"true tira o limite de gasto da conta (sem amount)"`
}

type spendCapPlan struct {
	account *advertising.AdAccount
	cap     *int64
}

type setSpendCapTool struct{ deps adManage }

func (t *setSpendCapTool) Meta() copilot.Meta { return adsMeta(workspace.ActionUpdate, true) }

func (t *setSpendCapTool) Definition() tools.Definition {
	return definition("set_ad_spend_cap",
		"Define ou tira o limite de gasto da conta de anúncios: quando a conta gasta esse total, a Meta para todos os anúncios. "+
			"Só um administrador da conta na Meta pode mudar, e o limite precisa ser maior do que a conta já gastou. Só depois da aprovação do usuário.",
		spendCapArgs{})
}

func (t *setSpendCapTool) plan(ctx context.Context, cc copilot.Context, args map[string]interface{}) (*spendCapPlan, error) {
	a, err := validateArgs[spendCapArgs](nil, cc, args)
	if err != nil {
		return nil, err
	}
	account, err := t.deps.ads.account(ctx, cc, a.AdAccountID)
	if err != nil {
		return nil, err
	}
	if a.Remove {
		if a.Amount != 0 {
			return nil, fmt.Errorf("%w: envie amount para definir o limite ou remove true para tirar, não os dois", errInvalidArgs)
		}
		if _, err := t.deps.SpendCap.CheckSpendCap(ctx, cc.WorkspaceID, account.ID, nil); err != nil {
			return nil, err
		}
		return &spendCapPlan{account: account}, nil
	}
	cap, err := advertising.AmountToMinor(account.Currency, a.Amount)
	if err != nil {
		return nil, fmt.Errorf("%w: amount deve ser maior que zero; para tirar o limite use remove true", errInvalidArgs)
	}
	if _, err := t.deps.SpendCap.CheckSpendCap(ctx, cc.WorkspaceID, account.ID, &cap); err != nil {
		if errors.Is(err, advertising.ErrInvalidBudget) {
			return nil, fmt.Errorf("%w: o limite precisa ser maior do que a conta já gastou (%s)", errInvalidArgs, minorText(account.Currency, account.AmountSpent))
		}
		return nil, err
	}
	return &spendCapPlan{account: account, cap: &cap}, nil
}

func spendCapText(currency string, cap *int64) string {
	if cap == nil {
		return "sem limite"
	}
	return minorText(currency, *cap) + " no total"
}

func (t *setSpendCapTool) Validate(ctx context.Context, cc copilot.Context, args map[string]interface{}) error {
	_, err := t.plan(ctx, cc, args)
	return adsValidation("set_ad_spend_cap", err)
}

func (t *setSpendCapTool) Describe(ctx context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	p, err := t.plan(ctx, cc, args)
	if err != nil {
		return []copilot.Field{{Key: "account", Value: "conta desconhecida"}}
	}
	currency := p.account.Currency
	return []copilot.Field{
		{Key: "account", Value: p.account.Name},
		{Key: "from", Value: spendCapText(currency, p.account.SpendCapLimit())},
		{Key: "to", Value: spendCapText(currency, p.cap)},
		{Key: "spent", Value: minorText(currency, p.account.AmountSpent)},
	}
}

func (t *setSpendCapTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	p, err := t.plan(ctx, cc, args)
	if err != nil {
		return adsFailure("set_ad_spend_cap", err)
	}
	account, err := t.deps.SpendCap.SetSpendCap(ctx, cc.WorkspaceID, p.account.ID, p.cap)
	if err != nil {
		return adsFailure("set_ad_spend_cap", err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{
		"account": account.Name, "spend_cap": spendCapText(p.account.Currency, p.cap),
	}}
}
