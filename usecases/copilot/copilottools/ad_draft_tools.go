package copilottools

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"vozko/domain/advertising"
	"vozko/domain/copilot"
	"vozko/domain/tools"
	"vozko/domain/workspace"
	adsuc "vozko/usecases/advertising"
)

type AdDrafts interface {
	Create(ctx context.Context, workspaceID, userID string, content advertising.AdDraft) (*adsuc.DraftView, error)
	List(ctx context.Context, workspaceID, accountID string) (*adsuc.DraftList, error)
	Get(ctx context.Context, workspaceID, id string) (*adsuc.DraftView, error)
	CheckEdit(ctx context.Context, workspaceID, id string, version int) (*adsuc.DraftView, error)
	Update(ctx context.Context, workspaceID, userID, id string, version int, content advertising.AdDraft) (*adsuc.DraftView, error)
	Publish(ctx context.Context, workspaceID, userID, id string, version int, actor advertising.Actor) (*advertising.PublishJob, error)
}

type AdReadiness interface {
	Readiness(ctx context.Context, workspaceID, accountID string) (*adsuc.Readiness, error)
}

type AdTargeting interface {
	Targeting(ctx context.Context, workspaceID, accountID string, kind advertising.TargetingSearchKind, query string) ([]advertising.TargetingOption, error)
	Reach(ctx context.Context, workspaceID, accountID string, t advertising.Targeting, p advertising.Placements, goal advertising.OptimizationGoal) (*advertising.ReachEstimate, error)
}

type AdForms interface {
	List(ctx context.Context, workspaceID, accountID, pageID string) ([]advertising.LeadForm, error)
	Check(ctx context.Context, workspaceID string, draft advertising.LeadFormDraft) (advertising.LeadFormDraft, error)
	Create(ctx context.Context, workspaceID string, draft advertising.LeadFormDraft) (*advertising.LeadForm, error)
}

type AdEditor interface {
	Detail(ctx context.Context, workspaceID, metaID string) (*advertising.ObjectDetail, error)
	CheckEdit(ctx context.Context, workspaceID, metaID string, edit advertising.ObjectEdit) (*advertising.ObjectDetail, error)
	Edit(ctx context.Context, workspaceID, metaID string, edit advertising.ObjectEdit) (*advertising.Object, error)
}

var readinessTitles = map[advertising.ReadinessKey]string{
	advertising.ReadyConnection:     "conexão com a Meta",
	advertising.ReadyRole:           "permissão de anunciante na conta",
	advertising.ReadyAccountStatus:  "conta ativa na Meta",
	advertising.ReadyAccountDetails: "moeda e fuso da conta",
	advertising.ReadyPaymentMethod:  "forma de pagamento",
	advertising.ReadyPage:           "página do Facebook",
	advertising.ReadyPhone:          "telefone verificado na Meta",
	advertising.ReadyEmail:          "e-mail verificado na Meta",
	advertising.ReadyAudienceTerms:  "termos de públicos personalizados",
	advertising.ReadyPixel:          "pixel",
}

var inAppSteps = map[advertising.InAppAction]string{
	advertising.ActionReconnect:   "reconectar a conta na tela Anúncios",
	advertising.ActionSync:        "atualizar os dados da conta na Visão geral da conta",
	advertising.ActionCreatePixel: "criar o pixel na Visão geral da conta",
}

type adReadinessTool struct{ deps AdsDeps }

func NewAdAccountReadinessTool(deps AdsDeps) copilot.Tool { return &adReadinessTool{deps: deps} }

func (t *adReadinessTool) Meta() copilot.Meta { return adsMeta(workspace.ActionRead, false) }

func (t *adReadinessTool) Definition() tools.Definition {
	return definition("ad_account_readiness",
		"O que falta na conta de anúncios para publicar, igual à Visão geral da conta. Mostra ao usuário um cartão com a lista e, em cada item "+
			"que se resolve na Meta, o botão que abre a tela exata da Meta e confere de novo quando ele volta. Não escreva links da Meta por conta "+
			"própria: o cartão já leva ao lugar certo. Depois de chamar, diga em poucas palavras o que falta e que o cartão leva até lá.",
		adAccountArgs{})
}

func (t *adReadinessTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a adAccountArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	account, err := t.deps.account(ctx, cc, a.AdAccountID)
	if err != nil {
		return adsFailure("ad_account_readiness", err)
	}
	r, err := t.deps.Readiness.Readiness(ctx, cc.WorkspaceID, account.ID)
	if err != nil {
		return adsFailure("ad_account_readiness", err)
	}
	blocking := r.Checklist.Blocking()
	items := make([]map[string]interface{}, 0, len(r.Checklist.Items))
	for _, item := range r.Checklist.Items {
		row := map[string]interface{}{"item": readinessTitles[item.Key], "state": string(item.State), "required": item.Required, "solved_at_meta": item.Action.Portal != ""}
		if step := inAppSteps[item.Action.InApp]; step != "" {
			row["in_vozko"] = step
		}
		items = append(items, row)
	}
	return copilot.Result{Status: copilot.StatusOK, Card: copilot.NewAdReadinessCard(account.ID), Data: map[string]interface{}{
		"account": account.Name, "ready_to_publish": len(blocking) == 0, "pending_required": len(blocking), "items": items,
	}}
}

