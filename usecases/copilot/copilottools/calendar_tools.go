package copilottools

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"vozko/domain/calendar"
	"vozko/domain/copilot"
	"vozko/domain/tools"
	"vozko/domain/workspace"
)

type createCalendarEventArgs struct {
	Title       string `json:"title" req:"true" desc:"título do compromisso"`
	Start       string `json:"start" req:"true" desc:"início com fuso, RFC3339 (ex.: 2026-09-26T10:00:00-03:00)"`
	End         string `json:"end" req:"true" desc:"fim com fuso, RFC3339"`
	Description string `json:"description" desc:"detalhes, por exemplo o assunto da ligação"`
}

type createCalendarEventTool struct{ create calendar.CreateEventUseCase }

func NewCreateCalendarEventTool(create calendar.CreateEventUseCase) copilot.Tool {
	return &createCalendarEventTool{create: create}
}

func (t *createCalendarEventTool) Meta() copilot.Meta {
	return copilot.Meta{Mutating: true, Resource: workspace.ResourceCalendar, Action: workspace.ActionCreate}
}

func (t *createCalendarEventTool) Definition() tools.Definition {
	return definition("create_calendar_event",
		"Cria um compromisso na agenda do próprio usuário (por exemplo, retornar a ligação de um cliente). Só depois da aprovação do usuário.",
		createCalendarEventArgs{})
}

func (t *createCalendarEventTool) Execute(_ context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a createCalendarEventArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	start, errStart := time.Parse(time.RFC3339, strings.TrimSpace(a.Start))
	end, errEnd := time.Parse(time.RFC3339, strings.TrimSpace(a.End))
	if errStart != nil || errEnd != nil {
		return copilot.Result{Status: copilot.StatusError, Message: "start e end devem ter data, hora e fuso (ex.: 2026-09-26T10:00:00-03:00)"}
	}
	event, err := t.create.Execute(calendar.CreateEventInput{
		WorkspaceID: cc.WorkspaceID,
		UserID:      cc.UserID,
		Title:       strings.TrimSpace(a.Title),
		Description: strings.TrimSpace(a.Description),
		StartTime:   start,
		EndTime:     end,
	})
	if err != nil {
		return calendarFailure(err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"created": true, "title": event.Title}}
}

func calendarFailure(err error) copilot.Result {
	switch {
	case errors.Is(err, calendar.ErrGoogleNotConnected):
		return copilot.Result{Status: copilot.StatusError, Message: "a agenda do Google não está conectada neste workspace; conecte em Agenda"}
	case errors.Is(err, calendar.ErrInvalidTimeRange):
		return copilot.Result{Status: copilot.StatusError, Message: "o fim precisa ser depois do início"}
	case errors.Is(err, calendar.ErrTitleRequired):
		return copilot.Result{Status: copilot.StatusError, Message: "o título é obrigatório"}
	}
	log.Printf("[copilot] create_calendar_event failed: %v", err)
	return copilot.Result{Status: copilot.StatusError, Message: "falha ao criar o compromisso"}
}

func (t *createCalendarEventTool) Validate(_ context.Context, cc copilot.Context, args map[string]interface{}) error {
	_, err := validateArgs[createCalendarEventArgs](nil, cc, args)
	return err
}
