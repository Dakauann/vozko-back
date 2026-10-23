package calendar

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	calendardomain "vozko/domain/calendar"
	"vozko/domain/shared"
	"vozko/infra/http/middleware"
)

type CalendarHandler struct {
	createEvent        calendardomain.CreateEventUseCase
	updateEvent        calendardomain.UpdateEventUseCase
	deleteEvent        calendardomain.DeleteEventUseCase
	getEvent           calendardomain.GetEventUseCase
	listEvents         calendardomain.ListEventsUseCase
	startWatch         calendardomain.StartWatchUseCase
	stopWatch          calendardomain.StopWatchUseCase
	handleNotification calendardomain.HandleNotificationUseCase
}

func NewCalendarHandler(
	createEvent calendardomain.CreateEventUseCase,
	updateEvent calendardomain.UpdateEventUseCase,
	deleteEvent calendardomain.DeleteEventUseCase,
	getEvent calendardomain.GetEventUseCase,
	listEvents calendardomain.ListEventsUseCase,
	startWatch calendardomain.StartWatchUseCase,
	stopWatch calendardomain.StopWatchUseCase,
	handleNotification calendardomain.HandleNotificationUseCase,
) *CalendarHandler {
	return &CalendarHandler{
		createEvent:        createEvent,
		updateEvent:        updateEvent,
		deleteEvent:        deleteEvent,
		getEvent:           getEvent,
		listEvents:         listEvents,
		startWatch:         startWatch,
		stopWatch:          stopWatch,
		handleNotification: handleNotification,
	}
}

// @Summary		Criar um evento na agenda
// @Description	Cria um novo evento na agenda do workspace. Quando o Google Agenda está conectado, o evento também é sincronizado com o Google.
// @Tags			Agenda
// @Accept			json
// @Produce		json
// @Param			request	body		CreateEventRequest	true	"Dados do evento"
// @Success		201	{object}	EventEnvelope
// @Failure		400	{object}	response.ErrorResponse
// @Failure		412	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/calendar/events [post]
func (h *CalendarHandler) Create(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	wsID := middleware.GetWorkspaceID(r)

	var req CreateEventRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{
			"title":     "string (required)",
			"startTime": "ISO 8601 datetime (required)",
			"endTime":   "ISO 8601 datetime (required)",
		})
		return
	}

	if errs := req.Validate(); errs != nil {
		response.WriteValidationError(w, errs)
		return
	}

	startTime, err := time.Parse(time.RFC3339, req.StartTime)
	if err != nil {
		response.WriteValidationError(w, map[string]string{"startTime": "must be valid ISO 8601 / RFC3339 datetime"})
		return
	}
	endTime, err := time.Parse(time.RFC3339, req.EndTime)
	if err != nil {
		response.WriteValidationError(w, map[string]string{"endTime": "must be valid ISO 8601 / RFC3339 datetime"})
		return
	}

	event, err := h.createEvent.Execute(calendardomain.CreateEventInput{
		WorkspaceID:             wsID,
		UserID:                  claims.UserID,
		Title:                   req.Title,
		Description:             req.Description,
		Location:                req.Location,
		StartTime:               startTime,
		EndTime:                 endTime,
		AllDay:                  req.AllDay,
		TimeZone:                req.TimeZone,
		Color:                   req.Color,
		Attendees:               req.Attendees,
		MeetingLink:             req.MeetingLink,
		CreateGoogleMeet:        req.CreateGoogleMeet,
		GuestsCanModify:         req.GuestsCanModify,
		GuestsCanInviteOthers:   req.GuestsCanInviteOthers,
		GuestsCanSeeOtherGuests: req.GuestsCanSeeOtherGuests,
		Visibility:              req.Visibility,
		Transparency:            req.Transparency,
		Recurrence:              req.Recurrence,
		RemindersUseDefault:     req.RemindersUseDefault,
		ReminderOverrides:       req.ReminderOverrides,
		SendUpdates:             req.SendUpdates,
	})
	if err != nil {
		if errors.Is(err, calendardomain.ErrTitleRequired) || errors.Is(err, calendardomain.ErrInvalidTimeRange) {
			response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
			return
		}
		if errors.Is(err, calendardomain.ErrGoogleNotConnected) {
			response.WriteError(w, http.StatusPreconditionFailed, "Google Calendar not connected", nil)
			return
		}
		log.Printf("[calendar] create error: %v", err)
		response.WriteError(w, http.StatusInternalServerError, "Failed to create event", nil)
		return
	}

	response.WriteSuccess(w, http.StatusCreated, EventEnvelope{Event: toEventResponse(event)})
}

