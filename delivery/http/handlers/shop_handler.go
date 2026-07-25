package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	"vozko/domain/shop"
	"vozko/infra/http/middleware"
)

type ShopHandler struct {
	createUseCase shop.CreateShopUseCase
	updateUseCase shop.UpdateShopUseCase
	getUseCase    shop.GetShopUseCase
	listUseCase   shop.ListShopsUseCase
}

func NewShopHandler(
	createUC shop.CreateShopUseCase,
	updateUC shop.UpdateShopUseCase,
	getUC shop.GetShopUseCase,
	listUC shop.ListShopsUseCase,
) *ShopHandler {
	return &ShopHandler{
		createUseCase: createUC,
		updateUseCase: updateUC,
		getUseCase:    getUC,
		listUseCase:   listUC,
	}
}

type createShopRequest struct {
	Name          string `json:"name"`
	Brand         string `json:"brand"`
	LogoMediaID   string `json:"logoMediaId"`
	BannerMediaID string `json:"bannerMediaId,omitempty"`
}

type updateShopRequest struct {
	Name          *string `json:"name,omitempty"`
	Brand         *string `json:"brand,omitempty"`
	LogoMediaID   *string `json:"logoMediaId,omitempty"`
	BannerMediaID *string `json:"bannerMediaId,omitempty"`
}

func (h *ShopHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createShopRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", map[string]string{
			"name":          "string (required)",
			"brand":         "string (required)",
			"logoMediaId":   "string (required)",
			"bannerMediaId": "string (optional)",
		})
		return
	}

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	input := shop.CreateShopInput{
		UserID:        claims.UserID,
		Name:          strings.TrimSpace(req.Name),
		Brand:         strings.TrimSpace(req.Brand),
		LogoMediaID:   strings.TrimSpace(req.LogoMediaID),
		BannerMediaID: strings.TrimSpace(req.BannerMediaID),
	}

	created, err := h.createUseCase.Execute(input)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusCreated, created)
}

func (h *ShopHandler) Update(w http.ResponseWriter, r *http.Request) {
	shopID, err := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid shop ID", nil)
		return
	}

	var req updateShopRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{
			"name":          "string (optional)",
			"brand":         "string (optional)",
			"logoMediaId":   "string (optional)",
			"bannerMediaId": "string (optional)",
		})
		return
	}

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	existing, err := h.getUseCase.Execute(shopID)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	if existing.UserID != claims.UserID {
		response.WriteError(w, http.StatusForbidden, "You don't have permission to update this shop", nil)
		return
	}

	input := shop.UpdateShopInput{
		Name:          req.Name,
		Brand:         req.Brand,
		LogoMediaID:   req.LogoMediaID,
		BannerMediaID: req.BannerMediaID,
	}

	updated, err := h.updateUseCase.Execute(shopID, input)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, updated)
}

func (h *ShopHandler) Get(w http.ResponseWriter, r *http.Request) {
	shopID, err := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid shop ID", nil)
		return
	}

	shopItem, err := h.getUseCase.Execute(shopID)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, shopItem)
}

func (h *ShopHandler) List(w http.ResponseWriter, r *http.Request) {
	values := r.URL.Query()

	page, _ := strconv.Atoi(values.Get("page"))
	if page < 1 {
		page = 1
	}

	pageSize, _ := strconv.Atoi(values.Get("pageSize"))
	if pageSize < 1 {
		pageSize = 20
	}

	var userID *string
	if uid := strings.TrimSpace(values.Get("userId")); uid != "" {
		userID = &uid
	}

	var search *string
	if s := strings.TrimSpace(values.Get("search")); s != "" {
		search = &s
	}

	input := shop.ListShopsInput{
		Page:     page,
		PageSize: pageSize,
		UserID:   userID,
		Search:   search,
	}

	result, err := h.listUseCase.Execute(input)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to list shops", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, result)
}

func (h *ShopHandler) GetMyShops(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	input := shop.ListShopsInput{
		Page:     1,
		PageSize: 20000,
		UserID:   &claims.UserID,
	}

	result, err := h.listUseCase.Execute(input)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to get shop", nil)
		return
	}

	if result.TotalItems == 0 {
		response.WriteError(w, http.StatusNotFound, "Shop not found", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, result)
}

func (h *ShopHandler) handleDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, shop.ErrShopNotFound):
		response.WriteError(w, http.StatusNotFound, err.Error(), nil)
	case errors.Is(err, shop.ErrShopNameRequired),
		errors.Is(err, shop.ErrShopBrandRequired),
		errors.Is(err, shop.ErrShopLogoRequired),
		errors.Is(err, shop.ErrShopNameTooShort),
		errors.Is(err, shop.ErrShopNameTooLong),
		errors.Is(err, shop.ErrInvalidMediaID):
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
	case errors.Is(err, shop.ErrUserAlreadyHasShop),
		errors.Is(err, shop.ErrShopLimitReached):
		response.WriteError(w, http.StatusConflict, err.Error(), nil)
	case errors.Is(err, shop.ErrUnauthorized):
		response.WriteError(w, http.StatusForbidden, err.Error(), nil)
	default:
		response.WriteError(w, http.StatusInternalServerError, "Internal server error", nil)
	}
}
