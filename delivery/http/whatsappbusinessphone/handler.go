package whatsappbusinessphone

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	"vozko/domain/auth"
	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/domain/user"
	businessphonedomain "vozko/domain/whatsapp/business_phone"
	"vozko/domain/workspace_phone_access"
	"vozko/infra/http/middleware"
)

type WhatsAppBusinessPhoneHandlerConfig struct {
	AccessToken string
}

type WhatsAppBusinessPhoneHandler struct {
	config                       WhatsAppBusinessPhoneHandlerConfig
	listUseCase                  businessphonedomain.ListUseCase
	getUseCase                   businessphonedomain.GetUseCase
	syncPhoneNumberUseCase       businessphonedomain.SyncPhoneNumberUseCase
	registerPhoneUseCase         businessphonedomain.RegisterPhoneUseCase
	deregisterPhoneUseCase       businessphonedomain.DeregisterPhoneUseCase
	releasePhoneUseCase          businessphonedomain.ReleasePhoneUseCase
	requestVerificationUseCase   businessphonedomain.RequestVerificationCodeUseCase
	verifyCodeUseCase            businessphonedomain.VerifyCodeUseCase
	updateBusinessProfileUseCase businessphonedomain.UpdateBusinessProfileUseCase
	getBusinessProfileUseCase    businessphonedomain.GetBusinessProfileUseCase
	deleteUseCase                businessphonedomain.DeletePhoneNumberUseCase
	unassignOwnerUseCase         businessphonedomain.UnassignOwnerUseCase
	phoneRepo                    businessphonedomain.Repository
	workspacePhoneAccessRepo     workspace_phone_access.Repository
	metaAPIService               businessphonedomain.MetaAPIService
	whatsappClientFactory        conversation.WhatsAppClientFactory
}

func NewWhatsAppBusinessPhoneHandler(
	config WhatsAppBusinessPhoneHandlerConfig,
	listUC businessphonedomain.ListUseCase,
	getUC businessphonedomain.GetUseCase,
	syncPhoneNumberUC businessphonedomain.SyncPhoneNumberUseCase,
	registerPhoneUC businessphonedomain.RegisterPhoneUseCase,
	deregisterPhoneUC businessphonedomain.DeregisterPhoneUseCase,
	releasePhoneUC businessphonedomain.ReleasePhoneUseCase,
	requestVerificationUC businessphonedomain.RequestVerificationCodeUseCase,
	verifyCodeUC businessphonedomain.VerifyCodeUseCase,
	updateBusinessProfileUC businessphonedomain.UpdateBusinessProfileUseCase,
	getBusinessProfileUC businessphonedomain.GetBusinessProfileUseCase,
	deleteUC businessphonedomain.DeletePhoneNumberUseCase,
	unassignOwnerUC businessphonedomain.UnassignOwnerUseCase,
	phoneRepo businessphonedomain.Repository,
	workspacePhoneAccessRepo workspace_phone_access.Repository,
	metaAPIService businessphonedomain.MetaAPIService,
	whatsappClientFactory conversation.WhatsAppClientFactory,
) *WhatsAppBusinessPhoneHandler {
	return &WhatsAppBusinessPhoneHandler{
		config:                       config,
		listUseCase:                  listUC,
		getUseCase:                   getUC,
		syncPhoneNumberUseCase:       syncPhoneNumberUC,
		registerPhoneUseCase:         registerPhoneUC,
		deregisterPhoneUseCase:       deregisterPhoneUC,
		releasePhoneUseCase:          releasePhoneUC,
		requestVerificationUseCase:   requestVerificationUC,
		verifyCodeUseCase:            verifyCodeUC,
		updateBusinessProfileUseCase: updateBusinessProfileUC,
		getBusinessProfileUseCase:    getBusinessProfileUC,
		deleteUseCase:                deleteUC,
		unassignOwnerUseCase:         unassignOwnerUC,
		phoneRepo:                    phoneRepo,
		workspacePhoneAccessRepo:     workspacePhoneAccessRepo,
		metaAPIService:               metaAPIService,
		whatsappClientFactory:        whatsappClientFactory,
	}
}

func (h *WhatsAppBusinessPhoneHandler) parsePhoneListInput(r *http.Request) businessphonedomain.ListInput {
	status := r.URL.Query().Get("status")
	qualityRating := r.URL.Query().Get("qualityRating")
	wabaID := r.URL.Query().Get("waba_id")
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("pageSize"))
	search := r.URL.Query().Get("search")

	return businessphonedomain.ListInput{
		WABAId:        wabaID,
		Status:        businessphonedomain.Status(status),
		QualityRating: businessphonedomain.QualityRating(qualityRating),
		Search:        search,
		Options: shared.QueryOptions{
			Pagination: shared.Pagination{
				Page:     page,
				PageSize: pageSize,
			},
		},
	}
}

