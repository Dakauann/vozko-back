package copilottools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"time"

	"vozko/domain/actor"
	"vozko/domain/campaign"
	"vozko/domain/copilot"
	"vozko/domain/crmfilter"
	"vozko/domain/customfield"
	"vozko/domain/lead"
	"vozko/domain/leadaction"
	"vozko/domain/selection"
	"vozko/domain/shared"
	"vozko/domain/tools"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	tmpl "vozko/domain/whatsapp/template"
	"vozko/domain/workspace"
	leadaction_usecase "vozko/usecases/leadaction"
)

const (
	defaultLeadActionLocale = "pt"
	leadActionKeyPrefix     = "elo-"
	cardCountRetention      = 24 * time.Hour
	previewMemoRetention    = 2 * time.Minute
	previewMemoCapacity     = 256
)

var (
	errProposalExpired  = errors.New("lead action: the counted card of this proposal is gone")
	errOutsideOfACard   = errors.New("lead action: no approval card")
	errCardArgsDiffered = errors.New("lead action: the approved arguments differ from the card")
)

type LeadActions interface {
	Preview(ctx context.Context, req leadaction_usecase.Request) (*leadaction.Preview, error)
	PreviewStatus(ctx context.Context, a leadaction_usecase.Actor, id string) (*leadaction.Preview, error)
	Start(ctx context.Context, req leadaction_usecase.Request) (leadaction_usecase.Outcome, error)
}

type ProposalStore interface {
	SetString(key, value string, ttl time.Duration) error
	GetString(key string) (string, error)
	Del(keys ...string) error
}

type LeadTemplates interface {
	Get(workspaceID, id string) (*tmpl.Template, error)
}

type LeadActionDeps struct {
	Leads     LeadDeps
	Actions   LeadActions
	Sends     LeadSends
	Proposals ProposalStore
	Templates LeadTemplates
	Names     actor.Namer
	Now       func() time.Time
}

func (d LeadActionDeps) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

type leadVariableArg struct {
	Source string `json:"source" desc:"literal, lead.first_name, lead.name, lead.nickname, lead.district, lead.city, lead.owner_name ou lead.custom:<chave do campo>"`
	Value  string `json:"value" desc:"o texto fixo, só com source literal"`
}

type prepareLeadActionArgs struct {
	leadFilterArgs
	Action          string            `json:"action" req:"true" enum:"classify,assign_owner,block,export,send_template,send_unofficial" desc:"o que fazer com os leads"`
	LeadIDs         []string          `json:"lead_ids" id:"true" desc:"lead_id exatos de search_leads (até 5000); omita para agir sobre o filtro"`
	Everyone        bool              `json:"everyone" desc:"todos os leads do workspace; só quando o usuário pediu toda a base"`
	Limit           int               `json:"limit" desc:"só os primeiros N do filtro, pela atividade mais recente"`
	FieldKey        string            `json:"field_key" desc:"classify: a chave do campo personalizado de lead"`
	Value           string            `json:"value" desc:"classify: o novo valor; num campo de seleção, uma das opções"`
	Clear           bool              `json:"clear" desc:"classify: limpa o campo em vez de preencher"`
	NewOwnerID      string            `json:"new_owner_id" desc:"assign_owner: member_id de list_assignable_members, me para o próprio usuário ou none para tirar o responsável"`
	Blocked         *bool             `json:"blocked" desc:"block: true bloqueia, false desbloqueia"`
	BusinessPhoneID string            `json:"business_phone_id" id:"true" desc:"block: número oficial para bloquear também no WhatsApp; send_template: o número que envia (list_business_phones)"`
	Addresses       bool              `json:"addresses" desc:"export: inclui o endereço completo; exige ver endereços completos"`
	Name            string            `json:"name" desc:"send_template e send_unofficial: nome da campanha"`
	DepartmentID    string            `json:"department_id" id:"true" desc:"send_template e send_unofficial: departamento da campanha (list_departments), quando o workspace tem departamentos"`
	Split           bool              `json:"split" desc:"send_template e send_unofficial: divide em várias campanhas acima de 150.000 leads"`
	TemplateID      string            `json:"template_id" id:"true" desc:"send_template: template_id de list_templates"`
	NumberID        string            `json:"number_id" id:"true" desc:"send_unofficial: number_id de list_unofficial_numbers"`
	Message         string            `json:"message" desc:"send_unofficial: o texto exato; variáveis {{1}}, {{2}} na ordem de variables"`
	DailyCap        int               `json:"daily_cap" desc:"send_unofficial: máximo de mensagens por dia"`
	Variables       []leadVariableArg `json:"variables" desc:"send_template e send_unofficial: uma ligação por variável, na ordem {{1}}, {{2}}"`
}

