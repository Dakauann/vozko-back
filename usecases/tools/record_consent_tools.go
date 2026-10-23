package tools_usecase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"vozko/domain/consentevidence"
	"vozko/domain/conversation"
	"vozko/domain/lead"
	"vozko/domain/shared"
	"vozko/domain/tools"
	lead_usecase "vozko/usecases/lead"
)

const (
	RecordOptOutToolName  = "record_opt_out"
	RecordConsentToolName = "record_consent"

	consentPurposeConfig    = "purpose"
	maxCitedMessageIDLength = 128
)

var (
	errConsentToolIncomplete = errors.New("consent tools: the consent commands and the message reader are required")
	errCitationMissing       = fmt.Errorf("%w: the message id was not given", lead.ErrConsentEvidenceRequired)
	errCitationMalformed     = fmt.Errorf("%w: the message id is not one the conversation shows", lead.ErrConsentEvidenceInvalid)
)

type ConsentRecorder interface {
	Record(ctx context.Context, workspaceID, leadID string, change lead.ConsentChange) (lead_usecase.ConsentOutcome, error)
}

type CitedMessages interface {
	GetByID(id string) (*conversation.Message, error)
}

type consentCitations struct {
	recorder ConsentRecorder
	messages CitedMessages
}

func newConsentCitations(recorder ConsentRecorder, messages CitedMessages) (consentCitations, error) {
	if recorder == nil || messages == nil {
		return consentCitations{}, errConsentToolIncomplete
	}
	return consentCitations{recorder: recorder, messages: messages}, nil
}

type recordOptOutTool struct{ consentCitations }

type recordConsentTool struct{ consentCitations }

func NewRecordOptOutTool(recorder ConsentRecorder, messages CitedMessages) (tools.Handler, error) {
	citations, err := newConsentCitations(recorder, messages)
	if err != nil {
		return nil, err
	}
	return &recordOptOutTool{citations}, nil
}

func NewRecordConsentTool(recorder ConsentRecorder, messages CitedMessages) (tools.Handler, error) {
	citations, err := newConsentCitations(recorder, messages)
	if err != nil {
		return nil, err
	}
	return &recordConsentTool{citations}, nil
}

func citedMessageParameter(description string) tools.Parameter {
	return tools.Parameter{
		Type:               "string",
		Description:        description,
		DisplayName:        "Mensagem citada",
		DisplayDescription: "Id da mensagem que serve de prova",
	}
}

func (t *recordOptOutTool) Definition() tools.Definition {
	return tools.Definition{
		Name:               RecordOptOutToolName,
		DisplayName:        "Parar de receber templates (opt-out)",
		DisplayDescription: "Registra que o lead pediu para não receber mais mensagens, com a mensagem dele como prova.",
		Description: `Registra que este lead pediu para não receber mais mensagens (opt-out), com a mensagem dele como prova.

QUANDO USAR:
- Somente diante de um pedido explícito para parar de receber mensagens desta empresa.

NÃO REGISTRE:
- desinteresse por um produto ou oferta;
- um pedido só sobre ligações;
- o pedido para cancelar um pedido, plano ou assinatura;
- uma reclamação sem pedido para parar de receber mensagens.

QUANDO HOUVER DÚVIDA:
- um pedido ambíguo não é registrado: pergunte ao lead se ele quer parar de receber mensagens;
- se ele confirmar, registre e cite a mensagem em que ele confirmou.

PARÂMETROS:
- message_id: o id, entre colchetes na lista "Mensagens citáveis" do contexto, da mensagem em que o lead fez o pedido explícito ou confirmou que quer parar.

O QUE O SERVIDOR CONFERE:
- a mensagem existe, é desta conversa e foi enviada pelo lead; caso contrário nada é registrado e a resposta traz o motivo.
- se o lead já tinha pedido, nada muda.
- depois do registro, novos envios de campanhas, fluxos e modelos para este lead são recusados.`,
		Parameters: map[string]tools.Parameter{
			"message_id": citedMessageParameter("Id da mensagem do lead com o pedido explícito ou com a confirmação, como aparece na lista de mensagens citáveis."),
		},
		Required:   []string{"message_id"},
		Visibility: []tools.ToolVisibility{tools.VisibilityMessaging},
		Category:   tools.CategoryAgentAction,
	}
}