// @Summary		Listar telefones comerciais do WhatsApp
// @Description	Retorna, de forma paginada, os telefones comerciais do WhatsApp Business aos quais o workspace tem acesso. Aceita filtros por status, avaliação de qualidade, WABA e termo de busca.
// @Tags			Telefones do WhatsApp Business
// @Produce		json
// @Param			status			query	string	false	"Status do telefone (ex.: CONNECTED, PENDING, DISCONNECTED)"
// @Param			qualityRating	query	string	false	"Avaliação de qualidade (ex.: GREEN, YELLOW, RED)"
// @Param			waba_id			query	string	false	"ID da conta do WhatsApp Business (WABA)"
// @Param			search			query	string	false	"Termo de busca"
// @Param			page			query	int		false	"Número da página (inicia em 1)"
// @Param			pageSize		query	int		false	"Quantidade de itens por página"
// @Success		200	{array}		businessphone.WhatsAppBusinessPhoneNumber
// @Failure		401	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/whatsapp/business-phones [get]
func (h *WhatsAppBusinessPhoneHandler) List(w http.ResponseWriter, r *http.Request) {
	if middleware.GetClaims(r) == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	input := h.parsePhoneListInput(r)

	wsID := middleware.GetWorkspaceID(r)
	input.OwnerWorkspaceID = wsID
	if h.workspacePhoneAccessRepo != nil {
		accessPhoneIDs, err := h.workspacePhoneAccessRepo.GetPhoneIDsForWorkspace(wsID)
		if err != nil {
			response.WriteError(w, http.StatusInternalServerError, "Failed to list WhatsApp Business phone numbers", nil)
			return
		}
		input.AccessPhoneIDs = accessPhoneIDs
	}

	result, err := h.listUseCase.Execute(input)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to list WhatsApp Business phone numbers", nil)
		return
	}

	response.WritePaginated(w, http.StatusOK, result.Items, response.PaginationMeta{
		Page:       result.Page,
		PageSize:   result.PageSize,
		TotalPages: result.TotalPages,
		TotalItems: result.TotalItems,
	})
}

func (h *WhatsAppBusinessPhoneHandler) ListAll(w http.ResponseWriter, r *http.Request) {
	input := h.parsePhoneListInput(r)

	result, err := h.listUseCase.Execute(input)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to list WhatsApp Business phone numbers", nil)
		return
	}

	response.WritePaginated(w, http.StatusOK, result.Items, response.PaginationMeta{
		Page:       result.Page,
		PageSize:   result.PageSize,
		TotalPages: result.TotalPages,
		TotalItems: result.TotalItems,
	})
}

// @Summary		Consultar telefone comercial do WhatsApp
// @Description	Retorna os detalhes de um telefone comercial do WhatsApp Business, desde que o workspace tenha acesso a ele.
// @Tags			Telefones do WhatsApp Business
// @Produce		json
// @Param			id	path	string	true	"ID do telefone"
// @Success		200	{object}	businessphone.WhatsAppBusinessPhoneNumber
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/whatsapp/business-phones/{id} [get]
func (h *WhatsAppBusinessPhoneHandler) Get(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	id := mux.Vars(r)["id"]
	if id == "" {
		response.WriteError(w, http.StatusBadRequest, "Phone ID is required", nil)
		return
	}

	phone, err := h.getUseCase.Execute(id)
	if err != nil {
		if errors.Is(err, businessphonedomain.ErrPhoneNumberNotFound) {
			response.WriteError(w, http.StatusNotFound, "WhatsApp Business phone number not found", nil)
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "Failed to get WhatsApp Business phone number", nil)
		return
	}

	wsID := middleware.GetWorkspaceID(r)
	allowed := phone.BelongsToWorkspace(wsID)
	if !allowed && h.workspacePhoneAccessRepo != nil {
		granted, accessErr := h.workspacePhoneAccessRepo.HasAccess(wsID, phone.ID)
		if accessErr != nil {
			response.WriteError(w, http.StatusInternalServerError, "Failed to get WhatsApp Business phone number", nil)
			return
		}
		allowed = granted
	}
	if !allowed {
		response.WriteError(w, http.StatusForbidden, "You do not have access to this phone number", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, phone)
}

func (h *WhatsAppBusinessPhoneHandler) GetAdmin(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if id == "" {
		response.WriteError(w, http.StatusBadRequest, "Phone ID is required", nil)
		return
	}

	phone, err := h.getUseCase.Execute(id)
	if err != nil {
		if errors.Is(err, businessphonedomain.ErrPhoneNumberNotFound) {
			response.WriteError(w, http.StatusNotFound, "WhatsApp Business phone number not found", nil)
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "Failed to get WhatsApp Business phone number", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, phone)
}

func (h *WhatsAppBusinessPhoneHandler) SyncOne(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if id == "" {
		response.WriteError(w, http.StatusBadRequest, "Phone ID is required", nil)
		return
	}

	phone, err := h.syncPhoneNumberUseCase.Execute(businessphonedomain.SyncPhoneNumberInput{
		PhoneID:     id,
		AccessToken: h.config.AccessToken,
	})
	if err != nil {
		handleBusinessPhoneError(w, err, "Failed to sync WhatsApp Business phone number")
		return
	}

	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"message":     "WhatsApp Business phone number synchronized successfully",
		"phoneNumber": phone,
	})
}

