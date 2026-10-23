package emailtools

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"vozko/domain/conversation"
	"vozko/domain/copilot"
	"vozko/domain/emailaddress"
	"vozko/domain/emailcampaign"
	"vozko/domain/emaildesign"
	"vozko/domain/tools"
	"vozko/domain/workspace"
	balance_usecase "vozko/usecases/balance"
	"vozko/usecases/copilot/copilottools"
	emailcampaign_usecase "vozko/usecases/emailcampaign"
	"vozko/usecases/transactionalemail"
)

const defaultOverviewDays = 30

type CampaignReader interface {
	Overview(ctx context.Context, a emailcampaign_usecase.Actor, days int) (emailcampaign_usecase.Overview, error)
	Metrics(ctx context.Context, a emailcampaign_usecase.Actor, campaignID string) (emailcampaign_usecase.CampaignMetrics, error)
	Get(ctx context.Context, a emailcampaign_usecase.Actor, campaignID string) (*emailcampaign.Campaign, error)
	GetDesign(ctx context.Context, a emailcampaign_usecase.Actor, designID string) (*emailcampaign.Design, error)
}

type Quoter interface {
	QuoteRun(ctx context.Context, workspaceID string, recipients int64) (balance_usecase.EmailQuote, error)
}

type EmailDeps struct {
	Sender    transactionalemail.Sender
	Campaigns CampaignReader
	Quotes    Quoter
}

func EmailTools(deps EmailDeps) []copilot.Tool {
	return []copilot.Tool{
		NewSendEmailTool(deps),
		NewEmailOverviewTool(deps),
		NewEmailCampaignMetricsTool(deps),
	}
}

func actorOf(cc copilot.Context) emailcampaign_usecase.Actor {
	return copilottools.ViewerOf(cc)
}

func failure(err error) copilot.Result {
	switch {
	case errors.Is(err, copilottools.ErrInvalidArgs):
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	case errors.Is(err, emailcampaign.ErrCampaignNotFound):
		return copilot.Result{Status: copilot.StatusError, Message: "campanha de e-mail desconhecida; use o id exato de email_overview"}
	case errors.Is(err, emailcampaign_usecase.ErrDaysInvalid):
		return copilot.Result{Status: copilot.StatusError, Message: "days vai de 1 a 90"}
	}
	return copilot.Result{Status: copilot.StatusError, Message: "a leitura do e-mail falhou; tente de novo em instantes"}
}

type sendEmailArgs struct {
	To       string `json:"to" req:"true" desc:"endereço do destinatário"`
	LeadID   string `json:"lead_id" id:"true" desc:"opcional: lead_id exato de search_leads, para preencher as variáveis do lead"`
	Subject  string `json:"subject" req:"true" desc:"assunto, em uma linha"`
	Text     string `json:"text" desc:"corpo em texto simples, parágrafos separados por uma linha em branco; use quando não houver design_id"`
	DesignID string `json:"design_id" id:"true" desc:"id de um design salvo em E-mail; use quando não houver text"`
}

var errOneBody = fmt.Errorf("%w: passe text ou design_id, um dos dois", copilottools.ErrInvalidArgs)

type sendEmailTool struct {
	deps EmailDeps
}

func NewSendEmailTool(deps EmailDeps) copilot.Tool {
	return &sendEmailTool{deps: deps}
}

func (t *sendEmailTool) Meta() copilot.Meta {
	return copilot.Meta{Mutating: true, Resource: workspace.ResourceEmailMarketing, Action: workspace.ActionStart}
}

func (t *sendEmailTool) Definition() tools.Definition {
	return copilottools.Definition("send_email",
		"Envia um e-mail transacional avulso pelo e-mail da plataforma, com o remetente padrão do workspace, para um endereço. "+
			"O corpo é text ou um design salvo (design_id), nunca os dois. Assunto e corpo aceitam {{first_name}}, {{name}}, {{email}}, {{company}} e {{field.chave}}, "+
			"preenchidos pelo lead em lead_id ou pelo lead que tem esse e-mail. Um envio é cobrado do saldo e devolvido se falhar; "+
			"endereços na lista de bloqueio e leads bloqueados são recusados. Só depois da aprovação do usuário.",
		sendEmailArgs{})
}

