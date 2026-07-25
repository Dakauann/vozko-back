package handlers

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	"vozko/domain/shipping"
	"vozko/infra/http/middleware"
)

type ShippingHandler struct {
	getAuthURLUC shipping.GetAuthorizationURLUseCase
	connectUC    shipping.ConnectProviderAccountUseCase
	listUC       shipping.ListProviderAccountsUseCase
}

func NewShippingHandler(getAuthURLUC shipping.GetAuthorizationURLUseCase, connectUC shipping.ConnectProviderAccountUseCase, listUC shipping.ListProviderAccountsUseCase) *ShippingHandler {
	return &ShippingHandler{
		getAuthURLUC: getAuthURLUC,
		connectUC:    connectUC,
		listUC:       listUC,
	}
}

type authorizationURLRequest struct {
	RedirectURI string   `json:"redirectUri"`
	Scopes      []string `json:"scopes"`
	State       string   `json:"state"`
}

type authorizationURLResponse struct {
	URL string `json:"url"`
}

type connectProviderRequest struct {
	Code        string   `json:"code"`
	RedirectURI string   `json:"redirectUri"`
	Scopes      []string `json:"scopes"`
	AccountID   string   `json:"accountId"`
}

type providerAccountResponse struct {
	ID          string          `json:"id"`
	Provider    string          `json:"provider"`
	Label       string          `json:"label"`
	ExternalID  string          `json:"externalId,omitempty"`
	Scopes      []string        `json:"scopes"`
	ExpiresAt   *time.Time      `json:"expiresAt,omitempty"`
	UpdatedAt   time.Time       `json:"updatedAt"`
	CreatedAt   time.Time       `json:"createdAt"`
	AppSettings json.RawMessage `json:"appSettings,omitempty"`
}

func (h *ShippingHandler) GetAuthorizationURL(w http.ResponseWriter, r *http.Request) {
	provider, err := h.resolveProvider(r)
	if err != nil {
		h.writeProviderError(w, err)
		return
	}

	var req authorizationURLRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{
			"redirectUri": "string (required)",
			"scopes":      "array of strings (optional)",
			"state":       "string (optional)",
		})
		return
	}

	url, err := h.getAuthURLUC.Execute(r.Context(), shipping.GetAuthorizationURLInput{
		Provider:    provider,
		RedirectURI: strings.TrimSpace(req.RedirectURI),
		Scopes:      req.Scopes,
		State:       strings.TrimSpace(req.State),
	})
	if err != nil {
		h.writeUseCaseError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, authorizationURLResponse{URL: url})
}

func (h *ShippingHandler) ConnectProviderAccount(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil || claims.UserID == "" {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	provider, err := h.resolveProvider(r)
	if err != nil {
		h.writeProviderError(w, err)
		return
	}

	var req connectProviderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{
			"code":        "string (required)",
			"redirectUri": "string (optional)",
			"scopes":      "array of strings (optional)",
			"accountId":   "string (optional)",
		})
		return
	}

	if strings.TrimSpace(req.Code) == "" {
		response.WriteValidationError(w, map[string]string{"code": "required"})
		return
	}

	account, err := h.connectUC.Execute(r.Context(), shipping.ConnectProviderAccountInput{
		Provider:    provider,
		UserID:      claims.UserID,
		Code:        strings.TrimSpace(req.Code),
		RedirectURI: strings.TrimSpace(req.RedirectURI),
		Scopes:      req.Scopes,
		AccountID:   strings.TrimSpace(req.AccountID),
	})
	if err != nil {
		log.Printf("connect provider account failed: provider=%s user=%s account=%s err=%v", provider, claims.UserID, strings.TrimSpace(req.AccountID), err)
		h.writeUseCaseError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, toProviderAccountResponse(account))
}