func (h *WhatsAppBusinessPhoneHandler) RequestVerification(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if id == "" {
		response.WriteError(w, http.StatusBadRequest, "Phone ID is required", nil)
		return
	}

	var req RequestVerificationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}

	method := businessphonedomain.VerificationMethodSMS
	if req.Method == "VOICE" {
		method = businessphonedomain.VerificationMethodVoice
	}

	err := h.requestVerificationUseCase.Execute(businessphonedomain.RequestVerificationCodeInput{
		PhoneID:     id,
		Method:      method,
		Language:    req.Language,
		AccessToken: h.config.AccessToken,
	})
	if err != nil {
		handleBusinessPhoneError(w, err, "Failed to request verification code")
		return
	}

	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"message": "Verification code sent successfully",
		"method":  method,
	})
}

func (h *WhatsAppBusinessPhoneHandler) VerifyCode(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if id == "" {
		response.WriteError(w, http.StatusBadRequest, "Phone ID is required", nil)
		return
	}

	var req VerifyCodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}

	if req.Code == "" {
		response.WriteError(w, http.StatusBadRequest, "Verification code is required", nil)
		return
	}

	phone, err := h.verifyCodeUseCase.Execute(businessphonedomain.VerifyCodeInput{
		PhoneID:     id,
		Code:        req.Code,
		AccessToken: h.config.AccessToken,
	})
	if err != nil {
		handleBusinessPhoneError(w, err, "Failed to verify code")
		return
	}

	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"message":     "Phone number verified successfully",
		"phoneNumber": phone,
	})
}

func (h *WhatsAppBusinessPhoneHandler) UpdateBusinessProfile(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if id == "" {
		response.WriteError(w, http.StatusBadRequest, "Phone ID is required", nil)
		return
	}

	var req UpdateBusinessProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}

	profile := businessphonedomain.BusinessProfile{
		About:                req.About,
		Address:              req.Address,
		Description:          req.Description,
		Email:                req.Email,
		ProfilePictureURL:    req.ProfilePictureURL,
		ProfilePictureHandle: req.ProfilePictureHandle,
		Websites:             req.Websites,
		Vertical:             businessphonedomain.BusinessVertical(req.Vertical),
	}

	phone, err := h.updateBusinessProfileUseCase.Execute(businessphonedomain.UpdateBusinessProfileInput{
		PhoneID:     id,
		Profile:     profile,
		AccessToken: h.config.AccessToken,
	})
	if err != nil {
		handleBusinessPhoneError(w, err, "Failed to update business profile")
		return
	}

	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"message":     "Business profile updated successfully",
		"phoneNumber": phone,
	})
}

func (h *WhatsAppBusinessPhoneHandler) GetBusinessProfile(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if id == "" {
		response.WriteError(w, http.StatusBadRequest, "Phone ID is required", nil)
		return
	}

	profile, err := h.getBusinessProfileUseCase.Execute(businessphonedomain.GetBusinessProfileInput{
		PhoneID:     id,
		AccessToken: h.config.AccessToken,
	})
	if err != nil {
		handleBusinessPhoneError(w, err, "Failed to get business profile")
		return
	}

	response.WriteSuccess(w, http.StatusOK, profile)
}

func (h *WhatsAppBusinessPhoneHandler) UploadProfilePicture(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Failed to parse form data. Max file size is 10MB", nil)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, "File is required. Use form field 'file'", nil)
		return
	}
	defer file.Close()

	contentType := header.Header.Get("Content-Type")
	if contentType != "image/jpeg" && contentType != "image/png" && contentType != "image/jpg" {
		response.WriteError(w, http.StatusBadRequest, "Invalid file type. Only JPEG and PNG images are supported", nil)
		return
	}

	data, err := io.ReadAll(file)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to read file", nil)
		return
	}

	handle, err := h.metaAPIService.UploadProfilePicture(data, header.Filename, h.config.AccessToken)
	if err != nil {
		handleBusinessPhoneError(w, err, "Failed to upload profile picture")
		return
	}

	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"message":              "Profile picture uploaded successfully",
		"profilePictureHandle": handle,
	})
}

func (h *WhatsAppBusinessPhoneHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if id == "" {
		response.WriteError(w, http.StatusBadRequest, "Phone ID is required", nil)
		return
	}

	if err := h.deleteUseCase.Execute(id); err != nil {
		handleBusinessPhoneError(w, err, "Failed to delete WhatsApp Business phone number")
		return
	}

	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"message": "WhatsApp Business phone number deleted successfully",
	})
}

func (h *WhatsAppBusinessPhoneHandler) Register(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if id == "" {
		response.WriteError(w, http.StatusBadRequest, "Phone ID is required", nil)
		return
	}

	var req RegisterPhoneRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}

	if req.Pin == "" {
		response.WriteError(w, http.StatusBadRequest, "A 6-digit PIN is required for registration (sets two-step verification)", nil)
		return
	}

	phone, err := h.registerPhoneUseCase.Execute(businessphonedomain.RegisterPhoneInput{
		PhoneID:     id,
		Pin:         req.Pin,
		AccessToken: h.config.AccessToken,
	})
	if err != nil {
		handleBusinessPhoneError(w, err, "Failed to register phone number with Cloud API")
		return
	}

	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"message":     "Phone number registered successfully with Cloud API",
		"phoneNumber": phone,
	})
}