type searchAdInterestsArgs struct {
	AdAccountID string `json:"ad_account_id" req:"true" desc:"ad_account_id de list_ad_accounts (também aceita o id da Meta ou o nome exato da conta)"`
	Query       string `json:"query" req:"true" desc:"assunto em poucas palavras, ex.: marketing digital, academia, CRM"`
}

type searchAdInterestsTool struct{ deps AdsDeps }

func NewSearchAdInterestsTool(deps AdsDeps) copilot.Tool { return &searchAdInterestsTool{deps: deps} }

func (t *searchAdInterestsTool) Meta() copilot.Meta { return adsMeta(workspace.ActionCreate, false) }

func (t *searchAdInterestsTool) Definition() tools.Definition {
	return definition("search_ad_interests",
		"Busca interesses da Meta para o público de um anúncio, com o tamanho aproximado de cada um. Use o campo interest em create_ad ou save_ad_draft. "+
			"Para quem não conhece anúncios, menos é mais: sem interesses a Meta encontra o público sozinha.",
		searchAdInterestsArgs{})
}

func (t *searchAdInterestsTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a searchAdInterestsArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	account, err := t.deps.account(ctx, cc, a.AdAccountID)
	if err != nil {
		return adsFailure("search_ad_interests", err)
	}
	found, err := t.deps.Targeting.Targeting(ctx, cc.WorkspaceID, account.ID, advertising.SearchInterests, a.Query)
	if err != nil {
		return adsFailure("search_ad_interests", err)
	}
	out := make([]map[string]interface{}, 0, len(found))
	for _, o := range found {
		out = append(out, map[string]interface{}{"interest": o.ID + ":" + o.Name, "name": o.Name, "audience_min": o.AudienceMin, "audience_max": o.AudienceMax})
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"interests": out}}
}

type estimateAdAudienceArgs struct {
	AdAccountID string   `json:"ad_account_id" req:"true" desc:"ad_account_id de list_ad_accounts (também aceita o id da Meta ou o nome exato da conta)"`
	Objective   string   `json:"objective" req:"true" enum:"OUTCOME_AWARENESS,OUTCOME_TRAFFIC,OUTCOME_ENGAGEMENT,OUTCOME_LEADS,OUTCOME_SALES" desc:"o mesmo objetivo do anúncio"`
	Destination string   `json:"destination" req:"true" enum:"WHATSAPP,MESSENGER,INSTAGRAM_DIRECT,WEBSITE,ON_AD,NONE" desc:"o mesmo destino do anúncio"`
	Locations   []string `json:"locations" req:"true" desc:"location de search_ad_locations"`
	AgeMin      int      `json:"age_min" desc:"idade mínima (padrão 18)"`
	AgeMax      int      `json:"age_max" desc:"idade máxima (padrão 65)"`
	Genders     []string `json:"genders" desc:"male e ou female; vazio para todos"`
	Interests   []string `json:"interests" desc:"interest de search_ad_interests"`
}

type estimateAdAudienceTool struct{ deps AdsDeps }

func NewEstimateAdAudienceTool(deps AdsDeps) copilot.Tool { return &estimateAdAudienceTool{deps: deps} }

func (t *estimateAdAudienceTool) Meta() copilot.Meta { return adsMeta(workspace.ActionCreate, false) }

func (t *estimateAdAudienceTool) Definition() tools.Definition {
	return definition("estimate_ad_audience",
		"Estima com a Meta quantas pessoas o público alcança antes de criar o anúncio. Use para avisar quando o público é pequeno demais ou amplo demais.",
		estimateAdAudienceArgs{})
}