type leadActionPlan struct {
	args    prepareLeadActionArgs
	request leadaction_usecase.Request
	field   *customfield.Definition
}

type prepareLeadActionTool struct {
	deps LeadActionDeps
	memo *proposalMemo[*leadaction.Preview]
}

func NewPrepareLeadActionTool(deps LeadActionDeps) copilot.Tool {
	return &prepareLeadActionTool{deps: deps, memo: newProposalMemo(deps.now, (*leadaction.Preview).Clone)}
}

func (t *prepareLeadActionTool) Meta() copilot.Meta {
	return copilot.Meta{Mutating: true, Resource: workspace.ResourceLeads, Action: workspace.ActionRead}
}

func (t *prepareLeadActionTool) Definition() tools.Definition {
	return definition("prepare_lead_action",
		"Propõe uma ação sobre um grupo de leads, escolhido por filtro (os mesmos argumentos de search_leads), por lead_ids ou, "+
			"só quando o usuário pedir, por toda a base (everyone). Nunca receba telefones: use lead_ids ou o filtro. A aprovação "+
			"mostra quantos leads entram, quantos são pulados e por quê, e o custo. classify, assign_owner, block e export acontecem "+
			"na aprovação. send_template e send_unofficial só preparam o disparo (campanhas paradas, com os motivos de pulo); "+
			"para enviar use start_lead_send. Só depois da aprovação do usuário.",
		prepareLeadActionArgs{})
}

func (t *prepareLeadActionTool) plan(ctx context.Context, cc copilot.Context, args map[string]interface{}) (leadActionPlan, error) {
	var a prepareLeadActionArgs
	if err := decodeArgs(args, &a); err != nil {
		return leadActionPlan{}, fmt.Errorf("%w: %v", errInvalidArgs, err)
	}
	if t.deps.Actions == nil || t.deps.Proposals == nil {
		return leadActionPlan{}, errLeadToolUnavailable
	}
	if err := refusePhoneQuery(a.Query); err != nil {
		return leadActionPlan{}, err
	}
	sel, err := t.selection(ctx, cc, a)
	if err != nil {
		return leadActionPlan{}, err
	}
	action := leadaction.Action(a.Action)
	params, field, err := t.params(cc, action, a)
	if err != nil {
		return leadActionPlan{}, err
	}
	return leadActionPlan{args: a, field: field, request: leadaction_usecase.Request{
		Actor:            viewerOf(cc),
		DepartmentID:     selectedDepartment(cc),
		DepartmentFilter: cc.Departments,
		Action:           action,
		Params:           params,
		Selection:        sel,
		Locale:           localeOf(cc),
	}}, nil
}

func localeOf(cc copilot.Context) string {
	if locale := strings.TrimSpace(cc.Locale); locale != "" {
		return locale
	}
	return defaultLeadActionLocale
}

func selectedDepartment(cc copilot.Context) string {
	if cc.Departments == nil || cc.Departments.SelectedDepartmentID == nil {
		return ""
	}
	return *cc.Departments.SelectedDepartmentID
}

const phoneLikeDigits = 8

func refusePhoneQuery(query string) error {
	digits := 0
	for _, r := range query {
		if r >= '0' && r <= '9' {
			digits++
		}
	}
	if digits >= phoneLikeDigits {
		return fmt.Errorf("%w: ações sobre leads nunca recebem telefones; busque com search_leads e passe os lead_ids", errInvalidArgs)
	}
	return nil
}

