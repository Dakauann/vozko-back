package callrouting

import (
	"errors"
	"net/http"
	"time"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/domain/callrouting"
	"vozko/domain/voip"
	callrouting_usecase "vozko/usecases/callrouting"
)

const presetPreviewMaxAge = 24 * time.Hour

type HandlerDeps struct {
	Queues   *callrouting_usecase.QueueCatalog
	Settings *callrouting_usecase.RoutingSettings
	Targets  *callrouting_usecase.TransferTargets
	Monitor  *callrouting_usecase.QueueMonitor
	Music    callrouting.HoldMusicLibrary
}

type Handler struct {
	deps HandlerDeps
}

func NewHandler(deps HandlerDeps) *Handler {
	return &Handler{deps: deps}
}

// @Summary		Criar fila de atendimento
// @Description	Cria uma fila telefônica no workspace. Quem atende vem de um departamento ou de uma lista de pessoas do workspace, nunca dos dois. A estratégia padrão entrega a ligação a quem está livre há mais tempo (longest_idle); round_robin reveza a partir do último chamado, fewest_calls prioriza quem atendeu menos e random sorteia. Sem música informada, a fila usa o preset padrão.
// @Tags			Filas de atendimento
// @Accept			json
// @Produce		json
// @Param			request	body		QueueRequest	true	"Dados da fila"
// @Success		201		{object}	QueueResponse
// @Failure		400		{object}	response.ErrorResponse
// @Failure		401		{object}	response.ErrorResponse
// @Failure		403		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/call-queues [post]
func (h *Handler) CreateQueue(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	var req QueueRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	queue, err := h.deps.Queues.Create(r.Context(), req.toDomain(workspaceID, ""))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusCreated, toQueueDTO(queue))
}

// @Summary		Listar filas de atendimento
// @Description	Lista as filas telefônicas do workspace com quem atende cada uma e a música de espera.
// @Tags			Filas de atendimento
// @Produce		json
// @Success		200	{array}		QueueResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/call-queues [get]
func (h *Handler) ListQueues(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	queues, err := h.deps.Queues.List(r.Context(), workspaceID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	items := make([]QueueResponse, 0, len(queues))
	for _, queue := range queues {
		items = append(items, toQueueDTO(queue))
	}
	response.WriteSuccess(w, http.StatusOK, items)
}

// @Summary		Obter fila de atendimento
// @Description	Retorna uma fila telefônica do workspace.
// @Tags			Filas de atendimento
// @Produce		json
// @Param			id	path		string	true	"ID da fila"
// @Success		200	{object}	QueueResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/call-queues/{id} [get]
func (h *Handler) GetQueue(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	queue, err := h.deps.Queues.Get(r.Context(), workspaceID, mux.Vars(r)["id"])
	if err != nil {
		writeDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toQueueDTO(queue))
}

// @Summary		Atualizar fila de atendimento
// @Description	Substitui a configuração de uma fila telefônica do workspace. Quem já está esperando continua na fila e passa a seguir a configuração nova na próxima tentativa.
// @Tags			Filas de atendimento
// @Accept			json
// @Produce		json
// @Param			id		path		string			true	"ID da fila"
// @Param			request	body		QueueRequest	true	"Configuração completa da fila"
// @Success		200		{object}	QueueResponse
// @Failure		400		{object}	response.ErrorResponse
// @Failure		401		{object}	response.ErrorResponse
// @Failure		403		{object}	response.ErrorResponse
// @Failure		404		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/call-queues/{id} [put]
func (h *Handler) UpdateQueue(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	var req QueueRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	queue, err := h.deps.Queues.Update(r.Context(), req.toDomain(workspaceID, mux.Vars(r)["id"]))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toQueueDTO(queue))
}

// @Summary		Remover fila de atendimento
// @Description	Remove uma fila telefônica do workspace. Fluxos de voz que enviam ligações a ela passam a seguir a saída de tempo esgotado.
// @Tags			Filas de atendimento
// @Produce		json
// @Param			id	path		string	true	"ID da fila"
// @Success		200	{object}	StatusResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/call-queues/{id} [delete]
func (h *Handler) DeleteQueue(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	if err := h.deps.Queues.Delete(r.Context(), workspaceID, mux.Vars(r)["id"]); err != nil {
		writeDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, StatusResponse{Status: "deleted"})
}

