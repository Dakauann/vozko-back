package analysis

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	analysisdomain "vozko/domain/analysis"
	"vozko/domain/shared"
	"vozko/infra/http/middleware"
)

type AnalysisHandler struct {
	listUseCase     analysisdomain.ListAnalysisUseCase
	statsUseCase    analysisdomain.GetAnalysisStatsUseCase
	entryAnalysisUC analysisdomain.GetEntryAnalysisUseCase
}

func NewAnalysisHandler(listUC analysisdomain.ListAnalysisUseCase, statsUC analysisdomain.GetAnalysisStatsUseCase, entryUC analysisdomain.GetEntryAnalysisUseCase) *AnalysisHandler {
	return &AnalysisHandler{
		listUseCase:     listUC,
		statsUseCase:    statsUC,
		entryAnalysisUC: entryUC,
	}
}

func (h *AnalysisHandler) List(w http.ResponseWriter, r *http.Request) {
	values := r.URL.Query()

	opts := shared.QueryOptions{
		Pagination: parsePagination(values),
		Sorts: parseSort(values, map[string]string{
			"createdat":         "created_at",
			"attendancequality": "attendance_quality",
			"interest":          "interest",
			"disposition":       "disposition",
			"sentiment":         "sentiment",
			"qualification":     "qualification",
		}),
	}

	input := analysisdomain.ListAnalysisInput{
		CampaignID:         strings.TrimSpace(values.Get("campaignId")),
		WhatsAppCampaignID: strings.TrimSpace(values.Get("whatsappCampaignId")),
		LeadID:             strings.TrimSpace(values.Get("leadId")),
		Options:            opts,
	}

	h.applyFilters(&input, values)

	result, err := h.listUseCase.Execute(input)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to fetch analysis records", nil)
		return
	}

	response.WritePaginated(w, http.StatusOK, result.Items, response.PaginationMeta{
		Page:       result.Page,
		PageSize:   result.PageSize,
		TotalPages: result.TotalPages,
		TotalItems: result.TotalItems,
	})
}

func (h *AnalysisHandler) ListByWhatsAppCampaign(w http.ResponseWriter, r *http.Request) {
	campaignID := mux.Vars(r)["id"]
	if campaignID == "" {
		response.WriteError(w, http.StatusBadRequest, "WhatsApp campaign ID is required", nil)
		return
	}

	values := r.URL.Query()

	opts := shared.QueryOptions{
		Pagination: parsePagination(values),
		Sorts: parseSort(values, map[string]string{
			"createdat":         "created_at",
			"attendancequality": "attendance_quality",
			"interest":          "interest",
			"disposition":       "disposition",
			"sentiment":         "sentiment",
			"qualification":     "qualification",
		}),
	}

	input := analysisdomain.ListAnalysisInput{
		WhatsAppCampaignID: campaignID,
		LeadID:             strings.TrimSpace(values.Get("leadId")),
		Options:            opts,
	}

	h.applyFilters(&input, values)

	result, err := h.listUseCase.Execute(input)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to fetch analysis records", nil)
		return
	}

	response.WritePaginated(w, http.StatusOK, result.Items, response.PaginationMeta{
		Page:       result.Page,
		PageSize:   result.PageSize,
		TotalPages: result.TotalPages,
		TotalItems: result.TotalItems,
	})
}

func (h *AnalysisHandler) ListWhatsAppAnalysis(w http.ResponseWriter, r *http.Request) {
	values := r.URL.Query()

	opts := shared.QueryOptions{
		Pagination: parsePagination(values),
		Sorts: parseSort(values, map[string]string{
			"createdat":         "created_at",
			"attendancequality": "attendance_quality",
			"interest":          "interest",
			"disposition":       "disposition",
			"sentiment":         "sentiment",
			"qualification":     "qualification",
		}),
	}

	input := analysisdomain.ListAnalysisInput{
		WhatsAppCampaignID: strings.TrimSpace(values.Get("whatsappCampaignId")),
		LeadID:             strings.TrimSpace(values.Get("leadId")),
		EntryType:          shared.EntryTypeWhatsApp,
		Options:            opts,
	}

	h.applyFilters(&input, values)

	result, err := h.listUseCase.Execute(input)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to fetch analysis records", nil)
		return
	}

	response.WritePaginated(w, http.StatusOK, result.Items, response.PaginationMeta{
		Page:       result.Page,
		PageSize:   result.PageSize,
		TotalPages: result.TotalPages,
		TotalItems: result.TotalItems,
	})
}

func (h *AnalysisHandler) GetStats(w http.ResponseWriter, r *http.Request) {
	values := r.URL.Query()

	input := analysisdomain.ListAnalysisInput{
		CampaignID:         strings.TrimSpace(values.Get("campaignId")),
		WhatsAppCampaignID: strings.TrimSpace(values.Get("whatsappCampaignId")),
		LeadID:             strings.TrimSpace(values.Get("leadId")),
	}

	h.applyFilters(&input, values)

	if h.statsUseCase == nil {
		response.WriteError(w, http.StatusInternalServerError, "Stats functionality not configured", nil)
		return
	}

	stats, err := h.statsUseCase.Execute(input)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to fetch analysis stats", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, stats)
}

