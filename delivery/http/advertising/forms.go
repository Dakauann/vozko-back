package advertisinghttp

import (
	"net/http"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	"vozko/domain/advertising"
	adsuc "vozko/usecases/advertising"
)

type FormLeadResponse struct {
	advertising.FormLead
	LeadName string `json:"leadName,omitempty"`
}

type FormLeadsResponse struct {
	Items []FormLeadResponse `json:"items"`
	Total int64              `json:"total"`
}

type ImportedResponse struct {
	Imported int `json:"imported"`
}

// @Summary		Formulários instantâneos da página
// @Description	Formulários de cadastro da página. Listar também passa a acompanhar os leads de cada formulário.
// @Tags			Anúncios
// @Produce		json
// @Param			id		path		string	true	"ID da conta de anúncios"
// @Param			pageId	path		string	true	"ID da página"
// @Success		200		{array}		advertising.LeadForm
// @Failure		422		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/accounts/{id}/pages/{pageId}/forms [get]
func (h *Handler) Forms(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	forms, err := h.d.Forms.List(r.Context(), workspaceOf(r), vars["id"], vars["pageId"])
	if err != nil {
		writeError(w, err, "Failed to list lead forms")
		return
	}
	response.WriteSuccess(w, http.StatusOK, nonNil(forms))
}

// @Summary		Criar formulário instantâneo
// @Description	Cria o formulário na página e passa a importar os leads para o CRM.
// @Tags			Anúncios
// @Accept			json
// @Produce		json
// @Param			body	body		advertising.LeadFormDraft	true	"formulário"
// @Success		201		{object}	advertising.LeadForm
// @Failure		422		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/forms [post]
func (h *Handler) CreateForm(w http.ResponseWriter, r *http.Request) {
	var draft advertising.LeadFormDraft
	if !decodeJSON(w, r, &draft) {
		return
	}
	form, err := h.d.Forms.Create(r.Context(), workspaceOf(r), draft)
	if err != nil {
		writeError(w, err, "Failed to create the lead form")
		return
	}
	response.WriteSuccess(w, http.StatusCreated, form)
}

// @Summary		Arquivar formulário instantâneo
// @Description	A conta de anúncios vem do formulário acompanhado pelo workspace.
// @Tags			Anúncios
// @Param			formId	path	string	true	"ID do formulário na Meta"
// @Success		204
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/forms/{formId}/archive [post]
func (h *Handler) ArchiveForm(w http.ResponseWriter, r *http.Request) {
	if err := h.d.Forms.Archive(r.Context(), workspaceOf(r), mux.Vars(r)["formId"]); err != nil {
		writeError(w, err, "Failed to archive the lead form")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// @Summary		Leads do formulário
// @Description	Leads importados do formulário, mais recentes primeiro, com o nome do lead no CRM.
// @Tags			Anúncios
// @Produce		json
// @Param			formId	path		string	true	"ID do formulário na Meta"
// @Param			limit	query		int		false	"itens por página (1 a 200, padrão 50)"
// @Param			offset	query		int		false	"itens a pular"
// @Success		200		{object}	FormLeadsResponse
// @Failure		404		{object}	response.ErrorResponse
// @Failure		422		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/forms/{formId}/leads [get]
func (h *Handler) FormLeads(w http.ResponseWriter, r *http.Request) {
	limit, err := intQuery(r, "limit")
	if err != nil {
		writeError(w, err, "Failed to list form leads")
		return
	}
	offset, err := intQuery(r, "offset")
	if err != nil {
		writeError(w, err, "Failed to list form leads")
		return
	}
	leads, total, err := h.d.Forms.Leads(r.Context(), workspaceOf(r), mux.Vars(r)["formId"], limit, offset)
	if err != nil {
		writeError(w, err, "Failed to list form leads")
		return
	}
	response.WriteSuccess(w, http.StatusOK, FormLeadsResponse{
		Items: presentAll(leads, func(l adsuc.FormLeadView) FormLeadResponse {
			return FormLeadResponse{FormLead: *l.Lead, LeadName: l.LeadName}
		}),
		Total: total,
	})
}

// @Summary		Importar leads do formulário agora
// @Description	Busca na Meta os leads novos do formulário e cria os contatos no CRM. A conta de anúncios vem do formulário acompanhado pelo workspace.
// @Tags			Anúncios
// @Produce		json
// @Param			formId	path		string	true	"ID do formulário na Meta"
// @Success		200		{object}	ImportedResponse
// @Failure		404		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/forms/{formId}/sync [post]
func (h *Handler) SyncForm(w http.ResponseWriter, r *http.Request) {
	imported, err := h.d.Forms.Sync(r.Context(), workspaceOf(r), mux.Vars(r)["formId"])
	if err != nil {
		writeError(w, err, "Failed to import form leads")
		return
	}
	response.WriteSuccess(w, http.StatusOK, ImportedResponse{Imported: imported})
}
