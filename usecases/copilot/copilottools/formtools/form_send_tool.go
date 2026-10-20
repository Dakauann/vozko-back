package formtools

import (
	"context"
	"fmt"
	"strings"

	"vozko/domain/conversation"
	"vozko/domain/copilot"
	"vozko/domain/shared"
	"vozko/domain/tools"
	wf "vozko/domain/whatsappform"
	"vozko/domain/workspace"
	"vozko/usecases/copilot/copilottools"
	whatsappform_usecase "vozko/usecases/whatsappform"
)

var (
	errNotSendableHere = fmt.Errorf("%w: o formulário não está pronto para envio no número desta conversa: ou não foi publicado na conta dele "+
		"(publique com publish_whatsapp_form) ou a Meta não está entregando agora (veja list_whatsapp_forms)", copilottools.ErrInvalidArgs)
	errUnknownConversation = fmt.Errorf("%w: %s; use o entry_id exato de search_conversations", copilottools.ErrInvalidArgs, copilottools.UnknownConversation)
)

type sendWhatsAppFormArgs struct {
	EntryID   string `json:"entry_id" req:"true" desc:"entry_id exato de search_conversations"`
	EntryType string `json:"entry_type" req:"true" enum:"whatsapp" desc:"entry_type exato de search_conversations; formulários só vão no WhatsApp oficial"`
	formRef
	Header string `json:"header" desc:"opcional: cabeçalho curto da mensagem"`
	Body   string `json:"body" req:"true" desc:"o texto da mensagem que acompanha o formulário"`
	Footer string `json:"footer" desc:"opcional: rodapé curto"`
	CTA    string `json:"cta" req:"true" desc:"o texto do botão que abre o formulário, curto e sem emoji"`
}

func (a sendWhatsAppFormArgs) messageCopy() wf.SendCopy {
	return wf.SendCopy{Header: strings.TrimSpace(a.Header), Body: strings.TrimSpace(a.Body), Footer: strings.TrimSpace(a.Footer), CTA: strings.TrimSpace(a.CTA)}
}

type sendWhatsAppFormTool struct {
	gate
	deps FormDeps
}

func NewSendWhatsAppFormTool(deps FormDeps) copilot.Tool {
	return &sendWhatsAppFormTool{gate: gated(true, workspace.ResourceWhatsAppForms, workspace.ActionSend, wf.CapabilitySend), deps: deps}
}

func (t *sendWhatsAppFormTool) Definition() tools.Definition {
	return copilottools.Definition("send_whatsapp_form",
		fmt.Sprintf("Envia um formulário publicado numa conversa do WhatsApp oficial, como uma mensagem com um botão que abre o formulário. "+
			"Só vai com a janela de 24 horas aberta; fora dela, a alternativa é um modelo aprovado com botão do formulário, enviado com send_template. "+
			"Limites: body até %d caracteres, cta até %d, header até %d, footer até %d. A resposta chega na conversa e aparece em whatsapp_form_responses. "+
			"Só depois da aprovação do usuário.",
			conversation.WhatsAppButtonBodyMaxLen, conversation.WhatsAppFlowCTAMaxLen, conversation.WhatsAppHeaderTextMaxLen, conversation.WhatsAppFooterMaxLen),
		sendWhatsAppFormArgs{})
}

func (t *sendWhatsAppFormTool) entry(cc copilot.Context, entryID string) (*conversation.InboxEntry, error) {
	if t.deps.Entries == nil {
		return nil, errUnknownConversation
	}
	entry, err := t.deps.Entries.LookupEntry(copilottools.ViewerOf(cc), strings.TrimSpace(entryID), shared.EntryTypeWhatsApp)
	if err != nil || entry == nil {
		return nil, errUnknownConversation
	}
	return entry, nil
}

func (t *sendWhatsAppFormTool) Validate(ctx context.Context, cc copilot.Context, args map[string]interface{}) error {
	a, err := copilottools.ValidateArgs[sendWhatsAppFormArgs](t.deps.Entries, cc, args)
	if err != nil {
		return err
	}
	if err := a.messageCopy().Validate(); err != nil {
		return refusalOf(err)
	}
	detail, err := activeForm(ctx, t.deps.Forms, cc, a.formRef)
	if err != nil {
		return refusalOf(err)
	}
	entry, err := t.entry(cc, a.EntryID)
	if err != nil {
		return err
	}
	sendable, err := t.deps.Forms.Sendable(ctx, actorOf(cc), whatsappform_usecase.SendableQuery{PhoneID: entry.BusinessPhoneID})
	if err != nil {
		return refusalOf(err)
	}
	if !offers(sendable, detail.Form.ID) {
		return errNotSendableHere
	}
	if !entry.WindowOpen {
		return refusalOf(wf.ErrOutsideWindow)
	}
	return nil
}

func offers(sendable []whatsappform_usecase.SendableForm, formID string) bool {
	for _, s := range sendable {
		if s.FormID == formID {
			return true
		}
	}
	return false
}

func (t *sendWhatsAppFormTool) Describe(ctx context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	var a sendWhatsAppFormArgs
	copilottools.BindArgs(args, &a)
	fields := []copilot.Field{{Key: "conversation", Value: copilottools.DescribeConversation(t.deps.Entries, cc, a.EntryID, a.EntryType)}}
	if detail, err := a.resolve(ctx, t.deps.Forms, actorOf(cc)); err == nil {
		fields = append(fields, copilot.Field{Key: "form", Value: detail.Form.Name})
	} else {
		fields = append(fields, copilot.Field{Key: "form", Value: "formulário desconhecido ou sem acesso"})
	}
	shown := a.messageCopy()
	if shown.Header != "" {
		fields = append(fields, copilot.Field{Key: "headerText", Value: shown.Header})
	}
	fields = append(fields, copilot.Field{Key: "body", Value: copilottools.ClipText(shown.Body, copilot.MaxFieldRunes)})
	if shown.Footer != "" {
		fields = append(fields, copilot.Field{Key: "footer", Value: shown.Footer})
	}
	return append(fields, copilot.Field{Key: "sending", Value: "botão " + shown.CTA + " abre o formulário"})
}

func (t *sendWhatsAppFormTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var in sendWhatsAppFormArgs
	if err := copilottools.DecodeArgs(args, &in); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	a := actorOf(cc)
	detail, err := in.resolve(ctx, t.deps.Forms, a)
	if err != nil {
		return failure(err)
	}
	send, err := t.deps.Forms.SendInConversation(ctx, a, whatsappform_usecase.SendInput{EntryID: strings.TrimSpace(in.EntryID), FormID: detail.Form.ID, Copy: in.messageCopy(), Origin: wf.OriginElo})
	if err != nil {
		return failure(err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{
		"sent": true, "send_id": send.ID, "form": detail.Form.Name, "status": string(send.Status),
		"next": "a resposta chega na conversa; leia com whatsapp_form_responses",
	}}
}