func (t *recordConsentTool) Definition() tools.Definition {
	return tools.Definition{
		Name:               RecordConsentToolName,
		DisplayName:        "Registrar consentimento",
		DisplayDescription: "Registra o consentimento do lead para receber mensagens quando ele responde sim a uma pergunta do próprio agente.",
		Description: `Registra o consentimento deste lead para receber mensagens, quando ele deu um sim explícito à sua própria pergunta sobre receber mensagens nesta conversa.

QUANDO USAR:
- Você perguntou ao lead se ele aceita receber mensagens e ele respondeu com um sim explícito a essa pergunta.

NÃO REGISTRE:
- interesse em um produto, uma oferta ou um atendimento;
- uma resposta a outra pergunta;
- uma resposta que não aceita receber mensagens.

QUANDO HOUVER DÚVIDA:
- uma resposta ambígua não é registrada: pergunte de novo, de forma direta, se ele aceita receber mensagens;
- se ele confirmar, registre com essa nova pergunta e cite a mensagem em que ele confirmou.

PARÂMETROS:
- question_message_id: o id da sua mensagem com a pergunta.
- message_id: o id da resposta do lead.
Os ids estão entre colchetes na lista "Mensagens citáveis" do contexto.

O QUE O SERVIDOR CONFERE:
- a pergunta é uma mensagem sua nesta conversa;
- a resposta foi enviada pelo lead nesta conversa, depois da pergunta;
- a resposta não é a mensagem com que o lead pediu para parar de receber mensagens;
caso contrário nada é registrado e a resposta traz o motivo.`,
		Parameters: map[string]tools.Parameter{
			"question_message_id": citedMessageParameter("Id da sua mensagem com a pergunta sobre receber mensagens."),
			"message_id":          citedMessageParameter("Id da resposta do lead à sua pergunta."),
		},
		Required:   []string{"question_message_id", "message_id"},
		Visibility: []tools.ToolVisibility{tools.VisibilityMessaging},
		Category:   tools.CategoryAgentAction,
		ConfigSchema: map[string]tools.ConfigParameter{
			consentPurposeConfig: {
				Type:               "string",
				Description:        "Finalidade gravada com o consentimento.",
				DisplayName:        "Finalidade",
				DisplayDescription: "Para que o lead aceita receber mensagens (opcional)",
			},
		},
	}
}

func (t *recordOptOutTool) Execute(ctx context.Context, params map[string]interface{}) (tools.ExecutionResult, error) {
	return t.ExecuteWithConfig(ctx, nil, params)
}

func (t *recordConsentTool) Execute(ctx context.Context, params map[string]interface{}) (tools.ExecutionResult, error) {
	return t.ExecuteWithConfig(ctx, nil, params)
}

func (t *recordOptOutTool) ExecuteWithConfig(ctx context.Context, config, params map[string]interface{}) (tools.ExecutionResult, error) {
	turn, ok := agentTurnOf(ctx, config)
	if !ok {
		return errResult(turnMissingAnswer), nil
	}
	request, err := t.cite(params["message_id"])
	if err != nil {
		return consentRefusal(err), nil
	}
	evidence, err := consentevidence.LeadMessage(turn.conversation, request)
	if err != nil {
		return consentRefusal(err), nil
	}
	return t.record(ctx, turn, lead.ConsentChange{Kind: lead.ConsentWithdraw, Evidence: evidence}, optOutAnswers)
}

func (t *recordConsentTool) ExecuteWithConfig(ctx context.Context, config, params map[string]interface{}) (tools.ExecutionResult, error) {
	turn, ok := agentTurnOf(ctx, config)
	if !ok {
		return errResult(turnMissingAnswer), nil
	}
	question, err := t.cite(params["question_message_id"])
	if err != nil {
		return consentRefusal(err), nil
	}
	answer, err := t.cite(params["message_id"])
	if err != nil {
		return consentRefusal(err), nil
	}
	evidence, err := consentevidence.AnswerToAgentQuestion(turn.conversation, turn.agentID, question, answer)
	if err != nil {
		return consentRefusal(err), nil
	}
	change := lead.ConsentChange{Kind: lead.ConsentGrant, Purpose: configString(config, consentPurposeConfig), Evidence: evidence}
	return t.record(ctx, turn, change, grantAnswers)
}

type agentTurn struct {
	workspaceID  string
	leadID       string
	agentID      string
	conversation consentevidence.Conversation
}

const turnMissingAnswer = "Esta ferramenta só funciona durante uma conversa de um agente com um lead; nada foi registrado."

