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
	"vozko/domain/crmfilter"
	"vozko/domain/leadaction"
	"vozko/domain/tools"
	"vozko/domain/workspace"
	adsuc "vozko/usecases/advertising"
)

type AdAudiences interface {
	List(ctx context.Context, workspaceID, accountID string) (*adsuc.AudienceList, error)
	CheckCustomerList(ctx context.Context, a adsuc.Requester, draft advertising.CustomerListDraft) (*adsuc.CustomerListResult, error)
	CreateCustomerList(ctx context.Context, a adsuc.Requester, draft advertising.CustomerListDraft) (*adsuc.CustomerListResult, error)
	CheckLookalike(ctx context.Context, workspaceID string, draft advertising.LookalikeDraft) (advertising.LookalikeDraft, *advertising.Audience, error)
	CreateLookalike(ctx context.Context, workspaceID string, draft advertising.LookalikeDraft) (*advertising.Audience, error)
	Delete(ctx context.Context, workspaceID, accountID, audienceID string) error
	SavedList(ctx context.Context, workspaceID string) ([]*advertising.SavedAudience, error)
	Save(ctx context.Context, workspaceID, userID string, s advertising.SavedAudience) (*advertising.SavedAudience, error)
	DeleteSaved(ctx context.Context, workspaceID, id string) error
}

type AdGrowthDeps struct {
	Audiences   AdAudiences
	Rules       AdRules
	Tests       AdSplitTests
	Conversions AdConversions
	Pixels      AdPixels
	Now         func() time.Time
}

func (d AdGrowthDeps) now() time.Time {
	if d.Now == nil {
		return time.Now()
	}
	return d.Now()
}

type adGrowth struct {
	deps AdGrowthDeps
	ads  AdsDeps
}

func growthFailure(tool, accountID string, err error) copilot.Result {
	result := adsFailure(tool, err)
	if errors.Is(err, advertising.ErrAudienceTermsNotAccepted) && accountID != "" {
		result.Card = copilot.NewAdReadinessCard(accountID)
	}
	return result
}

func (g adGrowth) audienceList(ctx context.Context, cc copilot.Context, accountID string) (*advertising.AdAccount, *adsuc.AudienceList, error) {
	account, err := g.ads.account(ctx, cc, accountID)
	if err != nil {
		return nil, nil, err
	}
	list, err := g.deps.Audiences.List(ctx, cc.WorkspaceID, account.ID)
	if err != nil {
		return nil, nil, err
	}
	return account, list, nil
}

func findAudience(list *adsuc.AudienceList, raw string) (*advertising.Audience, error) {
	id := strings.TrimSpace(raw)
	for i := range list.Audiences {
		if list.Audiences[i].MetaID == id {
			return &list.Audiences[i], nil
		}
	}
	return nil, advertising.ErrAudienceNotFound
}

func audienceRefs(list *adsuc.AudienceList, raw []string) ([]advertising.TargetRef, error) {
	refs := make([]advertising.TargetRef, 0, len(raw))
	for _, id := range raw {
		audience, err := findAudience(list, id)
		if err != nil {
			return nil, err
		}
		refs = append(refs, advertising.TargetRef{ID: audience.MetaID, Name: audience.Name})
	}
	return refs, nil
}

func approxSize(n int64) interface{} {
	if n < 0 {
		return nil
	}
	return n
}

func audienceRow(a advertising.Audience) map[string]interface{} {
	row := map[string]interface{}{
		"audience_id": a.MetaID, "name": a.Name, "kind": string(a.Kind), "ready": a.Ready(),
		"size_min": approxSize(a.ApproxLower), "size_max": approxSize(a.ApproxUpper),
	}
	if a.DeliveryDescription != "" {
		row["status"] = a.DeliveryDescription
	}
	if a.Kind == advertising.AudienceLookalike {
		row["source_audience_id"], row["country"], row["percent"] = a.OriginAudienceID, a.LookalikeCountry, int(a.LookalikeRatio*100+0.5)
	}
	return row
}