// @Summary		Filas para transferência
// @Description	Lista as filas para onde quem está em uma ligação pode transferi-la, com quantas pessoas esperam e quantos atendentes estão livres agora. Os colegas disponíveis chegam pela presença do WebSocket de sessão de chamada; a transferência é feita pelo evento call:transfer.
// @Tags			Filas de atendimento
// @Produce		json
// @Success		200	{array}		QueueTargetResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/call-queues/transfer-targets [get]
func (h *Handler) ListTransferTargets(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	targets, err := h.deps.Targets.Queues(r.Context(), workspaceID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	items := make([]QueueTargetResponse, 0, len(targets))
	for _, target := range targets {
		items = append(items, toQueueTargetDTO(target))
	}
	response.WriteSuccess(w, http.StatusOK, items)
}

// @Summary		Filas ao vivo
// @Description	Mostra, em cada fila, quem está esperando e há quanto tempo, e o estado de cada atendente: livre, tocando, em ligação, em pausa após a ligação ou fora (discador fechado ou sem permissão para atender). Os números vêm da memória do servidor e mudam a cada ligação; consulte de novo para acompanhar.
// @Tags			Filas de atendimento
// @Produce		json
// @Success		200	{array}		QueueLiveResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/call-queues/live [get]
func (h *Handler) LiveQueues(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	live, err := h.deps.Monitor.Live(r.Context(), workspaceID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	now := time.Now()
	items := make([]QueueLiveResponse, 0, len(live))
	for _, queue := range live {
		items = append(items, toQueueLiveDTO(queue, now))
	}
	response.WriteSuccess(w, http.StatusOK, items)
}

// @Summary		Números das filas no período
// @Description	Para cada fila, no período informado (até 31 dias): ligações recebidas, atendidas, abandonadas por quem ligou e que esgotaram o tempo, espera média até o atendimento e nível de serviço (fração das ligações atendidas em até 20 segundos).
// @Tags			Filas de atendimento
// @Produce		json
// @Param			from	query		string	true	"Início do período (RFC 3339 ou AAAA-MM-DD)"
// @Param			to		query		string	true	"Fim do período (RFC 3339 ou AAAA-MM-DD, inclusivo)"
// @Success		200		{array}		QueueStatsResponse
// @Failure		400		{object}	response.ErrorResponse
// @Failure		401		{object}	response.ErrorResponse
// @Failure		403		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/call-queues/stats [get]
func (h *Handler) QueueStats(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	from := httpx.ParseDateBound(r.URL.Query().Get("from"), false)
	to := httpx.ParseDateBound(r.URL.Query().Get("to"), true)
	if from == nil || to == nil {
		writeDomainError(w, callrouting.ErrInvalidStatsWindow)
		return
	}
	tallies, err := h.deps.Monitor.Stats(r.Context(), workspaceID, *from, *to)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	items := make([]QueueStatsResponse, 0, len(tallies))
	for _, tally := range tallies {
		items = append(items, toQueueStatsDTO(tally))
	}
	response.WriteSuccess(w, http.StatusOK, items)
}

// @Summary		Música de espera do workspace
// @Description	Retorna a música tocada para quem espera uma transferência direta para um colega. Cada fila tem a sua própria música.
// @Tags			Filas de atendimento
// @Produce		json
// @Success		200	{object}	SettingsResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/call-routing/settings [get]
func (h *Handler) GetSettings(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	settings, err := h.deps.Settings.Get(r.Context(), workspaceID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, SettingsResponse{HoldMusic: toHoldMusicDTO(settings.HoldMusic)})
}

// @Summary		Definir música de espera do workspace
// @Description	Define a música de espera das transferências diretas: um preset ou um áudio enviado à biblioteca de mídia que possa tocar em ligações. Sem música informada, volta ao preset padrão.
// @Tags			Filas de atendimento
// @Accept			json
// @Produce		json
// @Param			request	body		SettingsRequest	true	"Música de espera"
// @Success		200		{object}	SettingsResponse
// @Failure		400		{object}	response.ErrorResponse
// @Failure		401		{object}	response.ErrorResponse
// @Failure		403		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/call-routing/settings [put]
func (h *Handler) SaveSettings(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	var req SettingsRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	settings, err := h.deps.Settings.Save(r.Context(), callrouting.Settings{WorkspaceID: workspaceID, HoldMusic: req.HoldMusic.toDomain()})
	if err != nil {
		writeDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, SettingsResponse{HoldMusic: toHoldMusicDTO(settings.HoldMusic)})
}

// @Summary		Listar músicas de espera prontas
// @Description	Lista os presets de música de espera que acompanham o produto, cada um com o seu estilo.
// @Tags			Filas de atendimento
// @Produce		json
// @Success		200	{array}		HoldPresetResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/hold-music/presets [get]
func (h *Handler) ListPresets(w http.ResponseWriter, r *http.Request) {
	presets := h.deps.Music.Presets()
	items := make([]HoldPresetResponse, 0, len(presets))
	for _, preset := range presets {
		items = append(items, HoldPresetResponse{ID: preset.ID, Name: preset.Name, Mood: preset.Mood})
	}
	response.WriteSuccess(w, http.StatusOK, items)
}

// @Summary		Ouvir música de espera pronta
// @Description	Devolve o preset em WAV (PCM 16 bits, 8 kHz, mono), exatamente como quem espera na linha vai ouvir.
// @Tags			Filas de atendimento
// @Produce		audio/wav
// @Param			id	path		string	true	"ID do preset"
// @Success		200	{file}		binary
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/hold-music/presets/{id}/audio [get]
func (h *Handler) PresetAudio(w http.ResponseWriter, r *http.Request) {
	pcm, ok := h.deps.Music.PresetAudio(mux.Vars(r)["id"])
	if !ok {
		response.WriteError(w, http.StatusNotFound, callrouting.ErrHoldMusicNotFound.Error(), nil)
		return
	}
	httpx.WriteBinary(w, voip.WAV(pcm, voip.PCMSampleRate, 1, 16), "audio/wav", "audio/wav", presetPreviewMaxAge)
}

func writeDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, callrouting.ErrQueueNotFound):
		response.WriteError(w, http.StatusNotFound, err.Error(), nil)
	case callrouting.IsInvalidInput(err):
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
	default:
		response.WriteError(w, http.StatusInternalServerError, "internal server error", nil)
	}
}
