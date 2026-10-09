package copilottools

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"

	"vozko/domain/campaign"
	"vozko/domain/copilot"
	"vozko/domain/customfield"
	"vozko/domain/lead"
	"vozko/domain/leadaction"
	"vozko/domain/leadarea"
	"vozko/domain/selection"
	"vozko/domain/shared"
	"vozko/domain/tools"
	tmpl "vozko/domain/whatsapp/template"
	"vozko/domain/workspace"
	leadsend_usecase "vozko/usecases/leadsend"
)

const (
	leadActionStagePrepare = "prepare"
	leadActionStageStart   = "start"
	leadActionStageCancel  = "cancel"
)

type LeadSends interface {
	Review(ctx context.Context, req leadsend_usecase.SendRequest) (*campaign.SendReview, error)
	Start(ctx context.Context, req leadsend_usecase.SendRequest) (*campaign.SendReview, error)
	Cancel(ctx context.Context, req leadsend_usecase.SendRequest) error
}

type leadActionPreview struct {
	Stage     string              `json:"stage"`
	Action    string              `json:"action"`
	Mode      string              `json:"mode,omitempty"`
	PreviewID string              `json:"previewId,omitempty"`
	Matched   int                 `json:"matched"`
	Selected  int                 `json:"selected"`
	Eligible  int                 `json:"eligible"`
	Partial   bool                `json:"partial"`
	Skipped   map[string]int      `json:"skipped"`
	Counted   map[string]int      `json:"counted,omitempty"`
	Quote     *campaign.SendQuote `json:"quote,omitempty"`
	Parts     []campaign.SendPart `json:"parts,omitempty"`
}

var sendSkipNames = map[campaign.SkipReason]string{
	campaign.SkipBlocked:                  "bloqueados",
	campaign.SkipOptedOut:                 "não querem receber mensagens",
	campaign.SkipNoIdentity:               "sem WhatsApp",
	campaign.SkipCooldown:                 "receberam mensagem há pouco (proteção contra spam)",
	campaign.SkipMissingVariable:          "sem dado para uma variável",
	campaign.SkipAlreadyInRunningCampaign: "já estão em outro envio em andamento",
	campaign.SkipOverCap:                  "acima do limite",
}

var sendCountedNames = map[campaign.CountedReason]string{
	campaign.CountedWindowOpen:        "com janela aberta (a Vozko cobra o modelo mesmo assim)",
	campaign.CountedNoConsentRecorded: "sem consentimento registrado",
}

type leadSendArgs struct {
	Channel     string   `json:"channel" req:"true" enum:"official,unofficial" desc:"o channel devolvido por prepare_lead_action"`
	CampaignIDs []string `json:"campaign_ids" req:"true" id:"true" desc:"os campaign_ids devolvidos por prepare_lead_action"`
}

type startLeadSendArgs struct {
	leadSendArgs
	FirstN int `json:"first_n" desc:"envia só para os primeiros N elegíveis, quando o saldo ou o limite mensal não cobre todos"`
}

func (a leadSendArgs) request(cc copilot.Context) leadsend_usecase.SendRequest {
	return leadsend_usecase.SendRequest{
		Actor: viewerOf(cc), Departments: cc.Departments, Channel: campaign.Channel(a.Channel), CampaignIDs: shared.DistinctTrimmed(a.CampaignIDs),
	}
}

type sendReviews struct {
	sends LeadSends
	memo  *proposalMemo[*campaign.SendReview]
}

func newSendReviews(deps LeadActionDeps) sendReviews {
	return sendReviews{sends: deps.Sends, memo: newProposalMemo(deps.now, (*campaign.SendReview).Clone)}
}

func (r sendReviews) of(ctx context.Context, cc copilot.Context, args map[string]interface{}, a leadSendArgs) (*campaign.SendReview, error) {
	if r.sends == nil {
		return nil, errLeadToolUnavailable
	}
	key := memoKey(cc, args)
	if review, ok := r.memo.get(key); ok {
		return review, nil
	}
	review, err := r.sends.Review(ctx, a.request(cc))
	if err != nil {
		return nil, err
	}
	if review == nil {
		return nil, errLeadToolUnavailable
	}
	r.memo.put(key, review)
	return review, nil
}

type startLeadSendTool struct {
	deps    LeadActionDeps
	reviews sendReviews
}

func NewStartLeadSendTool(deps LeadActionDeps) copilot.Tool {
	return &startLeadSendTool{deps: deps, reviews: newSendReviews(deps)}
}

