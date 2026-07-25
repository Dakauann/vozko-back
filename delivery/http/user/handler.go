package user

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/domain/customer"
	"vozko/domain/shared"
	userdomain "vozko/domain/user"
	workspaceplan_usecase "vozko/domain/workspace/workspace_plan"
	"vozko/infra/http/middleware"
)

type UserHandler struct {
	listUseCase       userdomain.ListUsersUseCase
	updateRoleUseCase userdomain.UpdateUserRoleUseCase
	getUseCase        userdomain.FindUserByIDUseCase
	updateUseCase     userdomain.UpdateUserUseCase
	deleteUseCase     userdomain.DeleteUserUseCase
	getSubscription   workspaceplan_usecase.GetWorkspaceSubscriptionUseCase
	documentValidator customer.DocumentValidator
}

func NewUserHandler(
	listUC userdomain.ListUsersUseCase,
	updateRoleUC userdomain.UpdateUserRoleUseCase,
	getUC userdomain.FindUserByIDUseCase,
	updateUC userdomain.UpdateUserUseCase,
	deleteUC userdomain.DeleteUserUseCase,
	getSubscription workspaceplan_usecase.GetWorkspaceSubscriptionUseCase,
	documentValidator customer.DocumentValidator,
) *UserHandler {
	return &UserHandler{
		listUseCase:       listUC,
		updateRoleUseCase: updateRoleUC,
		getUseCase:        getUC,
		updateUseCase:     updateUC,
		deleteUseCase:     deleteUC,
		getSubscription:   getSubscription,
		documentValidator: documentValidator,
	}
}

// @Summary		Consultar o usuário autenticado
// @Description	Retorna os dados do usuário autenticado, incluindo perfil, papel, tipo de cliente e o plano do workspace atual. O email é retornado parcialmente ofuscado.
// @Tags			Usuário
// @Produce		json
// @Success		200	{object}	UserInfoResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/user/me [get]
func (h *UserHandler) GetMe(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	authenticatedUser, err := h.getUseCase.Execute(claims.UserID)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to fetch user", nil)
		return
	}

	obfuscatedEmail := obfuscateEmail(authenticatedUser.Email)

	var planResp *PlanResponse
	workspaceID := middleware.GetWorkspaceID(r)
	if h.getSubscription != nil && workspaceID != "" {
		subscription, err := h.getSubscription.Execute(workspaceID)
		if err != nil {
			log.Printf("user handler: failed to fetch workspace subscription for %s: %v", workspaceID, err)
		}
		if err == nil && subscription != nil && subscription.Plan != nil {
			planResp = &PlanResponse{
				Name:               subscription.Plan.Name,
				MaxCallChannels:    subscription.Plan.MaxCallChannels,
				MaxBranches:        subscription.Plan.MaxBranches,
				MaxHoldMusicTracks: subscription.Plan.MaxHoldMusicTracks,
			}
		}
	}

	maskedDoc, hasDoc := maskUserDocument(authenticatedUser)
	userInfo := UserInfoResponse{
		ID:            authenticatedUser.ID,
		Username:      authenticatedUser.Username,
		Email:         obfuscatedEmail,
		Picture:       authenticatedUser.Picture,
		Role:          string(authenticatedUser.Role),
		CustomerType:  string(authenticatedUser.CustomerType),
		EmailVerified: authenticatedUser.EmailVerified,
		HasDocument:   hasDoc,
		Document:      maskedDoc,
		CreatedAt:     authenticatedUser.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:     authenticatedUser.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
		Plan:          planResp,
	}

	response.WriteSuccess(w, http.StatusOK, userInfo)
}