func (t *estimateAdAudienceTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a estimateAdAudienceArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	account, err := t.deps.account(ctx, cc, a.AdAccountID)
	if err != nil {
		return adsFailure("estimate_ad_audience", err)
	}
	goal, ok := advertising.Objective(a.Objective).DefaultGoal(advertising.Destination(a.Destination))
	if !ok {
		return copilot.Result{Status: copilot.StatusError, Message: fmt.Sprintf("o objetivo %s não leva para %s", a.Objective, a.Destination)}
	}
	locations, err := parseLocations(a.Locations)
	if err != nil {
		return adsFailure("estimate_ad_audience", err)
	}
	interests, err := parseTargetRefs(a.Interests)
	if err != nil {
		return adsFailure("estimate_ad_audience", err)
	}
	targeting := advertising.Targeting{Locations: locations, AgeMin: a.AgeMin, AgeMax: a.AgeMax, Genders: genders(a.Genders), Interests: interests}
	estimate, err := t.deps.Targeting.Reach(ctx, cc.WorkspaceID, account.ID, targeting, advertising.Placements{Automatic: true}, goal)
	if err != nil {
		return adsFailure("estimate_ad_audience", err)
	}
	if !estimate.Ready {
		return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"estimate_ready": false, "message": "a Meta ainda não tem estimativa para esse público"}}
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"estimate_ready": true, "people_min": estimate.Lower, "people_max": estimate.Upper}}
}

type listLeadFormsArgs struct {
	AdAccountID string `json:"ad_account_id" req:"true" desc:"ad_account_id de list_ad_accounts (também aceita o id da Meta ou o nome exato da conta)"`
	PageID      string `json:"page_id" req:"true" desc:"page_id exato de list_ad_pages"`
}

type listLeadFormsTool struct{ deps AdsDeps }

func NewListLeadFormsTool(deps AdsDeps) copilot.Tool { return &listLeadFormsTool{deps: deps} }

func (t *listLeadFormsTool) Meta() copilot.Meta { return adsMeta(workspace.ActionRead, false) }

func (t *listLeadFormsTool) Definition() tools.Definition {
	return definition("list_lead_forms",
		"Lista os formulários instantâneos de cadastro de uma página, para anúncios com destino ON_AD. Sem formulário ativo, o usuário cria um na tela Formulários e leads.",
		listLeadFormsArgs{})
}

func (t *listLeadFormsTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a listLeadFormsArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	account, err := t.deps.account(ctx, cc, a.AdAccountID)
	if err != nil {
		return adsFailure("list_lead_forms", err)
	}
	forms, err := t.deps.Forms.List(ctx, cc.WorkspaceID, account.ID, strings.TrimSpace(a.PageID))
	if err != nil {
		return adsFailure("list_lead_forms", err)
	}
	out := make([]map[string]interface{}, 0, len(forms))
	for _, f := range forms {
		out = append(out, map[string]interface{}{"lead_form_id": f.MetaID, "name": f.Name, "status": string(f.Status), "leads": f.LeadsCount})
	}
	var card *copilot.ActionCard
	if len(out) == 0 {
		card, _ = copilot.NewNavigationCard(workspace.ScreenAdsForms, "")
	}
	return copilot.Result{Status: copilot.StatusOK, Card: card, Data: map[string]interface{}{"forms": out}}
}

type createLeadFormArgs struct {
	AdAccountID        string   `json:"ad_account_id" req:"true" desc:"ad_account_id de list_ad_accounts (também aceita o id da Meta ou o nome exato da conta)"`
	PageID             string   `json:"page_id" req:"true" desc:"page_id exato de list_ad_pages, com lead_terms_accepted true"`
	Name               string   `json:"name" req:"true" desc:"nome interno do formulário"`
	Questions          []string `json:"questions" req:"true" desc:"campos pedidos à pessoa, na ordem: FULL_NAME, EMAIL, PHONE, CITY, STATE, COMPANY_NAME, JOB_TITLE"`
	IntroTitle         string   `json:"intro_title" req:"true" desc:"título no topo do formulário"`
	IntroText          string   `json:"intro_text" req:"true" desc:"uma ou duas frases sobre o que a pessoa recebe ao se cadastrar"`
	PrivacyURL         string   `json:"privacy_url" req:"true" desc:"link https da política de privacidade da empresa (a Meta exige); peça ao usuário, nunca invente"`
	ThankYouTitle      string   `json:"thank_you_title" req:"true" desc:"título da tela final"`
	ThankYouBody       string   `json:"thank_you_body" desc:"texto da tela final"`
	ThankYouURL        string   `json:"thank_you_url" req:"true" desc:"link https aberto pelo botão da tela final, ex.: o site da empresa"`
	ThankYouButtonText string   `json:"thank_you_button_text" req:"true" desc:"texto do botão da tela final, até 60 caracteres"`
}

var leadFormQuestions = map[string]advertising.QuestionType{
	"FULL_NAME": advertising.QuestionFullName, "EMAIL": advertising.QuestionEmail, "PHONE": advertising.QuestionPhone,
	"CITY": advertising.QuestionCity, "STATE": advertising.QuestionState, "COMPANY_NAME": advertising.QuestionCompany,
	"JOB_TITLE": advertising.QuestionJobTitle,
}