// @Summary		Listar análises
// @Description	Retorna as análises de conversas e chamadas do workspace do usuário. Aceita filtros por campanha, lead, tipo de entrada, interesse, disposição, sentimento, qualificação, próxima ação e faixas de qualidade de atendimento e de número de mensagens, além de paginação e ordenação.
// @Tags			Análises
// @Produce		json
// @Param			campaignId				query	string	false	"ID da campanha de voz"
// @Param			whatsappCampaignId		query	string	false	"ID da campanha de WhatsApp"
// @Param			leadId					query	string	false	"ID do lead"
// @Param			entryType				query	string	false	"Tipo de entrada ('voice' ou 'whatsapp')"
// @Param			interest				query	string	false	"Filtro por interesse"
// @Param			disposition				query	string	false	"Filtro por disposição"
// @Param			sentiment				query	string	false	"Filtro por sentimento"
// @Param			qualification			query	string	false	"Filtro por qualificação"
// @Param			nextAction				query	string	false	"Filtro por próxima ação"
// @Param			attendanceQualityMin	query	int		false	"Qualidade mínima de atendimento"
// @Param			attendanceQualityMax	query	int		false	"Qualidade máxima de atendimento"
// @Param			messageCountMin			query	int		false	"Número mínimo de mensagens"
// @Param			messageCountMax			query	int		false	"Número máximo de mensagens"
// @Param			page					query	int		false	"Número da página"
// @Param			pageSize				query	int		false	"Tamanho da página"
// @Param			sort					query	string	false	"Ordenação (ex.: createdAt:desc)"
// @Success		200	{array}		analysis.Analysis
// @Failure		401	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/analysis [get]
func (h *AnalysisHandler) ListForUser(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	values := r.URL.Query()

	opts := shared.QueryOptions{
		Pagination: parsePagination(values),
		Sorts: parseSort(values, map[string]string{
			"createdat":         "created_at",
			"attendancequality": "attendance_quality",
			"interest":          "interest",
			"disposition":       "disposition",
			"sentiment":         "sentiment",
			"qualification":     "qualification",
		}),
	}

	wsID := middleware.GetWorkspaceID(r)

	input := analysisdomain.ListAnalysisInput{
		CampaignID:         strings.TrimSpace(values.Get("campaignId")),
		WhatsAppCampaignID: strings.TrimSpace(values.Get("whatsappCampaignId")),
		LeadID:             strings.TrimSpace(values.Get("leadId")),
		WorkspaceID:        wsID,
		Options:            opts,
	}

	h.applyFilters(&input, values)

	result, err := h.listUseCase.Execute(input)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to fetch analysis records", nil)
		return
	}

	response.WritePaginated(w, http.StatusOK, result.Items, response.PaginationMeta{
		Page:       result.Page,
		PageSize:   result.PageSize,
		TotalPages: result.TotalPages,
		TotalItems: result.TotalItems,
	})
}

// @Summary		Estatísticas de análises
// @Description	Retorna estatísticas agregadas das análises do workspace do usuário, como médias de qualidade de atendimento e contagens por interesse, disposição, sentimento e qualificação, aplicando os mesmos filtros da listagem.
// @Tags			Análises
// @Produce		json
// @Param			campaignId			query	string	false	"ID da campanha de voz"
// @Param			whatsappCampaignId	query	string	false	"ID da campanha de WhatsApp"
// @Param			leadId				query	string	false	"ID do lead"
// @Success		200	{object}	analysis.AnalysisStats
// @Failure		401	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/analysis/stats [get]
func (h *AnalysisHandler) GetStatsForUser(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	values := r.URL.Query()

	wsID := middleware.GetWorkspaceID(r)

	input := analysisdomain.ListAnalysisInput{
		CampaignID:         strings.TrimSpace(values.Get("campaignId")),
		WhatsAppCampaignID: strings.TrimSpace(values.Get("whatsappCampaignId")),
		LeadID:             strings.TrimSpace(values.Get("leadId")),
		WorkspaceID:        wsID,
	}

	h.applyFilters(&input, values)

	if h.statsUseCase == nil {
		response.WriteError(w, http.StatusInternalServerError, "Stats functionality not configured", nil)
		return
	}

	stats, err := h.statsUseCase.Execute(input)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to fetch analysis stats", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, stats)
}

