package crmtools

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"vozko/domain/copilot"
	"vozko/domain/media"
	"vozko/domain/tools"
	tmpl "vozko/domain/whatsapp/template"
	"vozko/domain/workspace"
	buttonaction_usecase "vozko/usecases/buttonaction"
	"vozko/usecases/copilot/copilottools"
)

type WorkspaceTemplate interface {
	Get(workspaceID, templateID string) (*tmpl.Template, error)
}

type TemplateEditor interface {
	EditTemplate(ctx context.Context, a buttonaction_usecase.Actor, req buttonaction_usecase.EditTemplateRequest) (*tmpl.Template, error)
}

type TemplateEditDeps struct {
	Templates WorkspaceTemplate
	Editor    TemplateEditor
	Media     media.GetMediaUseCase
}

type editTemplateArgs struct {
	TemplateID    string `json:"template_id" req:"true" desc:"template_id exato de list_templates" id:"true"`
	Category      string `json:"category" desc:"nova categoria, só para modelo reprovado ou pausado; vazio mantém a atual"`
	RemoveHeader  bool   `json:"remove_header" desc:"true tira o cabeçalho atual"`
	RemoveFooter  bool   `json:"remove_footer" desc:"true tira o rodapé atual"`
	RemoveButtons bool   `json:"remove_buttons" desc:"true tira todos os botões atuais"`
	templateContentArgs
}

type editTemplateTool struct{ deps TemplateEditDeps }

func NewEditTemplateTool(deps TemplateEditDeps) copilot.Tool {
	return &editTemplateTool{deps: deps}
}

func (t *editTemplateTool) Meta() copilot.Meta {
	return copilot.Meta{Mutating: true, Resource: workspace.ResourceWhatsAppTemplates, Action: workspace.ActionUpdate}
}

func (t *editTemplateTool) Definition() tools.Definition {
	def := copilottools.Definition("edit_template",
		"Edita o conteúdo de um modelo existente e envia a nova versão para a revisão da Meta; enquanto ela revisa, campanhas não usam o modelo. "+
			"Mande sempre o corpo; cabeçalho, rodapé e botões atuais ficam como estão, a não ser que venham novos (header_text ou header_media_id, footer, quick_replies) "+
			"ou remove_header, remove_footer, remove_buttons. quick_replies troca todos os botões, inclusive os de link e telefone. "+
			"Nome e idioma não mudam; a categoria só muda em modelo reprovado ou pausado. Modelos de AUTHENTICATION não são editados aqui. "+
			"A Meta aceita uma edição por dia e dez a cada 30 dias num modelo aprovado, e nenhuma campanha em andamento ou agendada pode estar usando o modelo. "+
			"Um modelo MARKETING precisa continuar mostrando ao lead como sair. Só depois da aprovação do usuário.", editTemplateArgs{})
	param := def.Parameters["category"]
	param.Enum = []string{string(tmpl.TemplateCategoryMarketing), string(tmpl.TemplateCategoryUtility)}
	def.Parameters["category"] = param
	return def
}

type templateEdit struct {
	stored *tmpl.Template
	edit   tmpl.Edit
}

func (t *editTemplateTool) prepare(cc copilot.Context, a editTemplateArgs) (templateEdit, error) {
	stored, err := t.deps.Templates.Get(cc.WorkspaceID, strings.TrimSpace(a.TemplateID))
	if err != nil {
		return templateEdit{}, err
	}
	components, headerURL, err := a.components(t.deps.Media, cc, a.keptFrom(stored.Components))
	if err != nil {
		return templateEdit{}, err
	}
	edit := tmpl.Edit{Category: tmpl.TemplateCategory(strings.ToUpper(strings.TrimSpace(a.Category))), Components: components, HeaderMediaURL: headerURL}
	return templateEdit{stored: stored, edit: edit}, nil
}

func (a editTemplateArgs) keptFrom(stored []tmpl.TemplateComponent) keptParts {
	var kept keptParts
	for _, comp := range stored {
		switch strings.ToUpper(strings.TrimSpace(comp.Type)) {
		case "HEADER":
			if !a.RemoveHeader {
				kept.header = &comp
			}
		case "FOOTER":
			if !a.RemoveFooter {
				kept.footer = &comp
			}
		case "BUTTONS":
			if !a.RemoveButtons {
				kept.buttons = &comp
			}
		}
	}
	return kept
}