func (a createLeadFormArgs) draft(account *advertising.AdAccount) (advertising.LeadFormDraft, error) {
	questions := make([]advertising.FormQuestion, 0, len(a.Questions))
	for _, raw := range a.Questions {
		kind, ok := leadFormQuestions[strings.ToUpper(strings.TrimSpace(raw))]
		if !ok {
			return advertising.LeadFormDraft{}, fmt.Errorf("%w: pergunta %q desconhecida", errInvalidArgs, raw)
		}
		questions = append(questions, advertising.FormQuestion{Type: kind})
	}
	return advertising.LeadFormDraft{
		AdAccountID: account.ID, PageID: strings.TrimSpace(a.PageID), Name: a.Name,
		Intro:     &advertising.FormIntro{Title: a.IntroTitle, Style: advertising.IntroParagraph, Content: []string{a.IntroText}},
		Questions: questions, PrivacyURL: strings.TrimSpace(a.PrivacyURL),
		ThankYouTitle: a.ThankYouTitle, ThankYouBody: a.ThankYouBody, ThankYouURL: strings.TrimSpace(a.ThankYouURL), ThankYouButtonText: a.ThankYouButtonText,
	}, nil
}

type createLeadFormTool struct{ deps AdsDeps }

func NewCreateLeadFormTool(deps AdsDeps) copilot.Tool { return &createLeadFormTool{deps: deps} }

func (t *createLeadFormTool) Meta() copilot.Meta { return adsMeta(workspace.ActionCreate, true) }

func (t *createLeadFormTool) Definition() tools.Definition {
	return definition("create_lead_form",
		"Cria na Meta um formulário instantâneo de cadastro para anúncios com destino ON_AD. A página precisa ter aceitado os termos de cadastros "+
			"(lead_terms_accepted em list_ad_pages); se não aceitou, leve o usuário com open_screen ads_forms, onde ele aceita e o Vozko confere. "+
			"Os cadastros chegam ao CRM do Vozko. Só depois da aprovação do usuário.",
		createLeadFormArgs{})
}

func (t *createLeadFormTool) plan(ctx context.Context, cc copilot.Context, args map[string]interface{}) (advertising.LeadFormDraft, error) {
	a, err := validateArgs[createLeadFormArgs](nil, cc, args)
	if err != nil {
		return advertising.LeadFormDraft{}, err
	}
	account, err := t.deps.account(ctx, cc, a.AdAccountID)
	if err != nil {
		return advertising.LeadFormDraft{}, err
	}
	draft, err := a.draft(account)
	if err != nil {
		return advertising.LeadFormDraft{}, err
	}
	return t.deps.Forms.Check(ctx, cc.WorkspaceID, draft)
}

func (t *createLeadFormTool) Validate(ctx context.Context, cc copilot.Context, args map[string]interface{}) error {
	_, err := t.plan(ctx, cc, args)
	return adsValidation("create_lead_form", err)
}

func (t *createLeadFormTool) Describe(ctx context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	d, err := t.plan(ctx, cc, args)
	if err != nil {
		return []copilot.Field{{Key: "form", Value: "formulário com campos a corrigir"}}
	}
	asked := make([]string, 0, len(d.Questions))
	for _, q := range d.Questions {
		asked = append(asked, string(q.Type))
	}
	return []copilot.Field{
		{Key: "name", Value: d.Name},
		{Key: "questions", Value: strings.Join(asked, ", ")},
		{Key: "privacy", Value: d.PrivacyURL},
		{Key: "thankYou", Value: d.ThankYouTitle + " (" + d.ThankYouButtonText + ": " + d.ThankYouURL + ")"},
	}
}

func (t *createLeadFormTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	d, err := t.plan(ctx, cc, args)
	if err != nil {
		return adsFailure("create_lead_form", err)
	}
	form, err := t.deps.Forms.Create(ctx, cc.WorkspaceID, d)
	if err != nil {
		return adsFailure("create_lead_form", err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"lead_form_id": form.MetaID, "name": form.Name}}
}

type saveAdDraftTool struct{ deps AdsDeps }

func NewSaveAdDraftTool(deps AdsDeps) copilot.Tool { return &saveAdDraftTool{deps: deps} }

func (t *saveAdDraftTool) Meta() copilot.Meta { return adsMeta(workspace.ActionCreate, true) }

func (t *saveAdDraftTool) Definition() tools.Definition {
	return adDraftDefinition("save_ad_draft",
		"Monta o anúncio completo e guarda como rascunho no Vozko, sem publicar e sem cobrar nada. Funciona mesmo quando a conta ainda não pode gastar "+
			"(sem forma de pagamento): o rascunho aparece em Campanhas como Em rascunho e o usuário publica depois em Conferir e publicar, ou pede para "+
			"você com publish_ad_draft. Recebe os mesmos campos de create_ad; para mudar depois, use update_ad_draft. Só depois da aprovação do usuário.",
		adDraftArgs{})
}