// @Summary		Atualizar um evento da agenda
// @Description	Atualiza os dados de um evento existente. Somente os campos enviados são alterados.
// @Tags			Agenda
// @Accept			json
// @Produce		json
// @Param			id		path	string				true	"Identificador do evento"
// @Param			request	body	UpdateEventRequest	true	"Campos a atualizar"
// @Success		200	{object}	EventEnvelope
// @Failure		400	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		412	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/calendar/events/{id} [put]
func (h *CalendarHandler) Update(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	wsID := middleware.GetWorkspaceID(r)
	eventID := mux.Vars(r)["id"]

	var req UpdateEventRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{"title": "string", "startTime": "ISO 8601"})
		return
	}

	input := calendardomain.UpdateEventInput{
		EventID:                 eventID,
		WorkspaceID:             wsID,
		UserID:                  claims.UserID,
		Title:                   req.Title,
		Description:             req.Description,
		Location:                req.Location,
		AllDay:                  req.AllDay,
		TimeZone:                req.TimeZone,
		Color:                   req.Color,
		Attendees:               req.Attendees,
		MeetingLink:             req.MeetingLink,
		GuestsCanModify:         req.GuestsCanModify,
		GuestsCanInviteOthers:   req.GuestsCanInviteOthers,
		GuestsCanSeeOtherGuests: req.GuestsCanSeeOtherGuests,
		Visibility:              req.Visibility,
		Transparency:            req.Transparency,
		Recurrence:              req.Recurrence,
		RemindersUseDefault:     req.RemindersUseDefault,
		ReminderOverrides:       req.ReminderOverrides,
		SendUpdates:             req.SendUpdates,
	}

	if req.StartTime != nil {
		t, err := time.Parse(time.RFC3339, *req.StartTime)
		if err != nil {
			response.WriteValidationError(w, map[string]string{"startTime": "must be valid ISO 8601"})
			return
		}
		input.StartTime = &t
	}
	if req.EndTime != nil {
		t, err := time.Parse(time.RFC3339, *req.EndTime)
		if err != nil {
			response.WriteValidationError(w, map[string]string{"endTime": "must be valid ISO 8601"})
			return
		}
		input.EndTime = &t
	}

	event, err := h.updateEvent.Execute(input)
	if err != nil {
		if errors.Is(err, calendardomain.ErrEventNotFound) {
			response.WriteError(w, http.StatusNotFound, "Event not found", nil)
			return
		}
		if errors.Is(err, calendardomain.ErrTitleRequired) || errors.Is(err, calendardomain.ErrInvalidTimeRange) {
			response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
			return
		}
		if errors.Is(err, calendardomain.ErrGoogleNotConnected) {
			response.WriteError(w, http.StatusPreconditionFailed, "Google Calendar not connected", nil)
			return
		}
		log.Printf("[calendar] update error: %v", err)
		response.WriteError(w, http.StatusInternalServerError, "Failed to update event", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, EventEnvelope{Event: toEventResponse(event)})
}