func (t *sendEmailTool) decode(args map[string]interface{}) (sendEmailArgs, error) {
	var in sendEmailArgs
	if err := copilottools.DecodeArgs(args, &in); err != nil {
		return in, fmt.Errorf("%w: %v", copilottools.ErrInvalidArgs, err)
	}
	if (strings.TrimSpace(in.Text) == "") == (strings.TrimSpace(in.DesignID) == "") {
		return in, errOneBody
	}
	return in, nil
}

func (t *sendEmailTool) Validate(ctx context.Context, cc copilot.Context, args map[string]interface{}) error {
	in, err := t.decode(args)
	if err != nil {
		return err
	}
	if _, err := emailaddress.Normalize(in.To); err != nil {
		return fmt.Errorf("%w: to não é um endereço de e-mail válido", copilottools.ErrInvalidArgs)
	}
	if issues := emaildesign.ValidateSubject(in.Subject); len(issues) > 0 {
		return fmt.Errorf("%w: o assunto é inválido (%s)", copilottools.ErrInvalidArgs, issues[0].Code)
	}
	if strings.TrimSpace(in.Text) != "" {
		if _, err := emaildesign.ExpandText(in.Text, emaildesign.Personalization{}); err != nil {
			return fmt.Errorf("%w: o texto tem uma variável desconhecida; use só {{first_name}}, {{name}}, {{email}}, {{company}} e {{field.chave}}", copilottools.ErrInvalidArgs)
		}
		return nil
	}
	if _, err := t.design(ctx, cc, in.DesignID); err != nil {
		return err
	}
	return nil
}

func (t *sendEmailTool) design(ctx context.Context, cc copilot.Context, designID string) (*emailcampaign.Design, error) {
	if t.deps.Campaigns == nil {
		return nil, fmt.Errorf("%w: design desconhecido", copilottools.ErrInvalidArgs)
	}
	found, err := t.deps.Campaigns.GetDesign(ctx, actorOf(cc), strings.TrimSpace(designID))
	if err != nil || found == nil {
		return nil, fmt.Errorf("%w: design desconhecido; use o id de um design salvo em E-mail", copilottools.ErrInvalidArgs)
	}
	return found, nil
}

func (t *sendEmailTool) Describe(ctx context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	var in sendEmailArgs
	copilottools.BindArgs(args, &in)
	fields := []copilot.Field{
		{Key: "to", Value: strings.TrimSpace(in.To)},
		{Key: "subject", Value: copilottools.ClipText(in.Subject, copilot.MaxFieldRunes)},
	}
	if strings.TrimSpace(in.Text) != "" {
		fields = append(fields, copilot.Field{Key: "text", Value: copilottools.ClipText(in.Text, copilot.MaxFieldRunes)})
	} else if found, err := t.design(ctx, cc, in.DesignID); err == nil {
		fields = append(fields, copilot.Field{Key: "design", Value: found.Name})
	}
	if t.deps.Quotes != nil {
		if quote, err := t.deps.Quotes.QuoteRun(ctx, cc.WorkspaceID, 1); err == nil {
			fields = append(fields, copilot.Field{Key: "cost", Value: fmt.Sprintf("US$ %.4f", float64(quote.TotalMicros)/1_000_000)})
		}
	}
	return fields
}