func (h *WhatsAppBusinessPhoneHandler) DeregisterForWorkspace(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	wsID := middleware.GetWorkspaceID(r)

	id := mux.Vars(r)["id"]
	if id == "" {
		response.WriteError(w, http.StatusBadRequest, "Phone ID is required", nil)
		return
	}

	phone := h.verifyPhoneOwnership(w, claims, id, wsID)
	if phone == nil {
		return
	}

	updated, err := h.deregisterPhoneUseCase.Execute(businessphonedomain.DeregisterPhoneInput{
		PhoneID:     id,
		AccessToken: phone.AccessToken,
	})
	if err != nil {
		handleBusinessPhoneError(w, err, "Failed to disconnect phone number from Cloud API")
		return
	}

	log.Printf("[business-phone] disconnected (deregistered): phone=%s waba=%s workspace=%s user=%s",
		phone.ID, phone.WABAId, wsID, claims.UserID)
	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"message":     "Phone number disconnected from the WhatsApp Cloud API. You can reconnect it at any time.",
		"phoneNumber": updated,
	})
}

func (h *WhatsAppBusinessPhoneHandler) ReleaseForWorkspace(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	wsID := middleware.GetWorkspaceID(r)

	id := mux.Vars(r)["id"]
	if id == "" {
		response.WriteError(w, http.StatusBadRequest, "Phone ID is required", nil)
		return
	}

	phone := h.verifyPhoneOwnership(w, claims, id, wsID)
	if phone == nil {
		return
	}

	var req ReleasePhoneRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}
	if strings.TrimSpace(req.ConfirmPhoneNumber) == "" {
		response.WriteError(w, http.StatusBadRequest, "Type the phone number to confirm permanent removal", nil)
		return
	}

	result, err := h.releasePhoneUseCase.Execute(businessphonedomain.ReleasePhoneInput{
		PhoneID:            id,
		AccessToken:        phone.AccessToken,
		ConfirmPhoneNumber: req.ConfirmPhoneNumber,
	})
	if err != nil {
		handleBusinessPhoneError(w, err, "Failed to remove phone number")
		return
	}

	log.Printf("[business-phone] removed (released): phone=%s waba=%s workspace=%s user=%s",
		phone.ID, phone.WABAId, wsID, claims.UserID)
	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"message": "Phone number removed. The previous owner can reclaim it via WhatsApp Manager or migrate it to another provider.",
		"result":  result,
	})
}

func handleBusinessPhoneError(w http.ResponseWriter, err error, defaultMsg string) {
	switch {
	case errors.Is(err, businessphonedomain.ErrPhoneNumberNotFound):
		response.WriteError(w, http.StatusNotFound, "WhatsApp Business phone number not found", nil)
	case errors.Is(err, businessphonedomain.ErrPhoneNumberAlreadyExists):
		response.WriteError(w, http.StatusConflict, "WhatsApp Business phone number already exists", nil)
	case errors.Is(err, businessphonedomain.ErrInvalidPhoneNumber):
		response.WriteError(w, http.StatusBadRequest, "Invalid phone number", nil)
	case errors.Is(err, businessphonedomain.ErrInvalidWABAId):
		response.WriteError(w, http.StatusBadRequest, "Invalid WABA ID", nil)
	case errors.Is(err, businessphonedomain.ErrInvalidAccessToken):
		response.WriteError(w, http.StatusUnauthorized, "Invalid access token", nil)
	case errors.Is(err, businessphonedomain.ErrInvalidVerificationCode):
		response.WriteError(w, http.StatusBadRequest, "Invalid verification code", nil)
	case errors.Is(err, businessphonedomain.ErrVerificationFailed):
		response.WriteError(w, http.StatusUnprocessableEntity, "Verification failed", nil)
	case errors.Is(err, businessphonedomain.ErrVerificationCodeExpired):
		response.WriteError(w, http.StatusGone, "Verification code expired", nil)
	case errors.Is(err, businessphonedomain.ErrTooManyVerificationAttempts):
		response.WriteError(w, http.StatusTooManyRequests, "Too many verification attempts", nil)
	case errors.Is(err, businessphonedomain.ErrPhoneNumberNotConnected):
		response.WriteError(w, http.StatusBadRequest, "Phone number not connected", nil)
	case errors.Is(err, businessphonedomain.ErrPhoneNumberStillConnected):
		response.WriteError(w, http.StatusConflict, err.Error(), nil)
	case errors.Is(err, businessphonedomain.ErrPhoneConfirmationMismatch):
		response.WriteError(w, http.StatusBadRequest, "The confirmation does not match this phone number", nil)
	case errors.Is(err, businessphonedomain.ErrProfileUpdateFailed):
		response.WriteError(w, http.StatusUnprocessableEntity, "Failed to update business profile", nil)
	case errors.Is(err, businessphonedomain.ErrInvalidBusinessVertical):
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
	case errors.Is(err, businessphonedomain.ErrProfilePictureUploadFailed):
		response.WriteError(w, http.StatusUnprocessableEntity, "Failed to upload profile picture", nil)
	case errors.Is(err, businessphonedomain.ErrMetaAPIError):
		lowerErr := strings.ToLower(err.Error())
		status := http.StatusBadGateway
		if strings.Contains(lowerErr, "temporarily unavailable") || strings.Contains(lowerErr, "transient: true") {
			status = http.StatusServiceUnavailable
		}
		response.WriteError(w, status, "Meta API error: "+err.Error(), nil)
	case errors.Is(err, businessphonedomain.ErrBusinessProfileUpdateFailed):
		response.WriteError(w, http.StatusInternalServerError, err.Error(), nil)
	default:
		response.WriteError(w, http.StatusInternalServerError, defaultMsg+": "+err.Error(), nil)
	}
}

