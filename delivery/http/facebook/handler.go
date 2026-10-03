package facebook

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/oauthpopup"
	"vozko/delivery/http/response"
	fbdomain "vozko/domain/facebook"
	"vozko/domain/shared"
	"vozko/infra/http/middleware"
	cauc "vozko/usecases/commentautomation"
	fbuc "vozko/usecases/facebook"
)

const (
	pagesPath          = "/dashboard/facebook-pages"
	popupMessageSource = "fb-business-login"
	maxRequestBody     = 1 << 20
)

type HandlerDeps struct {
	Connect             *fbuc.ConnectPagesUseCase
	Pages               *fbuc.PageUseCases
	Health              *fbuc.HealthCheckUseCase
	ThreadControl       *fbuc.ThreadControlUseCase
	Posts               *fbuc.PostUseCases
	Comments            *fbuc.CommentUseCases
	Profile             *fbuc.MessengerProfileUseCases
	Rules               *cauc.Manager
	PictureURL          func(key string) string
	HumanAgentAvailable bool
	FrontendBaseURL     string
}

type Handler struct {
	connect         *fbuc.ConnectPagesUseCase
	pages           *fbuc.PageUseCases
	health          *fbuc.HealthCheckUseCase
	threadControl   *fbuc.ThreadControlUseCase
	posts           *fbuc.PostUseCases
	comments        *fbuc.CommentUseCases
	profile         *fbuc.MessengerProfileUseCases
	rules           *cauc.Manager
	present         pagePresenter
	frontendBaseURL string
}

func NewHandler(d HandlerDeps) *Handler {
	return &Handler{
		connect:         d.Connect,
		pages:           d.Pages,
		health:          d.Health,
		threadControl:   d.ThreadControl,
		posts:           d.Posts,
		comments:        d.Comments,
		profile:         d.Profile,
		rules:           d.Rules,
		present:         pagePresenter{pictureURL: d.PictureURL, humanAgentAvailable: d.HumanAgentAvailable},
		frontendBaseURL: strings.TrimRight(d.FrontendBaseURL, "/"),
	}
}

func (h *Handler) StartConnect(w http.ResponseWriter, r *http.Request) {
	userID := ""
	if claims := middleware.GetClaims(r); claims != nil {
		userID = claims.UserID
	}
	out, err := h.connect.Start(r.Context(), fbuc.StartConnectInput{
		WorkspaceID: middleware.GetWorkspaceID(r),
		UserID:      userID,
		ReturnPath:  r.URL.Query().Get("returnPath"),
		Popup:       r.URL.Query().Get("popup") == "1",
	})
	if err != nil {
		log.Printf("[facebook] connect start failed: %v", err)
		response.WriteError(w, http.StatusInternalServerError, "Failed to start the Facebook connection", nil)
		return
	}
	if r.URL.Query().Get("redirect") == "1" {
		http.Redirect(w, r, out.AuthorizeURL, http.StatusFound)
		return
	}
	response.WriteSuccess(w, http.StatusOK, ConnectStartResponse{AuthorizeURL: out.AuthorizeURL})
}

type pageOutcomeResponse struct {
	ID       string   `json:"id,omitempty"`
	FBPageID string   `json:"fbPageId"`
	Name     string   `json:"name"`
	Outcome  string   `json:"outcome"`
	Missing  []string `json:"missing"`
	Warning  string   `json:"warning,omitempty"`
}

func (h *Handler) HandleCallback(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	out, err := h.connect.Complete(r.Context(), fbuc.CompleteConnectInput{
		Code:        query.Get("code"),
		State:       query.Get("state"),
		Error:       query.Get("error"),
		ErrorReason: query.Get("error_reason"),
	})
	if err != nil {
		log.Printf("[facebook] connect failed: %v", err)
		status, reason := "error", connectErrorCode(err)
		if reason == "declined" {
			status = "cancelled"
		}
		var connectErr *fbuc.ConnectError
		if errors.As(err, &connectErr) && connectErr.Popup {
			oauthpopup.WriteResult(w, h.frontendBaseURL, map[string]any{"source": popupMessageSource, "status": status, "reason": reason})
			return
		}
		returnPath := pagesPath
		if connectErr != nil {
			returnPath = connectErr.ReturnPath
		}
		oauthpopup.Redirect(w, r, h.frontendBaseURL, returnPath, pagesPath, url.Values{"facebook": {status}, "reason": {reason}})
		return
	}

	pages := make([]pageOutcomeResponse, 0, len(out.Pages))
	for _, p := range out.Pages {
		missing := make([]string, 0, len(p.Missing))
		for _, m := range p.Missing {
			missing = append(missing, string(m))
		}
		pages = append(pages, pageOutcomeResponse{ID: p.PageID, FBPageID: p.FBPageID, Name: p.Name, Outcome: string(p.Outcome), Missing: missing, Warning: p.Warning})
	}
	connected := out.ConnectedCount()
	status := "connected"
	if connected < len(out.Pages) {
		status = "partial"
	}
	if out.Popup {
		oauthpopup.WriteResult(w, h.frontendBaseURL, map[string]any{"source": popupMessageSource, "status": status, "pages": pages})
		return
	}
	oauthpopup.Redirect(w, r, h.frontendBaseURL, out.ReturnPath, pagesPath, url.Values{
		"facebook":  {status},
		"connected": {strconv.Itoa(connected)},
		"skipped":   {strconv.Itoa(len(out.Pages) - connected)},
	})
}

