package formtools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/copilot"
	"vozko/domain/report"
	"vozko/domain/shared"
	wf "vozko/domain/whatsappform"
	"vozko/domain/workspace"
	"vozko/usecases/copilot/copilottools"
	report_usecase "vozko/usecases/report"
	whatsappform_usecase "vozko/usecases/whatsappform"
)

type FormService interface {
	List(ctx context.Context, a whatsappform_usecase.Actor, search string, archived bool) ([]wf.FormSummary, error)
	Get(ctx context.Context, a whatsappform_usecase.Actor, formID string) (whatsappform_usecase.FormDetail, error)
	Create(ctx context.Context, a whatsappform_usecase.Actor, in whatsappform_usecase.CreateInput) (whatsappform_usecase.FormDetail, error)
	SaveDraft(ctx context.Context, a whatsappform_usecase.Actor, formID string, doc wf.Document, baseVersionID string) (wf.Version, error)
	Check(ctx context.Context, a whatsappform_usecase.Actor, doc wf.Document) (wf.Problems, error)
	Publish(ctx context.Context, a whatsappform_usecase.Actor, formID string) (whatsappform_usecase.PublishResult, error)
	PublishTargets(ctx context.Context, a whatsappform_usecase.Actor) ([]whatsappform_usecase.PublishTarget, error)
	Sendable(ctx context.Context, a whatsappform_usecase.Actor, q whatsappform_usecase.SendableQuery) ([]whatsappform_usecase.SendableForm, error)
	SendInConversation(ctx context.Context, a whatsappform_usecase.Actor, in whatsappform_usecase.SendInput) (wf.Send, error)
	ListResponses(ctx context.Context, a whatsappform_usecase.Actor, q wf.ResponseQuery) (wf.Page[whatsappform_usecase.ResponseRow], error)
	Stats(ctx context.Context, a whatsappform_usecase.Actor, formID string, p whatsappform_usecase.Period) (wf.Stats, error)
	Distribution(ctx context.Context, a whatsappform_usecase.Actor, formID, fieldKey string, p whatsappform_usecase.Period) (map[string]int64, error)
	ScaffoldAutomation(ctx context.Context, a whatsappform_usecase.Actor, formID string) (string, error)
}

type ReportStarter interface {
	Create(in report_usecase.CreateInput) (*report.Job, error)
}

type FormDeps struct {
	Forms   FormService
	Reports ReportStarter
	Entries conversation.EntryLookup
}

func FormTools(deps FormDeps) []copilot.Tool {
	return []copilot.Tool{
		NewListWhatsAppFormsTool(deps),
		NewWhatsAppFormResultsTool(deps),
		NewWhatsAppFormResponsesTool(deps),
		NewCreateWhatsAppFormTool(deps),
		NewPublishWhatsAppFormTool(deps),
		NewSendWhatsAppFormTool(deps),
		NewCreateFormAutomationTool(deps),
		NewExportWhatsAppFormResponsesTool(deps),
	}
}

func actorOf(cc copilot.Context) whatsappform_usecase.Actor {
	return copilottools.ViewerOf(cc)
}

func capabilityEntries(keys ...workspace.CapabilityKey) []workspace.PermissionEntry {
	entries, err := workspace.CapabilitiesRequire(keys)
	if err != nil || len(entries) == 0 {
		return []workspace.PermissionEntry{{Resource: workspace.Resource(fmt.Sprint(keys))}}
	}
	return entries
}

type gate struct {
	meta  copilot.Meta
	needs []workspace.PermissionEntry
}

func gated(mutating bool, resource workspace.Resource, action workspace.Action, keys ...workspace.CapabilityKey) gate {
	return gate{meta: copilot.Meta{Mutating: mutating, Resource: resource, Action: action}, needs: capabilityEntries(keys...)}
}

func (g gate) Meta() copilot.Meta { return g.meta }

func (g gate) AlsoRequires() []workspace.PermissionEntry {
	return append([]workspace.PermissionEntry(nil), g.needs...)
}

type formRef struct {
	FormID   string `json:"form_id" id:"true" desc:"form_id exato de list_whatsapp_forms"`
	FormName string `json:"form_name" desc:"ou o nome exato do formulário, quando não tiver o form_id"`
}

var errNoForm = fmt.Errorf("%w: diga qual formulário com form_id (de list_whatsapp_forms) ou form_name", copilottools.ErrInvalidArgs)

func (r formRef) resolve(ctx context.Context, forms FormService, a whatsappform_usecase.Actor) (whatsappform_usecase.FormDetail, error) {
	if id := strings.TrimSpace(r.FormID); id != "" {
		return forms.Get(ctx, a, id)
	}
	name := strings.TrimSpace(r.FormName)
	if name == "" {
		return whatsappform_usecase.FormDetail{}, errNoForm
	}
	summaries, err := forms.List(ctx, a, name, false)
	if err != nil {
		return whatsappform_usecase.FormDetail{}, err
	}
	for _, s := range summaries {
		if strings.EqualFold(strings.TrimSpace(s.Form.Name), name) {
			return forms.Get(ctx, a, s.Form.ID)
		}
	}
	return whatsappform_usecase.FormDetail{}, wf.ErrFormNotFound
}

type periodArgs struct {
	From string `json:"from" desc:"opcional: início do período, AAAA-MM-DD"`
	To   string `json:"to" desc:"opcional: fim do período, AAAA-MM-DD, inclusive"`
}

var errPeriod = fmt.Errorf("%w: from e to são datas AAAA-MM-DD", copilottools.ErrInvalidArgs)

func (p periodArgs) period() (whatsappform_usecase.Period, error) {
	from, err := bound(p.From, false)
	if err != nil {
		return whatsappform_usecase.Period{}, err
	}
	to, err := bound(p.To, true)
	if err != nil {
		return whatsappform_usecase.Period{}, err
	}
	return whatsappform_usecase.Period{From: from, To: to}, nil
}

func bound(raw string, endOfDay bool) (*time.Time, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	at := shared.ParseDateBound(raw, endOfDay)
	if at == nil {
		return nil, errPeriod
	}
	return at, nil
}

func (p periodArgs) shown() string {
	from, to := shownDay(p.From), shownDay(p.To)
	switch {
	case from != "" && to != "":
		return from + " a " + to
	case from != "":
		return "desde " + from
	case to != "":
		return "até " + to
	}
	return "todas as respostas"
}

func shownDay(raw string) string {
	at := shared.ParseDateBound(raw, false)
	if at == nil {
		return strings.TrimSpace(raw)
	}
	return at.Format("02/01/2006")
}

func stamp(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

func documentOf(detail whatsappform_usecase.FormDetail) wf.Document {
	if detail.Published != nil {
		return detail.Published.Document
	}
	return detail.Draft.Document
}
