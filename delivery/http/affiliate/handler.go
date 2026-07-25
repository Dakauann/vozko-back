package affiliate

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	affiliatedomain "vozko/domain/affiliate"
	"vozko/infra/http/middleware"
)

type AffiliateHandler struct {
	register     affiliatedomain.RegisterAffiliateUseCase
	getMy        affiliatedomain.GetMyAffiliateUseCase
	updateMy     affiliatedomain.UpdateMyAffiliateUseCase
	listReferr   affiliatedomain.ListReferralsUseCase
	listEarn     affiliatedomain.ListEarningsUseCase
	getMyStats   affiliatedomain.GetAffiliateStatsUseCase
	validateCode affiliatedomain.ValidateReferralCodeUseCase
	adminList    affiliatedomain.AdminListAffiliatesUseCase
	adminGet     affiliatedomain.AdminGetAffiliateUseCase
	adminUpdate  affiliatedomain.AdminUpdateAffiliateUseCase
}

func NewAffiliateHandler(
	register affiliatedomain.RegisterAffiliateUseCase,
	getMy affiliatedomain.GetMyAffiliateUseCase,
	updateMy affiliatedomain.UpdateMyAffiliateUseCase,
	listReferr affiliatedomain.ListReferralsUseCase,
	listEarn affiliatedomain.ListEarningsUseCase,
	getMyStats affiliatedomain.GetAffiliateStatsUseCase,
	validateCode affiliatedomain.ValidateReferralCodeUseCase,
	adminList affiliatedomain.AdminListAffiliatesUseCase,
	adminGet affiliatedomain.AdminGetAffiliateUseCase,
	adminUpdate affiliatedomain.AdminUpdateAffiliateUseCase,
) *AffiliateHandler {
	return &AffiliateHandler{
		register:     register,
		getMy:        getMy,
		updateMy:     updateMy,
		listReferr:   listReferr,
		listEarn:     listEarn,
		getMyStats:   getMyStats,
		validateCode: validateCode,
		adminList:    adminList,
		adminGet:     adminGet,
		adminUpdate:  adminUpdate,
	}
}

// @Summary		Tornar-se um afiliado
// @Description	Registra o usuário autenticado como afiliado, criando sua marca e vinculando a carteira Asaas que receberá as comissões das indicações.
// @Tags			Afiliados
// @Accept			json
// @Produce		json
// @Param			request	body		RegisterAffiliateRequest	true	"Dados de cadastro do afiliado"
// @Success		201	{object}	AffiliateResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		409	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/affiliate/register [post]
func (h *AffiliateHandler) Register(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	var req RegisterAffiliateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{
			"asaasWalletId": "string (required)",
			"brandName":     "string (required, 3-128 chars)",
			"brandLogoUrl":  "string (required, http(s):// URL)",
			"code":          "string (optional, 3-32 chars [A-Z0-9-])",
		})
		return
	}

	aff, err := h.register.Execute(r.Context(), affiliatedomain.RegisterAffiliateInput{
		UserID:        claims.UserID,
		Code:          req.Code,
		BrandName:     req.BrandName,
		BrandLogoURL:  req.BrandLogoURL,
		AsaasWalletID: req.AsaasWalletID,
	})
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusCreated, toAffiliateResponse(aff))
}

// @Summary		Consultar meu perfil de afiliado
// @Description	Retorna o perfil de afiliado do usuário autenticado, incluindo os totais de indicações e ganhos acumulados.
// @Tags			Afiliados
// @Produce		json
// @Success		200	{object}	AffiliateProfileResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/affiliate/me [get]
func (h *AffiliateHandler) GetMe(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	profile, err := h.getMy.Execute(r.Context(), claims.UserID)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toAffiliateProfileResponse(profile))
}

// @Summary		Atualizar meu perfil de afiliado
// @Description	Atualiza os dados da marca e a carteira Asaas do afiliado autenticado. Todos os campos são opcionais.
// @Tags			Afiliados
// @Accept			json
// @Produce		json
// @Param			request	body		UpdateAffiliateRequest	true	"Campos do perfil a atualizar"
// @Success		200	{object}	AffiliateResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/affiliate/me [put]
func (h *AffiliateHandler) UpdateMe(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	var req UpdateAffiliateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{
			"asaasWalletId": "string (optional)",
			"brandName":     "string (optional)",
			"brandLogoUrl":  "string (optional)",
		})
		return
	}
	aff, err := h.updateMy.Execute(r.Context(), affiliatedomain.UpdateAffiliateProfileInput{
		UserID:        claims.UserID,
		AsaasWalletID: req.AsaasWalletID,
		BrandName:     req.BrandName,
		BrandLogoURL:  req.BrandLogoURL,
	})
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toAffiliateResponse(aff))
}

