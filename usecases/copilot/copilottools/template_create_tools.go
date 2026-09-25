package copilottools

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"vozko/domain/copilot"
	"vozko/domain/media"
	"vozko/domain/shared"
	"vozko/domain/tools"
	businessphone "vozko/domain/whatsapp/business_phone"
	tmpl "vozko/domain/whatsapp/template"
	"vozko/domain/workspace"
)

const defaultTemplateLanguage = "pt_BR"

type TemplateCreateDeps struct {
	Phones    businessphone.WorkspacePhonesUseCase
	Templates tmpl.WorkspaceTemplatesUseCase
	Media     media.GetMediaUseCase
}

type listBusinessPhonesTool struct{ deps TemplateCreateDeps }

func NewListBusinessPhonesTool(deps TemplateCreateDeps) copilot.Tool {
	return &listBusinessPhonesTool{deps: deps}
}

func (t *listBusinessPhonesTool) Meta() copilot.Meta {
	return copilot.Meta{Resource: workspace.ResourceBusinessPhones, Action: workspace.ActionRead}
}

func (t *listBusinessPhonesTool) Definition() tools.Definition {
	return definition("list_business_phones", "Lista os números do WhatsApp oficial que o workspace pode usar (para criar modelos e campanhas).", struct{}{})
}

func (t *listBusinessPhonesTool) Execute(_ context.Context, cc copilot.Context, _ map[string]interface{}) copilot.Result {
	out, err := t.deps.Phones.List(cc.WorkspaceID, businessphone.ListInput{Options: shared.QueryOptions{Pagination: shared.Pagination{Page: 1, PageSize: catalogPageSize}}})
	if err != nil {
		log.Printf("[copilot] list_business_phones failed: %v", err)
		return copilot.Result{Status: copilot.StatusError, Message: "falha ao consultar os números"}
	}
	phones := make([]map[string]interface{}, 0, len(out.Items))
	for _, p := range out.Items {
		if p != nil {
			phones = append(phones, map[string]interface{}{"business_phone_id": p.ID, "number": p.DisplayPhoneNumber, "name": p.VerifiedName})
		}
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"phones": phones}}
}

type createTemplateArgs struct {
	BusinessPhoneID string   `json:"business_phone_id" req:"true" desc:"business_phone_id exato de list_business_phones" id:"true"`
	Name            string   `json:"name" req:"true" desc:"nome técnico: minúsculas, números e _ (ex.: pedido_enviado)"`
	Category        string   `json:"category" req:"true" desc:"MARKETING (promoção) ou UTILITY (aviso de pedido, cobrança, lembrete)"`
	Language        string   `json:"language" desc:"idioma, padrão pt_BR"`
	HeaderText      string   `json:"header_text" desc:"título curto opcional (sem variáveis)"`
	HeaderMediaID   string   `json:"header_media_id" desc:"media_id de um anexo para o cabeçalho (imagem, vídeo ou documento)" id:"true"`
	Body            string   `json:"body" req:"true" desc:"texto da mensagem; variáveis como {{1}}, {{2}}"`
	BodyExamples    []string `json:"body_examples" desc:"um exemplo para cada variável do texto, na ordem"`
	Footer          string   `json:"footer" desc:"rodapé curto opcional, sem variáveis"`
	QuickReplies    []string `json:"quick_replies" desc:"textos de botões de resposta rápida (até 3)"`
}

type createTemplateTool struct{ deps TemplateCreateDeps }

func NewCreateTemplateTool(deps TemplateCreateDeps) copilot.Tool {
	return &createTemplateTool{deps: deps}
}

func (t *createTemplateTool) Meta() copilot.Meta {
	return copilot.Meta{Mutating: true, Resource: workspace.ResourceWhatsAppTemplates, Action: workspace.ActionCreate}
}

