package calendar

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
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
	connectGoogle      calendardomain.ConnectGoogleUseCase
	disconnectGoogle   calendardomain.DisconnectGoogleUseCase
	getConnection      calendardomain.GetConnectionUseCase
	getAuthURL         calendardomain.GetAuthURLUseCase
	startWatch         calendardomain.StartWatchUseCase
	stopWatch          calendardomain.StopWatchUseCase
	handleNotification calendardomain.HandleNotificationUseCase
	googleOAuthEnabled bool
	stateSignerKey     string
}

func NewCalendarHandler(
	createEvent calendardomain.CreateEventUseCase,
	updateEvent calendardomain.UpdateEventUseCase,
	deleteEvent calendardomain.DeleteEventUseCase,
	getEvent calendardomain.GetEventUseCase,
	listEvents calendardomain.ListEventsUseCase,
	connectGoogle calendardomain.ConnectGoogleUseCase,
	disconnectGoogle calendardomain.DisconnectGoogleUseCase,
	getConnection calendardomain.GetConnectionUseCase,
	getAuthURL calendardomain.GetAuthURLUseCase,
	startWatch calendardomain.StartWatchUseCase,
	stopWatch calendardomain.StopWatchUseCase,
	handleNotification calendardomain.HandleNotificationUseCase,
	googleOAuthEnabled bool,
	stateSignerKey string,
) *CalendarHandler {
	return &CalendarHandler{
		createEvent:        createEvent,
		updateEvent:        updateEvent,
		deleteEvent:        deleteEvent,
		getEvent:           getEvent,
		listEvents:         listEvents,
		connectGoogle:      connectGoogle,
		disconnectGoogle:   disconnectGoogle,
		getConnection:      getConnection,
		getAuthURL:         getAuthURL,
		startWatch:         startWatch,
		stopWatch:          stopWatch,
		handleNotification: handleNotification,
		googleOAuthEnabled: googleOAuthEnabled,
		stateSignerKey:     stateSignerKey,
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

// @Summary		Conectar o Google Agenda
// @Description	Conclui a conexão com o Google Agenda trocando o código de autorização OAuth pelas credenciais de acesso do workspace.
// @Tags			Agenda
// @Accept			json
// @Produce		json
// @Param			request	body		ConnectGoogleRequest	true	"Código de autorização OAuth"
// @Success		200	{object}	ConnectionEnvelope
// @Failure		400	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/calendar/google/connect [post]
func (h *CalendarHandler) ConnectGoogle(w http.ResponseWriter, r *http.Request) {
	wsID := middleware.GetWorkspaceID(r)

	var req ConnectGoogleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{
			"code":        "string (required)",
			"redirectUri": "string (required)",
		})
		return
	}

	conn, err := h.connectGoogle.Execute(calendardomain.ConnectGoogleInput{
		WorkspaceID: wsID,
		Code:        req.Code,
		RedirectURI: req.RedirectURI,
	})
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to connect Google Calendar", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, ConnectionEnvelope{Connection: toConnectionResponse(conn)})
}

// @Summary		Desconectar o Google Agenda
// @Description	Remove a conexão com o Google Agenda do workspace.
// @Tags			Agenda
// @Produce		json
// @Success		200	{object}	DisconnectedResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/calendar/google/disconnect [delete]
func (h *CalendarHandler) DisconnectGoogle(w http.ResponseWriter, r *http.Request) {
	wsID := middleware.GetWorkspaceID(r)

	if err := h.disconnectGoogle.Execute(wsID); err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to disconnect Google Calendar", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, DisconnectedResponse{Disconnected: true})
}

// @Summary		Consultar a conexão com o Google Agenda
// @Description	Retorna o status da conexão do workspace com o Google Agenda.
// @Tags			Agenda
// @Produce		json
// @Success		200	{object}	ConnectionStatusResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/calendar/google/status [get]
func (h *CalendarHandler) GetConnection(w http.ResponseWriter, r *http.Request) {
	wsID := middleware.GetWorkspaceID(r)

	conn, err := h.getConnection.Execute(wsID)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to get connection status", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, ConnectionStatusResponse{
		Connected:  conn != nil,
		Connection: toConnectionResponse(conn),
	})
}