func savedAudienceRow(s *advertising.SavedAudience) map[string]interface{} {
	locations := make([]string, 0, len(s.Targeting.Locations))
	for _, l := range s.Targeting.Locations {
		locations = append(locations, string(l.Kind)+":"+l.Key)
	}
	return map[string]interface{}{
		"saved_audience_id": s.ID, "name": s.Name, "locations": locations,
		"age_min": s.Targeting.AgeMin, "age_max": s.Targeting.AgeMax,
		"interests": len(s.Targeting.Interests), "custom_audiences": len(s.Targeting.CustomAudiences),
	}
}

type listAdAudiencesTool struct{ adGrowth }

func (t *listAdAudiencesTool) Meta() copilot.Meta { return adsMeta(workspace.ActionRead, false) }

func (t *listAdAudiencesTool) Definition() tools.Definition {
	return definition("list_ad_audiences",
		"Lista os públicos personalizados e semelhantes da conta na Meta (tamanho aproximado e se já pode ser usado) e os públicos salvos do Vozko. "+
			"O audience_id de cada público é o que create_ad e save_ad_draft aceitam em custom_audiences. Diz também se a conta aceitou os termos "+
			"de públicos personalizados da Meta (terms_accepted); sem eles nenhum público de clientes pode ser criado.",
		adAccountArgs{})
}

func (t *listAdAudiencesTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a adAccountArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	account, list, err := t.audienceList(ctx, cc, a.AdAccountID)
	if err != nil {
		return growthFailure("list_ad_audiences", "", err)
	}
	saved, err := t.deps.Audiences.SavedList(ctx, cc.WorkspaceID)
	if err != nil {
		return growthFailure("list_ad_audiences", account.ID, err)
	}
	audiences := make([]map[string]interface{}, 0, len(list.Audiences))
	for _, audience := range list.Audiences {
		audiences = append(audiences, audienceRow(audience))
	}
	savedRows := make([]map[string]interface{}, 0, len(saved))
	for _, s := range saved {
		savedRows = append(savedRows, savedAudienceRow(s))
	}
	result := copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{
		"terms_accepted": list.TermsAccepted, "audiences": audiences, "saved_audiences": savedRows,
	}}
	if !list.TermsAccepted {
		result.Card = copilot.NewAdReadinessCard(account.ID)
	}
	return result
}

type customerListArgs struct {
	AdAccountID  string   `json:"ad_account_id" req:"true" desc:"ad_account_id de list_ad_accounts (também aceita o id da Meta ou o nome exato da conta)"`
	Name         string   `json:"name" req:"true" desc:"nome do público na Meta"`
	Description  string   `json:"description" desc:"descrição curta opcional, até 100 caracteres"`
	StageIDs     []string `json:"stage_ids" id:"true" desc:"stage_id de list_pipelines: só contatos nessas etapas"`
	LabelIDs     []string `json:"label_ids" id:"true" desc:"label_id de list_labels: só contatos com essas etiquetas"`
	CreatedAfter string   `json:"created_after" desc:"só contatos criados a partir de YYYY-MM-DD"`
	ActiveAfter  string   `json:"active_after" desc:"só contatos com atividade a partir de YYYY-MM-DD"`
}

func (a customerListArgs) filter() (crmfilter.Filter, error) {
	var filter crmfilter.Filter
	if len(a.StageIDs) > 0 {
		filter.Groups = append(filter.Groups, predicate(crmfilter.FieldStage, crmfilter.OpIn, a.StageIDs...))
	}
	if len(a.LabelIDs) > 0 {
		filter.Groups = append(filter.Groups, predicate(crmfilter.FieldLabel, crmfilter.OpIn, a.LabelIDs...))
	}
	if day := strings.TrimSpace(a.CreatedAfter); day != "" {
		filter.Groups = append(filter.Groups, predicate(crmfilter.FieldCreatedAt, crmfilter.OpAfter, day))
	}
	if day := strings.TrimSpace(a.ActiveAfter); day != "" {
		filter.Groups = append(filter.Groups, predicate(crmfilter.FieldLastActivityAt, crmfilter.OpAfter, day))
	}
	if err := filter.Validate(); err != nil {
		return crmfilter.Filter{}, fmt.Errorf("%w: filtro de contatos inválido (%v); datas em YYYY-MM-DD", errInvalidArgs, err)
	}
	return filter, nil
}

type customerListPlan struct {
	account *advertising.AdAccount
	draft   advertising.CustomerListDraft
	checked *adsuc.CustomerListResult
}