func (t *prepareLeadActionTool) selection(ctx context.Context, cc copilot.Context, a prepareLeadActionArgs) (selection.Selection, error) {
	ids := shared.DistinctTrimmed(a.LeadIDs)
	if len(ids) > 0 {
		if a.leadFilterArgs.given() || a.Everyone || a.Limit != 0 {
			return selection.Selection{}, fmt.Errorf("%w: passe lead_ids ou um filtro, nunca os dois", errInvalidArgs)
		}
		if len(ids) > selection.MaxExplicitIDs {
			return selection.Selection{}, fmt.Errorf("%w: no máximo %d lead_ids; acima disso use um filtro", errInvalidArgs, selection.MaxExplicitIDs)
		}
		return selection.Selection{Mode: selection.ModeIDs, IDs: ids}, nil
	}
	filter, err := leadFilterOf(ctx, cc, t.deps.Leads, a.leadFilterArgs)
	if err != nil {
		return selection.Selection{}, err
	}
	if a.Limit < 0 || a.Limit > selection.MaxFirstN {
		return selection.Selection{}, fmt.Errorf("%w: limit vai de 1 a %d", errInvalidArgs, selection.MaxFirstN)
	}
	if filter.IsEmpty() {
		if !a.Everyone || a.Limit != 0 {
			return selection.Selection{}, fmt.Errorf("%w: diga quais leads: um filtro, lead_ids ou, só se o usuário pediu toda a base, everyone=true", errInvalidArgs)
		}
		return selection.Selection{Mode: selection.ModeEveryone}, nil
	}
	if a.Everyone {
		return selection.Selection{}, fmt.Errorf("%w: everyone é toda a base; não junte everyone com filtro", errInvalidArgs)
	}
	if a.Limit > 0 {
		return selection.Selection{
			Mode: selection.ModeFirstN, Filter: &filter, Limit: a.Limit,
			Sort: []crmfilter.Sort{{Field: crmfilter.Field(lead.SortLastActivityAt), Desc: true}},
		}, nil
	}
	return selection.Selection{Mode: selection.ModeAllMatching, Filter: &filter}, nil
}

func (t *prepareLeadActionTool) params(cc copilot.Context, action leadaction.Action, a prepareLeadActionArgs) (leadaction.Params, *customfield.Definition, error) {
	switch action {
	case leadaction.ActionClassify:
		return t.classifyParams(cc, a)
	case leadaction.ActionAssignOwner:
		owner := strings.TrimSpace(a.NewOwnerID)
		switch strings.ToLower(owner) {
		case "":
			return leadaction.Params{}, nil, fmt.Errorf("%w: new_owner_id é obrigatório: member_id, me ou none", errInvalidArgs)
		case ownerMe:
			owner = cc.UserID
		case ownerNone:
			owner = ""
		}
		return leadaction.Params{OwnerID: &owner}, nil, nil
	case leadaction.ActionBlock:
		if a.Blocked == nil {
			return leadaction.Params{}, nil, fmt.Errorf("%w: blocked é obrigatório: true bloqueia, false desbloqueia", errInvalidArgs)
		}
		blocked := *a.Blocked
		return leadaction.Params{Blocked: &blocked, BusinessPhoneID: strings.TrimSpace(a.BusinessPhoneID)}, nil, nil
	case leadaction.ActionExport:
		return leadaction.Params{Format: leadaction.ExportFormatCSV, Addresses: a.Addresses}, nil, nil
	case leadaction.ActionSendTemplate, leadaction.ActionSendUnofficial:
		send := leadaction.SendParams{
			Name: strings.TrimSpace(a.Name), DepartmentID: strings.TrimSpace(a.DepartmentID), Split: a.Split, Bindings: bindingsOf(a.Variables),
		}
		if action == leadaction.ActionSendTemplate {
			send.BusinessPhoneID, send.TemplateID = strings.TrimSpace(a.BusinessPhoneID), strings.TrimSpace(a.TemplateID)
		} else {
			send.InstanceID, send.DailyCap = strings.TrimSpace(a.NumberID), a.DailyCap
			if text := strings.TrimSpace(a.Message); text != "" {
				send.Message = &uwc.MessageSpec{Kind: uwc.KindText, Bodies: []string{text}}
			}
		}
		params := leadaction.Params{Send: &send}
		if err := params.Validate(action); err != nil {
			return leadaction.Params{}, nil, fmt.Errorf("%w: faltam dados do disparo (%v)", errInvalidArgs, err)
		}
		return params, nil, nil
	}
	return leadaction.Params{}, nil, fmt.Errorf("%w: action desconhecida", errInvalidArgs)
}