func (t *saveAdDraftTool) check(ctx context.Context, cc copilot.Context, args map[string]interface{}) (*adsuc.Preflight, error) {
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
	return t.deps.Publish.Check(ctx, cc.WorkspaceID, draft)
}

func (t *saveAdDraftTool) Validate(ctx context.Context, cc copilot.Context, args map[string]interface{}) error {
	_, err := t.check(ctx, cc, args)
	return adsValidation("save_ad_draft", err)
}

func (t *saveAdDraftTool) Describe(ctx context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	pre, err := t.check(ctx, cc, args)
	if err != nil {
		return []copilot.Field{{Key: "ad", Value: "anúncio com campos a corrigir"}}
	}
	return append(draftFields(pre), copilot.Field{Key: "draft", Value: "fica como rascunho em Campanhas; nada é publicado nem cobrado agora"})
}

func (t *saveAdDraftTool) Preview(ctx context.Context, cc copilot.Context, args map[string]interface{}) *copilot.Preview {
	pre, err := t.check(ctx, cc, args)
	if err != nil {
		return nil
	}
	return &copilot.Preview{Kind: PreviewAdCreative, Data: creativePreview(pre, pre.Draft.Ads[0].Creative)}
}

func (t *saveAdDraftTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	pre, err := t.check(ctx, cc, args)
	if err != nil {
		return adsFailure("save_ad_draft", err)
	}
	view, err := t.deps.Drafts.Create(ctx, cc.WorkspaceID, cc.UserID, pre.Draft)
	if err != nil {
		return adsFailure("save_ad_draft", err)
	}
	card, _ := copilot.NewNavigationCard(workspace.ScreenAdsManager, "")
	return copilot.Result{Status: copilot.StatusOK, Card: card, Data: map[string]interface{}{
		"draft_id": view.Draft.ID, "version": view.Draft.Version, "can_publish_now": pre.Account.CanSpend() == nil,
	}}
}

type listAdDraftsTool struct{ deps AdsDeps }

func NewListAdDraftsTool(deps AdsDeps) copilot.Tool { return &listAdDraftsTool{deps: deps} }

func (t *listAdDraftsTool) Meta() copilot.Meta { return adsMeta(workspace.ActionRead, false) }

func (t *listAdDraftsTool) Definition() tools.Definition {
	return definition("list_ad_drafts",
		"Lista os rascunhos de anúncio de uma conta, com o estado (editing, publishing, failed), a version atual e o erro da última publicação, se houver.",
		adAccountArgs{})
}

func (t *listAdDraftsTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a adAccountArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	account, err := t.deps.account(ctx, cc, a.AdAccountID)
	if err != nil {
		return adsFailure("list_ad_drafts", err)
	}
	list, err := t.deps.Drafts.List(ctx, cc.WorkspaceID, account.ID)
	if err != nil {
		return adsFailure("list_ad_drafts", err)
	}
	out := make([]map[string]interface{}, 0, len(list.Drafts))
	for _, v := range list.Drafts {
		row := map[string]interface{}{
			"draft_id": v.Draft.ID, "version": v.Draft.Version, "campaign": v.Draft.Content.Campaign.Name, "state": string(v.State),
			"ads": len(v.Draft.Content.Ads), "updated_at": v.Draft.UpdatedAt.Format("2006-01-02 15:04"),
		}
		if v.Job != nil && v.Job.ErrorMessage != "" {
			row["last_error"] = v.Job.ErrorMessage
		}
		out = append(out, row)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"drafts": out}}
}

type adDraftIDArgs struct {
	DraftID string `json:"draft_id" req:"true" id:"true" desc:"draft_id de list_ad_drafts ou save_ad_draft"`
}

type adDraftVersionArgs struct {
	adDraftIDArgs
	Version int `json:"version" req:"true" desc:"version do rascunho lida em get_ad_draft, list_ad_drafts ou save_ad_draft; se o rascunho mudou depois disso, nada muda"`
}

type publishAdDraftTool struct{ deps AdsDeps }

func NewPublishAdDraftTool(deps AdsDeps) copilot.Tool { return &publishAdDraftTool{deps: deps} }

func (t *publishAdDraftTool) Meta() copilot.Meta { return adsMeta(workspace.ActionCreate, true) }

func (t *publishAdDraftTool) Definition() tools.Definition {
	return definition("publish_ad_draft",
		"Publica na Meta um rascunho salvo, exatamente na version mostrada ao usuário; se o rascunho mudou depois, nada é publicado. "+
			"Exige a conta pronta para gastar; se faltar algo, use ad_account_readiness para mandar o link. "+
			"Cobra a taxa por anúncio publicado do saldo; o gasto com a Meta sai da conta de anúncios. Só depois da aprovação do usuário.",
		adDraftVersionArgs{})
}

