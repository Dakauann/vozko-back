package advertisinghttp

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"vozko/delivery/http/response"
	"vozko/domain/advertising"
	"vozko/domain/shared"
	"vozko/domain/user"
	"vozko/infra/http/middleware"
	adsuc "vozko/usecases/advertising"
)

const (
	managerPath        = "/dashboard/advertising"
	popupMessageSource = "meta-ads-login"
	maxRequestBody     = 1 << 20
	maxJobsListed      = 50
)

type Deps struct {
	Connect         *adsuc.ConnectUseCase
	Accounts        *adsuc.AccountsUseCase
	Sync            *adsuc.SyncUseCase
	Manage          *adsuc.ManageUseCase
	Report          *adsuc.ReportUseCase
	Live            *adsuc.LiveUseCase
	Assets          *adsuc.AssetsUseCase
	Audience        *adsuc.AudienceUseCase
	Forms           *adsuc.FormsUseCase
	Rules           *adsuc.RulesUseCase
	SplitTests      *adsuc.SplitTestUseCase
	Conversions     *adsuc.ConversionsUseCase
	Publish         *adsuc.PublishUseCase
	Origins         *adsuc.OriginUseCase
	FrontendBaseURL string
}

type Handler struct {
	d   Deps
	now func() time.Time
}

func NewHandler(d Deps) *Handler {
	d.FrontendBaseURL = strings.TrimRight(d.FrontendBaseURL, "/")
	return &Handler{d: d, now: func() time.Time { return time.Now().UTC() }}
}

func workspaceOf(r *http.Request) string { return middleware.GetWorkspaceID(r) }

func personOf(r *http.Request) shared.Person {
	claims := middleware.GetClaims(r)
	if claims == nil {
		return shared.Person{}
	}
	return shared.Person{UserID: claims.UserID, SystemAdmin: claims.Role == string(user.RoleAdmin)}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, out any) bool {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBody+1))
	if err != nil || len(body) > maxRequestBody {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return false
	}
	if err := json.Unmarshal(body, out); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return false
	}
	return true
}

func requiredQuery(r *http.Request, name string) (string, error) {
	value := strings.TrimSpace(r.URL.Query().Get(name))
	if value == "" {
		return "", advertising.FieldError(name, "required")
	}
	return value, nil
}

func flagQuery(r *http.Request, name string) (bool, error) {
	switch strings.TrimSpace(r.URL.Query().Get(name)) {
	case "", "0", "false":
		return false, nil
	case "1", "true":
		return true, nil
	}
	return false, advertising.FieldError(name, "invalid")
}

func intQuery(r *http.Request, name string) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, advertising.FieldError(name, "invalid")
	}
	return value, nil
}

func listParam(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func typedList[T ~string](raw string) []T {
	return advertising.Typed[T](listParam(raw))
}

func presentAll[T, R any](items []T, present func(T) R) []R {
	out := make([]R, 0, len(items))
	for _, item := range items {
		out = append(out, present(item))
	}
	return out
}

func nonNil[T any](items []T) []T {
	if items == nil {
		return []T{}
	}
	return items
}