func (t *sendEmailTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	in, err := t.decode(args)
	if err != nil {
		return failure(err)
	}
	if t.deps.Sender == nil {
		return copilot.Result{Status: copilot.StatusError, Message: "o envio de e-mail não está disponível agora (" + transactionalemail.CodeUnavailable + "); nada foi enviado"}
	}
	key := strings.TrimSpace(cc.ProposalID)
	if key == "" {
		key = uuid.NewString()
	}
	result, err := t.deps.Sender.Send(ctx, transactionalemail.Request{
		WorkspaceID:    cc.WorkspaceID,
		To:             in.To,
		Subject:        in.Subject,
		Body:           transactionalemail.Body{Text: in.Text, DesignID: strings.TrimSpace(in.DesignID)},
		LeadID:         strings.TrimSpace(in.LeadID),
		IdempotencyKey: "copilot:" + key,
		Origin:         transactionalemail.OriginCopilot,
		SentBy:         conversation.SentByAI(""),
	})
	if err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: fmt.Sprintf("o e-mail não foi enviado (%s): %v%s", transactionalemail.CodeOf(err), err, transactionalemail.RetryHint(err))}
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{
		"sent": true, "message_id": result.MessageID, "to": result.To, "repeated": result.Repeated,
	}}
}

type emailOverviewArgs struct {
	Days int `json:"days" desc:"opcional: quantos dias até hoje, de 1 a 90; padrão 30"`
}

type emailOverviewTool struct {
	deps EmailDeps
}

func NewEmailOverviewTool(deps EmailDeps) copilot.Tool {
	return &emailOverviewTool{deps: deps}
}

func (t *emailOverviewTool) Meta() copilot.Meta {
	return copilot.Meta{Resource: workspace.ResourceEmailMarketing, Action: workspace.ActionRead}
}

func (t *emailOverviewTool) Definition() tools.Definition {
	return copilottools.Definition("email_overview",
		"Resultados do e-mail marketing do workspace no período: totais (enviados, entregues, aberturas, cliques, bounces, reclamações, descadastros e taxas), "+
			"a série diária, as campanhas com mais resultado (com o id de cada uma para email_campaign_metrics) e a reputação de envio.",
		emailOverviewArgs{})
}

func (t *emailOverviewTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var in emailOverviewArgs
	if err := copilottools.DecodeArgs(args, &in); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	if in.Days == 0 {
		in.Days = defaultOverviewDays
	}
	if t.deps.Campaigns == nil {
		return failure(errors.New("unwired"))
	}
	overview, err := t.deps.Campaigns.Overview(ctx, actorOf(cc), in.Days)
	if err != nil {
		return failure(err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: overview}
}

type emailCampaignMetricsArgs struct {
	CampaignID string `json:"campaign_id" req:"true" id:"true" desc:"id exato da campanha, de email_overview"`
}

type emailCampaignMetricsTool struct {
	deps EmailDeps
}

func NewEmailCampaignMetricsTool(deps EmailDeps) copilot.Tool {
	return &emailCampaignMetricsTool{deps: deps}
}

func (t *emailCampaignMetricsTool) Meta() copilot.Meta {
	return copilot.Meta{Resource: workspace.ResourceEmailMarketing, Action: workspace.ActionRead}
}

func (t *emailCampaignMetricsTool) Definition() tools.Definition {
	return copilottools.Definition("email_campaign_metrics",
		"Resultados de uma campanha de e-mail: situação, enviados, entregues, aberturas, cliques, bounces, reclamações, descadastros e taxas, "+
			"e, em teste A/B, o resultado de cada variante e a vencedora.",
		emailCampaignMetricsArgs{})
}

func (t *emailCampaignMetricsTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var in emailCampaignMetricsArgs
	if err := copilottools.DecodeArgs(args, &in); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	if t.deps.Campaigns == nil {
		return failure(errors.New("unwired"))
	}
	a := actorOf(cc)
	campaign, err := t.deps.Campaigns.Get(ctx, a, in.CampaignID)
	if err != nil {
		return failure(err)
	}
	metrics, err := t.deps.Campaigns.Metrics(ctx, a, campaign.ID)
	if err != nil {
		return failure(err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{
		"campaign_id": campaign.ID, "name": campaign.Name, "metrics": metrics,
	}}
}