func (t *startLeadSendTool) Meta() copilot.Meta {
	return copilot.Meta{Mutating: true, Resource: workspace.ResourceLeads, Action: workspace.ActionRead}
}

func (t *startLeadSendTool) Definition() tools.Definition {
	return definition("start_lead_send",
		"Inicia o envio de um disparo preparado por prepare_lead_action. A aprovação mostra quem recebe, quem foi pulado e por quê, "+
			"o custo final e o saldo. Nunca inicie sem o usuário pedir explicitamente. Só depois da aprovação do usuário.",
		startLeadSendArgs{})
}

func (t *startLeadSendTool) reviewed(ctx context.Context, cc copilot.Context, args map[string]interface{}) (startLeadSendArgs, *campaign.SendReview, error) {
	var a startLeadSendArgs
	if err := decodeArgs(args, &a); err != nil {
		return a, nil, fmt.Errorf("%w: %v", errInvalidArgs, err)
	}
	review, err := t.reviews.of(ctx, cc, args, a.leadSendArgs)
	return a, review, err
}

func (t *startLeadSendTool) allowed(ctx context.Context, cc copilot.Context, args map[string]interface{}) (*campaign.SendReview, int, error) {
	a, review, err := t.reviewed(ctx, cc, args)
	if err != nil {
		return nil, 0, err
	}
	if review.Started {
		return nil, 0, fmt.Errorf("%w: esse disparo já foi iniciado; acompanhe em campanhas", errInvalidArgs)
	}
	recipients, err := review.Budget(review.Eligible).Allow(a.FirstN)
	switch {
	case errors.Is(err, campaign.ErrNothingEligible):
		return nil, 0, fmt.Errorf("%w: ninguém elegível nesse disparo; descarte com cancel_lead_send", errInvalidArgs)
	case errors.Is(err, campaign.ErrFirstNInvalid):
		return nil, 0, fmt.Errorf("%w: first_n vai de 1 a %d", errInvalidArgs, review.Eligible)
	case err != nil:
		return nil, 0, err
	}
	return review, recipients, nil
}

func finalCost(r *campaign.SendReview, recipients int) (tmpl.SendCost, error) {
	return tmpl.Quote(r.Quote.UnitPriceMicros, int64(recipients), r.Quote.BalanceMicros, r.Quote.Currency)
}

func (t *startLeadSendTool) Validate(ctx context.Context, cc copilot.Context, args map[string]interface{}) error {
	review, recipients, err := t.allowed(ctx, cc, args)
	if err != nil {
		return refusalOf(err)
	}
	if review.Channel != campaign.ChannelOfficial {
		return nil
	}
	if _, err := finalCost(review, recipients); err != nil {
		return refusalOf(err)
	}
	return nil
}

func (t *startLeadSendTool) Describe(ctx context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	review, recipients, err := t.allowed(ctx, cc, args)
	if err != nil {
		return []copilot.Field{{Key: "campaign", Value: "disparo que não pode ser iniciado: " + refusalOf(err).Error()}}
	}
	fields := []copilot.Field{
		{Key: "campaign", Value: partNames(review.Parts)},
		{Key: "recipients", Value: strconv.Itoa(recipients)},
	}
	if skipped := sendSkipLabel(review.Skipped); skipped != "" {
		fields = append(fields, copilot.Field{Key: "skipped", Value: skipped})
	}
	if countedOnly := sendCountedLabel(review.Counted); countedOnly != "" {
		fields = append(fields, copilot.Field{Key: "counted", Value: countedOnly})
	}
	if review.Channel == campaign.ChannelOfficial {
		cost, err := finalCost(review, recipients)
		if err != nil {
			return append(fields, copilot.Field{Key: "finalCost", Value: "não foi possível calcular: " + refusalOf(err).Error()})
		}
		fields = append(fields,
			copilot.Field{Key: "finalCost", Value: formatUSD(cost.CostMicros)},
			copilot.Field{Key: "balance", Value: formatUSD(cost.BalanceMicros)},
		)
	}
	if review.Quote.DailyCap > 0 {
		fields = append(fields,
			copilot.Field{Key: "dailyCap", Value: strconv.Itoa(review.Quote.DailyCap)},
			copilot.Field{Key: "estimatedDays", Value: strconv.Itoa(review.Quote.EstimatedDays)},
		)
	}
	return fields
}