func (t *publishAdDraftTool) preflight(ctx context.Context, cc copilot.Context, args map[string]interface{}) (*adsuc.DraftView, *adsuc.Preflight, error) {
	a, err := validateArgs[adDraftVersionArgs](nil, cc, args)
	if err != nil {
		return nil, nil, err
	}
	view, err := t.deps.Drafts.CheckEdit(ctx, cc.WorkspaceID, a.DraftID, a.Version)
	if err != nil {
		return nil, nil, err
	}
	pre, err := t.deps.Publish.Preflight(ctx, cc.WorkspaceID, view.Draft.Content)
	if err != nil {
		return nil, nil, err
	}
	return view, pre, nil
}

func (t *publishAdDraftTool) Validate(ctx context.Context, cc copilot.Context, args map[string]interface{}) error {
	_, _, err := t.preflight(ctx, cc, args)
	return adsValidation("publish_ad_draft", err)
}

func (t *publishAdDraftTool) Describe(ctx context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	_, pre, err := t.preflight(ctx, cc, args)
	if err != nil {
		return []copilot.Field{{Key: "draft", Value: "rascunho com campos a corrigir"}}
	}
	return draftFields(pre)
}

func (t *publishAdDraftTool) Preview(ctx context.Context, cc copilot.Context, args map[string]interface{}) *copilot.Preview {
	_, pre, err := t.preflight(ctx, cc, args)
	if err != nil {
		return nil
	}
	return &copilot.Preview{Kind: PreviewAdCreative, Data: creativePreview(pre, pre.Draft.Ads[0].Creative)}
}

func (t *publishAdDraftTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	view, _, err := t.preflight(ctx, cc, args)
	if err != nil {
		return adsFailure("publish_ad_draft", err)
	}
	job, err := t.deps.Drafts.Publish(ctx, cc.WorkspaceID, cc.UserID, view.Draft.ID, view.Draft.Version, advertising.ActorAssistant)
	if err != nil {
		return adsFailure("publish_ad_draft", err)
	}
	return publishResult(job)
}

type editAdTextArgs struct {
	MetaID string `json:"meta_id" req:"true" desc:"meta_id exato de ads_results (um anúncio; para name também campanha ou conjunto)"`
	Field  string `json:"field" req:"true" enum:"name,primaryText,headline,description,link" desc:"o que mudar"`
	Value  string `json:"value" desc:"o novo texto; vazio limpa título, descrição ou link"`
}

func (a editAdTextArgs) change() advertising.BulkChange {
	return advertising.BulkChange{Field: advertising.BulkField(a.Field), Mode: advertising.BulkSet, Value: a.Value}
}

type editAdTextTool struct{ deps AdsDeps }

func NewEditAdTextTool(deps AdsDeps) copilot.Tool { return &editAdTextTool{deps: deps} }

func (t *editAdTextTool) Meta() copilot.Meta { return adsMeta(workspace.ActionUpdate, true) }

func (t *editAdTextTool) Definition() tools.Definition {
	return definition("edit_ad_text",
		"Muda o nome de uma campanha, conjunto ou anúncio, ou o texto principal, o título, a descrição ou o link de um anúncio publicado. "+
			"Texto novo passa de novo pela revisão da Meta. Só depois da aprovação do usuário.",
		editAdTextArgs{})
}

type adTextPlan struct {
	id      string
	change  advertising.BulkChange
	object  *advertising.Object
	current string
}

func (t *editAdTextTool) plan(ctx context.Context, cc copilot.Context, args map[string]interface{}) (*adTextPlan, error) {
	a, err := validateArgs[editAdTextArgs](nil, cc, args)
	if err != nil {
		return nil, err
	}
	id, err := metaID(a.MetaID)
	if err != nil {
		return nil, err
	}
	change := a.change()
	object, err := single(t.deps.Bulk.CheckEdit(ctx, cc.WorkspaceID, []string{id}, change))
	if err != nil {
		return nil, err
	}
	detail, err := t.deps.Editor.Detail(ctx, cc.WorkspaceID, id)
	if err != nil {
		return nil, err
	}
	return &adTextPlan{id: id, change: change, object: object, current: change.Current(*detail)}, nil
}

func quotedOrEmpty(value string) string {
	if strings.TrimSpace(value) == "" {
		return "vazio"
	}
	return "\"" + value + "\""
}

func single(results []adsuc.BulkResult, err error) (*advertising.Object, error) {
	if err != nil {
		return nil, err
	}
	if len(results) != 1 {
		return nil, advertising.ErrObjectNotFound
	}
	return results[0].Object, results[0].Err
}

func (t *editAdTextTool) Validate(ctx context.Context, cc copilot.Context, args map[string]interface{}) error {
	_, err := t.plan(ctx, cc, args)
	return adsValidation("edit_ad_text", err)
}

