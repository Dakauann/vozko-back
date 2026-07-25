package ticket

import (
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/domain/shared"
	ticketdomain "vozko/domain/ticket"
	"vozko/infra/http/middleware"
)

type TicketHandler struct {
	listUserTickets ticketdomain.ListUserTicketsUseCase
	getByOrder      ticketdomain.GetTicketByOrderUseCase
	uploadDocument  ticketdomain.UploadTicketDocumentUseCase
	listTickets     ticketdomain.ListTicketsUseCase
	updateStatus    ticketdomain.UpdateTicketStatusUseCase
	generateLabel   ticketdomain.GenerateLabelUseCase
}

func NewTicketHandler(
	listUserTickets ticketdomain.ListUserTicketsUseCase,
	getByOrder ticketdomain.GetTicketByOrderUseCase,
	uploadDocument ticketdomain.UploadTicketDocumentUseCase,
	listTickets ticketdomain.ListTicketsUseCase,
	updateStatus ticketdomain.UpdateTicketStatusUseCase,
	generateLabel ticketdomain.GenerateLabelUseCase,
) *TicketHandler {
	return &TicketHandler{
		listUserTickets: listUserTickets,
		getByOrder:      getByOrder,
		uploadDocument:  uploadDocument,
		listTickets:     listTickets,
		updateStatus:    updateStatus,
		generateLabel:   generateLabel,
	}
}

// @Summary		Listar meus tickets
// @Description	Retorna a lista paginada dos tickets de envio do usuário autenticado, com filtros por status, busca textual e período.
// @Tags			Tickets
// @Produce		json
// @Param			page		query		int		false	"Número da página (inicia em 1)"
// @Param			pageSize	query		int		false	"Quantidade de itens por página"
// @Param			status		query		string	false	"Filtrar por status (aceita vários separados por vírgula)"
// @Param			search		query		string	false	"Busca textual"
// @Param			sort		query		string	false	"Ordenação (ex.: createdAt:desc, status:asc)"
// @Success		200	{array}		ticket.Ticket
// @Failure		401	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/tickets [get]
func (h *TicketHandler) ListUserTickets(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	query := r.URL.Query()
	options := shared.QueryOptions{
		Pagination: httpx.ParsePagination(query),
		Sorts:      parseSort(query, map[string]string{"createdat": "createdAt", "updatedat": "updatedAt", "status": "status", "laststatusat": "lastStatusAt"}),
		Filters:    parseTicketFilters(query),
	}

	result, err := h.listUserTickets.Execute(ticketdomain.ListUserTicketsInput{
		UserID:  claims.UserID,
		Options: options,
	})
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to load tickets", nil)
		return
	}

	response.WritePaginated(w, http.StatusOK, result.Items, response.PaginationMeta{
		Page:       result.Page,
		PageSize:   result.PageSize,
		TotalPages: result.TotalPages,
		TotalItems: result.TotalItems,
	})
}

// @Summary		Obter ticket de um pedido
// @Description	Retorna o ticket de envio associado a um pedido do usuário autenticado.
// @Tags			Tickets
// @Produce		json
// @Param			orderId	path		string	true	"ID do pedido"
// @Success		200	{object}	ticket.Ticket
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/orders/{orderId}/ticket [get]
func (h *TicketHandler) GetByOrder(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	vars := mux.Vars(r)
	orderID := vars["orderId"]
	if orderID == "" {
		response.WriteValidationError(w, map[string]string{"orderId": "required"})
		return
	}

	tick, err := h.getByOrder.Execute(claims.UserID, orderID)
	if err != nil {
		switch err {
		case ticketdomain.ErrTicketNotFound:
			response.WriteError(w, http.StatusNotFound, "Ticket not found", nil)
		case ticketdomain.ErrTicketUnauthorized:
			response.WriteError(w, http.StatusForbidden, "Forbidden", nil)
		default:
			response.WriteError(w, http.StatusInternalServerError, "Failed to load ticket", nil)
		}
		return
	}

	response.WriteSuccess(w, http.StatusOK, tick)
}