type createCustomerListAudienceTool struct{ adGrowth }

func (t *createCustomerListAudienceTool) Meta() copilot.Meta {
	return adsMeta(workspace.ActionCreate, true)
}

func (t *createCustomerListAudienceTool) AlsoRequires() []workspace.PermissionEntry {
	entries, ok := workspace.CapabilityRequires(leadaction.CapabilityMetaAudience)
	if !ok {
		return []workspace.PermissionEntry{{Resource: workspace.Resource(leadaction.CapabilityMetaAudience)}}
	}
	return entries
}

func (t *createCustomerListAudienceTool) Definition() tools.Definition {
	return definition("create_customer_list_audience",
		"Cria na Meta um público personalizado com os contatos do CRM, filtrados por etapa, etiqueta e datas. Os telefones e nomes saem do Vozko "+
			"criptografados (hash), como a Meta exige; nenhum dado pessoal aparece aqui, só quantos contatos entram. Exige que a conta tenha aceitado "+
			"os termos de públicos personalizados da Meta: se não aceitou, chame ad_account_readiness. A Meta leva algumas horas para calcular o "+
			"tamanho. Só depois da aprovação do usuário.",
		customerListArgs{})
}

func (t *createCustomerListAudienceTool) plan(ctx context.Context, cc copilot.Context, args map[string]interface{}) (*customerListPlan, error) {
	a, err := validateArgs[customerListArgs](nil, cc, args)
	if err != nil {
		return nil, err
	}
	filter, err := a.filter()
	if err != nil {
		return nil, err
	}
	account, err := t.ads.account(ctx, cc, a.AdAccountID)
	if err != nil {
		return nil, err
	}
	draft := advertising.CustomerListDraft{
		AdAccountID: account.ID, Name: a.Name, Description: strings.TrimSpace(a.Description),
		Source: advertising.SourceCRM, CRMFilter: filter,
	}
	checked, err := t.deps.Audiences.CheckCustomerList(ctx, audienceRequester(cc), draft)
	if err != nil {
		return nil, err
	}
	return &customerListPlan{account: account, draft: draft, checked: checked}, nil
}

func (t *createCustomerListAudienceTool) Validate(ctx context.Context, cc copilot.Context, args map[string]interface{}) error {
	_, err := t.plan(ctx, cc, args)
	return adsValidation("create_customer_list_audience", err)
}

func (t *createCustomerListAudienceTool) Describe(ctx context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	p, err := t.plan(ctx, cc, args)
	if err != nil {
		return []copilot.Field{{Key: "audience", Value: "público com campos a corrigir"}}
	}
	return []copilot.Field{
		{Key: "account", Value: p.account.Name},
		{Key: "audience", Value: p.checked.Audience.Name},
		{Key: "contacts", Value: strconv.Itoa(p.checked.Matched) + " contatos do CRM, enviados criptografados"},
	}
}