func (t *createTemplateTool) Definition() tools.Definition {
	def := definition("create_template",
		"Cria um modelo de mensagem do WhatsApp e envia para aprovação da Meta. As regras da Meta são verificadas antes do envio "+
			"(nome, variáveis, exemplos, tamanhos, rodapé e botões). A aprovação da Meta leva de minutos a horas; o status aparece em "+
			"list_templates. Só depois da aprovação do usuário.", createTemplateArgs{})
	param := def.Parameters["category"]
	param.Enum = []string{string(tmpl.TemplateCategoryMarketing), string(tmpl.TemplateCategoryUtility)}
	def.Parameters["category"] = param
	return def
}

func (a createTemplateArgs) name() string {
	return strings.ToLower(strings.TrimSpace(a.Name))
}

func (a createTemplateArgs) language() string {
	if language := strings.TrimSpace(a.Language); language != "" {
		return language
	}
	return defaultTemplateLanguage
}

func (a createTemplateArgs) category() tmpl.TemplateCategory {
	return tmpl.TemplateCategory(strings.ToUpper(strings.TrimSpace(a.Category)))
}

type TemplatePreview struct {
	Name           string                   `json:"name"`
	Language       string                   `json:"language"`
	Category       string                   `json:"category"`
	Components     []tmpl.TemplateComponent `json:"components"`
	HeaderMediaURL string                   `json:"headerMediaUrl,omitempty"`
}

func (t *createTemplateTool) Preview(_ context.Context, cc copilot.Context, args map[string]interface{}) *copilot.Preview {
	var a createTemplateArgs
	bindArgs(args, &a)
	components, headerURL, err := t.components(cc, a)
	if err != nil {
		return nil
	}
	preview := TemplatePreview{Name: a.name(), Language: a.language(), Category: string(a.category()), Components: components}
	if headerURL != nil {
		preview.HeaderMediaURL = *headerURL
	}
	return &copilot.Preview{Kind: copilot.PreviewWhatsAppTemplate, Data: preview}
}

func (t *createTemplateTool) Describe(_ context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	var a createTemplateArgs
	bindArgs(args, &a)
	return []copilot.Field{
		{Key: "name", Value: a.name()},
		{Key: "category", Value: string(a.category())},
		{Key: "language", Value: a.language()},
		{Key: "phone", Value: phoneLabel(t.deps.Phones, cc, a.BusinessPhoneID)},
	}
}