func (t *editAdTextTool) Describe(ctx context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	p, err := t.plan(ctx, cc, args)
	if err != nil {
		return []copilot.Field{{Key: "item", Value: "item desconhecido"}}
	}
	return []copilot.Field{
		{Key: "item", Value: p.object.Name},
		{Key: "level", Value: levelNames[p.object.Level]},
		{Key: "field", Value: bulkFieldNames[p.change.Field]},
		{Key: "from", Value: quotedOrEmpty(p.current)},
		{Key: "change", Value: bulkChangeText(p.change)},
	}
}

func (t *editAdTextTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	p, err := t.plan(ctx, cc, args)
	if err != nil {
		return adsFailure("edit_ad_text", err)
	}
	object, err := single(t.deps.Bulk.Edit(ctx, cc.WorkspaceID, []string{p.id}, p.change))
	if err != nil {
		return adsFailure("edit_ad_text", err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"meta_id": object.MetaID, "name": object.Name}}
}

type getAdDraftTool struct{ deps AdsDeps }

func NewGetAdDraftTool(deps AdsDeps) copilot.Tool { return &getAdDraftTool{deps: deps} }

func (t *getAdDraftTool) Meta() copilot.Meta { return adsMeta(workspace.ActionRead, false) }

func (t *getAdDraftTool) Definition() tools.Definition {
	return definition("get_ad_draft",
		"Lê um rascunho de anúncio salvo: o estado, a version e as configurações atuais com os mesmos nomes de campo de save_ad_draft. "+
			"Use antes de update_ad_draft ou publish_ad_draft para mostrar ao usuário o que está lá e mudar só o que ele pedir.",
		adDraftIDArgs{})
}

func (t *getAdDraftTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a adDraftIDArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	view, err := t.deps.Drafts.Get(ctx, cc.WorkspaceID, a.DraftID)
	if err != nil {
		return adsFailure("get_ad_draft", err)
	}
	account, err := t.deps.account(ctx, cc, view.Draft.AdAccountID)
	if err != nil {
		return adsFailure("get_ad_draft", err)
	}
	content := view.Draft.Content
	data := map[string]interface{}{
		"draft_id": view.Draft.ID, "version": view.Draft.Version, "state": string(view.State), "ads": len(content.Ads),
		"settings": compactArgs(draftArgs(content, account)),
	}
	if len(content.Ads) > 1 {
		data["note"] = "rascunho com vários anúncios: os criativos só mudam na tela do editor; os demais campos mudam com update_ad_draft"
	}
	if view.Job != nil && view.Job.ErrorMessage != "" {
		data["last_error"] = view.Job.ErrorMessage
	}
	return copilot.Result{Status: copilot.StatusOK, Data: data}
}

func compactArgs(a adDraftArgs) map[string]interface{} {
	raw, err := json.Marshal(a)
	if err != nil {
		return map[string]interface{}{}
	}
	var all map[string]interface{}
	if err := json.Unmarshal(raw, &all); err != nil {
		return map[string]interface{}{}
	}
	out := make(map[string]interface{}, len(all))
	for key, value := range all {
		if !emptyValue(value) {
			out[key] = value
		}
	}
	return out
}

func emptyValue(v interface{}) bool {
	switch value := v.(type) {
	case nil:
		return true
	case string:
		return value == ""
	case float64:
		return value == 0
	case bool:
		return !value
	case []interface{}:
		return len(value) == 0
	}
	return false
}

type updateAdDraftArgs struct {
	adDraftVersionArgs
	adDraftArgs
}

type updateAdDraftTool struct{ deps AdsDeps }

func NewUpdateAdDraftTool(deps AdsDeps) copilot.Tool { return &updateAdDraftTool{deps: deps} }

func (t *updateAdDraftTool) Meta() copilot.Meta { return adsMeta(workspace.ActionCreate, true) }

func (t *updateAdDraftTool) Definition() tools.Definition {
	def := adDraftDefinition("update_ad_draft",
		"Muda um rascunho de anúncio salvo, sem publicar e sem cobrar nada. Passe draft_id, a version lida em get_ad_draft e só os campos que mudam, "+
			"com os mesmos nomes de save_ad_draft; os demais ficam como estão. Se o rascunho mudou depois da leitura, nada muda. "+
			"Listas como locations, interests ou cards são trocadas inteiras. "+
			"A conta do rascunho não muda. Só depois da aprovação do usuário.",
		updateAdDraftArgs{})
	delete(def.Parameters, "ad_account_id")
	def.Required = []string{"draft_id", "version"}
	return def
}

type draftEdit struct {
	view    *adsuc.DraftView
	pre     *adsuc.Preflight
	changed []string
}

