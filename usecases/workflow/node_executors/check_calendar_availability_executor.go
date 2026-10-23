package node_executors

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"vozko/domain/calendar"
	"vozko/domain/workflow"
)

type checkCalendarAvailabilityExecutor struct {
	repo   calendar.Repository
	google calendar.GoogleCalendarAPI
	access calendar.AccessProvider
}

func NewCheckCalendarAvailabilityExecutor(repo calendar.Repository, google calendar.GoogleCalendarAPI, access calendar.AccessProvider) workflow.NodeExecutor {
	return &checkCalendarAvailabilityExecutor{repo: repo, google: google, access: access}
}

func (e *checkCalendarAvailabilityExecutor) Definition() workflow.NodeDefinition {
	return workflow.NodeDefinition{
		Type:         workflow.NodeTypeActionCheckCalendarAvailability,
		TestBehavior: workflow.NodeTestExternalCall,
		Category:     workflow.NodeCategoryAction,
		Scopes:       []workflow.NodeScope{workflow.NodeScopeShared},
		Label:        "Verificar Disponibilidade",
		Description:  "Consulta o Google Calendar do workspace e retorna horários livres no período informado.",
		Icon:         "CalendarBlank",
		Guidance: workflow.NodeGuidance{
			When:     "Para consultar horários livres no Google Calendar.",
			Behavior: "date_from/date_to aceitam YYYY-MM-DD (dia de expediente inteiro) ou data/hora ISO/RFC3339. date_to vazio consulta até o fim do expediente. Datas iguais sem hora consultam um único dia. Use timezone do workspace. Não invente disponibilidade; só confirme após success=true.",
		},
		Outputs: []workflow.HandleDefinition{
			{ID: "sucesso", Label: "Sucesso"},
			{ID: "erro", Label: "Erro", Optional: true},
		},
		OutputKeys: []workflow.OutputKeyDefinition{
			{Key: "success", Description: "true quando a consulta foi bem-sucedida", Label: "Deu certo", Type: workflow.VariableTypeBoolean},
			{Key: "available_slots", Description: "Lista de horários livres formatados", Label: "Horários livres", Type: workflow.VariableTypeText},
			{Key: "available_slots_json", Description: "JSON array com objetos {start, end} dos slots livres", Label: "Horários livres (JSON)", Type: workflow.VariableTypeText},
			{Key: "busy_count", Description: "Quantidade de eventos ocupados encontrados", Label: "Eventos ocupados", Type: workflow.VariableTypeNumber},
			{Key: "date_from", Description: "Início do período consultado (RFC3339)", Label: "Início do período", Type: workflow.VariableTypeDate},
			{Key: "date_to", Description: "Fim do período consultado (RFC3339)", Label: "Fim do período", Type: workflow.VariableTypeDate},
			{Key: "timezone", Description: "Timezone efetivamente usada", Label: "Fuso horário", Type: workflow.VariableTypeText},
			{Key: "calendar_email", Description: "Conta Google conectada ao workspace", Label: "Conta Google", Type: workflow.VariableTypeText},
			{Key: "error", Description: "Descrição do erro quando a consulta falha", Label: "Erro", Type: workflow.VariableTypeText},
		},
		DefaultConfig: map[string]interface{}{
			"date_from":     "{{var.date_from}}",
			"date_to":       "",
			"timezone":      "America/Sao_Paulo",
			"slot_duration": 30,
			"working_start": "08:00",
			"working_end":   "18:00",
			"work_days":     "weekdays",
		},
		ConfigSchema: []workflow.ConfigField{
			{Key: "date_from", Label: "Data/hora início", Type: "text", Placeholder: "YYYY-MM-DD ou 2026-04-10T08:00:00-03:00 ou {{var.date_from}}", Required: true},
			{Key: "date_to", Label: "Data/hora fim", Type: "text", Placeholder: "Opcional. Se vazio, consulta até o fim do horário comercial do dia"},
			{Key: "timezone", Label: "Timezone", Type: "text", Placeholder: "America/Sao_Paulo", Required: true},
			{Key: "slot_duration", Label: "Duração do slot (minutos)", Type: "number", Placeholder: "30"},
			{Key: "working_start", Label: "Início do expediente", Type: "text", Placeholder: "08:00"},
			{Key: "working_end", Label: "Fim do expediente", Type: "text", Placeholder: "18:00"},
			{
				Key:   "work_days",
				Label: "Dias de trabalho",
				Type:  "select",
				Options: []workflow.ConfigFieldOption{
					{Value: "weekdays", Label: "Segunda a Sexta"},
					{Value: "weekdays_saturday", Label: "Segunda a Sábado"},
					{Value: "all_week", Label: "Todos os dias"},
				},
			},
		},
	}
}