func (t *createTemplateTool) Execute(_ context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a createTemplateArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	category := a.category()
	if category != tmpl.TemplateCategoryMarketing && category != tmpl.TemplateCategoryUtility {
		return copilot.Result{Status: copilot.StatusError, Message: "category deve ser MARKETING ou UTILITY"}
	}
	phoneID, err := knownID(a.BusinessPhoneID, "business_phone_id", "list_business_phones")
	if err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	components, headerURL, err := t.components(cc, a)
	if err != nil {
		return templateCreateFailure(err)
	}
	out, err := t.deps.Templates.Create(cc.WorkspaceID, cc.UserID, tmpl.CreateTemplateInput{
		BusinessPhoneID: phoneID,
		Name:            a.name(),
		Language:        a.language(),
		Category:        category,
		Components:      components,
		HeaderMediaURL:  headerURL,
	})
	if err != nil {
		return templateCreateFailure(err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{
		"template_id": out.ID, "name": out.Name, "status": out.Status, "rejected_reason": out.RejectedReason,
	}}
}

func (t *createTemplateTool) components(cc copilot.Context, a createTemplateArgs) ([]tmpl.TemplateComponent, *string, error) {
	var out []tmpl.TemplateComponent
	var headerURL *string
	switch {
	case strings.TrimSpace(a.HeaderMediaID) != "":
		m, err := t.deps.Media.GetMedia(cc.WorkspaceID, strings.TrimSpace(a.HeaderMediaID))
		if err != nil {
			return nil, nil, err
		}
		url := m.URL
		headerURL = &url
		out = append(out, tmpl.TemplateComponent{Type: "HEADER", Format: headerFormat(m.Type), Example: &tmpl.TemplateExample{HeaderHandle: []string{url}}})
	case strings.TrimSpace(a.HeaderText) != "":
		out = append(out, tmpl.TemplateComponent{Type: "HEADER", Format: "TEXT", Text: strings.TrimSpace(a.HeaderText)})
	}
	body := tmpl.TemplateComponent{Type: "BODY", Text: strings.TrimSpace(a.Body)}
	if len(a.BodyExamples) > 0 {
		body.Example = &tmpl.TemplateExample{BodyText: [][]string{a.BodyExamples}}
	}
	out = append(out, body)
	if footer := strings.TrimSpace(a.Footer); footer != "" {
		out = append(out, tmpl.TemplateComponent{Type: "FOOTER", Text: footer})
	}
	if len(a.QuickReplies) > 0 {
		buttons := make([]tmpl.TemplateButton, 0, len(a.QuickReplies))
		for _, text := range a.QuickReplies {
			buttons = append(buttons, tmpl.TemplateButton{Type: "QUICK_REPLY", Text: strings.TrimSpace(text)})
		}
		out = append(out, tmpl.TemplateComponent{Type: "BUTTONS", Buttons: buttons})
	}
	return out, headerURL, nil
}

func headerFormat(kind media.MediaType) string {
	switch kind {
	case media.MediaTypeProductImage:
		return "IMAGE"
	case media.MediaTypeProductVideo, media.MediaTypeVslVideo:
		return "VIDEO"
	}
	return "DOCUMENT"
}

func templateCreateFailure(err error) copilot.Result {
	switch {
	case errors.Is(err, tmpl.ErrPhoneOutsideWorkspace):
		return copilot.Result{Status: copilot.StatusDenied, Message: "esse número não é deste workspace; use list_business_phones"}
	case errors.Is(err, media.ErrMediaNotFound):
		return copilot.Result{Status: copilot.StatusError, Message: "anexo desconhecido; peça ao usuário para anexar o arquivo na conversa"}
	case errors.Is(err, tmpl.ErrHeaderMediaOutsideStorage):
		return copilot.Result{Status: copilot.StatusError, Message: "a mídia do cabeçalho precisa ser um arquivo anexado ou da biblioteca de mídias"}
	}
	if code := tmpl.ErrorCode(err); code != "" && code != tmpl.CodeUnknown {
		return copilot.Result{Status: copilot.StatusError, Message: "o modelo não segue as regras da Meta (" + code + "): " + err.Error() + ". Corrija e proponha de novo."}
	}
	log.Printf("[copilot] create_template failed: %v", err)
	return copilot.Result{Status: copilot.StatusError, Message: "a Meta recusou o modelo ou não respondeu; confira na tela de modelos"}
}

func (t *createTemplateTool) Validate(_ context.Context, cc copilot.Context, args map[string]interface{}) error {
	a, err := validateArgs[createTemplateArgs](nil, cc, args)
	if err != nil {
		return err
	}
	if category := a.category(); category != tmpl.TemplateCategoryMarketing && category != tmpl.TemplateCategoryUtility {
		return fmt.Errorf("%w: category deve ser MARKETING ou UTILITY", errInvalidArgs)
	}
	if err := requirePhone(t.deps.Phones, cc, a.BusinessPhoneID); err != nil {
		return err
	}
	components, _, err := t.components(cc, a)
	if err != nil {
		return fmt.Errorf("%w: a mídia do cabeçalho precisa ser um arquivo anexado nesta conversa", errInvalidArgs)
	}
	if err := tmpl.ValidateDraft(a.name(), a.category(), components); err != nil {
		return fmt.Errorf("%w: o modelo não segue as regras da Meta: %v", errInvalidArgs, err)
	}
	return nil
}