func (t *editTemplateTool) Preview(_ context.Context, cc copilot.Context, args map[string]interface{}) *copilot.Preview {
	var a editTemplateArgs
	copilottools.BindArgs(args, &a)
	prepared, err := t.prepare(cc, a)
	if err != nil {
		return nil
	}
	preview := TemplatePreview{
		Name: prepared.stored.Name, Language: prepared.stored.Language,
		Category: string(prepared.edit.CategoryFor(prepared.stored)), Components: prepared.edit.Components,
	}
	if prepared.edit.HeaderMediaURL != nil {
		preview.HeaderMediaURL = *prepared.edit.HeaderMediaURL
	} else if _, kept := tmpl.MediaHeaderOf(prepared.edit.Components); kept {
		preview.HeaderMediaURL = prepared.stored.GetHeaderMediaURL()
	}
	return &copilot.Preview{Kind: copilot.PreviewWhatsAppTemplate, Data: preview}
}

func (t *editTemplateTool) Describe(_ context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	var a editTemplateArgs
	copilottools.BindArgs(args, &a)
	prepared, err := t.prepare(cc, a)
	if err != nil {
		return nil
	}
	return []copilot.Field{
		{Key: "name", Value: prepared.stored.Name},
		{Key: "category", Value: string(prepared.edit.CategoryFor(prepared.stored))},
		{Key: "language", Value: prepared.stored.Language},
	}
}

func (t *editTemplateTool) Validate(_ context.Context, cc copilot.Context, args map[string]interface{}) error {
	a, err := copilottools.ValidateArgs[editTemplateArgs](nil, cc, args)
	if err != nil {
		return err
	}
	prepared, err := t.prepare(cc, a)
	if err != nil {
		return fmt.Errorf("%w: %s", copilottools.ErrInvalidArgs, templateRefusalText(err))
	}
	if err := tmpl.CheckEdit(prepared.stored, prepared.edit, nil, time.Now()); err != nil {
		return fmt.Errorf("%w: %s", copilottools.ErrInvalidArgs, templateRefusalText(err))
	}
	if _, err := a.exitActions(prepared.edit.Components); err != nil {
		return err
	}
	return nil
}

func (t *editTemplateTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a editTemplateArgs
	if err := copilottools.DecodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	if t.deps.Editor == nil {
		return copilot.Result{Status: copilot.StatusError, Message: "a edição de modelos está indisponível agora"}
	}
	prepared, err := t.prepare(cc, a)
	if err != nil {
		return templateEditFailure(err)
	}
	actions, err := a.exitActions(prepared.edit.Components)
	if err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	edited, err := t.deps.Editor.EditTemplate(ctx, copilottools.ButtonActor(cc), buttonaction_usecase.EditTemplateRequest{
		TemplateID:       prepared.stored.ID,
		Edit:             prepared.edit,
		Actions:          actions,
		ExitConfirmation: strings.TrimSpace(a.ExitConfirmation),
	})
	if err != nil {
		return templateEditFailure(err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{
		"template_id": edited.ID, "name": edited.Name, "status": edited.Status,
		"note": "a nova versão foi para a revisão da Meta; o status aparece em list_templates",
	}}
}

func templateEditFailure(err error) copilot.Result {
	if message := templateEditMessage(err); message != "" {
		return copilot.Result{Status: copilot.StatusError, Message: message}
	}
	return templateFailure(err)
}

func templateRefusalText(err error) string {
	if message := templateEditMessage(err); message != "" {
		return message
	}
	return "o modelo não segue as regras da Meta (" + tmpl.ErrorCode(err) + "): " + err.Error()
}

func templateEditMessage(err error) string {
	var window *tmpl.EditWindowError
	switch {
	case errors.As(err, &window):
		return "a Meta aceita uma edição por dia e dez a cada 30 dias neste modelo; a próxima edição é possível a partir de " + window.NextAt.UTC().Format(time.RFC3339)
	case errors.Is(err, tmpl.ErrTemplateInUse):
		return "uma campanha em andamento ou agendada usa este modelo; ela precisa terminar ou ser pausada antes da edição"
	case errors.Is(err, tmpl.ErrEditNotOwner):
		return "só um workspace com um número na conta do modelo pode editá-lo"
	case errors.Is(err, tmpl.ErrTemplateNotEditable):
		return "só modelos aprovados, reprovados ou pausados podem ser editados; este está em revisão ou desativado"
	case errors.Is(err, tmpl.ErrEditCategoryLocked):
		return "um modelo aprovado mantém a categoria; deixe category vazio"
	case errors.Is(err, tmpl.ErrEditUnsupported):
		return "este modelo tem partes que a plataforma não edita aqui (autenticação, botões de fluxo ou de código); a edição precisa ser feita na tela do modelo ou na Meta"
	case errors.Is(err, tmpl.ErrTemplateAccessDenied), errors.Is(err, tmpl.ErrTemplateNotFound):
		return "modelo desconhecido ou fora deste workspace; use list_templates"
	}
	return ""
}
