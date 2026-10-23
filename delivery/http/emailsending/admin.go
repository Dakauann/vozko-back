package emailsending

import (
	"context"
	"net/http"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	es "vozko/domain/emailsending"
	esuc "vozko/usecases/emailsending"
)

type AdminService interface {
	List(ctx context.Context, offset, limit int) ([]esuc.AccountHealthView, int64, error)
	Pause(ctx context.Context, workspaceID, reason string) (*es.SenderAccount, error)
	Reinstate(ctx context.Context, workspaceID string) (*es.SenderAccount, error)
	Promote(ctx context.Context, workspaceID string) (*es.SenderAccount, error)
	SendQuota(ctx context.Context) (esuc.SendQuotaView, error)
}

type AdminHandler struct {
	admin AdminService
}

func NewAdminHandler(admin AdminService) *AdminHandler {
	if admin == nil {
		panic("email http: the admin service is required")
	}
	return &AdminHandler{admin: admin}
}

func (h *AdminHandler) ListAccounts(w http.ResponseWriter, r *http.Request) {
	p := httpx.ParsePagination(r.URL.Query())
	views, total, err := h.admin.List(r.Context(), (p.Page-1)*p.PageSize, p.PageSize)
	if err != nil {
		writeError(w, err, "admin list accounts")
		return
	}
	out := make([]AdminAccountResponse, 0, len(views))
	for _, v := range views {
		out = append(out, presentAdminAccount(v))
	}
	pages := int((total + int64(p.PageSize) - 1) / int64(p.PageSize))
	response.WritePaginated(w, http.StatusOK, out, response.PaginationMeta{Page: p.Page, PageSize: p.PageSize, TotalPages: pages, TotalItems: total})
}

func (h *AdminHandler) SendQuota(w http.ResponseWriter, r *http.Request) {
	view, err := h.admin.SendQuota(r.Context())
	if err != nil {
		writeError(w, err, "admin send quota")
		return
	}
	response.WriteSuccess(w, http.StatusOK, presentSendQuota(view))
}

func (h *AdminHandler) Pause(w http.ResponseWriter, r *http.Request) {
	var req PauseAccountRequest
	if r.ContentLength != 0 && !decode(w, r, &req) {
		return
	}
	acct, err := h.admin.Pause(r.Context(), mux.Vars(r)["workspaceId"], req.Reason)
	if err != nil {
		writeError(w, err, "admin pause")
		return
	}
	response.WriteSuccess(w, http.StatusOK, presentAccount(*acct))
}

func (h *AdminHandler) Reinstate(w http.ResponseWriter, r *http.Request) {
	acct, err := h.admin.Reinstate(r.Context(), mux.Vars(r)["workspaceId"])
	if err != nil {
		writeError(w, err, "admin reinstate")
		return
	}
	response.WriteSuccess(w, http.StatusOK, presentAccount(*acct))
}

func (h *AdminHandler) Promote(w http.ResponseWriter, r *http.Request) {
	acct, err := h.admin.Promote(r.Context(), mux.Vars(r)["workspaceId"])
	if err != nil {
		writeError(w, err, "admin promote")
		return
	}
	response.WriteSuccess(w, http.StatusOK, presentAccount(*acct))
}
