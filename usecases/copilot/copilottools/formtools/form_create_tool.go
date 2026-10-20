package formtools

import (
	"context"
	"fmt"
	"strings"

	"vozko/domain/copilot"
	"vozko/domain/tools"
	wf "vozko/domain/whatsappform"
	"vozko/domain/workspace"
	"vozko/usecases/copilot/copilottools"
	whatsappform_usecase "vozko/usecases/whatsappform"
)

var errTwoSources = fmt.Errorf("%w: mande starter ou document, nunca os dois", copilottools.ErrInvalidArgs)

var kindWords = map[wf.FieldKind]string{
	wf.FieldShortText: "texto curto", wf.FieldEmail: "e-mail", wf.FieldPhone: "telefone", wf.FieldNumber: "número",
	wf.FieldLongText: "texto longo", wf.FieldRadio: "escolha única", wf.FieldDropdown: "lista de escolha",
	wf.FieldCheckbox: "várias escolhas", wf.FieldMultiChips: "várias escolhas em etiquetas", wf.FieldDate: "data",
	wf.FieldDateRange: "período", wf.FieldOptIn: "aceite", wf.FieldPhoto: "foto", wf.FieldDocument: "documento",
}

var bindingWords = map[wf.BindingTarget]string{
	wf.BindName: "nome do lead", wf.BindEmail: "e-mail do lead", wf.BindBirthDate: "nascimento do lead",
	wf.BindAddressZip: "CEP do lead", wf.BindAddressCity: "cidade do lead", wf.BindAddressState: "estado do lead",
	wf.BindWhatsAppConsent: "aceite de mensagens do lead",
}

type createWhatsAppFormArgs struct {
	Name        string       `json:"name" req:"true" desc:"nome do formulário, único no workspace"`
	Description string       `json:"description" desc:"opcional: para que serve o formulário"`
	Starter     string       `json:"starter" desc:"opcional: começa de um modelo pronto; sem starter nem document o formulário nasce em branco"`
	Document    *wf.Document `json:"document" desc:"opcional: o documento completo do formulário, no lugar de starter"`
}

func (a createWhatsAppFormArgs) source() (wf.Document, string, error) {
	switch {
	case a.Starter != "" && a.Document != nil:
		return wf.Document{}, "", errTwoSources
	case a.Starter != "":
		starter, ok := wf.StarterByKey(strings.TrimSpace(a.Starter))
		if !ok {
			return wf.Document{}, "", wf.ErrStarterUnknown
		}
		return starter.Document, "Modelo inicial " + starter.Name, nil
	case a.Document != nil:
		doc := wf.CloneDocument(*a.Document)
		if doc.Schema == 0 {
			doc.Schema = wf.DocumentSchema
		}
		return doc, "Documento montado pela Elo", nil
	}
	return wf.BlankDocument(), "Em branco", nil
}

type createWhatsAppFormTool struct {
	gate
	deps FormDeps
}

func NewCreateWhatsAppFormTool(deps FormDeps) copilot.Tool {
	return &createWhatsAppFormTool{gate: gated(true, workspace.ResourceWhatsAppForms, workspace.ActionCreate, wf.CapabilityBuild), deps: deps}
}

func (t *createWhatsAppFormTool) Definition() tools.Definition {
	def := copilottools.Definition("create_whatsapp_form",
		"Cria um formulário do WhatsApp (WhatsApp Flow) em rascunho, de um starter ou de um document completo. Não publica nem envia: depois use "+
			"publish_whatsapp_form. O documento tem categories ("+joined(wf.Categories)+") e screens em ordem; cada tela tem key, title, "+
			"submitLabel, blocks e route (next, ou branchOn com branches de optionId para next; sem rota é a última tela). Cada bloco tem key e "+
			"display (kind "+joined(wf.DisplayKinds)+") ou field (key, kind "+joined(wf.FieldKinds)+", label, question opcional acima do campo, "+
			"required, sensitive, options com id e title nas escolhas, binding com target "+bindingTargets()+"). Keys em minúsculas, "+
			"letras, números e _, começando por letra. A aprovação mostra as telas; um documento com problemas volta com a lista para corrigir.",
		createWhatsAppFormArgs{})
	param := def.Parameters["starter"]
	param.Enum = starterKeys()
	def.Parameters["starter"] = param
	return def
}

func joined[T ~string](values []T) string {
	parts := make([]string, 0, len(values))
	for _, v := range values {
		parts = append(parts, string(v))
	}
	return strings.Join(parts, ", ")
}

func bindingTargets() string {
	return joined([]wf.BindingTarget{
		wf.BindName, wf.BindEmail, wf.BindBirthDate, wf.BindAddressZip, wf.BindAddressCity, wf.BindAddressState,
		wf.BindWhatsAppConsent, wf.BindCustomField,
	}) + " (custom_field com customFieldKey)"
}