// @Summary		Obter a URL de autorização do Google Agenda
// @Description	Retorna a URL de autorização OAuth para iniciar a conexão do workspace com o Google Agenda.
// @Tags			Agenda
// @Produce		json
// @Success		200	{object}	AuthURLResponse
// @Security		BearerAuth
// @Router			/calendar/google/auth-url [get]
func (h *CalendarHandler) GetAuthURL(w http.ResponseWriter, r *http.Request) {
	if !h.googleOAuthEnabled {
		response.WriteError(w, http.StatusServiceUnavailable, "Google Calendar integration is not configured", nil)
		return
	}

	wsID := middleware.GetWorkspaceID(r)

	apiBase := os.Getenv("API_BASE_URL")
	if apiBase == "" {
		apiBase = "http://localhost:8080"
	}
	redirectURI := strings.TrimRight(apiBase, "/") + "/calendar/google/callback"

	statePayload := fmt.Sprintf("%s:%d", wsID, time.Now().Unix())
	sig := signState(statePayload, h.stateSignerKey)
	state := base64.URLEncoding.EncodeToString([]byte(statePayload + "." + sig))

	authURL := h.getAuthURL.Execute(redirectURI, state)

	response.WriteSuccess(w, http.StatusOK, AuthURLResponse{AuthURL: authURL})
}

// @Summary		Callback de autorização do Google Agenda
// @Description	Endpoint público que recebe o retorno do fluxo OAuth do Google Agenda e redireciona o usuário de volta ao painel de integrações.
// @Tags			Agenda
// @Produce		json
// @Param			code	query	string	false	"Código de autorização retornado pelo Google"
// @Param			state	query	string	false	"Estado assinado gerado ao iniciar o fluxo"
// @Param			error	query	string	false	"Erro retornado pelo Google, quando houver"
// @Success		302	{string}	string	"Redireciona para o painel de integrações"
// @Router			/calendar/google/callback [get]
func (h *CalendarHandler) GoogleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	frontendURL := strings.TrimRight(os.Getenv("FRONTEND_URL"), "/")
	if frontendURL == "" {
		frontendURL = "http://localhost:3000"
	}
	redirectError := func(reason string) {
		http.Redirect(w, r, frontendURL+"/dashboard/integrations?error="+reason, http.StatusFound)
	}
	if !h.googleOAuthEnabled {
		redirectError("not_configured")
		return
	}

	code := r.URL.Query().Get("code")
	stateB64 := r.URL.Query().Get("state")

	if errParam := r.URL.Query().Get("error"); errParam != "" {
		log.Printf("[GoogleOAuthCallback] Google returned error: %s", errParam)
		redirectError("access_denied")
		return
	}

	if code == "" || stateB64 == "" {
		redirectError("missing_params")
		return
	}

	stateBytes, err := base64.URLEncoding.DecodeString(stateB64)
	if err != nil {
		log.Printf("[GoogleOAuthCallback] Invalid state encoding: %v", err)
		redirectError("invalid_state")
		return
	}

	stateStr := string(stateBytes)
	dotIdx := strings.LastIndex(stateStr, ".")
	if dotIdx < 0 {
		redirectError("invalid_state")
		return
	}

	payload := stateStr[:dotIdx]
	sig := stateStr[dotIdx+1:]
	if !verifyState(payload, sig, h.stateSignerKey) {
		log.Printf("[GoogleOAuthCallback] State signature verification failed")
		redirectError("invalid_state")
		return
	}

	parts := strings.SplitN(payload, ":", 2)
	if len(parts) != 2 {
		redirectError("invalid_state")
		return
	}

	wsID := parts[0]
	ts, _ := strconv.ParseInt(parts[1], 10, 64)

	if time.Now().Unix()-ts > 600 {
		redirectError("expired")
		return
	}

	apiBase := os.Getenv("API_BASE_URL")
	if apiBase == "" {
		apiBase = "http://localhost:8080"
	}
	redirectURI := strings.TrimRight(apiBase, "/") + "/calendar/google/callback"

	_, err = h.connectGoogle.Execute(calendardomain.ConnectGoogleInput{
		WorkspaceID: wsID,
		Code:        code,
		RedirectURI: redirectURI,
	})
	if err != nil {
		log.Printf("[GoogleOAuthCallback] Failed to connect: %v", err)
		redirectError("connect_failed")
		return
	}

	http.Redirect(w, r, frontendURL+"/dashboard/integrations?connected=true", http.StatusFound)
}

func signState(payload, signerKey string) string {
	mac := hmac.New(sha256.New, []byte(signerKey))
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

func verifyState(payload, signature, signerKey string) bool {
	expected := signState(payload, signerKey)
	return hmac.Equal([]byte(expected), []byte(signature))
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