func (e *checkCalendarAvailabilityExecutor) Execute(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
	if e.repo == nil || e.google == nil || e.access == nil {
		return calAvailFailure(ctx, "integração Google Calendar indisponível no servidor"), nil
	}

	workspaceID := calendarWorkspaceID(ctx)
	if workspaceID == "" {
		return calAvailFailure(ctx, "workspace do fluxo não encontrado"), nil
	}

	googleAccess, err := e.access.CalendarAccess(context.Background(), workspaceID)
	if err != nil {
		return calAvailFailure(ctx, calendarAccessFailure(err)), nil
	}

	timezoneName := strings.TrimSpace(calendarConfigText(ctx, "timezone"))
	if timezoneName == "" {
		timezoneName = "America/Sao_Paulo"
	}
	loc, err := time.LoadLocation(timezoneName)
	if err != nil {
		return calAvailFailure(ctx, fmt.Sprintf("timezone inválida: %s", timezoneName)), nil
	}

	fromRaw := strings.TrimSpace(calendarConfigText(ctx, "date_from"))
	if fromRaw == "" {
		return calAvailFailure(ctx, "o campo Data/hora início é obrigatório"), nil
	}
	hours := calendar.WorkingHours{
		Start:    calendar.ParseClock(calendarConfigText(ctx, "working_start"), calendar.DefaultWorkStart),
		End:      calendar.ParseClock(calendarConfigText(ctx, "working_end"), calendar.DefaultWorkEnd),
		Days:     calendar.ParseWorkDays(calendarConfigText(ctx, "work_days")),
		Location: loc,
	}
	dateFrom, dateTo, err := calendar.AvailabilityRange(fromRaw, calendarConfigText(ctx, "date_to"), hours.Start, hours.End, loc)
	if err != nil {
		return calAvailFailure(ctx, err.Error()), nil
	}

	slotMinutes := 30
	if raw, ok := ctx.Node.Config["slot_duration"]; ok {
		switch v := raw.(type) {
		case float64:
			if v > 0 {
				slotMinutes = int(v)
			}
		case int:
			if v > 0 {
				slotMinutes = v
			}
		}
	}

	accessToken := googleAccess.Token

	events, err := e.google.ListGoogleEvents(accessToken, dateFrom, dateTo, "", 250)
	if err != nil {
		return calAvailFailure(ctx, fmt.Sprintf("falha ao consultar eventos: %v", err)), nil
	}

	events, err = calendar.WithLocalEvents(e.repo, workspaceID, dateFrom, dateTo, events)
	if err != nil {
		return calAvailFailure(ctx, err.Error()), nil
	}

	slots := calendar.FreeSlots(dateFrom, dateTo, events, time.Duration(slotMinutes)*time.Minute, hours)

	var textParts []string
	var jsonSlots []map[string]string
	for _, s := range slots {
		textParts = append(textParts, fmt.Sprintf("%s a %s",
			s.Start.In(loc).Format("02/01 15:04"),
			s.End.In(loc).Format("15:04"),
		))
		jsonSlots = append(jsonSlots, map[string]string{
			"start": s.Start.Format(time.RFC3339),
			"end":   s.End.Format(time.RFC3339),
		})
	}

	log.Printf("[workflow][check_calendar_availability][run:%s] workspace=%s found %d busy events, %d free slots in [%s..%s]",
		calendarRunID(ctx), workspaceID, len(events), len(slots),
		dateFrom.Format(time.RFC3339), dateTo.Format(time.RFC3339))

	availableText := strings.Join(textParts, "\n")
	if len(textParts) == 0 {
		availableText = "Nenhum horário disponível no período."
	}

	return routeOutput(&workflow.NodeResult{
		Output: map[string]interface{}{
			"success":              true,
			"available_slots":      availableText,
			"available_slots_json": jsonSlots,
			"busy_count":           len(events),
			"date_from":            dateFrom.Format(time.RFC3339),
			"date_to":              dateTo.Format(time.RFC3339),
			"timezone":             timezoneName,
			"calendar_email":       googleAccess.Email,
		},
	}, outgoingEdges(ctx), "sucesso"), nil
}

func calAvailFailure(ctx *workflow.NodeContext, message string) *workflow.NodeResult {
	return routeFailure(&workflow.NodeResult{
		Output: map[string]interface{}{
			"success": false,
			"error":   message,
		},
	}, outgoingEdges(ctx), "erro")
}
