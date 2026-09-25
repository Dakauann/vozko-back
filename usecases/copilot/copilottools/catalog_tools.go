package copilottools

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"vozko/domain/calendar"
	"vozko/domain/copilot"
	"vozko/domain/label"
	"vozko/domain/shared"
	"vozko/domain/stage"
	"vozko/domain/tools"
	tmpl "vozko/domain/whatsapp/template"
	"vozko/domain/workflow"
	"vozko/domain/workspace"
)

const (
	catalogPageSize     = 20
	calendarDefaultDays = 7
	calendarMaxDays     = 62
)

type CatalogDeps struct {
	Templates tmpl.WorkspaceTemplatesUseCase
	Funnels   stage.ListFunnelStagesUseCase
	Labels    label.ListLabelsUseCase
	Events    calendar.ListEventsUseCase
	Workflows workflow.ListWorkflowsUseCase
	Now       Clock
}

func readMeta(resource workspace.Resource) copilot.Meta {
	return copilot.Meta{Resource: resource, Action: workspace.ActionRead}
}

func catalogFailure(tool string, err error) copilot.Result {
	if res, ok := departmentFailure(err); ok {
		return res
	}
	log.Printf("[copilot] %s failed: %v", tool, err)
	return copilot.Result{Status: copilot.StatusError, Message: "falha ao consultar os dados"}
}

type listTemplatesArgs struct {
	Search      string `json:"search" desc:"parte do nome do modelo"`
	AllStatuses bool   `json:"all_statuses" desc:"inclui modelos ainda não aprovados pela Meta"`
	Page        int    `json:"page" desc:"página, começa em 1"`
}

type listTemplatesTool struct{ deps CatalogDeps }

func NewListTemplatesTool(deps CatalogDeps) copilot.Tool { return &listTemplatesTool{deps: deps} }

func (t *listTemplatesTool) Meta() copilot.Meta { return readMeta(workspace.ResourceWhatsAppTemplates) }

func (t *listTemplatesTool) Definition() tools.Definition {
	return definition("list_templates",
		"Lista os modelos de mensagem do WhatsApp do workspace (aprovados, por padrão): nome, idioma, categoria, se está pronto "+
			"para envio, quantas variáveis ({{1}}, {{2}}...) o texto pede e o texto do corpo.",
		listTemplatesArgs{})
}

func (t *listTemplatesTool) Execute(_ context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a listTemplatesArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	page := max(a.Page, 1)
	input := tmpl.ListInput{
		Search:  strings.TrimSpace(a.Search),
		Options: shared.QueryOptions{Pagination: shared.Pagination{Page: page, PageSize: catalogPageSize}},
	}
	if !a.AllStatuses {
		input.Status = tmpl.TemplateStatusApproved
	}
	out, err := t.deps.Templates.List(cc.WorkspaceID, input)
	if err != nil {
		return catalogFailure("list_templates", err)
	}
	templates := make([]map[string]interface{}, 0, len(out.Items))
	for _, item := range out.Items {
		if item == nil {
			continue
		}
		templates = append(templates, map[string]interface{}{
			"template_id": item.ID,
			"name":        item.Name,
			"language":    item.Language,
			"category":    item.Category,
			"status":      item.Status,
			"ready":       item.IsReadyToSend(),
			"variables":   item.ParameterCount(),
			"body":        clipText(item.GetBodyText(), 400),
		})
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{
		"page": page, "total": out.TotalItems, "has_more": page < out.TotalPages, "templates": templates,
	}}
}

type listPipelinesTool struct{ deps CatalogDeps }

func NewListPipelinesTool(deps CatalogDeps) copilot.Tool { return &listPipelinesTool{deps: deps} }

func (t *listPipelinesTool) Meta() copilot.Meta { return readMeta(workspace.ResourceStages) }

func (t *listPipelinesTool) Definition() tools.Definition {
	return definition("list_pipelines",
		"Lista os funis de atendimento do workspace com as etapas de cada um, em ordem (inicial, ganho, perdido).",
		struct{}{})
}

func (t *listPipelinesTool) Execute(_ context.Context, cc copilot.Context, _ map[string]interface{}) copilot.Result {
	funnels, err := t.deps.Funnels.Execute(cc.WorkspaceID)
	if err != nil {
		return catalogFailure("list_pipelines", err)
	}
	out := make([]map[string]interface{}, 0, len(funnels))
	for _, f := range funnels {
		stages := make([]map[string]interface{}, 0, len(f.Stages))
		for _, s := range f.Stages {
			if s == nil {
				continue
			}
			stages = append(stages, map[string]interface{}{
				"stage_id": s.ID, "name": s.Name, "initial": s.IsInitial, "won": s.IsWon, "lost": s.IsLost,
			})
		}
		out = append(out, map[string]interface{}{
			"pipeline_id": f.PipelineID, "name": f.PipelineName, "default": f.IsDefault, "stages": stages,
		})
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"pipelines": out}}
}

type listLabelsTool struct{ deps CatalogDeps }

func NewListLabelsTool(deps CatalogDeps) copilot.Tool { return &listLabelsTool{deps: deps} }

func (t *listLabelsTool) Meta() copilot.Meta { return readMeta(workspace.ResourceLabels) }

func (t *listLabelsTool) Definition() tools.Definition {
	return definition("list_labels", "Lista as etiquetas de conversa do workspace.", struct{}{})
}