// @Summary		Obter análise de uma entrada
// @Description	Retorna a análise mais recente de uma conversa ou chamada específica, identificada pelo ID da entrada e pelo tipo ('voice' ou 'whatsapp').
// @Tags			Análises
// @Produce		json
// @Param			entryId		path	string	true	"ID da entrada"
// @Param			entryType	query	string	true	"Tipo da entrada ('voice' ou 'whatsapp')"
// @Success		200	{object}	analysis.Analysis
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/analysis/entry/{entryId} [get]
func (h *AnalysisHandler) GetByEntry(w http.ResponseWriter, r *http.Request) {
	entryID := mux.Vars(r)["entryId"]
	if entryID == "" {
		response.WriteError(w, http.StatusBadRequest, "entryId is required", nil)
		return
	}

	entryType := shared.EntryType(strings.TrimSpace(r.URL.Query().Get("entryType")))
	if !entryType.Valid() {
		response.WriteError(w, http.StatusBadRequest, "valid entryType query param is required (voice or whatsapp)", nil)
		return
	}

	if h.entryAnalysisUC == nil {
		response.WriteError(w, http.StatusInternalServerError, "Entry analysis not configured", nil)
		return
	}

	result, err := h.entryAnalysisUC.Execute(entryID, entryType)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to fetch entry analysis", nil)
		return
	}

	if result == nil {
		response.WriteSuccess(w, http.StatusOK, nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, result)
}

func (h *AnalysisHandler) applyFilters(input *analysisdomain.ListAnalysisInput, values map[string][]string) {
	if entryType := strings.TrimSpace(getValue(values, "entryType")); entryType != "" && !input.EntryType.Valid() {
		input.EntryType = shared.EntryType(strings.ToLower(entryType))
	}

	if interest := strings.TrimSpace(getValue(values, "interest")); interest != "" {
		input.Interest = analysisdomain.Interest(strings.ToLower(interest))
	}

	if disposition := strings.TrimSpace(getValue(values, "disposition")); disposition != "" {
		input.Disposition = analysisdomain.Disposition(strings.ToLower(disposition))
	}

	if sentiment := strings.TrimSpace(getValue(values, "sentiment")); sentiment != "" {
		input.Sentiment = analysisdomain.Sentiment(strings.ToLower(sentiment))
	}

	if qualification := strings.TrimSpace(getValue(values, "qualification")); qualification != "" {
		input.Qualification = analysisdomain.Qualification(strings.ToLower(qualification))
	}

	if nextAction := strings.TrimSpace(getValue(values, "nextAction")); nextAction != "" {
		input.NextAction = analysisdomain.NextAction(strings.ToLower(nextAction))
	}

	if minQuality := strings.TrimSpace(getValue(values, "attendanceQualityMin")); minQuality != "" {
		if val, err := strconv.Atoi(minQuality); err == nil {
			input.AttendanceQualityMin = &val
		}
	}

	if maxQuality := strings.TrimSpace(getValue(values, "attendanceQualityMax")); maxQuality != "" {
		if val, err := strconv.Atoi(maxQuality); err == nil {
			input.AttendanceQualityMax = &val
		}
	}

	if minMessages := strings.TrimSpace(getValue(values, "messageCountMin")); minMessages != "" {
		if val, err := strconv.Atoi(minMessages); err == nil {
			input.MessageCountMin = &val
		}
	}

	if maxMessages := strings.TrimSpace(getValue(values, "messageCountMax")); maxMessages != "" {
		if val, err := strconv.Atoi(maxMessages); err == nil {
			input.MessageCountMax = &val
		}
	}
}

func getValue(values map[string][]string, key string) string {
	if v, ok := values[key]; ok && len(v) > 0 {
		return v[0]
	}
	return ""
}

func parsePagination(values url.Values) shared.Pagination {
	page := 1
	if v := strings.TrimSpace(values.Get("page")); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			page = parsed
		}
	}

	pageSize := shared.DefaultPageSize
	if v := strings.TrimSpace(values.Get("pageSize")); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			pageSize = parsed
		}
	}

	return shared.Pagination{Page: page, PageSize: pageSize}
}

func parseSort(values url.Values, allowed map[string]string) []shared.Sort {
	rawSorts := values["sort"]
	if len(rawSorts) == 0 {
		return nil
	}

	sorts := make([]shared.Sort, 0)
	for _, raw := range rawSorts {
		entries := strings.Split(raw, ",")
		for _, entry := range entries {
			entry = strings.TrimSpace(entry)
			if entry == "" {
				continue
			}
			parts := strings.Split(entry, ":")
			fieldKey := strings.ToLower(strings.TrimSpace(parts[0]))
			field, ok := allowed[fieldKey]
			if !ok {
				continue
			}

			direction := shared.SortAsc
			if len(parts) > 1 {
				if dir := strings.ToLower(strings.TrimSpace(parts[1])); dir == string(shared.SortDesc) {
					direction = shared.SortDesc
				}
			}

			sorts = append(sorts, shared.Sort{Field: field, Direction: direction})
		}
	}

	return sorts
}