// @Summary		Remover um evento da agenda
// @Description	Remove um evento da agenda do workspace. Quando o Google Agenda está conectado, o evento também é removido do Google.
// @Tags			Agenda
// @Produce		json
// @Param			id	path	string	true	"Identificador do evento"
// @Success		200	{object}	DeletedResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		412	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/calendar/events/{id} [delete]
func (h *CalendarHandler) Delete(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	wsID := middleware.GetWorkspaceID(r)
	eventID := mux.Vars(r)["id"]

	if err := h.deleteEvent.Execute(eventID, wsID, claims.UserID); err != nil {
		if errors.Is(err, calendardomain.ErrEventNotFound) {
			response.WriteError(w, http.StatusNotFound, "Event not found", nil)
			return
		}
		if errors.Is(err, calendardomain.ErrGoogleNotConnected) {
			response.WriteError(w, http.StatusPreconditionFailed, "Google Calendar not connected", nil)
			return
		}
		log.Printf("[calendar] delete error: %v", err)
		response.WriteError(w, http.StatusInternalServerError, "Failed to delete event", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, DeletedResponse{Deleted: true})
}

// @Summary		Consultar um evento da agenda
// @Description	Retorna os detalhes de um evento da agenda a partir do seu identificador.
// @Tags			Agenda
// @Produce		json
// @Param			id	path	string	true	"Identificador do evento"
// @Success		200	{object}	EventEnvelope
// @Failure		404	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/calendar/events/{id} [get]
func (h *CalendarHandler) Get(w http.ResponseWriter, r *http.Request) {
	wsID := middleware.GetWorkspaceID(r)
	eventID := mux.Vars(r)["id"]

	event, err := h.getEvent.Execute(eventID, wsID)
	if err != nil {
		if errors.Is(err, calendardomain.ErrEventNotFound) {
			response.WriteError(w, http.StatusNotFound, "Event not found", nil)
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "Failed to get event", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, EventEnvelope{Event: toEventResponse(event)})
}

// @Summary		Listar eventos da agenda
// @Description	Retorna a lista paginada de eventos da agenda do workspace, com filtros opcionais por período e busca textual.
// @Tags			Agenda
// @Produce		json
// @Param			page		query		int		false	"Número da página (inicia em 1)"
// @Param			pageSize	query		int		false	"Quantidade de itens por página"
// @Param			search		query		string	false	"Busca textual por título ou descrição"
// @Param			from		query		string	false	"Data inicial no formato RFC3339"
// @Param			to			query		string	false	"Data final no formato RFC3339"
// @Success		200	{object}	EventListResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/calendar/events [get]
func (h *CalendarHandler) List(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	wsID := middleware.GetWorkspaceID(r)

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("pageSize"))
	search := r.URL.Query().Get("search")

	input := calendardomain.ListEventsInput{
		WorkspaceID: wsID,
		UserID:      claims.UserID,
		Search:      search,
		Pagination:  shared.Pagination{Page: page, PageSize: pageSize},
	}

	if from := r.URL.Query().Get("from"); from != "" {
		t, err := time.Parse(time.RFC3339, from)
		if err == nil {
			input.From = &t
		}
	}
	if to := r.URL.Query().Get("to"); to != "" {
		t, err := time.Parse(time.RFC3339, to)
		if err == nil {
			input.To = &t
		}
	}

	result, err := h.listEvents.Execute(input)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to list events", nil)
		return
	}

	response.WritePaginated(w, http.StatusOK, toEventResponses(result.Items), response.PaginationMeta{
		Page:       result.Page,
		PageSize:   result.PageSize,
		TotalPages: result.TotalPages,
		TotalItems: result.TotalItems,
	})
}

// @Summary		Iniciar o monitoramento do Google Agenda
// @Description	Registra um canal de notificações para sincronizar automaticamente as alterações do Google Agenda.
// @Tags			Agenda
// @Produce		json
// @Success		200	{object}	WatchEnvelope
// @Failure		412	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/calendar/google/watch [post]
func (h *CalendarHandler) StartWatch(w http.ResponseWriter, r *http.Request) {
	wsID := middleware.GetWorkspaceID(r)

	ch, err := h.startWatch.Execute(wsID)
	if err != nil {
		if errors.Is(err, calendardomain.ErrGoogleNotConnected) {
			response.WriteError(w, http.StatusPreconditionFailed, "Google Calendar not connected", nil)
			return
		}
		log.Printf("[calendar] start watch error: %v", err)
		response.WriteError(w, http.StatusInternalServerError, "Failed to start calendar watch", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, WatchEnvelope{
		Channel: WatchChannelResponse{
			ChannelID:  ch.ChannelID,
			Expiration: ch.Expiration,
		},
	})
}

// @Summary		Encerrar o monitoramento do Google Agenda
// @Description	Cancela o canal de notificações de sincronização do Google Agenda do workspace.
// @Tags			Agenda
// @Produce		json
// @Success		200	{object}	StoppedResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/calendar/google/watch [delete]
func (h *CalendarHandler) StopWatch(w http.ResponseWriter, r *http.Request) {
	wsID := middleware.GetWorkspaceID(r)

	if err := h.stopWatch.Execute(wsID); err != nil {
		log.Printf("[calendar] stop watch error: %v", err)
		response.WriteError(w, http.StatusInternalServerError, "Failed to stop calendar watch", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, StoppedResponse{Stopped: true})
}

// @Summary		Webhook de notificações do Google Agenda
// @Description	Endpoint público que recebe as notificações push do Google Agenda para sincronizar os eventos alterados.
// @Tags			Agenda
// @Success		200	{string}	string	"Notificação recebida"
// @Failure		400	{string}	string	"Cabeçalho de canal ausente"
// @Router			/webhooks/google-calendar [post]
func (h *CalendarHandler) HandleGoogleCalendarWebhook(w http.ResponseWriter, r *http.Request) {
	channelID := r.Header.Get("X-Goog-Channel-Id")
	resourceID := r.Header.Get("X-Goog-Resource-Id")
	token := r.Header.Get("X-Goog-Channel-Token")
	resourceState := r.Header.Get("X-Goog-Resource-State")

	if channelID == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if err := h.handleNotification.Execute(channelID, resourceID, token, resourceState); err != nil {
		log.Printf("[calendar-webhook] error handling notification for channel %s: %v", channelID, err)

	}

	w.WriteHeader(http.StatusOK)
}