func (t *startLeadSendTool) Preview(ctx context.Context, cc copilot.Context, args map[string]interface{}) *copilot.Preview {
	review, recipients, err := t.allowed(ctx, cc, args)
	if err != nil {
		return nil
	}
	return reviewPreview(leadActionStageStart, review, recipients)
}

func (t *startLeadSendTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a startLeadSendArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	if t.deps.Sends == nil {
		return leadActionFailure(errLeadToolUnavailable)
	}
	req := a.request(cc)
	req.FirstN = a.FirstN
	review, err := t.deps.Sends.Start(ctx, req)
	if err != nil {
		return leadActionFailure(err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: sendReviewData(review, "o envio começou e segue no ritmo das campanhas; acompanhe em campanhas")}
}

type cancelLeadSendTool struct {
	deps    LeadActionDeps
	reviews sendReviews
}

func NewCancelLeadSendTool(deps LeadActionDeps) copilot.Tool {
	return &cancelLeadSendTool{deps: deps, reviews: newSendReviews(deps)}
}

func (t *cancelLeadSendTool) Meta() copilot.Meta {
	return copilot.Meta{Mutating: true, Resource: workspace.ResourceLeads, Action: workspace.ActionRead}
}

func (t *cancelLeadSendTool) Definition() tools.Definition {
	return definition("cancel_lead_send",
		"Descarta um disparo preparado por prepare_lead_action e ainda não iniciado: apaga as campanhas paradas. Só depois da aprovação do usuário.",
		leadSendArgs{})
}

func (t *cancelLeadSendTool) reviewed(ctx context.Context, cc copilot.Context, args map[string]interface{}) (leadSendArgs, *campaign.SendReview, error) {
	var a leadSendArgs
	if err := decodeArgs(args, &a); err != nil {
		return a, nil, fmt.Errorf("%w: %v", errInvalidArgs, err)
	}
	review, err := t.reviews.of(ctx, cc, args, a)
	return a, review, err
}

func (t *cancelLeadSendTool) Validate(ctx context.Context, cc copilot.Context, args map[string]interface{}) error {
	_, review, err := t.reviewed(ctx, cc, args)
	if err != nil {
		return refusalOf(err)
	}
	if review.Started {
		return fmt.Errorf("%w: esse disparo já foi iniciado e não pode ser descartado; pause pela tela de campanhas", errInvalidArgs)
	}
	return nil
}

func (t *cancelLeadSendTool) Describe(ctx context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	_, review, err := t.reviewed(ctx, cc, args)
	if err != nil {
		return []copilot.Field{{Key: "campaign", Value: "disparo desconhecido ou sem acesso"}}
	}
	return []copilot.Field{
		{Key: "campaign", Value: partNames(review.Parts)},
		{Key: "entries", Value: strconv.Itoa(review.Entries)},
	}
}

func (t *cancelLeadSendTool) Preview(ctx context.Context, cc copilot.Context, args map[string]interface{}) *copilot.Preview {
	_, review, err := t.reviewed(ctx, cc, args)
	if err != nil {
		return nil
	}
	return reviewPreview(leadActionStageCancel, review, review.Eligible)
}

func (t *cancelLeadSendTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a leadSendArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	if t.deps.Sends == nil {
		return leadActionFailure(errLeadToolUnavailable)
	}
	if err := t.deps.Sends.Cancel(ctx, a.request(cc)); err != nil {
		return leadActionFailure(err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"cancelled": true}}
}

func reviewPreview(stage string, r *campaign.SendReview, recipients int) *copilot.Preview {
	action, _ := leadaction.ActionOfChannel(r.Channel)
	data := leadActionPreview{
		Stage: stage, Action: string(action), Matched: r.Entries, Selected: r.Entries, Eligible: recipients,
		Skipped: map[string]int{}, Counted: map[string]int{}, Parts: r.Parts,
	}
	quote := r.Quote
	data.Quote = &quote
	for reason, n := range r.Skipped {
		data.Skipped[string(reason)] = n
	}
	for reason, n := range r.Counted {
		data.Counted[string(reason)] = n
	}
	return &copilot.Preview{Kind: copilot.PreviewLeadAction, Data: data}
}

func partNames(parts []campaign.SendPart) string {
	names := make([]string, 0, len(parts))
	for _, p := range parts {
		names = append(names, p.Name)
	}
	return strings.Join(names, ", ")
}

func sendSkipLabel(skipped map[campaign.SkipReason]int) string {
	counts := map[string]int{}
	for reason, n := range skipped {
		name, ok := sendSkipNames[reason]
		if !ok {
			name = string(reason)
		}
		counts[name] += n
	}
	return countsLabel(counts)
}

func sendCountedLabel(countedOnly map[campaign.CountedReason]int) string {
	counts := map[string]int{}
	for reason, n := range countedOnly {
		name, ok := sendCountedNames[reason]
		if !ok {
			name = string(reason)
		}
		counts[name] += n
	}
	return countsLabel(counts)
}

func leadActionFailure(err error) copilot.Result {
	var changed *selection.CountChangedError
	var budget *campaign.BudgetRefusal
	denied := func(message string) copilot.Result {
		return copilot.Result{Status: copilot.StatusDenied, Message: message}
	}
	failed := func(message string) copilot.Result {
		return copilot.Result{Status: copilot.StatusError, Message: message}
	}
	switch {
	case errors.Is(err, errInvalidArgs):
		return failed(err.Error())
	case errors.Is(err, errOutsideOfACard):
		return failed("ações sobre leads só rodam por um cartão de aprovação; proponha a ação")
	case errors.Is(err, errProposalExpired):
		return failed("a contagem desta proposta expirou; proponha de novo para o usuário aprovar a contagem atual")
	case errors.Is(err, errCardArgsDiffered):
		return failed("a ação aprovada não é a que o cartão mostrou; proponha de novo")
	case errors.As(err, &changed):
		return failed(fmt.Sprintf("a seleção mudou desde a aprovação (eram %d leads, agora são %d); proponha de novo", changed.Expected, changed.Matched))
	case errors.Is(err, selection.ErrCountChanged), errors.Is(err, selection.ErrFingerprintMismatch):
		return failed("a seleção mudou desde a aprovação; proponha de novo")
	case errors.Is(err, leadaction.ErrUnavailable):
		return failed("as ações sobre leads não estão disponíveis neste servidor agora")
	case errors.Is(err, leadaction.ErrForbidden), errors.Is(err, selection.ErrScopeDenied):
		return denied("o usuário não tem permissão para essa ação sobre leads")
	case errors.Is(err, tmpl.ErrPricingUnavailable):
		return failed("o preço desse modelo não está disponível agora")
	case errors.Is(err, campaign.ErrCreationScopeMissing):
		return denied("o usuário não pode criar campanhas em nenhum departamento")
	case errors.Is(err, campaign.ErrDepartmentRequired):
		return failed("o workspace tem departamentos: pergunte qual (list_departments) e passe department_id")
	case errors.As(err, &budget):
		return failed(fmt.Sprintf("%s; cabem %d leads: passe first_n=%d ou peça ao usuário para resolver antes", budgetRefusalName(campaign.ErrorCode(budget.Reason)), budget.Fits, budget.Fits))
	case errors.Is(err, campaign.ErrNothingEligible), errors.Is(err, leadaction.ErrSelectionEmpty):
		return failed("nenhum lead elegível nessa seleção")
	case errors.Is(err, campaign.ErrAlreadyStarted):
		return failed("esse disparo já foi iniciado")
	case errors.Is(err, campaign.ErrSendPreparing):
		return failed("o disparo ainda está sendo preparado; tente de novo em instantes")
	case errors.Is(err, leadaction.ErrSelectionTooLarge), errors.Is(err, campaign.ErrSelectionTooLarge), errors.Is(err, campaign.ErrSelectionOverCampaignCap):
		return failed("seleção grande demais para essa ação; use um filtro menor, limit ou split=true num disparo")
	case errors.Is(err, leadaction.ErrPhoneUnavailable):
		return failed("esse número oficial não pode ser usado por este workspace; use list_business_phones")
	}
	if res, known := sharedLeadFailure(err); known {
		return res
	}
	if code := refusalCode(err); code != "" {
		return failed("a ação foi recusada (" + code + ")")
	}
	log.Printf("[copilot] lead action failed: %v", err)
	return failed("falha ao preparar a ação sobre os leads")
}

func refusalCode(err error) string {
	for _, code := range []func(error) string{selection.ErrorCode, leadaction.ErrorCode, campaign.ErrorCode, customfield.ErrorCode, lead.ErrorCode, leadarea.ErrorCode} {
		if c := code(err); c != "" {
			return c
		}
	}
	return ""
}