func (t *listLabelsTool) Execute(_ context.Context, cc copilot.Context, _ map[string]interface{}) copilot.Result {
	labels, err := t.deps.Labels.Execute(cc.WorkspaceID)
	if err != nil {
		return catalogFailure("list_labels", err)
	}
	out := make([]map[string]interface{}, 0, len(labels))
	for _, l := range labels {
		if l != nil {
			out = append(out, map[string]interface{}{"label_id": l.ID, "name": l.Name})
		}
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"labels": out}}
}

type listCalendarEventsArgs struct {
	DateFrom string `json:"date_from" desc:"início YYYY-MM-DD; omita para hoje"`
	DateTo   string `json:"date_to" desc:"fim YYYY-MM-DD (inclusivo); omita para uma semana"`
	Search   string `json:"search" desc:"parte do título"`
}

type listCalendarEventsTool struct{ deps CatalogDeps }

func NewListCalendarEventsTool(deps CatalogDeps) copilot.Tool {
	return &listCalendarEventsTool{deps: deps}
}

func (t *listCalendarEventsTool) Meta() copilot.Meta { return readMeta(workspace.ResourceCalendar) }

func (t *listCalendarEventsTool) Definition() tools.Definition {
	return definition("list_calendar_events", fmt.Sprintf(
		"Lista os compromissos da agenda do próprio usuário num período (até %d dias; padrão, os próximos %d).",
		calendarMaxDays, calendarDefaultDays), listCalendarEventsArgs{})
}

func (t *listCalendarEventsTool) Execute(_ context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a listCalendarEventsArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	from, until, err := t.window(a)
	if err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	out, err := t.deps.Events.Execute(calendar.ListEventsInput{
		WorkspaceID: cc.WorkspaceID,
		UserID:      cc.UserID,
		From:        &from,
		To:          &until,
		Search:      strings.TrimSpace(a.Search),
		Pagination:  shared.Pagination{Page: 1, PageSize: catalogPageSize},
	})
	if err != nil {
		return catalogFailure("list_calendar_events", err)
	}
	events := make([]map[string]interface{}, 0, len(out.Items))
	for _, e := range out.Items {
		if e == nil {
			continue
		}
		events = append(events, map[string]interface{}{
			"title":    e.Title,
			"start":    e.StartTime.UTC().Format(time.RFC3339),
			"end":      e.EndTime.UTC().Format(time.RFC3339),
			"all_day":  e.AllDay,
			"location": e.Location,
		})
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{
		"from": from.Format(time.DateOnly), "to": until.AddDate(0, 0, -1).Format(time.DateOnly),
		"total": out.TotalItems, "events": events,
	}}
}

func (t *listCalendarEventsTool) window(a listCalendarEventsArgs) (time.Time, time.Time, error) {
	start, err := dayStart(a.DateFrom, "date_from")
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	end, err := dayStart(a.DateTo, "date_to")
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	from := t.deps.Now().UTC().Truncate(24 * time.Hour)
	if start != nil {
		from = *start
	}
	until := from.AddDate(0, 0, calendarDefaultDays)
	if end != nil {
		until = end.AddDate(0, 0, 1)
	}
	if !until.After(from) {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: date_to deve ser depois de date_from", errInvalidArgs)
	}
	if until.Sub(from) > calendarMaxDays*24*time.Hour {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: consulte no máximo %d dias por vez", errInvalidArgs, calendarMaxDays)
	}
	return from, until, nil
}

type listWorkflowsArgs struct {
	Search string `json:"search" desc:"parte do nome da automação"`
	Page   int    `json:"page" desc:"página, começa em 1"`
}

type listWorkflowsTool struct{ deps CatalogDeps }

func NewListWorkflowsTool(deps CatalogDeps) copilot.Tool { return &listWorkflowsTool{deps: deps} }

func (t *listWorkflowsTool) Meta() copilot.Meta { return readMeta(workspace.ResourceWorkflows) }

func (t *listWorkflowsTool) Definition() tools.Definition {
	return definition("list_workflows",
		"Lista as automações (workflows) que o usuário pode ver, com o status (active, paused, draft, archived) e o tipo de gatilho.",
		listWorkflowsArgs{})
}

func (t *listWorkflowsTool) Execute(_ context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a listWorkflowsArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	page := max(a.Page, 1)
	departments, blocked := cc.Departments.ListScope()
	if blocked {
		return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"page": page, "total": 0, "has_more": false, "workflows": []interface{}{}}}
	}
	out, err := t.deps.Workflows.Execute(workflow.ListWorkflowsInput{
		WorkspaceID:   cc.WorkspaceID,
		DepartmentIDs: departments,
		Search:        strings.TrimSpace(a.Search),
		Options:       shared.QueryOptions{Pagination: shared.Pagination{Page: page, PageSize: catalogPageSize}},
	})
	if err != nil {
		return catalogFailure("list_workflows", err)
	}
	workflows := make([]map[string]interface{}, 0, len(out.Items))
	for _, w := range out.Items {
		if w == nil {
			continue
		}
		workflows = append(workflows, map[string]interface{}{
			"workflow_id": w.ID, "name": w.Name, "status": w.Status, "trigger": w.TriggerType, "description": clipText(w.Description, 200),
		})
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{
		"page": page, "total": out.TotalItems, "has_more": page < out.TotalPages, "workflows": workflows,
	}}
}