func agentTurnOf(ctx context.Context, config map[string]interface{}) (agentTurn, bool) {
	turn := agentTurn{
		workspaceID: configString(config, "__workspace_id"),
		leadID:      configString(config, "__lead_id"),
		agentID:     agentIDOf(ctx, config),
		conversation: consentevidence.Conversation{
			EntryID:   configString(config, "__entry_id"),
			EntryType: shared.EntryType(configString(config, "__entry_type")),
		},
	}
	complete := turn.workspaceID != "" && turn.leadID != "" && turn.agentID != "" &&
		turn.conversation.EntryID != "" && turn.conversation.EntryType != ""
	return turn, complete
}

func (c consentCitations) cite(param interface{}) (*conversation.Message, error) {
	raw, isText := param.(string)
	id := strings.TrimSpace(raw)
	switch {
	case param == nil || (isText && id == ""):
		return nil, errCitationMissing
	case !isText || len(id) > maxCitedMessageIDLength || strings.ContainsAny(id, " \t\r\n"):
		return nil, errCitationMalformed
	}
	message, err := c.messages.GetByID(id)
	if errors.Is(err, conversation.ErrMessageNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading cited message %s: %w", id, err)
	}
	return message, nil
}

type consentAnswers struct {
	changed   string
	unchanged string
	context   string
}

var optOutAnswers = consentAnswers{
	changed:   "O lead parou de receber templates. Novos envios de campanhas, fluxos e modelos para este lead serão recusados.",
	unchanged: "Este lead já tinha pedido para não receber mensagens; nada mudou.",
	context:   "O lead parou de receber templates",
}

var grantAnswers = consentAnswers{
	changed:   "Consentimento registrado.",
	unchanged: "Este lead já tinha consentimento registrado; nada mudou.",
	context:   "Consentimento do lead registrado",
}

func (c consentCitations) record(ctx context.Context, turn agentTurn, change lead.ConsentChange, answers consentAnswers) (tools.ExecutionResult, error) {
	change.Source = lead.ConsentAIAgent
	change.Actor = lead.ConsentActor{Type: lead.ConsentActorAgent, ID: turn.agentID}
	outcome, err := c.recorder.Record(ctx, turn.workspaceID, turn.leadID, change)
	if err != nil {
		return consentRefusal(err), nil
	}
	if !outcome.Changed {
		return tools.ExecutionResult{Result: answers.unchanged}, nil
	}
	return tools.ExecutionResult{Result: answers.changed, ContextUpdateText: answers.context}, nil
}

var citationReasons = []struct {
	err    error
	reason string
}{
	{lead.ErrConsentEvidenceIsTheWithdrawal, "a mensagem citada é o pedido de saída do lead, não um consentimento"},
	{errCitationMissing, "informe o id da mensagem"},
	{errCitationMalformed, "o id informado não é de uma mensagem da lista de mensagens citáveis"},
	{consentevidence.ErrMessageUnknown, "a mensagem citada não existe; use um id da lista de mensagens citáveis"},
	{consentevidence.ErrQuestionUnknown, "a pergunta citada não existe; cite o id da sua mensagem com a pergunta"},
	{consentevidence.ErrOutsideConversation, "a mensagem citada não é desta conversa"},
	{consentevidence.ErrNotFromLead, "a mensagem citada não foi enviada pelo lead"},
	{consentevidence.ErrNotTheAgentsQuestion, "a pergunta citada não é uma mensagem sua"},
	{consentevidence.ErrAnswerBeforeQuestion, "a resposta do lead é anterior à sua pergunta"},
	{lead.ErrLeadNotFound, "o lead desta conversa não foi encontrado"},
}

func consentRefusal(err error) tools.ExecutionResult {
	for _, known := range citationReasons {
		if errors.Is(err, known.err) {
			return codedConsentRefusal(err, known.reason)
		}
	}
	if lead.IsInputRefusal(err) {
		return codedConsentRefusal(err, "o registro foi recusado pelas regras de consentimento")
	}
	log.Printf("[consent-tools] recording failed: %v", err)
	return errResult("Não foi possível registrar agora; tente novamente. Nada foi registrado.")
}

func codedConsentRefusal(err error, reason string) tools.ExecutionResult {
	return errResult(fmt.Sprintf("Nada foi registrado (%s): %s.", lead.ErrorCode(err), reason))
}

var (
	_ tools.Handler = (*recordOptOutTool)(nil)
	_ tools.Handler = (*recordConsentTool)(nil)
)