// @Summary		Listar minhas indicações
// @Description	Retorna a lista paginada de workspaces indicados pelo afiliado autenticado.
// @Tags			Afiliados
// @Produce		json
// @Param			page		query		int	false	"Número da página (inicia em 1)"
// @Param			pageSize	query		int	false	"Quantidade de itens por página"
// @Success		200	{object}	ReferralListResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/affiliate/referrals [get]
func (h *AffiliateHandler) ListReferrals(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	pag := httpx.ParsePagination(r.URL.Query())
	items, total, err := h.listReferr.Execute(r.Context(), claims.UserID, pag.Page, pag.PageSize)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WritePaginated(w, http.StatusOK, toReferralResponses(items), response.PaginationMeta{
		Page:       pag.Page,
		PageSize:   pag.PageSize,
		TotalItems: total,
		TotalPages: pagesFor(total, pag.PageSize),
	})
}

// @Summary		Listar meus ganhos
// @Description	Retorna a lista paginada de comissões registradas para o afiliado autenticado.
// @Tags			Afiliados
// @Produce		json
// @Param			page		query		int	false	"Número da página (inicia em 1)"
// @Param			pageSize	query		int	false	"Quantidade de itens por página"
// @Success		200	{object}	EarningListResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/affiliate/earnings [get]
func (h *AffiliateHandler) ListEarnings(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	pag := httpx.ParsePagination(r.URL.Query())
	items, total, err := h.listEarn.Execute(r.Context(), claims.UserID, pag.Page, pag.PageSize)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WritePaginated(w, http.StatusOK, toEarningResponses(items), response.PaginationMeta{
		Page:       pag.Page,
		PageSize:   pag.PageSize,
		TotalItems: total,
		TotalPages: pagesFor(total, pag.PageSize),
	})
}

// @Summary		Validar um código de indicação
// @Description	Verifica se um código de indicação existe e está ativo, retornando os dados públicos da marca do afiliado para exibição no cadastro.
// @Tags			Afiliados
// @Produce		json
// @Param			code	path		string	true	"Código de indicação a validar"
// @Success		200	{object}	ReferralValidationResponse
// @Failure		500	{object}	response.ErrorResponse
// @Router			/affiliate/validate/{code} [get]
func (h *AffiliateHandler) ValidateCode(w http.ResponseWriter, r *http.Request) {
	code := mux.Vars(r)["code"]
	result, err := h.validateCode.Execute(r.Context(), code)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to validate code", nil)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toReferralValidationResponse(result))
}

func (h *AffiliateHandler) AdminList(w http.ResponseWriter, r *http.Request) {
	pag := httpx.ParsePagination(r.URL.Query())
	items, total, err := h.adminList.Execute(r.Context(), pag.Page, pag.PageSize)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WritePaginated(w, http.StatusOK, toAffiliateResponses(items), response.PaginationMeta{
		Page:       pag.Page,
		PageSize:   pag.PageSize,
		TotalItems: total,
		TotalPages: pagesFor(total, pag.PageSize),
	})
}

func (h *AffiliateHandler) AdminGet(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	profile, err := h.adminGet.Execute(r.Context(), id)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toAffiliateProfileResponse(profile))
}

func (h *AffiliateHandler) AdminUpdate(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	var req AdminUpdateAffiliateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{
			"commissionPct": "number (optional, 0-0.30)",
			"active":        "boolean (optional)",
			"tier":          "string (optional, one of: affiliate, reseller)",
		})
		return
	}
	var tierPtr *affiliatedomain.Tier
	if req.Tier != nil {
		t := affiliatedomain.Tier(*req.Tier)
		tierPtr = &t
	}
	aff, err := h.adminUpdate.Execute(r.Context(), affiliatedomain.AdminUpdateAffiliateInput{
		ID:            id,
		CommissionPct: req.CommissionPct,
		Active:        req.Active,
		Tier:          tierPtr,
	})
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toAffiliateResponse(aff))
}

func (h *AffiliateHandler) handleDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, affiliatedomain.ErrAffiliateNotFound):
		response.WriteError(w, http.StatusNotFound, err.Error(), nil)
	case errors.Is(err, affiliatedomain.ErrAffiliateAlreadyExists),
		errors.Is(err, affiliatedomain.ErrCodeAlreadyTaken),
		errors.Is(err, affiliatedomain.ErrWorkspaceAlreadyReferred):
		response.WriteError(w, http.StatusConflict, err.Error(), nil)
	case errors.Is(err, affiliatedomain.ErrInvalidCode),
		errors.Is(err, affiliatedomain.ErrInvalidBrandName),
		errors.Is(err, affiliatedomain.ErrInvalidBrandLogoURL),
		errors.Is(err, affiliatedomain.ErrInvalidAsaasWalletID),
		errors.Is(err, affiliatedomain.ErrInvalidCommissionPct),
		errors.Is(err, affiliatedomain.ErrInvalidTier),
		errors.Is(err, affiliatedomain.ErrInvalidReferralCode),
		errors.Is(err, affiliatedomain.ErrSelfReferral):
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
	case errors.Is(err, affiliatedomain.ErrUnauthorized):
		response.WriteError(w, http.StatusUnauthorized, err.Error(), nil)
	default:
		response.WriteError(w, http.StatusInternalServerError, "Internal server error", nil)
	}
}

func pagesFor(total int64, pageSize int) int {
	if total <= 0 || pageSize <= 0 {
		return 0
	}
	return int((total + int64(pageSize) - 1) / int64(pageSize))
}