func (t *createWhatsAppFormTool) Validate(ctx context.Context, cc copilot.Context, args map[string]interface{}) error {
	a, err := copilottools.ValidateArgs[createWhatsAppFormArgs](nil, cc, args)
	if err != nil {
		return err
	}
	if _, err := wf.NormalizeName(a.Name); err != nil {
		return refusalOf(err)
	}
	if _, err := wf.NormalizeDescription(a.Description); err != nil {
		return refusalOf(err)
	}
	doc, _, err := a.source()
	if err != nil {
		return refusalOf(err)
	}
	return checked(ctx, t.deps.Forms, actorOf(cc), doc)
}

func checked(ctx context.Context, forms FormService, a whatsappform_usecase.Actor, doc wf.Document) error {
	problems, err := forms.Check(ctx, a, doc)
	if err != nil {
		return refusalOf(err)
	}
	if len(problems) > 0 {
		return fmt.Errorf("%w: o formulário tem problemas; corrija e proponha de novo: %s", copilottools.ErrInvalidArgs, problemsText(problems))
	}
	return nil
}

func (t *createWhatsAppFormTool) Describe(_ context.Context, _ copilot.Context, args map[string]interface{}) []copilot.Field {
	var a createWhatsAppFormArgs
	copilottools.BindArgs(args, &a)
	fields := []copilot.Field{{Key: "name", Value: strings.TrimSpace(a.Name)}}
	if description := strings.TrimSpace(a.Description); description != "" {
		fields = append(fields, copilot.Field{Key: "description", Value: description})
	}
	doc, kind, err := a.source()
	if err != nil {
		return append(fields, copilot.Field{Key: "kind", Value: "origem do formulário inválida"})
	}
	fields = append(fields, copilot.Field{Key: "kind", Value: kind})
	return append(fields, screenFields(doc)...)
}

func screenFields(doc wf.Document) []copilot.Field {
	out := make([]copilot.Field, 0, len(doc.Screens))
	for i, s := range doc.Screens {
		var parts []string
		for _, b := range s.Blocks {
			if b.Field != nil {
				parts = append(parts, fieldLine(*b.Field))
			}
		}
		line := fmt.Sprintf("Tela %d, %s: ", i+1, strings.TrimSpace(s.Title))
		if len(parts) == 0 {
			line += "só texto"
		} else {
			line += strings.Join(parts, "; ")
		}
		out = append(out, copilot.Field{Key: "questions", Value: copilottools.ClipText(line, copilot.MaxFieldRunes)})
	}
	return out
}

func fieldLine(f wf.Field) string {
	flags := []string{kindWords[f.Kind]}
	if f.Required {
		flags = append(flags, "obrigatório")
	} else {
		flags = append(flags, "opcional")
	}
	if f.IsSensitive() {
		flags = append(flags, "sensível")
	}
	if f.Binding != nil {
		target := bindingWords[f.Binding.Target]
		if f.Binding.Target == wf.BindCustomField {
			target = "campo " + f.Binding.CustomFieldKey + " do lead"
		}
		flags = append(flags, "salva em "+target)
	}
	line := f.Prompt() + " (" + strings.Join(flags, ", ") + ")"
	if len(f.Options) > 0 {
		titles := make([]string, 0, len(f.Options))
		for _, o := range f.Options {
			titles = append(titles, o.Title)
		}
		line += ": " + strings.Join(titles, ", ")
	}
	return line
}

func (t *createWhatsAppFormTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var in createWhatsAppFormArgs
	if err := copilottools.DecodeArgs(args, &in); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	doc, _, err := in.source()
	if err != nil {
		return failure(err)
	}
	a := actorOf(cc)
	detail, err := t.deps.Forms.Create(ctx, a, whatsappform_usecase.CreateInput{Name: in.Name, Description: in.Description, Starter: strings.TrimSpace(in.Starter)})
	if err != nil {
		return failure(err)
	}
	draft := detail.Draft
	if in.Document != nil {
		saved, err := t.deps.Forms.SaveDraft(ctx, a, detail.Form.ID, doc, detail.Form.DraftVersionID)
		if err != nil {
			res := failure(err)
			res.Message = fmt.Sprintf("o formulário %q foi criado (form_id %s), mas o rascunho com o documento não foi salvo: %s", detail.Form.Name, detail.Form.ID, res.Message)
			return res
		}
		draft = saved
	}
	data := map[string]interface{}{
		"form_id": detail.Form.ID, "name": detail.Form.Name, "draft_version": draft.Number,
		"next": "o formulário está em rascunho; para usar nas conversas, publique com publish_whatsapp_form quando o usuário pedir",
	}
	if len(draft.Problems) > 0 {
		data["problems"] = problemsText(draft.Problems)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: data}
}