// @Summary		Listar segmentos de negócio
// @Description	Retorna a lista de segmentos de negócio (verticais) aceitos pela Meta para o perfil comercial do telefone.
// @Tags			Telefones do WhatsApp Business
// @Produce		json
// @Success		200	{array}		VerticalOption
// @Security		BearerAuth
// @Router			/whatsapp/business-phones/verticals [get]
func (h *WhatsAppBusinessPhoneHandler) ListVerticals(w http.ResponseWriter, _ *http.Request) {
	verticals := []map[string]string{
		{"value": "ALCOHOL", "label": "Alcoholic Beverages"},
		{"value": "APPAREL", "label": "Clothing and Apparel"},
		{"value": "AUTO", "label": "Automotive"},
		{"value": "BEAUTY", "label": "Beauty, Spa and Salon"},
		{"value": "EDU", "label": "Education"},
		{"value": "ENTERTAIN", "label": "Entertainment"},
		{"value": "EVENT_PLAN", "label": "Event Planning and Service"},
		{"value": "FINANCE", "label": "Finance and Banking"},
		{"value": "GOVT", "label": "Public Service"},
		{"value": "GROCERY", "label": "Food and Grocery"},
		{"value": "HEALTH", "label": "Medical and Health"},
		{"value": "HOTEL", "label": "Hotel and Lodging"},
		{"value": "MATRIMONY_SERVICE", "label": "Matrimony Service"},
		{"value": "NONPROFIT", "label": "Non-profit"},
		{"value": "ONLINE_GAMBLING", "label": "Online Gambling & Gaming"},
		{"value": "OTC_DRUGS", "label": "Over-the-Counter Drugs"},
		{"value": "OTHER", "label": "Other"},
		{"value": "PHYSICAL_GAMBLING", "label": "Non-Online Gambling & Gaming"},
		{"value": "PROF_SERVICES", "label": "Professional Services"},
		{"value": "RESTAURANT", "label": "Restaurant"},
		{"value": "RETAIL", "label": "Shopping and Retail"},
		{"value": "TRAVEL", "label": "Travel and Transportation"},
	}
	response.WriteSuccess(w, http.StatusOK, verticals)
}

func (h *WhatsAppBusinessPhoneHandler) AssignOwner(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	id := mux.Vars(r)["id"]
	if id == "" {
		response.WriteError(w, http.StatusBadRequest, "Phone ID is required", nil)
		return
	}

	var req AssignOwnerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}

	if strings.TrimSpace(req.WorkspaceID) == "" {
		response.WriteError(w, http.StatusBadRequest, "workspaceId is required", nil)
		return
	}

	phone, err := h.phoneRepo.FindByID(id)
	if err != nil {
		if errors.Is(err, businessphonedomain.ErrPhoneNumberNotFound) {
			response.WriteError(w, http.StatusNotFound, "WhatsApp Business phone number not found", nil)
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "Failed to find phone number", nil)
		return
	}

	if err := phone.AssignOwner(req.WorkspaceID, claims.UserID, time.Now()); err != nil {
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
		return
	}

	if err := h.phoneRepo.Update(id, phone); err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to assign owner", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, phone)
}

func (h *WhatsAppBusinessPhoneHandler) UnassignOwner(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	id := mux.Vars(r)["id"]
	if id == "" {
		response.WriteError(w, http.StatusBadRequest, "Phone ID is required", nil)
		return
	}

	if err := h.unassignOwnerUseCase.Execute(id); err != nil {
		handleBusinessPhoneError(w, err, "Failed to unassign phone owner")
		return
	}

	log.Printf("[business-phone] owner unassigned: phone=%s by=%s", id, claims.UserID)
	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"message": "Phone number returned to the pool. It can now be assigned to another workspace.",
	})
}