func (t *createCustomerListAudienceTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	p, err := t.plan(ctx, cc, args)
	if err != nil {
		return growthFailure("create_customer_list_audience", argAccount(args), err)
	}
	created, err := t.deps.Audiences.CreateCustomerList(ctx, audienceRequester(cc), p.draft)
	if err != nil {
		return growthFailure("create_customer_list_audience", p.account.ID, err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{
		"audience_id": created.Audience.MetaID, "name": created.Audience.Name,
		"contacts_sent": created.Matched, "contacts_without_phone": created.Skipped,
	}}
}

func audienceRequester(cc copilot.Context) adsuc.Requester {
	return adsuc.Requester{WorkspaceID: cc.WorkspaceID, UserID: cc.UserID, IsAdmin: cc.SystemAdmin}
}

func argAccount(args map[string]interface{}) string {
	id, _ := args["ad_account_id"].(string)
	return strings.TrimSpace(id)
}

type lookalikeArgs struct {
	AdAccountID      string `json:"ad_account_id" req:"true" desc:"ad_account_id de list_ad_accounts (também aceita o id da Meta ou o nome exato da conta)"`
	Name             string `json:"name" req:"true" desc:"nome do público semelhante"`
	SourceAudienceID string `json:"source_audience_id" req:"true" desc:"audience_id exato de list_ad_audiences, o público de origem (de preferência com 1.000 pessoas ou mais)"`
	Country          string `json:"country" desc:"país de duas letras, ex.: BR (padrão)"`
	Percent          int    `json:"percent" req:"true" desc:"tamanho de 1 a 10: 1 é o mais parecido com a origem, 10 o mais amplo"`
}

type lookalikePlan struct {
	account *advertising.AdAccount
	source  *advertising.Audience
	draft   advertising.LookalikeDraft
}

type createLookalikeAudienceTool struct{ adGrowth }

func (t *createLookalikeAudienceTool) Meta() copilot.Meta {
	return adsMeta(workspace.ActionCreate, true)
}

func (t *createLookalikeAudienceTool) Definition() tools.Definition {
	return definition("create_lookalike_audience",
		"Cria na Meta um público semelhante a um público existente da conta, num país. Exige os termos de públicos personalizados aceitos. "+
			"Só depois da aprovação do usuário.",
		lookalikeArgs{})
}

func (t *createLookalikeAudienceTool) plan(ctx context.Context, cc copilot.Context, args map[string]interface{}) (*lookalikePlan, error) {
	a, err := validateArgs[lookalikeArgs](nil, cc, args)
	if err != nil {
		return nil, err
	}
	account, err := t.ads.account(ctx, cc, a.AdAccountID)
	if err != nil {
		return nil, err
	}
	draft := advertising.LookalikeDraft{AdAccountID: account.ID, Name: a.Name, OriginAudienceID: strings.TrimSpace(a.SourceAudienceID), Percent: a.Percent, Country: a.Country}
	checked, source, err := t.deps.Audiences.CheckLookalike(ctx, cc.WorkspaceID, draft)
	if err != nil {
		return nil, err
	}
	return &lookalikePlan{account: account, source: source, draft: checked}, nil
}

func (t *createLookalikeAudienceTool) Validate(ctx context.Context, cc copilot.Context, args map[string]interface{}) error {
	_, err := t.plan(ctx, cc, args)
	return adsValidation("create_lookalike_audience", err)
}

func (t *createLookalikeAudienceTool) Describe(ctx context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	p, err := t.plan(ctx, cc, args)
	if err != nil {
		return []copilot.Field{{Key: "audience", Value: "público com campos a corrigir"}}
	}
	return []copilot.Field{
		{Key: "account", Value: p.account.Name},
		{Key: "audience", Value: p.draft.Name},
		{Key: "source", Value: p.source.Name},
		{Key: "country", Value: p.draft.Country},
		{Key: "size", Value: strconv.Itoa(p.draft.Percent) + "%"},
	}
}

func (t *createLookalikeAudienceTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	p, err := t.plan(ctx, cc, args)
	if err != nil {
		return growthFailure("create_lookalike_audience", argAccount(args), err)
	}
	created, err := t.deps.Audiences.CreateLookalike(ctx, cc.WorkspaceID, p.draft)
	if err != nil {
		return growthFailure("create_lookalike_audience", p.account.ID, err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"audience_id": created.MetaID, "name": created.Name}}
}

type savedAudienceArgs struct {
	AdAccountID             string   `json:"ad_account_id" req:"true" desc:"ad_account_id de list_ad_accounts (também aceita o id da Meta ou o nome exato da conta)"`
	Name                    string   `json:"name" req:"true" desc:"nome do público salvo"`
	Locations               []string `json:"locations" req:"true" desc:"location de search_ad_locations, ex.: country:BR"`
	AgeMin                  int      `json:"age_min" desc:"idade mínima, 13 a 65 (padrão 18)"`
	AgeMax                  int      `json:"age_max" desc:"idade máxima, 13 a 65, 65 significa 65+ (padrão 65)"`
	Genders                 []string `json:"genders" desc:"male e ou female; vazio para todos"`
	Interests               []string `json:"interests" desc:"interest de search_ad_interests"`
	CustomAudiences         []string `json:"custom_audiences" desc:"audience_id de list_ad_audiences para incluir"`
	ExcludedCustomAudiences []string `json:"excluded_custom_audiences" desc:"audience_id de list_ad_audiences para excluir"`
}

type savedAudiencePlan struct {
	account  *advertising.AdAccount
	audience advertising.SavedAudience
}

type createSavedAudienceTool struct{ adGrowth }

func (t *createSavedAudienceTool) Meta() copilot.Meta { return adsMeta(workspace.ActionCreate, true) }

func (t *createSavedAudienceTool) Definition() tools.Definition {
	return definition("create_saved_audience",
		"Guarda no Vozko um público pronto (lugares, idade, gênero, interesses e públicos personalizados) para reaproveitar ao criar anúncios. "+
			"Não muda nada na Meta. Só depois da aprovação do usuário.",
		savedAudienceArgs{})
}

func (t *createSavedAudienceTool) plan(ctx context.Context, cc copilot.Context, args map[string]interface{}) (*savedAudiencePlan, error) {
	a, err := validateArgs[savedAudienceArgs](nil, cc, args)
	if err != nil {
		return nil, err
	}
	locations, err := parseLocations(a.Locations)
	if err != nil {
		return nil, err
	}
	interests, err := parseTargetRefs(a.Interests)
	if err != nil {
		return nil, err
	}
	account, list, err := t.audienceList(ctx, cc, a.AdAccountID)
	if err != nil {
		return nil, err
	}
	if locations, err = t.ads.Assets.NameLocations(ctx, cc.WorkspaceID, account.ID, locations); err != nil {
		return nil, err
	}
	included, err := audienceRefs(list, a.CustomAudiences)
	if err != nil {
		return nil, err
	}
	excluded, err := audienceRefs(list, a.ExcludedCustomAudiences)
	if err != nil {
		return nil, err
	}
	s := advertising.SavedAudience{Name: a.Name, Targeting: advertising.Targeting{
		Locations: locations, AgeMin: a.AgeMin, AgeMax: a.AgeMax, Genders: genders(a.Genders), Interests: interests,
		CustomAudiences: included, ExcludedCustomAudiences: excluded,
	}}
	s.Normalize()
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return &savedAudiencePlan{account: account, audience: s}, nil
}

func (t *createSavedAudienceTool) Validate(ctx context.Context, cc copilot.Context, args map[string]interface{}) error {
	_, err := t.plan(ctx, cc, args)
	return adsValidation("create_saved_audience", err)
}

func (t *createSavedAudienceTool) Describe(ctx context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	p, err := t.plan(ctx, cc, args)
	if err != nil {
		return []copilot.Field{{Key: "audience", Value: "público com campos a corrigir"}}
	}
	target := p.audience.Targeting
	fields := []copilot.Field{
		{Key: "audience", Value: p.audience.Name},
		{Key: "locations", Value: strconv.Itoa(len(target.Locations)) + " lugares"},
		{Key: "ages", Value: strconv.Itoa(target.AgeMin) + " a " + strconv.Itoa(target.AgeMax)},
	}
	if names := refNames(target.CustomAudiences); names != "" {
		fields = append(fields, copilot.Field{Key: "includes", Value: names})
	}
	if names := refNames(target.ExcludedCustomAudiences); names != "" {
		fields = append(fields, copilot.Field{Key: "excludes", Value: names})
	}
	return fields
}

func refNames(refs []advertising.TargetRef) string {
	names := make([]string, 0, len(refs))
	for _, r := range refs {
		names = append(names, r.Name)
	}
	return strings.Join(names, ", ")
}

func (t *createSavedAudienceTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	p, err := t.plan(ctx, cc, args)
	if err != nil {
		return growthFailure("create_saved_audience", argAccount(args), err)
	}
	saved, err := t.deps.Audiences.Save(ctx, cc.WorkspaceID, cc.UserID, p.audience)
	if err != nil {
		return growthFailure("create_saved_audience", p.account.ID, err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"saved_audience_id": saved.ID, "name": saved.Name}}
}

type deleteAdAudienceArgs struct {
	AdAccountID     string `json:"ad_account_id" req:"true" desc:"ad_account_id de list_ad_accounts (também aceita o id da Meta ou o nome exato da conta)"`
	AudienceID      string `json:"audience_id" desc:"audience_id de list_ad_audiences, para apagar um público da Meta"`
	SavedAudienceID string `json:"saved_audience_id" id:"true" desc:"saved_audience_id de list_ad_audiences, para apagar um público salvo do Vozko"`
}

type audienceRemoval struct {
	account *advertising.AdAccount
	metaID  string
	savedID string
	name    string
}

type deleteAdAudienceTool struct{ adGrowth }

func (t *deleteAdAudienceTool) Meta() copilot.Meta { return adsMeta(workspace.ActionDelete, true) }

func (t *deleteAdAudienceTool) Definition() tools.Definition {
	return definition("delete_ad_audience",
		"Apaga um público personalizado ou semelhante da Meta (audience_id) ou um público salvo do Vozko (saved_audience_id), um por vez. "+
			"Anúncios que usam o público da Meta deixam de alcançá-lo. Só depois da aprovação do usuário.",
		deleteAdAudienceArgs{})
}

func (t *deleteAdAudienceTool) plan(ctx context.Context, cc copilot.Context, args map[string]interface{}) (*audienceRemoval, error) {
	a, err := validateArgs[deleteAdAudienceArgs](nil, cc, args)
	if err != nil {
		return nil, err
	}
	metaAudience, savedAudience := strings.TrimSpace(a.AudienceID), strings.TrimSpace(a.SavedAudienceID)
	if (metaAudience == "") == (savedAudience == "") {
		return nil, fmt.Errorf("%w: informe audience_id ou saved_audience_id, um dos dois", errInvalidArgs)
	}
	account, list, err := t.audienceList(ctx, cc, a.AdAccountID)
	if err != nil {
		return nil, err
	}
	if metaAudience != "" {
		audience, err := findAudience(list, metaAudience)
		if err != nil {
			return nil, err
		}
		return &audienceRemoval{account: account, metaID: audience.MetaID, name: audience.Name}, nil
	}
	saved, err := t.deps.Audiences.SavedList(ctx, cc.WorkspaceID)
	if err != nil {
		return nil, err
	}
	for _, s := range saved {
		if s.ID == savedAudience {
			return &audienceRemoval{account: account, savedID: s.ID, name: s.Name}, nil
		}
	}
	return nil, advertising.ErrSavedAudienceNotFound
}

func (t *deleteAdAudienceTool) Validate(ctx context.Context, cc copilot.Context, args map[string]interface{}) error {
	_, err := t.plan(ctx, cc, args)
	return adsValidation("delete_ad_audience", err)
}

func (t *deleteAdAudienceTool) Describe(ctx context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	p, err := t.plan(ctx, cc, args)
	if err != nil {
		return []copilot.Field{{Key: "audience", Value: "público desconhecido"}}
	}
	where := "público da Meta"
	if p.savedID != "" {
		where = "público salvo do Vozko"
	}
	return []copilot.Field{{Key: "audience", Value: p.name}, {Key: "kind", Value: where}}
}

func (t *deleteAdAudienceTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	p, err := t.plan(ctx, cc, args)
	if err != nil {
		return growthFailure("delete_ad_audience", argAccount(args), err)
	}
	if p.savedID != "" {
		err = t.deps.Audiences.DeleteSaved(ctx, cc.WorkspaceID, p.savedID)
	} else {
		err = t.deps.Audiences.Delete(ctx, cc.WorkspaceID, p.account.ID, p.metaID)
	}
	if err != nil {
		return growthFailure("delete_ad_audience", p.account.ID, err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"deleted": p.name}}
}

func AdGrowthTools(deps AdGrowthDeps, ads AdsDeps) []copilot.Tool {
	g := adGrowth{deps: deps, ads: ads}
	return []copilot.Tool{
		&listAdAudiencesTool{g}, &createCustomerListAudienceTool{g}, &createLookalikeAudienceTool{g},
		&createSavedAudienceTool{g}, &deleteAdAudienceTool{g},
		&listAdRulesTool{g}, &createAdRuleTool{g}, &setAdRuleStatusTool{g}, &deleteAdRuleTool{g}, &adRuleHistoryTool{g},
		&listAdTestsTool{g}, &createAdTestTool{g},
		&getAdConversionSettingsTool{g}, &saveAdConversionSettingsTool{g}, &connectAdDatasetTool{g},
		&listAdPixelsTool{g}, &createAdPixelTool{g}, &recentAdConversionsTool{g},
	}
}
