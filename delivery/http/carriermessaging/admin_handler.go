package carriermessaging

import (
	"net/http"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/domain/rcs"
)

type AdminPauseRequest struct {
	Reason string `json:"reason"`
}

func (h *Handler) AdminListSenders(w http.ResponseWriter, r *http.Request) {
	var status *rcs.SenderStatus
	if raw := r.URL.Query().Get("status"); raw != "" {
		s := rcs.SenderStatus(raw)
		status = &s
	}
	senders, err := h.api.AdminListSenders(r.Context(), status)
	if err != nil {
		writeError(w, err, "admin list senders")
		return
	}
	out := SendersResponse{Senders: make([]SenderDTO, 0, len(senders))}
	for _, s := range senders {
		dto := senderDTO(s, h.api.SuggestedDescription(*s))
		dto.WorkspaceID = s.WorkspaceID
		out.Senders = append(out.Senders, dto)
	}
	response.WriteSuccess(w, http.StatusOK, out)
}

func (h *Handler) AdminPauseSender(w http.ResponseWriter, r *http.Request) {
	var body AdminPauseRequest
	if !httpx.DecodeStrictJSON(w, r, &body) {
		return
	}
	reason := rcs.PauseReason(body.Reason)
	switch reason {
	case rcs.PauseComplaints, rcs.PauseOptOutRate, rcs.PauseUnpaidBalance, rcs.PauseManual:
	default:
		response.WriteRefusal(w, refusalPause)
		return
	}
	s, err := h.api.AdminPauseSender(r.Context(), mux.Vars(r)["id"], reason)
	if err != nil {
		writeError(w, err, "admin pause sender")
		return
	}
	h.sender(w, s, http.StatusOK)
}

func (h *Handler) AdminResumeSender(w http.ResponseWriter, r *http.Request) {
	s, err := h.api.AdminResumeSender(r.Context(), mux.Vars(r)["id"])
	if err != nil {
		writeError(w, err, "admin resume sender")
		return
	}
	h.sender(w, s, http.StatusOK)
}

func (h *Handler) AdminListRoutes(w http.ResponseWriter, r *http.Request) {
	routes, err := h.api.AdminListRoutes(r.Context())
	if err != nil {
		writeError(w, err, "admin list routes")
		return
	}
	response.WriteSuccess(w, http.StatusOK, routesResponse(routes, true))
}

func (h *Handler) AdminAssignNumber(w http.ResponseWriter, r *http.Request) {
	var body RouteNumberRequest
	if !httpx.DecodeStrictJSON(w, r, &body) {
		return
	}
	route, err := h.api.AdminAssignNumber(r.Context(), mux.Vars(r)["id"], body.Number)
	if err != nil {
		writeError(w, err, "admin assign number")
		return
	}
	response.WriteSuccess(w, http.StatusOK, routeDTO(route))
}

func (h *Handler) AdminReleaseNumber(w http.ResponseWriter, r *http.Request) {
	route, err := h.api.AdminReleaseNumber(r.Context(), mux.Vars(r)["id"])
	if err != nil {
		writeError(w, err, "admin release number")
		return
	}
	response.WriteSuccess(w, http.StatusOK, routeDTO(route))
}
