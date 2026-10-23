package formtools

import (
	"errors"
	"fmt"
	"log"
	"strings"

	"vozko/domain/conversation"
	"vozko/domain/copilot"
	"vozko/domain/report"
	wf "vozko/domain/whatsappform"
	"vozko/usecases/copilot/copilottools"
	report_usecase "vozko/usecases/report"
	workflow_usecase "vozko/usecases/workflow"
)

const (
	outsideWindow = "a janela de 24 horas desta conversa está fechada, e um formulário avulso só vai com ela aberta. " +
		"Para alcançar a pessoa agora, envie com send_template um modelo aprovado que tenha um botão deste formulário; " +
		"o botão de formulário é configurado no modelo pela tela de Modelos"
	unknownFailure = "a ação com o formulário falhou; tente de novo em instantes"
)

var copyLimits = fmt.Sprintf("o texto do envio é inválido: body (a mensagem) é obrigatório e vai até %d caracteres, cta (o botão) é obrigatório, sem emoji, e vai até %d, header até %d e footer até %d",
	conversation.WhatsAppButtonBodyMaxLen, conversation.WhatsAppFlowCTAMaxLen, conversation.WhatsAppHeaderTextMaxLen, conversation.WhatsAppFooterMaxLen)

type refusal struct {
	err     error
	status  copilot.Status
	message string
}

var refusals = []refusal{
	{wf.ErrForbidden, copilot.StatusDenied, "o usuário não tem a permissão de formulários do WhatsApp que esta ação pede, ou não alcança esta conversa"},
	{wf.ErrWorkspaceRequired, copilot.StatusError, "a sessão não tem workspace nem usuário; nada foi feito"},
	{wf.ErrFormNotFound, copilot.StatusError, "formulário desconhecido; use o form_id ou o nome exato de list_whatsapp_forms"},
	{wf.ErrFormArchived, copilot.StatusError, "este formulário está arquivado; ele não é publicado, enviado nem editado"},
	{wf.ErrNameRequired, copilot.StatusError, "o formulário precisa de um nome"},
	{wf.ErrNameTooLong, copilot.StatusError, fmt.Sprintf("o nome do formulário vai até %d caracteres", wf.MaxNameLength)},
	{wf.ErrNameTaken, copilot.StatusError, "já existe um formulário com este nome no workspace; escolha outro nome"},
	{wf.ErrDescriptionTooLong, copilot.StatusError, fmt.Sprintf("a descrição do formulário vai até %d caracteres", wf.MaxDescriptionLength)},
	{wf.ErrStarterUnknown, copilot.StatusError, "starter desconhecido; use um destes: " + strings.Join(starterKeys(), ", ")},
	{wf.ErrStaleDraft, copilot.StatusError, "o rascunho mudou enquanto isso; leia o formulário de novo antes de mudar"},
	{wf.ErrVersionNotFound, copilot.StatusError, "a versão do formulário não foi encontrada; leia o formulário de novo"},
	{wf.ErrNotOwner, copilot.StatusError, "só o workspace dono do número pode usar formulários nele"},
	{wf.ErrProviderUnsupported, copilot.StatusError, "formulários só funcionam em números do WhatsApp oficial conectados direto à Meta"},
	{wf.ErrNoEligibleAccount, copilot.StatusError, "o workspace não tem nenhum número do WhatsApp oficial conectado e verificado para publicar o formulário"},
	{wf.ErrNoAccountChosen, copilot.StatusError, "diga em quais contas do WhatsApp publicar, com os ids de conta mostrados em list_whatsapp_forms ou pelo usuário"},
	{wf.ErrNoPublication, copilot.StatusError, "o formulário não está publicado na conta do WhatsApp do número desta conversa; publique antes com publish_whatsapp_form"},
	{wf.ErrPublicationNotSendable, copilot.StatusError, "a Meta não está entregando este formulário agora (bloqueado, limitado ou descontinuado); veja a situação em list_whatsapp_forms"},
	{wf.ErrPublicationNotFound, copilot.StatusError, "o formulário não tem publicação nesta conta; publique com publish_whatsapp_form"},
	{wf.ErrOutsideWindow, copilot.StatusError, outsideWindow},
	{conversation.ErrWindowClosed, copilot.StatusError, outsideWindow},
	{wf.ErrChannelUnsupported, copilot.StatusError, "formulários só são enviados em conversas do WhatsApp oficial"},
	{wf.ErrCopyInvalid, copilot.StatusError, copyLimits},
	{conversation.ErrWhatsAppFlowCopyInvalid, copilot.StatusError, copyLimits},
	{wf.ErrListQueryInvalid, copilot.StatusError, "período ou filtro inválido: from e to são datas AAAA-MM-DD, com from antes de to"},
	{wf.ErrResponseNotFound, copilot.StatusError, "resposta desconhecida ou fora do alcance do usuário"},
	{wf.ErrFormInUse, copilot.StatusError, "um envio em andamento ou agendado ainda usa este formulário"},
	{wf.ErrMetaRejected, copilot.StatusError, "a Meta recusou o formulário; veja os problemas no construtor"},
	{conversation.ErrSendOutcomeUnknown, copilot.StatusError, "o WhatsApp pode ter entregue o formulário, mas a resposta se perdeu; confira a conversa antes de enviar de novo"},
	{conversation.ErrUnauthorized, copilot.StatusDenied, "o usuário não tem acesso a esta conversa"},
	{workflow_usecase.ErrNoDefaultDealFunnel, copilot.StatusError, "o workspace não tem um funil de oportunidades padrão, que a automação do formulário usa; defina um funil padrão e tente de novo"},
	{report.ErrNotAllowed, copilot.StatusDenied, "o usuário não tem permissão para exportar as respostas deste formulário"},
	{report.ErrInvalidParams, copilot.StatusError, "os dados da exportação são inválidos; confira o formulário e o período"},
	{report.ErrKindNotOffered, copilot.StatusError, "esta exportação não está disponível por aqui"},
	{report_usecase.ErrNotConfigured, copilot.StatusError, "a exportação de relatórios está indisponível agora"},
}