func connectErrorCode(err error) string {
	if code, ok := oauthpopup.StateErrorCode(err); ok {
		return code
	}
	switch {
	case errors.Is(err, fbdomain.ErrAuthorizationDenied):
		return "declined"
	case errors.Is(err, fbdomain.ErrNoPagesGranted):
		return "no_pages_granted"
	case errors.Is(err, fbdomain.ErrGrantUnverifiable):
		return "grant_unverifiable"
	default:
		return "connect_failed"
	}
}

func (h *Handler) ListPages(w http.ResponseWriter, r *http.Request) {
	input := fbdomain.ListPagesInput{
		WorkspaceID: middleware.GetWorkspaceID(r),
		Search:      strings.TrimSpace(r.URL.Query().Get("search")),
		Options:     shared.QueryOptions{Pagination: httpx.ParsePagination(r.URL.Query())},
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("status")); raw != "" {
		if status := fbdomain.Status(strings.ToUpper(raw)); status.Valid() {
			input.Status = &status
		}
	}
	result, err := h.pages.List(r.Context(), input)
	if err != nil {
		writeDomainError(w, err, "Failed to list Facebook pages")
		return
	}
	response.WritePaginated(w, http.StatusOK, h.present.pages(result.Items), response.PaginationMeta{
		Page: result.Page, PageSize: result.PageSize, TotalItems: result.TotalItems, TotalPages: result.TotalPages,
	})
}

func (h *Handler) GetPage(w http.ResponseWriter, r *http.Request) {
	page, err := h.pages.Get(r.Context(), middleware.GetWorkspaceID(r), mux.Vars(r)["id"])
	if err != nil {
		writeDomainError(w, err, "Failed to load the Facebook page")
		return
	}
	response.WriteSuccess(w, http.StatusOK, h.present.page(page))
}

func (h *Handler) UpdatePage(w http.ResponseWriter, r *http.Request) {
	var req UpdatePageRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	page, err := h.pages.UpdateConfig(r.Context(), fbuc.UpdatePageConfigInput{
		WorkspaceID:          middleware.GetWorkspaceID(r),
		ID:                   mux.Vars(r)["id"],
		DepartmentID:         req.DepartmentID,
		AgentID:              req.AgentID,
		WorkflowID:           req.WorkflowID,
		PipelineID:           req.PipelineID,
		EnableAgentResponses: req.EnableAgentResponses,
		EnableWorkflow:       req.EnableWorkflow,
		EnableAnalysis:       req.EnableAnalysis,
		EnableAutoStaging:    req.EnableAutoStaging,
		EnableAutoMemory:     req.EnableAutoMemory,
		AutomationDisclosure: req.AutomationDisclosure,
	})
	if err != nil {
		writeDomainError(w, err, "Failed to update the Facebook page")
		return
	}
	response.WriteSuccess(w, http.StatusOK, h.present.page(page))
}

func (h *Handler) DisconnectPage(w http.ResponseWriter, r *http.Request) {
	warning, err := h.pages.Disconnect(r.Context(), middleware.GetWorkspaceID(r), mux.Vars(r)["id"])
	if err != nil {
		writeDomainError(w, err, "Failed to disconnect the Facebook page")
		return
	}
	response.WriteSuccess(w, http.StatusOK, map[string]string{"status": "disconnected", "warning": warning})
}

func (h *Handler) CheckHealth(w http.ResponseWriter, r *http.Request) {
	page, err := h.health.CheckPage(r.Context(), middleware.GetWorkspaceID(r), mux.Vars(r)["id"])
	if err != nil {
		writeDomainError(w, err, "Failed to check the Facebook page")
		return
	}
	response.WriteSuccess(w, http.StatusOK, h.present.page(page))
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	if err := json.NewDecoder(io.LimitReader(r.Body, maxRequestBody)).Decode(target); err != nil {
		response.WriteInvalidBodyError(w, nil)
		return false
	}
	return true
}