func (h *ShippingHandler) ReconnectProviderAccount(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil || claims.UserID == "" {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	vars := mux.Vars(r)
	accountID := strings.TrimSpace(vars["accountId"])
	if accountID == "" {
		response.WriteValidationError(w, map[string]string{"accountId": "required"})
		return
	}

	provider, err := h.resolveProvider(r)
	if err != nil {
		h.writeProviderError(w, err)
		return
	}

	var req connectProviderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{
			"code":        "string (required)",
			"redirectUri": "string (optional)",
			"scopes":      "array of strings (optional)",
		})
		return
	}

	if strings.TrimSpace(req.Code) == "" {
		response.WriteValidationError(w, map[string]string{"code": "required"})
		return
	}

	account, err := h.connectUC.Execute(r.Context(), shipping.ConnectProviderAccountInput{
		Provider:    provider,
		UserID:      claims.UserID,
		Code:        strings.TrimSpace(req.Code),
		RedirectURI: strings.TrimSpace(req.RedirectURI),
		Scopes:      req.Scopes,
		AccountID:   accountID,
	})
	if err != nil {
		log.Printf("reconnect provider account failed: provider=%s user=%s account=%s err=%v", provider, claims.UserID, accountID, err)
		h.writeUseCaseError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, toProviderAccountResponse(account))
}

func (h *ShippingHandler) ListProviderAccounts(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil || claims.UserID == "" {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	provider, err := h.resolveProvider(r)
	if err != nil {
		h.writeProviderError(w, err)
		return
	}

	accounts, err := h.listUC.Execute(r.Context(), shipping.ListProviderAccountsInput{
		Provider: provider,
		UserID:   claims.UserID,
	})
	if err != nil {
		log.Printf("list provider accounts failed: provider=%s user=%s err=%v", provider, claims.UserID, err)
		h.writeUseCaseError(w, err)
		return
	}

	responses := make([]providerAccountResponse, 0, len(accounts))
	for _, account := range accounts {
		responses = append(responses, toProviderAccountResponse(account))
	}

	response.WriteSuccess(w, http.StatusOK, responses)
}

func (h *ShippingHandler) resolveProvider(r *http.Request) (shipping.Provider, error) {
	provider := strings.ToLower(mux.Vars(r)["provider"])
	switch provider {
	case string(shipping.ProviderMelhorEnvio):
		return shipping.ProviderMelhorEnvio, nil
	default:
		return "", shipping.ErrProviderNotConfigured
	}
}

func (h *ShippingHandler) writeProviderError(w http.ResponseWriter, err error) {
	if errors.Is(err, shipping.ErrProviderNotConfigured) {
		response.WriteError(w, http.StatusNotFound, "Shipping provider not supported", nil)
		return
	}
	response.WriteError(w, http.StatusInternalServerError, "Internal server error", nil)
}

func (h *ShippingHandler) writeUseCaseError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, shipping.ErrProviderNotConfigured):
		response.WriteError(w, http.StatusServiceUnavailable, err.Error(), nil)
	case errors.Is(err, shipping.ErrAccountNotFound):
		response.WriteError(w, http.StatusNotFound, err.Error(), nil)
	case errors.Is(err, shipping.ErrAccountOwnership):
		response.WriteError(w, http.StatusForbidden, err.Error(), nil)
	case errors.Is(err, shipping.ErrAuthorizationFailed):
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
	case errors.Is(err, shipping.ErrTokenRefreshFailed):
		response.WriteError(w, http.StatusBadGateway, err.Error(), nil)
	default:
		response.WriteError(w, http.StatusInternalServerError, "Internal server error", nil)
	}
}

func toProviderAccountResponse(account *shipping.ProviderAccount) providerAccountResponse {
	var expiresAt *time.Time
	if !account.Token.ExpiresAt.IsZero() {
		expires := account.Token.ExpiresAt
		expiresAt = &expires
	}

	var settings json.RawMessage
	if len(account.AppSettings) > 0 {
		settings = json.RawMessage(account.AppSettings)
	}

	return providerAccountResponse{
		ID:          account.ID,
		Provider:    string(account.Provider),
		Label:       account.Label,
		ExternalID:  account.ExternalID,
		Scopes:      account.Token.Scopes,
		ExpiresAt:   expiresAt,
		UpdatedAt:   account.UpdatedAt,
		CreatedAt:   account.CreatedAt,
		AppSettings: settings,
	}
}