func bindingsOf(variables []leadVariableArg) []campaign.VariableBinding {
	out := make([]campaign.VariableBinding, 0, len(variables))
	for _, v := range variables {
		out = append(out, campaign.VariableBinding{Source: campaign.BindingSource(strings.TrimSpace(v.Source)), Value: strings.TrimSpace(v.Value)})
	}
	return out
}

func (t *prepareLeadActionTool) classifyParams(cc copilot.Context, a prepareLeadActionArgs) (leadaction.Params, *customfield.Definition, error) {
	key := strings.TrimSpace(a.FieldKey)
	if key == "" {
		return leadaction.Params{}, nil, fmt.Errorf("%w: field_key é obrigatório em classify", errInvalidArgs)
	}
	if t.deps.Leads.Definitions == nil {
		return leadaction.Params{}, nil, errLeadToolUnavailable
	}
	defs, err := t.deps.Leads.Definitions.ListByObject(cc.WorkspaceID, customfield.ObjectLead)
	if err != nil {
		return leadaction.Params{}, nil, err
	}
	var def *customfield.Definition
	for _, candidate := range defs {
		if candidate != nil && candidate.Key == key {
			def = candidate
		}
	}
	if def != nil && !customfield.VisibleTo(def, eloFieldViewer) {
		return leadaction.Params{}, nil, fmt.Errorf("%w: %s é um campo sensível, e campos sensíveis nunca passam pela Elo; o usuário classifica pela tela de Leads", errInvalidArgs, key)
	}
	if def == nil {
		return leadaction.Params{}, nil, fmt.Errorf("%w: campo %q desconhecido; campos de lead: %s", errInvalidArgs, key, fieldHintsOf(defs))
	}
	if a.Clear {
		return leadaction.Params{Key: key, Value: json.RawMessage("null")}, def, nil
	}
	value, err := def.CoerceLocal(a.Value)
	if err != nil {
		return leadaction.Params{}, nil, fmt.Errorf("%w: valor %q não serve para o campo %s; campos de lead: %s", errInvalidArgs, a.Value, key, fieldHintsOf(defs))
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return leadaction.Params{}, nil, err
	}
	return leadaction.Params{Key: key, Value: raw}, def, nil
}

func fieldHintsOf(defs []*customfield.Definition) string {
	var hints []string
	for _, def := range defs {
		if !customfield.VisibleTo(def, eloFieldViewer) {
			continue
		}
		if len(def.Options) > 0 {
			hints = append(hints, def.Key+" ("+strings.Join(def.Options, ", ")+")")
			continue
		}
		hints = append(hints, def.Key)
	}
	sort.Strings(hints)
	if len(hints) == 0 {
		return "nenhum"
	}
	return strings.Join(hints, "; ")
}

func (t *prepareLeadActionTool) counted(ctx context.Context, cc copilot.Context, args map[string]interface{}) (leadActionPlan, *leadaction.Preview, error) {
	p, err := t.plan(ctx, cc, args)
	if err != nil {
		return p, nil, err
	}
	key := memoKey(cc, args)
	if preview, ok := t.memo.get(key); ok {
		return p, preview, nil
	}
	preview, err := t.deps.Actions.Preview(ctx, p.request)
	if err != nil {
		return p, nil, err
	}
	if preview == nil {
		return p, nil, errLeadToolUnavailable
	}
	t.memo.put(key, preview)
	return p, preview, nil
}

func (t *prepareLeadActionTool) Validate(ctx context.Context, cc copilot.Context, args map[string]interface{}) error {
	if strings.TrimSpace(cc.ProposalID) == "" {
		return refusalOf(errOutsideOfACard)
	}
	if _, err := t.plan(ctx, cc, args); err != nil {
		return refusalOf(err)
	}
	card, found, err := t.card(cc)
	if err != nil {
		return refusalOf(err)
	}
	if found {
		return t.validateApproval(ctx, cc, args, card)
	}
	p, preview, err := t.counted(ctx, cc, args)
	if err != nil {
		return refusalOf(err)
	}
	if preview.Status == leadaction.PreviewFailed {
		return fmt.Errorf("%w: a contagem falhou (%s)", errInvalidArgs, preview.FailureCode)
	}
	if selectionSize(p.request.Selection, preview) == 0 {
		return fmt.Errorf("%w: nenhum lead nessa seleção; confira o filtro com lead_geo_summary ou search_leads", errInvalidArgs)
	}
	if q := preview.Send; q != nil {
		if q.SplitRequired {
			return fmt.Errorf("%w: são %d leads, acima de %d por campanha; passe split=true para dividir em %d campanhas", errInvalidArgs, q.Count, q.MaxPerCampaign, q.Parts)
		}
		if q.Refusal == campaign.ErrorCode(tmpl.ErrPricingUnavailable) {
			return refusalOf(tmpl.ErrPricingUnavailable)
		}
	}
	return nil
}