func starterKeys() []string {
	starters := wf.Starters()
	keys := make([]string, 0, len(starters))
	for _, s := range starters {
		keys = append(keys, s.Key)
	}
	return keys
}

func failure(err error) copilot.Result {
	var rejection *wf.MetaRejection
	if errors.As(err, &rejection) {
		return copilot.Result{Status: copilot.StatusError, Message: "a Meta recusou o formulário: " + strings.TrimSpace(rejection.Message)}
	}
	var problems wf.Problems
	if errors.As(err, &problems) && len(problems) > 0 {
		return copilot.Result{Status: copilot.StatusError, Message: "o formulário tem problemas a corrigir: " + problemsText(problems)}
	}
	if errors.Is(err, copilottools.ErrInvalidArgs) {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	for _, r := range refusals {
		if errors.Is(err, r.err) {
			return copilot.Result{Status: r.status, Message: r.message}
		}
	}
	if errors.Is(err, wf.ErrDocumentInvalid) {
		return copilot.Result{Status: copilot.StatusError, Message: "o formulário tem problemas a corrigir antes de publicar; leia o rascunho no construtor"}
	}
	log.Printf("[copilot] whatsapp form tool failed: %v", err)
	return copilot.Result{Status: copilot.StatusError, Message: unknownFailure}
}

func refusalOf(err error) error {
	if errors.Is(err, copilottools.ErrInvalidArgs) {
		return err
	}
	return fmt.Errorf("%w: %s", copilottools.ErrInvalidArgs, failure(err).Message)
}

func codeMessage(code string) string {
	if code == "" {
		return ""
	}
	for _, r := range refusals {
		if wf.ErrorCode(r.err) == code {
			return r.message
		}
	}
	return ""
}

var problemWords = map[string]string{
	wf.ProblemRequired:         "obrigatório e está vazio",
	wf.ProblemTooLong:          "longo demais",
	wf.ProblemTooMany:          "itens demais",
	wf.ProblemTooFew:           "itens de menos",
	wf.ProblemDuplicate:        "repetido",
	wf.ProblemInvalid:          "inválido",
	wf.ProblemNotAllowed:       "não permitido aqui",
	wf.ProblemUnknownTarget:    "aponta para algo que não existe",
	wf.ProblemBackwardRoute:    "volta para uma tela anterior; as rotas só seguem adiante",
	wf.ProblemUnreachable:      "nenhuma rota chega a esta tela",
	wf.ProblemNoInput:          "o formulário não tem nenhum campo para responder",
	wf.ProblemBindingUnknown:   "salva num campo do lead que não existe",
	wf.ProblemBindingKind:      "o tipo da pergunta não combina com o campo do lead",
	wf.ProblemBindingSensitive: "resposta sensível não pode ser salva no lead",
	wf.ProblemBindingDuplicate: "outra pergunta já salva neste campo do lead",
	wf.ProblemBindingOptions:   "as opções não batem com as do campo do lead",
	wf.ProblemMetaValidation:   "recusado pela Meta",
}

func problemsText(problems wf.Problems) string {
	parts := make([]string, 0, len(problems))
	for _, p := range problems {
		parts = append(parts, problemText(p))
	}
	return strings.Join(parts, "; ")
}

func problemText(p wf.Problem) string {
	var where []string
	if p.Screen != "" {
		where = append(where, "tela "+p.Screen)
	}
	if p.Block != "" {
		where = append(where, "bloco "+p.Block)
	}
	if p.Property != "" {
		where = append(where, p.Property)
	}
	words, ok := problemWords[p.Code]
	if !ok {
		words = p.Code
	}
	switch {
	case p.Limit > 0 && (p.Code == wf.ProblemTooLong || p.Code == wf.ProblemTooMany):
		words += fmt.Sprintf(" (até %d)", p.Limit)
	case p.Limit > 0 && p.Code == wf.ProblemTooFew:
		words += fmt.Sprintf(" (no mínimo %d)", p.Limit)
	}
	if message := strings.TrimSpace(p.Message); message != "" {
		words += ": " + message
	}
	if len(where) == 0 {
		return words
	}
	return strings.Join(where, ", ") + ": " + words
}
