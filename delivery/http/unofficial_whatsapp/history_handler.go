package unofficial_whatsapp

import (
	"errors"
	"net/http"
	"time"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	uw "vozko/domain/unofficial_whatsapp"
	uwuc "vozko/usecases/unofficial_whatsapp"
)

type historySyncDTO struct {
	ID                string     `json:"id"`
	Status            string     `json:"status"`
	Trigger           string     `json:"trigger"`
	WindowFrom        time.Time  `json:"windowFrom"`
	OldestMessageAt   *time.Time `json:"oldestMessageAt,omitempty"`
	NewestMessageAt   *time.Time `json:"newestMessageAt,omitempty"`
	Passes            int        `json:"passes"`
	MessagesImported  int        `json:"messagesImported"`
	MessagesDuplicate int        `json:"messagesDuplicate"`
	MessagesSkipped   int        `json:"messagesSkipped"`
	MessagesFailed    int        `json:"messagesFailed"`
	NextPollAt        *time.Time `json:"nextPollAt,omitempty"`
	PollUntil         time.Time  `json:"pollUntil"`
	Paused            bool       `json:"paused"`
	Reason            string     `json:"reason,omitempty"`
	LastError         string     `json:"lastError,omitempty"`
	StartedAt         *time.Time `json:"startedAt,omitempty"`
	FinishedAt        *time.Time `json:"finishedAt,omitempty"`
	CreatedAt         time.Time  `json:"createdAt"`
}

func toHistorySyncDTO(s *uw.HistorySync) historySyncDTO {
	dto := historySyncDTO{
		ID:                s.ID,
		Status:            string(s.Status),
		Trigger:           string(s.Trigger),
		WindowFrom:        s.WindowFrom,
		OldestMessageAt:   s.OldestMessageAt,
		NewestMessageAt:   s.NewestMessageAt,
		Passes:            s.Passes,
		MessagesImported:  s.MessagesImported,
		MessagesDuplicate: s.MessagesDuplicate,
		MessagesSkipped:   s.MessagesSkipped,
		MessagesFailed:    s.MessagesFailed,
		PollUntil:         s.PollUntil,
		Paused:            s.PausedSince != nil,
		Reason:            s.Reason,
		LastError:         s.LastError,
		StartedAt:         s.StartedAt,
		FinishedAt:        s.FinishedAt,
		CreatedAt:         s.CreatedAt,
	}
	if s.Status.Active() {
		next := s.NextPollAt
		dto.NextPollAt = &next
	}
	return dto
}

func (h *Handler) SetHistorySync(history *uwuc.HistorySyncUseCase) {
	h.history = history
}

func (h *Handler) GetHistorySync(w http.ResponseWriter, r *http.Request) {
	if !h.historyAvailable(w) {
		return
	}
	workspaceID, scope, ok := h.requireScope(w, r)
	if !ok {
		return
	}
	run, err := h.history.Latest(r.Context(), mux.Vars(r)["id"], workspaceID, scope)
	if errors.Is(err, uw.ErrHistorySyncNotFound) {
		response.WriteSuccess(w, http.StatusOK, nil)
		return
	}
	if err != nil {
		writeHistoryError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toHistorySyncDTO(run))
}

func (h *Handler) RequestHistorySync(w http.ResponseWriter, r *http.Request) {
	if !h.historyAvailable(w) {
		return
	}
	workspaceID, scope, ok := h.requireScope(w, r)
	if !ok {
		return
	}
	run, err := h.history.Request(r.Context(), mux.Vars(r)["id"], workspaceID, scope)
	if err != nil {
		writeHistoryError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusAccepted, toHistorySyncDTO(run))
}

func (h *Handler) historyAvailable(w http.ResponseWriter) bool {
	if h.history == nil {
		response.WriteError(w, http.StatusServiceUnavailable, "history import is disabled", nil)
		return false
	}
	return true
}

func writeHistoryError(w http.ResponseWriter, err error) {
	if errors.Is(err, uw.ErrHistoryImportOff) {
		response.WriteError(w, http.StatusConflict, "history import is switched off for this number", nil)
		return
	}
	if errors.Is(err, uw.ErrHistorySyncCooldown) {
		response.WriteError(w, http.StatusTooManyRequests, err.Error(), nil)
		return
	}
	writeDomainError(w, err)
}