func (h *WhatsAppBusinessPhoneHandler) verifyPhoneOwnership(w http.ResponseWriter, claims *auth.Claims, phoneID, wsID string) *businessphonedomain.WhatsAppBusinessPhoneNumber {
	phone, err := h.phoneRepo.FindByID(phoneID)
	if err != nil {
		if errors.Is(err, businessphonedomain.ErrPhoneNumberNotFound) {
			response.WriteError(w, http.StatusNotFound, "WhatsApp Business phone number not found", nil)
			return nil
		}
		response.WriteError(w, http.StatusInternalServerError, "Failed to look up phone number", nil)
		return nil
	}

	isAdmin := claims != nil && claims.Role == string(user.RoleAdmin)

	if !phone.BelongsToWorkspace(wsID) && !isAdmin {
		response.WriteError(w, http.StatusForbidden, "This phone does not belong to your workspace", nil)
		return nil
	}
	if isAdmin && !phone.BelongsToWorkspace(wsID) {
		log.Printf("[business-phone] admin acting on non-owned phone: admin=%s phone=%s ownerWorkspace=%q activeWorkspace=%s",
			claims.UserID, phone.ID, phone.OwnerWorkspaceID, wsID)
	}

	return phone
}

// @Summary		Sincronizar telefone comercial do WhatsApp
// @Description	Sincroniza, com a Meta, os dados de um telefone comercial do WhatsApp Business pertencente ao workspace.
// @Tags			Telefones do WhatsApp Business
// @Produce		json
// @Param			id	path	string	true	"ID do telefone"
// @Success		200	{object}	MessageResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/whatsapp/business-phones/{id}/sync [post]
func (h *WhatsAppBusinessPhoneHandler) SyncOneForWorkspace(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	wsID := middleware.GetWorkspaceID(r)

	id := mux.Vars(r)["id"]
	if id == "" {
		response.WriteError(w, http.StatusBadRequest, "Phone ID is required", nil)
		return
	}

	phone := h.verifyPhoneOwnership(w, claims, id, wsID)
	if phone == nil {
		return
	}

	result, err := h.syncPhoneNumberUseCase.Execute(businessphonedomain.SyncPhoneNumberInput{
		PhoneID:     id,
		AccessToken: phone.AccessToken,
	})
	if err != nil {
		handleBusinessPhoneError(w, err, "Failed to sync WhatsApp Business phone number")
		return
	}

	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"message":     "WhatsApp Business phone number synchronized successfully",
		"phoneNumber": result,
	})
}

// @Summary		Consultar status de chamadas do telefone
// @Description	Retorna se as chamadas do WhatsApp estão habilitadas para um telefone comercial pertencente ao workspace.
// @Tags			Telefones do WhatsApp Business
// @Produce		json
// @Param			id	path	string	true	"ID do telefone"
// @Success		200	{object}	CallingStatusResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/whatsapp/business-phones/{id}/calling [get]
func (h *WhatsAppBusinessPhoneHandler) GetCallingStatusForWorkspace(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	wsID := middleware.GetWorkspaceID(r)
	id := mux.Vars(r)["id"]
	if id == "" {
		response.WriteError(w, http.StatusBadRequest, "Phone ID is required", nil)
		return
	}

	phone := h.verifyPhoneOwnership(w, claims, id, wsID)
	if phone == nil {
		return
	}

	enabled, err := h.readCallingStatus(r.Context(), phone)
	if err != nil {
		handleBusinessPhoneError(w, err, "Failed to read WhatsApp calling status")
		return
	}

	if phone.CallsEnabled != enabled {
		_ = h.phoneRepo.UpdateCallsEnabled(phone.ID, enabled)
	}

	response.WriteSuccess(w, http.StatusOK, map[string]any{"enabled": enabled})
}

func (h *WhatsAppBusinessPhoneHandler) readCallingStatus(ctx context.Context, phone *businessphonedomain.WhatsAppBusinessPhoneNumber) (bool, error) {
	if phone.Provider.IsDialog360() {
		calling, err := h.dialog360CallingClient(phone.ID)
		if err != nil {
			return false, err
		}
		return calling.GetCallingStatus(ctx)
	}
	return h.metaAPIService.GetCallingStatus(phone.MetaPhoneNumberID, phone.AccessToken)
}

func (h *WhatsAppBusinessPhoneHandler) writeCallingStatus(ctx context.Context, phone *businessphonedomain.WhatsAppBusinessPhoneNumber, enabled bool) error {
	if phone.Provider.IsDialog360() {
		calling, err := h.dialog360CallingClient(phone.ID)
		if err != nil {
			return err
		}
		return calling.SetCallingStatus(ctx, enabled)
	}
	return h.metaAPIService.SetCallingStatus(phone.MetaPhoneNumberID, enabled, phone.AccessToken)
}

func (h *WhatsAppBusinessPhoneHandler) dialog360CallingClient(phoneID string) (conversation.WhatsAppCallingClient, error) {
	if h.whatsappClientFactory == nil {
		return nil, businessphonedomain.ErrUnsupportedForProvider
	}
	client, err := h.whatsappClientFactory.ClientForPhone(phoneID)
	if err != nil {
		return nil, err
	}
	calling, ok := client.(conversation.WhatsAppCallingClient)
	if !ok {
		return nil, businessphonedomain.ErrUnsupportedForProvider
	}
	return calling, nil
}