func (t *updateAdDraftTool) plan(ctx context.Context, cc copilot.Context, args map[string]interface{}) (*draftEdit, error) {
	ref, err := validateArgs[adDraftVersionArgs](nil, cc, args)
	if err != nil {
		return nil, err
	}
	changes, err := draftChanges(args)
	if err != nil {
		return nil, err
	}
	view, err := t.deps.Drafts.CheckEdit(ctx, cc.WorkspaceID, ref.DraftID, ref.Version)
	if err != nil {
		return nil, err
	}
	account, err := t.deps.account(ctx, cc, view.Draft.AdAccountID)
	if err != nil {
		return nil, err
	}
	content, err := patchDraft(view.Draft.Content, account, changes)
	if err != nil {
		return nil, err
	}
	if _, touched := changes["locations"]; touched {
		if content, err = t.deps.namedDraft(ctx, cc, content); err != nil {
			return nil, err
		}
	}
	pre, err := t.deps.Publish.Check(ctx, cc.WorkspaceID, content)
	if err != nil {
		return nil, err
	}
	changed := make([]string, 0, len(changes))
	for key := range changes {
		changed = append(changed, key)
	}
	sort.Strings(changed)
	return &draftEdit{view: view, pre: pre, changed: changed}, nil
}

func draftChanges(args map[string]interface{}) (map[string]interface{}, error) {
	allowed := boundFields(reflect.TypeOf(adDraftArgs{}))
	changes := make(map[string]interface{}, len(args))
	for key, value := range args {
		switch _, known := allowed[key]; {
		case key == "draft_id", key == "version":
		case key == "ad_account_id":
			return nil, fmt.Errorf("%w: a conta de um rascunho não muda; salve um rascunho novo na outra conta", errInvalidArgs)
		case !known:
			return nil, fmt.Errorf("%w: campo %q desconhecido; use os nomes de get_ad_draft", errInvalidArgs, key)
		default:
			changes[key] = value
		}
	}
	if len(changes) == 0 {
		return nil, fmt.Errorf("%w: diga o que mudar: passe draft_id, version e só os campos novos", errInvalidArgs)
	}
	return changes, nil
}

func (t *updateAdDraftTool) Validate(ctx context.Context, cc copilot.Context, args map[string]interface{}) error {
	_, err := t.plan(ctx, cc, args)
	return adsValidation("update_ad_draft", err)
}

func (t *updateAdDraftTool) Describe(ctx context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	edit, err := t.plan(ctx, cc, args)
	if err != nil {
		return []copilot.Field{{Key: "draft", Value: "rascunho com campos a corrigir"}}
	}
	return append(draftFields(edit.pre),
		copilot.Field{Key: "changes", Value: strings.Join(edit.changed, ", ")},
		copilot.Field{Key: "draft", Value: "continua como rascunho em Campanhas; nada é publicado nem cobrado agora"})
}

func (t *updateAdDraftTool) Preview(ctx context.Context, cc copilot.Context, args map[string]interface{}) *copilot.Preview {
	edit, err := t.plan(ctx, cc, args)
	if err != nil {
		return nil
	}
	return &copilot.Preview{Kind: PreviewAdCreative, Data: creativePreview(edit.pre, edit.pre.Draft.Ads[0].Creative)}
}

func (t *updateAdDraftTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	edit, err := t.plan(ctx, cc, args)
	if err != nil {
		return adsFailure("update_ad_draft", err)
	}
	saved := edit.view.Draft
	view, err := t.deps.Drafts.Update(ctx, cc.WorkspaceID, cc.UserID, saved.ID, saved.Version, edit.pre.Draft)
	if err != nil {
		return adsFailure("update_ad_draft", err)
	}
	card, _ := copilot.NewNavigationCard(workspace.ScreenAdsManager, "")
	return copilot.Result{Status: copilot.StatusOK, Card: card, Data: map[string]interface{}{
		"draft_id": view.Draft.ID, "version": view.Draft.Version, "changed": edit.changed, "can_publish_now": edit.pre.Account.CanSpend() == nil,
	}}
}

const publishInProgress = "a publicação foi aceita, cobrada uma vez e continua em segundo plano; diga ao usuário que ela aparece em Anúncios em instantes e nunca proponha publicar de novo"

func publishResult(job *advertising.PublishJob) copilot.Result {
	data := map[string]interface{}{"publish_status": string(job.Status), "campaign_meta_id": job.CampaignID()}
	if id, ok := job.Progress.Ads[0]; ok {
		data["ad_meta_id"] = id
	}
	if job.ErrorMessage != "" {
		data["message"] = job.ErrorMessage
	} else if !job.Status.Terminal() {
		data["message"] = publishInProgress
	}
	status := copilot.StatusOK
	if job.Status == advertising.JobFailed {
		status = copilot.StatusError
	}
	return copilot.Result{Status: status, Data: data}
}
