package formtools

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"vozko/domain/copilot"
	"vozko/domain/tools"
	wf "vozko/domain/whatsappform"
	"vozko/domain/workspace"
	"vozko/usecases/copilot/copilottools"
	whatsappform_usecase "vozko/usecases/whatsappform"
)

var statusWords = map[wf.PublicationStatus]string{
	wf.PublicationCreating: "publicando", wf.PublicationDraft: "rascunho na Meta", wf.PublicationPublished: "publicado",
	wf.PublicationThrottled: "limitado pela Meta", wf.PublicationBlocked: "bloqueado pela Meta",
	wf.PublicationDeprecated: "descontinuado", wf.PublicationFailed: "falhou",
}

type publishArgs struct {
	FormID   string   `json:"form_id" id:"true" desc:"form_id exato de list_whatsapp_forms"`
	FormName string   `json:"form_name" desc:"ou o nome exato do formulário, quando não tiver o form_id"`
	Accounts []string `json:"accounts" desc:"ids das contas do WhatsApp (WABA) onde publicar; pergunte ao usuário quando houver mais de uma"`
}

func (p publishArgs) ref() formRef {
	return formRef{FormID: p.FormID, FormName: p.FormName}
}

type publishWhatsAppFormTool struct {
	gate
	deps FormDeps
}

func NewPublishWhatsAppFormTool(deps FormDeps) copilot.Tool {
	return &publishWhatsAppFormTool{gate: gated(true, workspace.ResourceWhatsAppForms, workspace.ActionConfigure, wf.CapabilityPublish), deps: deps}
}

func (t *publishWhatsAppFormTool) Definition() tools.Definition {
	return copilottools.Definition("publish_whatsapp_form",
		"Publica o rascunho de um formulário do WhatsApp na Meta, nas contas do WhatsApp oficial escolhidas em accounts; depois disso ele pode "+
			"ser enviado nas conversas. O rascunho é conferido antes da aprovação, e um rascunho com problemas volta com a lista para corrigir. "+
			"Depois da aprovação devolve o resultado de cada conta. Só depois da aprovação do usuário.",
		publishArgs{})
}

func activeForm(ctx context.Context, forms FormService, cc copilot.Context, ref formRef) (whatsappform_usecase.FormDetail, error) {
	detail, err := ref.resolve(ctx, forms, actorOf(cc))
	if err != nil {
		return whatsappform_usecase.FormDetail{}, err
	}
	if err := detail.Form.EnsureActive(); err != nil {
		return whatsappform_usecase.FormDetail{}, err
	}
	return detail, nil
}

func (t *publishWhatsAppFormTool) Validate(ctx context.Context, cc copilot.Context, args map[string]interface{}) error {
	in, err := copilottools.ValidateArgs[publishArgs](nil, cc, args)
	if err != nil {
		return err
	}
	detail, err := activeForm(ctx, t.deps.Forms, cc, in.ref())
	if err != nil {
		return refusalOf(err)
	}
	if len(in.Accounts) == 0 {
		return fmt.Errorf("%w: diga em accounts em quais contas publicar; contas do workspace: %s", copilottools.ErrInvalidArgs, t.destination(ctx, cc, detail.Publications, nil))
	}
	if len(detail.Draft.Problems) > 0 {
		return fmt.Errorf("%w: o rascunho tem problemas; corrija e proponha de novo: %s", copilottools.ErrInvalidArgs, problemsText(detail.Draft.Problems))
	}
	return checked(ctx, t.deps.Forms, actorOf(cc), detail.Draft.Document)
}

func (t *publishWhatsAppFormTool) Describe(ctx context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	var in publishArgs
	copilottools.BindArgs(args, &in)
	detail, err := in.ref().resolve(ctx, t.deps.Forms, actorOf(cc))
	if err != nil {
		return []copilot.Field{{Key: "form", Value: "formulário desconhecido ou sem acesso"}}
	}
	return []copilot.Field{
		{Key: "form", Value: detail.Form.Name},
		{Key: "draft", Value: fmt.Sprintf("versão %d", detail.Draft.Number)},
		{Key: "destination", Value: copilottools.ClipText(t.destination(ctx, cc, detail.Publications, in.Accounts), copilot.MaxFieldRunes)},
	}
}

func (t *publishWhatsAppFormTool) destination(ctx context.Context, cc copilot.Context, pubs []wf.Publication, chosen []string) string {
	targets, err := t.deps.Forms.PublishTargets(ctx, actorOf(cc))
	if err != nil || len(targets) == 0 {
		return "nenhuma conta do WhatsApp oficial conectada ao workspace"
	}
	current := map[string]wf.PublicationStatus{}
	for _, p := range pubs {
		current[p.MetaWABAID] = p.Status
	}
	parts := make([]string, 0, len(targets))
	for _, target := range targets {
		if len(chosen) > 0 && !slices.Contains(chosen, target.MetaWABAID) {
			continue
		}
		var state string
		switch status, published := current[target.MetaWABAID]; {
		case !target.Supported:
			state = "provedor sem suporte a formulários"
		case published:
			state = statusOf(status)
		default:
			state = "primeira publicação"
		}
		if target.Supported && !target.BusinessVerified {
			state += "; empresa não verificada na Meta, a Meta pode recusar"
		}
		parts = append(parts, target.MetaWABAID+" ("+state+")")
	}
	return strings.Join(parts, ", ")
}

func statusOf(status wf.PublicationStatus) string {
	if words, ok := statusWords[status]; ok {
		return words
	}
	return string(status)
}

func (t *publishWhatsAppFormTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var in publishArgs
	if err := copilottools.DecodeArgs(args, &in); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	a := actorOf(cc)
	detail, err := in.ref().resolve(ctx, t.deps.Forms, a)
	if err != nil {
		return failure(err)
	}
	result, err := t.deps.Forms.Publish(ctx, a, detail.Form.ID, in.Accounts)
	if err != nil {
		return failure(err)
	}
	outcomes := make([]map[string]interface{}, 0, len(result.Outcomes))
	published := 0
	for _, o := range result.Outcomes {
		row := map[string]interface{}{"account": o.MetaWABAID, "status": string(o.Status)}
		if problem := outcomeProblem(o); problem != "" {
			row["problem"] = problem
		} else if o.Status == wf.PublicationPublished {
			published++
		}
		outcomes = append(outcomes, row)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{
		"form_id": detail.Form.ID, "name": detail.Form.Name, "outcomes": outcomes, "published_on": published,
	}}
}

func outcomeProblem(o whatsappform_usecase.PublishOutcome) string {
	var parts []string
	if message := codeMessage(o.Code); message != "" {
		parts = append(parts, message)
	} else if o.Error != "" {
		parts = append(parts, o.Error)
	}
	if len(o.Problems) > 0 {
		parts = append(parts, problemsText(o.Problems))
	}
	return strings.Join(parts, ": ")
}