func (h *UserHandler) List(w http.ResponseWriter, r *http.Request) {
	values := r.URL.Query()

	input := userdomain.ListUsersInput{
		Search: strings.TrimSpace(values.Get("search")),
		QueryOptions: shared.QueryOptions{
			Pagination: httpx.ParsePagination(values),
		},
	}

	if roleStr := strings.TrimSpace(values.Get("role")); roleStr != "" {
		role := userdomain.Role(roleStr)
		if role != userdomain.RoleAdmin && role != userdomain.RoleUser {
			response.WriteValidationError(w, map[string]string{"role": "must be 'admin' or 'user'"})
			return
		}
		input.Role = &role
	}

	result, err := h.listUseCase.Execute(input)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to list users", nil)
		return
	}

	items := make([]UserListItemResponse, len(result.Items))
	for i, u := range result.Items {
		items[i] = UserListItemResponse{
			ID:            u.ID,
			Username:      u.Username,
			Email:         u.Email,
			Picture:       u.Picture,
			Role:          string(u.Role),
			CustomerType:  string(u.CustomerType),
			EmailVerified: u.EmailVerified,
			CreatedAt:     u.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
			UpdatedAt:     u.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
		}
	}

	response.WriteSuccess(w, http.StatusOK, shared.PaginatedResult[UserListItemResponse]{
		Items:      items,
		Page:       result.Page,
		PageSize:   result.PageSize,
		TotalItems: result.TotalItems,
		TotalPages: result.TotalPages,
	})
}

func (h *UserHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	u, err := h.getUseCase.Execute(id)
	if err != nil {
		if errors.Is(err, userdomain.ErrNotFound) {
			response.WriteError(w, http.StatusNotFound, "User not found", nil)
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "Failed to get user", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, UserDetailResponse{
		ID:            u.ID,
		Username:      u.Username,
		Email:         u.Email,
		Picture:       u.Picture,
		Role:          string(u.Role),
		CustomerType:  string(u.CustomerType),
		CPF:           u.CPF,
		CNPJ:          u.CNPJ,
		EmailVerified: u.EmailVerified,
		CreatedAt:     u.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:     u.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	})
}

func (h *UserHandler) UpdateRole(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	targetUserID := mux.Vars(r)["id"]

	var req updateRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}

	if errs := req.Validate(); errs != nil {
		response.WriteValidationError(w, errs)
		return
	}

	newRole := userdomain.Role(req.Role)

	err := h.updateRoleUseCase.Execute(claims.UserID, targetUserID, newRole)
	if err != nil {
		switch {
		case errors.Is(err, userdomain.ErrNotFound):
			response.WriteError(w, http.StatusNotFound, "User not found", nil)
		case errors.Is(err, userdomain.ErrCannotModifySelf):
			response.WriteError(w, http.StatusForbidden, "Cannot modify your own role", nil)
		case errors.Is(err, userdomain.ErrLastAdmin):
			response.WriteError(w, http.StatusConflict, "Cannot remove role from the last admin", nil)
		case errors.Is(err, userdomain.ErrInvalidRole):
			response.WriteValidationError(w, map[string]string{"role": "invalid role"})
		default:
			response.WriteError(w, http.StatusInternalServerError, "Failed to update user role", nil)
		}
		return
	}

	u, err := h.getUseCase.Execute(targetUserID)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to get updated user", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, UserListItemResponse{
		ID:            u.ID,
		Username:      u.Username,
		Email:         u.Email,
		Picture:       u.Picture,
		Role:          string(u.Role),
		CustomerType:  string(u.CustomerType),
		EmailVerified: u.EmailVerified,
		CreatedAt:     u.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:     u.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	})
}

func obfuscateEmail(email string) string {
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return "****@****"
	}

	localPart := parts[0]
	domain := parts[1]

	if len(localPart) == 0 {
		return "****@" + domain
	}

	visibleChars := len(localPart)
	if visibleChars > 5 {
		visibleChars = 5
	} else if visibleChars > 1 {
		visibleChars = len(localPart) / 2
		if visibleChars < 1 {
			visibleChars = 1
		}
	}

	return localPart[:visibleChars] + "****@" + domain
}