// @Summary		Atualizar status de chamadas do telefone
// @Description	Habilita ou desabilita as chamadas do WhatsApp para um telefone comercial pertencente ao workspace.
// @Tags			Telefones do WhatsApp Business
// @Accept			json
// @Produce		json
// @Param			id		path		string					true	"ID do telefone"
// @Param			request	body		SetCallingStatusRequest	true	"Novo status de chamadas"
// @Success		200	{object}	CallingStatusResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/whatsapp/business-phones/{id}/calling [put]
func (h *WhatsAppBusinessPhoneHandler) SetCallingStatusForWorkspace(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	wsID := middleware.GetWorkspaceID(r)
	id := mux.Vars(r)["id"]
	if id == "" {
		response.WriteError(w, http.StatusBadRequest, "Phone ID is required", nil)
		return
	}

	var body SetCallingStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}

	phone := h.verifyPhoneOwnership(w, claims, id, wsID)
	if phone == nil {
		return
	}

	log.Printf("[business-phone] calling status change requested: phone=%s metaPhoneID=%s waba=%s workspace=%s user=%s enabled=%v",
		phone.ID, phone.MetaPhoneNumberID, phone.WABAId, wsID, claims.UserID, body.Enabled)

	if err := h.writeCallingStatus(r.Context(), phone, body.Enabled); err != nil {
		log.Printf("[business-phone] FAILED to set calling status: phone=%s provider=%s metaPhoneID=%s waba=%s workspace=%s user=%s enabled=%v: %v",
			phone.ID, phone.Provider, phone.MetaPhoneNumberID, phone.WABAId, wsID, claims.UserID, body.Enabled, err)
		handleBusinessPhoneError(w, err, "Failed to update WhatsApp calling status")
		return
	}

	if body.Enabled && phone.WABAId != "" && !phone.Provider.IsDialog360() {
		if err := h.metaAPIService.SubscribeWebhooks(phone.WABAId, phone.AccessToken); err != nil {
			log.Printf("[business-phone] calls webhook subscribe failed for waba %s: %v", phone.WABAId, err)
		}
	}

	if err := h.phoneRepo.UpdateCallsEnabled(phone.ID, body.Enabled); err != nil {
		log.Printf("[business-phone] failed to persist calls_enabled for phone %s: %v", phone.ID, err)
	}

	log.Printf("[business-phone] calling status updated: phone=%s metaPhoneID=%s workspace=%s user=%s enabled=%v",
		phone.ID, phone.MetaPhoneNumberID, wsID, claims.UserID, body.Enabled)

	response.WriteSuccess(w, http.StatusOK, map[string]any{"enabled": body.Enabled})
}

// @Summary		Solicitar código de verificação
// @Description	Solicita o envio de um código de verificação (SMS ou VOICE) para um telefone comercial pertencente ao workspace.
// @Tags			Telefones do WhatsApp Business
// @Accept			json
// @Produce		json
// @Param			id		path		string						true	"ID do telefone"
// @Param			request	body		RequestVerificationRequest	true	"Método e idioma da verificação"
// @Success		200	{object}	MessageResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/whatsapp/business-phones/{id}/request-verification [post]
func (h *WhatsAppBusinessPhoneHandler) RequestVerificationForWorkspace(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	wsID := middleware.GetWorkspaceID(r)

	id := mux.Vars(r)["id"]
	if id == "" {
		response.WriteError(w, http.StatusBadRequest, "Phone ID is required", nil)
		return
	}

	phone := h.verifyPhoneOwnership(w, claims, id, wsID)
	if phone == nil {
		return
	}

	var req RequestVerificationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}

	method := businessphonedomain.VerificationMethodSMS
	if req.Method == "VOICE" {
		method = businessphonedomain.VerificationMethodVoice
	}

	err := h.requestVerificationUseCase.Execute(businessphonedomain.RequestVerificationCodeInput{
		PhoneID:     id,
		Method:      method,
		Language:    req.Language,
		AccessToken: phone.AccessToken,
	})
	if err != nil {
		handleBusinessPhoneError(w, err, "Failed to request verification code")
		return
	}

	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"message": "Verification code sent successfully",
		"method":  method,
	})
}

// @Summary		Confirmar código de verificação
// @Description	Confirma o código de verificação recebido para um telefone comercial pertencente ao workspace.
// @Tags			Telefones do WhatsApp Business
// @Accept			json
// @Produce		json
// @Param			id		path		string				true	"ID do telefone"
// @Param			request	body		VerifyCodeRequest	true	"Código de verificação"
// @Success		200	{object}	MessageResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/whatsapp/business-phones/{id}/verify-code [post]
func (h *WhatsAppBusinessPhoneHandler) VerifyCodeForWorkspace(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	wsID := middleware.GetWorkspaceID(r)

	id := mux.Vars(r)["id"]
	if id == "" {
		response.WriteError(w, http.StatusBadRequest, "Phone ID is required", nil)
		return
	}

	phone := h.verifyPhoneOwnership(w, claims, id, wsID)
	if phone == nil {
		return
	}

	var req VerifyCodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}

	if req.Code == "" {
		response.WriteError(w, http.StatusBadRequest, "Verification code is required", nil)
		return
	}

	result, err := h.verifyCodeUseCase.Execute(businessphonedomain.VerifyCodeInput{
		PhoneID:     id,
		Code:        req.Code,
		AccessToken: phone.AccessToken,
	})
	if err != nil {
		handleBusinessPhoneError(w, err, "Failed to verify code")
		return
	}

	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"message":     "Phone number verified successfully",
		"phoneNumber": result,
	})
}