func (t *prepareLeadActionTool) validateApproval(ctx context.Context, cc copilot.Context, args map[string]interface{}, card cardRecord) error {
	if card.Args != argsDigest(args) {
		return refusalOf(errCardArgsDiffered)
	}
	if card.PreviewID == "" {
		return nil
	}
	status, err := t.deps.Actions.PreviewStatus(ctx, viewerOf(cc), card.PreviewID)
	if errors.Is(err, leadaction.ErrPreviewNotFound) {
		return nil
	}
	if err != nil {
		return refusalOf(err)
	}
	if status != nil && status.Status == leadaction.PreviewFailed {
		return fmt.Errorf("%w: a contagem do cartão falhou (%s); proponha de novo", errInvalidArgs, status.FailureCode)
	}
	return nil
}

func refusalOf(err error) error {
	if errors.Is(err, errInvalidArgs) {
		return err
	}
	return fmt.Errorf("%w: %s", errInvalidArgs, leadActionFailure(err).Message)
}

func (t *prepareLeadActionTool) Describe(ctx context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	p, preview, err := t.counted(ctx, cc, args)
	if err != nil {
		return []copilot.Field{{Key: "leads", Value: "não foi possível contar os leads"}}
	}
	t.keepCard(cc, args, preview)
	fields := []copilot.Field{
		{Key: "action", Value: t.actionLabel(cc, p)},
		{Key: "leads", Value: selectionLabel(p.request.Selection, selectionSize(p.request.Selection, preview), preview.Result.Matched)},
	}
	if p.request.Action.Runs() {
		partial := ""
		if preview.Status == leadaction.PreviewRunning {
			partial = " (contagem parcial, ainda calculando)"
		}
		fields = append(fields, copilot.Field{Key: "changes", Value: strconv.Itoa(preview.Result.Eligible) + partial})
		if skipped := editSkipLabel(preview.Result.Skipped); skipped != "" {
			fields = append(fields, copilot.Field{Key: "skipped", Value: skipped + partial})
		}
	}
	return append(fields, t.sendFields(cc, p, preview)...)
}

func (t *prepareLeadActionTool) keepCard(cc copilot.Context, args map[string]interface{}, preview *leadaction.Preview) {
	if strings.TrimSpace(cc.ProposalID) == "" {
		return
	}
	raw, err := json.Marshal(cardRecord{
		Expected: preview.Result.ExpectedCount, Fingerprint: preview.Result.Fingerprint, PreviewID: preview.ID, Args: argsDigest(args),
	})
	if err == nil {
		err = t.deps.Proposals.SetString(cardKey(cc), string(raw), cardCountRetention)
	}
	if err != nil {
		log.Printf("[copilot] the counted card of prepare_lead_action was not kept, so its approval will be refused: %v", err)
	}
}

func (t *prepareLeadActionTool) Preview(ctx context.Context, cc copilot.Context, args map[string]interface{}) *copilot.Preview {
	p, preview, err := t.counted(ctx, cc, args)
	if err != nil {
		return nil
	}
	data := leadActionPreview{
		Stage: leadActionStagePrepare, Action: string(p.request.Action), Mode: string(p.request.Selection.Mode), PreviewID: preview.ID,
		Matched: preview.Result.Matched, Selected: selectionSize(p.request.Selection, preview), Eligible: preview.Result.Eligible,
		Partial: preview.Status == leadaction.PreviewRunning, Skipped: map[string]int{}, Quote: preview.Send,
	}
	for reason, n := range preview.Result.Skipped {
		data.Skipped[string(reason)] = n
	}
	return &copilot.Preview{Kind: copilot.PreviewLeadAction, Data: data}
}

