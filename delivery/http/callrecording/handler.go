package callrecording

import (
	"net/http"
	"strconv"
	"strings"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	recordingsdomain "vozko/domain/calls/recordings"
)

type CallRecordingHandler struct {
	recordingQuery recordingsdomain.QueryUseCase
}

func NewCallRecordingHandler(recordingQuery recordingsdomain.QueryUseCase) *CallRecordingHandler {
	return &CallRecordingHandler{
		recordingQuery: recordingQuery,
	}
}

func (h *CallRecordingHandler) ListRecordings(w http.ResponseWriter, r *http.Request) {
	values := r.URL.Query()

	filters := recordingsdomain.ListFilters{
		Pagination: httpx.ParsePagination(values),
		LeadID:     strings.TrimSpace(values.Get("leadId")),
		EntryID:    strings.TrimSpace(values.Get("entryId")),
		SortBy:     strings.TrimSpace(values.Get("sortBy")),
		SortDesc:   strings.ToLower(strings.TrimSpace(values.Get("sortOrder"))) == "desc",
	}

	if minDur := strings.TrimSpace(values.Get("minDuration")); minDur != "" {
		if val, err := strconv.Atoi(minDur); err == nil && val >= 0 {
			filters.MinDuration = &val
		}
	}
	if maxDur := strings.TrimSpace(values.Get("maxDuration")); maxDur != "" {
		if val, err := strconv.Atoi(maxDur); err == nil && val > 0 {
			filters.MaxDuration = &val
		}
	}

	if filters.SortBy == "" {
		filters.SortBy = "created_at"
		filters.SortDesc = true
	}

	result, err := h.recordingQuery.List(filters)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to fetch call recordings", nil)
		return
	}

	items := make([]CallRecordingResponse, len(result.Items))
	for i, rec := range result.Items {
		items[i] = toCallRecordingResponse(rec)
	}

	response.WriteSuccess(w, http.StatusOK, CallRecordingListResponse{
		Items:      items,
		Page:       result.Page,
		PageSize:   result.PageSize,
		TotalItems: result.TotalItems,
		TotalPages: result.TotalPages,
	})
}

func (h *CallRecordingHandler) GetByCallID(w http.ResponseWriter, r *http.Request) {
	callID := strings.TrimSpace(r.URL.Query().Get("callId"))
	if callID == "" {
		response.WriteError(w, http.StatusBadRequest, "Call ID is required", nil)
		return
	}

	record, err := h.recordingQuery.GetByCallID(callID)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to fetch call recording", nil)
		return
	}
	if record == nil {
		response.WriteError(w, http.StatusNotFound, "Call recording not found", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, toCallRecordingResponse(record))
}

func (h *CallRecordingHandler) GetByLeadID(w http.ResponseWriter, r *http.Request) {
	leadID := strings.TrimSpace(r.URL.Query().Get("leadId"))
	if leadID == "" {
		response.WriteError(w, http.StatusBadRequest, "Lead ID is required", nil)
		return
	}

	records, err := h.recordingQuery.GetByLeadID(leadID)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to fetch call recordings", nil)
		return
	}

	items := make([]CallRecordingResponse, len(records))
	for i, rec := range records {
		items[i] = toCallRecordingResponse(rec)
	}

	response.WriteSuccess(w, http.StatusOK, CallRecordingCollectionResponse{
		Items: items,
		Total: len(items),
	})
}

// @Summary		Listar gravações de chamadas por entrada de campanha
// @Description	Retorna as gravações de chamadas associadas a uma entrada de campanha do workspace autenticado.
// @Tags			Gravações de chamadas
// @Produce		json
// @Param			entryId	query		string	true	"ID da entrada de campanha"
// @Success		200	{object}	CallRecordingCollectionResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/call-recordings/by-entry [get]
func (h *CallRecordingHandler) GetByEntryID(w http.ResponseWriter, r *http.Request) {
	entryID := strings.TrimSpace(r.URL.Query().Get("entryId"))
	if entryID == "" {
		response.WriteError(w, http.StatusBadRequest, "Entry ID is required", nil)
		return
	}

	records, err := h.recordingQuery.GetByEntryID(entryID)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to fetch call recordings", nil)
		return
	}

	items := make([]CallRecordingResponse, len(records))
	for i, rec := range records {
		items[i] = toCallRecordingResponse(rec)
	}

	response.WriteSuccess(w, http.StatusOK, CallRecordingCollectionResponse{
		Items: items,
		Total: len(items),
	})
}