// @Summary		Enviar documento do ticket
// @Description	Faz o upload de um documento (RG, comprovante de endereço ou receita médica) para um ticket de envio do usuário autenticado.
// @Tags			Tickets
// @Accept			multipart/form-data
// @Produce		json
// @Param			ticketId		path		string	true	"ID do ticket"
// @Param			documentType	formData	string	true	"Tipo do documento (rg, address_proof, medical_prescription)"
// @Param			file			formData	file	true	"Arquivo do documento"
// @Success		201	{object}	ticket.Document
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/tickets/{ticketId}/documents [post]
func (h *TicketHandler) UploadDocument(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	vars := mux.Vars(r)
	ticketID := vars["ticketId"]
	if ticketID == "" {
		response.WriteValidationError(w, map[string]string{"ticketId": "required"})
		return
	}

	if err := r.ParseMultipartForm(10 << 20); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid multipart form", nil)
		return
	}

	documentType := r.FormValue("documentType")
	if documentType == "" {
		documentType = r.FormValue("type")
	}
	if documentType == "" {
		response.WriteValidationError(w, map[string]string{"documentType": "required"})
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		if err == http.ErrMissingFile {
			response.WriteValidationError(w, map[string]string{"file": "required"})
			return
		}
		response.WriteError(w, http.StatusBadRequest, "Failed to read file", nil)
		return
	}
	defer file.Close()

	content, err := readAll(file)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to process file", nil)
		return
	}

	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = http.DetectContentType(content)
	}

	doc, err := h.uploadDocument.Execute(claims.UserID, ticketID, documentType, header.Filename, contentType, content)
	if err != nil {
		switch err {
		case ticketdomain.ErrDocumentTypeRequired:
			response.WriteValidationError(w, map[string]string{"documentType": "required"})
		case ticketdomain.ErrInvalidDocumentType:
			response.WriteValidationError(w, map[string]string{"documentType": "invalid type"})
		case ticketdomain.ErrTicketNotFound:
			response.WriteError(w, http.StatusNotFound, "Ticket not found", nil)
			return
		case ticketdomain.ErrTicketUnauthorized:
			response.WriteError(w, http.StatusForbidden, "Forbidden", nil)
		case ticketdomain.ErrDocumentUnsupportedFormat:
			response.WriteValidationError(w, map[string]string{"file": "unsupported file format"})
		case ticketdomain.ErrDocumentUploadRequired:
			response.WriteValidationError(w, map[string]string{"file": "required"})
		default:
			response.WriteError(w, http.StatusInternalServerError, "Failed to upload document", nil)
		}
		return
	}

	response.WriteSuccess(w, http.StatusCreated, doc)
}

func (h *TicketHandler) ListAll(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	options := shared.QueryOptions{
		Pagination: httpx.ParsePagination(query),
		Sorts:      parseSort(query, map[string]string{"createdat": "createdAt", "updatedat": "updatedAt", "status": "status", "laststatusat": "lastStatusAt"}),
		Filters:    parseTicketFilters(query),
	}

	result, err := h.listTickets.Execute(ticketdomain.ListTicketsInput{Options: options})
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to list tickets", nil)
		return
	}

	response.WritePaginated(w, http.StatusOK, result.Items, response.PaginationMeta{
		Page:       result.Page,
		PageSize:   result.PageSize,
		TotalPages: result.TotalPages,
		TotalItems: result.TotalItems,
	})
}

func (h *TicketHandler) UpdateStatus(w http.ResponseWriter, r *http.Request) {
	var req UpdateStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{
			"status":       "string (required)",
			"trackingCode": "string (optional)",
		})
		return
	}

	vars := mux.Vars(r)
	ticketID := vars["ticketId"]
	if ticketID == "" {
		response.WriteValidationError(w, map[string]string{"ticketId": "required"})
		return
	}

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	status, err := ticketdomain.ValidateStatus(req.Status)
	if err != nil {
		response.WriteValidationError(w, map[string]string{"status": "invalid or unsupported status"})
		return
	}

	updated, err := h.updateStatus.Execute(ticketID, claims.UserID, status, req.TrackingCode)
	if err != nil {
		switch err {
		case ticketdomain.ErrTicketNotFound:
			response.WriteError(w, http.StatusNotFound, "Ticket not found", nil)
		case ticketdomain.ErrInvalidStatusTransition:
			response.WriteValidationError(w, map[string]string{"status": "invalid transition"})
		case ticketdomain.ErrTrackingCodeRequired:
			response.WriteValidationError(w, map[string]string{"trackingCode": "required for this status"})
		default:
			response.WriteError(w, http.StatusInternalServerError, "Failed to update ticket status", nil)
		}
		return
	}

	response.WriteSuccess(w, http.StatusOK, updated)
}

func (h *TicketHandler) GenerateLabel(w http.ResponseWriter, r *http.Request) {
	var req GenerateLabelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{
			"trackingCode": "string (required)",
		})
		return
	}

	if errs := req.Validate(); errs != nil {
		response.WriteValidationError(w, errs)
		return
	}

	vars := mux.Vars(r)
	ticketID := vars["ticketId"]
	if ticketID == "" {
		response.WriteValidationError(w, map[string]string{"ticketId": "required"})
		return
	}

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	label, err := h.generateLabel.Execute(ticketID, claims.UserID, req.TrackingCode)
	if err != nil {
		switch err {
		case ticketdomain.ErrTicketNotFound:
			response.WriteError(w, http.StatusNotFound, "Ticket not found", nil)
		case ticketdomain.ErrInvalidStatusTransition:
			response.WriteValidationError(w, map[string]string{"status": "ticket not ready for label"})
		case ticketdomain.ErrTrackingCodeRequired:
			response.WriteValidationError(w, map[string]string{"trackingCode": "required"})
		default:
			response.WriteError(w, http.StatusInternalServerError, "Failed to generate label", nil)
		}
		return
	}

	response.WriteSuccess(w, http.StatusOK, label)
}

func readAll(file multipart.File) ([]byte, error) {
	return io.ReadAll(file)
}