// @Summary		Atualizar perfil comercial do telefone
// @Description	Atualiza o perfil comercial (sobre, endereço, descrição, email, sites e segmento) de um telefone comercial pertencente ao workspace.
// @Tags			Telefones do WhatsApp Business
// @Accept			json
// @Produce		json
// @Param			id		path		string							true	"ID do telefone"
// @Param			request	body		UpdateBusinessProfileRequest	true	"Dados do perfil comercial"
// @Success		200	{object}	MessageResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/whatsapp/business-phones/{id}/business-profile [put]
func (h *WhatsAppBusinessPhoneHandler) UpdateBusinessProfileForWorkspace(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	wsID := middleware.GetWorkspaceID(r)

	id := mux.Vars(r)["id"]
	if id == "" {
		response.WriteError(w, http.StatusBadRequest, "Phone ID is required", nil)
		return
	}

	phone := h.verifyPhoneOwnership(w, claims, id, wsID)
	if phone == nil {
		return
	}

	var req UpdateBusinessProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}

	profile := businessphonedomain.BusinessProfile{
		About:                req.About,
		Address:              req.Address,
		Description:          req.Description,
		Email:                req.Email,
		ProfilePictureURL:    req.ProfilePictureURL,
		ProfilePictureHandle: req.ProfilePictureHandle,
		Websites:             req.Websites,
		Vertical:             businessphonedomain.BusinessVertical(req.Vertical),
	}

	result, err := h.updateBusinessProfileUseCase.Execute(businessphonedomain.UpdateBusinessProfileInput{
		PhoneID:     id,
		Profile:     profile,
		AccessToken: phone.AccessToken,
	})
	if err != nil {
		handleBusinessPhoneError(w, err, "Failed to update business profile")
		return
	}

	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"message":     "Business profile updated successfully",
		"phoneNumber": result,
	})
}

// @Summary		Consultar perfil comercial do telefone
// @Description	Retorna o perfil comercial de um telefone comercial do WhatsApp Business pertencente ao workspace.
// @Tags			Telefones do WhatsApp Business
// @Produce		json
// @Param			id	path	string	true	"ID do telefone"
// @Success		200	{object}	businessphone.BusinessProfile
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/whatsapp/business-phones/{id}/business-profile [get]
func (h *WhatsAppBusinessPhoneHandler) GetBusinessProfileForWorkspace(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	wsID := middleware.GetWorkspaceID(r)

	id := mux.Vars(r)["id"]
	if id == "" {
		response.WriteError(w, http.StatusBadRequest, "Phone ID is required", nil)
		return
	}

	phone := h.verifyPhoneOwnership(w, claims, id, wsID)
	if phone == nil {
		return
	}

	profile, err := h.getBusinessProfileUseCase.Execute(businessphonedomain.GetBusinessProfileInput{
		PhoneID:     id,
		AccessToken: phone.AccessToken,
	})
	if err != nil {
		handleBusinessPhoneError(w, err, "Failed to get business profile")
		return
	}

	response.WriteSuccess(w, http.StatusOK, profile)
}

// @Summary		Enviar foto do perfil comercial
// @Description	Faz o upload de uma imagem (JPEG ou PNG) para uso como foto do perfil comercial de um telefone pertencente ao workspace. Tamanho máximo de 10MB.
// @Tags			Telefones do WhatsApp Business
// @Accept			mpfd
// @Produce		json
// @Param			id		path		string	true	"ID do telefone"
// @Param			file	formData	file	true	"Arquivo de imagem (JPEG ou PNG)"
// @Success		200	{object}	MessageResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/whatsapp/business-phones/{id}/upload-profile-picture [post]
func (h *WhatsAppBusinessPhoneHandler) UploadProfilePictureForWorkspace(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	wsID := middleware.GetWorkspaceID(r)

	id := mux.Vars(r)["id"]
	if id == "" {
		response.WriteError(w, http.StatusBadRequest, "Phone ID is required", nil)
		return
	}

	phone := h.verifyPhoneOwnership(w, claims, id, wsID)
	if phone == nil {
		return
	}

	if err := r.ParseMultipartForm(10 << 20); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Failed to parse form data. Max file size is 10MB", nil)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, "File is required. Use form field 'file'", nil)
		return
	}
	defer file.Close()

	contentType := header.Header.Get("Content-Type")
	if contentType != "image/jpeg" && contentType != "image/png" && contentType != "image/jpg" {
		response.WriteError(w, http.StatusBadRequest, "Invalid file type. Only JPEG and PNG images are supported", nil)
		return
	}

	data, err := io.ReadAll(file)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to read file", nil)
		return
	}

	handle, err := h.metaAPIService.UploadProfilePicture(data, header.Filename, phone.AccessToken)
	if err != nil {
		handleBusinessPhoneError(w, err, "Failed to upload profile picture")
		return
	}

	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"message":              "Profile picture uploaded successfully",
		"profilePictureHandle": handle,
	})
}