// @Summary		Atualizar o perfil do usuário autenticado
// @Description	Atualiza o nome de usuário e/ou a foto do usuário autenticado. Campos vazios ou ausentes são ignorados.
// @Tags			Usuário
// @Accept			json
// @Produce		json
// @Param			request	body		UpdateProfileRequest	true	"Dados do perfil a atualizar"
// @Success		200	{object}	UserInfoResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/user/me [patch]
func (h *UserHandler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	var req UpdateProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}

	u, err := h.getUseCase.Execute(claims.UserID)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to fetch user", nil)
		return
	}

	if strings.TrimSpace(req.Username) != "" {
		u.Username = strings.TrimSpace(req.Username)
	}
	if req.Picture != "" {
		u.Picture = req.Picture
	}

	if strings.TrimSpace(req.Document) != "" {
		if err := h.applyDocument(u, req.Document); err != nil {
			switch {
			case errors.Is(err, errDocumentInvalid):
				response.WriteErrorWithCode(w, http.StatusUnprocessableEntity, "invalid_document", "CPF/CNPJ inválido. Verifique os números informados.", nil)
			case errors.Is(err, errDocumentImmutable):
				response.WriteErrorWithCode(w, http.StatusConflict, "document_already_set", "Já existe um CPF/CNPJ cadastrado. Entre em contato com o suporte para alterá-lo.", nil)
			default:
				response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
			}
			return
		}
	}

	if err := h.updateUseCase.Execute(u.ID, u); err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to update profile", nil)
		return
	}

	masked, has := maskUserDocument(u)
	response.WriteSuccess(w, http.StatusOK, UserInfoResponse{
		ID:            u.ID,
		Username:      u.Username,
		Email:         obfuscateEmail(u.Email),
		Picture:       u.Picture,
		Role:          string(u.Role),
		CustomerType:  string(u.CustomerType),
		EmailVerified: u.EmailVerified,
		HasDocument:   has,
		Document:      masked,
		CreatedAt:     u.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:     u.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	})
}

var (
	errDocumentInvalid   = errors.New("invalid document")
	errDocumentImmutable = errors.New("document already set")
)

// applyDocument validates and sets the user's CPF/CNPJ. It is set-once: once a document exists it
// cannot be changed here (an Asaas customer is keyed to it), though re-submitting the same value is a
// no-op. An 11-digit document is stored as CPF (individual), 14 digits as CNPJ (company).
func (h *UserHandler) applyDocument(u *userdomain.User, raw string) error {
	if h.documentValidator == nil || !h.documentValidator.ValidateCPFOrCNPJ(raw) {
		return errDocumentInvalid
	}
	normalized := h.documentValidator.Normalize(raw)

	existing := strings.TrimSpace(u.CPF)
	if existing == "" {
		existing = strings.TrimSpace(u.CNPJ)
	}
	if existing != "" {
		if existing == normalized {
			return nil // idempotent: same document re-submitted
		}
		return errDocumentImmutable
	}

	switch len(normalized) {
	case 11:
		u.CPF = normalized
		u.CNPJ = ""
		u.CustomerType = userdomain.CustomerTypeIndividual
	case 14:
		u.CNPJ = normalized
		u.CPF = ""
		u.CustomerType = userdomain.CustomerTypeCompany
	default:
		return errDocumentInvalid
	}
	return nil
}

// maskUserDocument returns the user's CPF/CNPJ with all but the last two digits masked, and whether a
// document is set. Example: "12345678909" -> "*********09".
func maskUserDocument(u *userdomain.User) (masked string, has bool) {
	doc := strings.TrimSpace(u.CPF)
	if doc == "" {
		doc = strings.TrimSpace(u.CNPJ)
	}
	if doc == "" {
		return "", false
	}
	if len(doc) <= 2 {
		return doc, true
	}
	return strings.Repeat("*", len(doc)-2) + doc[len(doc)-2:], true
}

// @Summary		Excluir a própria conta
// @Description	Exclui permanentemente a conta do usuário autenticado após validar a senha atual. Atende à diretriz 5.1.1(v) da App Store (exclusão de conta no aplicativo).
// @Tags			Usuário
// @Accept			json
// @Produce		json
// @Param			request	body		DeleteMeRequest	true	"Senha atual para confirmar a exclusão"
// @Success		200	{object}	MessageResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/user/me [delete]
func (h *UserHandler) DeleteMe(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	var req DeleteMeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{"currentPassword": "string (required)"})
		return
	}
	if errs := req.Validate(); errs != nil {
		response.WriteValidationError(w, errs)
		return
	}

	err := h.deleteUseCase.Execute(claims.UserID, req.CurrentPassword)
	if err != nil {
		switch {
		case errors.Is(err, userdomain.ErrInvalidPassword):
			response.WriteError(w, http.StatusUnauthorized, "Current password is incorrect", nil)
		case errors.Is(err, userdomain.ErrNotFound):
			response.WriteSuccess(w, http.StatusOK, MessageResponse{Message: "Account deleted"})
		default:
			response.WriteError(w, http.StatusInternalServerError, "Failed to delete account", nil)
		}
		return
	}

	response.WriteSuccess(w, http.StatusOK, MessageResponse{Message: "Account deleted successfully"})
}