func (t *prepareLeadActionTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	if strings.TrimSpace(cc.ProposalID) == "" {
		return leadActionFailure(errOutsideOfACard)
	}
	p, err := t.plan(ctx, cc, args)
	if err != nil {
		return leadActionFailure(err)
	}
	card, err := t.consumeCard(cc, args)
	if err != nil {
		return leadActionFailure(err)
	}
	req := p.request
	req.IdempotencyKey = leadActionKeyPrefix + cc.ProposalID
	if req.Selection.Mode != selection.ModeIDs {
		req.Selection.ExpectedCount, req.Selection.Fingerprint = card.Expected, card.Fingerprint
	}
	outcome, err := t.deps.Actions.Start(ctx, req)
	if err != nil {
		return leadActionFailure(err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: outcomeData(outcome)}
}

func (t *prepareLeadActionTool) card(cc copilot.Context) (cardRecord, bool, error) {
	raw, err := t.deps.Proposals.GetString(cardKey(cc))
	if err != nil {
		return cardRecord{}, false, err
	}
	if strings.TrimSpace(raw) == "" {
		return cardRecord{}, false, nil
	}
	var card cardRecord
	if json.Unmarshal([]byte(raw), &card) != nil || card.Args == "" {
		return cardRecord{}, false, errProposalExpired
	}
	return card, true, nil
}

func (t *prepareLeadActionTool) consumeCard(cc copilot.Context, args map[string]interface{}) (cardRecord, error) {
	card, found, err := t.card(cc)
	if err != nil {
		return cardRecord{}, err
	}
	if !found {
		return cardRecord{}, errProposalExpired
	}
	if card.Args != argsDigest(args) {
		return cardRecord{}, errCardArgsDiffered
	}
	if err := t.deps.Proposals.Del(cardKey(cc)); err != nil {
		return cardRecord{}, err
	}
	return card, nil
}

func outcomeData(o leadaction_usecase.Outcome) map[string]interface{} {
	switch {
	case o.Run != nil:
		return map[string]interface{}{
			"run_id": o.Run.ID, "action": string(o.Run.Action), "status": string(o.Run.Status),
			"matched": o.Run.Result.Matched, "selected": o.Run.Result.Selected,
			"next": "a mudança roda em segundo plano; a tela de Leads mostra o andamento",
		}
	case o.Report != nil:
		return map[string]interface{}{
			"report_id": o.Report.ID, "status": string(o.Report.Status),
			"next": "o arquivo fica pronto em Relatórios; só quem pediu pode baixar",
		}
	case o.Send != nil:
		return sendReviewData(o.Send, "para enviar, start_lead_send com channel e campaign_ids; para descartar, cancel_lead_send")
	}
	return map[string]interface{}{"started": true}
}

func sendReviewData(r *campaign.SendReview, next string) map[string]interface{} {
	ids := make([]string, 0, len(r.Parts))
	parts := make([]map[string]interface{}, 0, len(r.Parts))
	for _, part := range r.Parts {
		ids = append(ids, part.CampaignID)
		parts = append(parts, map[string]interface{}{"campaign_id": part.CampaignID, "name": part.Name, "status": string(part.Status), "eligible": part.Eligible})
	}
	skipped := map[string]int{}
	for reason, n := range r.Skipped {
		skipped[string(reason)] = n
	}
	countedOnly := map[string]int{}
	for reason, n := range r.Counted {
		countedOnly[string(reason)] = n
	}
	data := map[string]interface{}{
		"channel": string(r.Channel), "campaign_ids": ids, "parts": parts, "entries": r.Entries, "eligible": r.Eligible,
		"skipped": skipped, "counted": countedOnly, "started": r.Started, "fits": r.Quote.Fits, "next": next,
	}
	if r.Channel == campaign.ChannelOfficial {
		data["estimated_cost"], data["balance"], data["affordable"] = formatUSD(r.Quote.CostMicros), formatUSD(r.Quote.BalanceMicros), r.Quote.Affordable
	}
	if r.Quote.Refusal != "" {
		data["refusal"] = r.Quote.Refusal
	}
	return data
}
