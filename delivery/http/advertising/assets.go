package advertisinghttp

import (
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	"vozko/domain/advertising"
	adsuc "vozko/usecases/advertising"
)

type NumberResponse struct {
	Kind   string `json:"kind"`
	Label  string `json:"label"`
	Number string `json:"number"`
}

type PageResponse struct {
	PageID            string           `json:"pageId"`
	Name              string           `json:"name"`
	PictureURL        string           `json:"pictureUrl,omitempty"`
	WhatsAppNumber    string           `json:"whatsAppNumber,omitempty"`
	InstagramUserID   string           `json:"instagramUserId,omitempty"`
	InstagramUsername string           `json:"instagramUsername,omitempty"`
	CanAdvertise      bool             `json:"canAdvertise"`
	LeadTermsAccepted bool             `json:"leadTermsAccepted"`
	Numbers           []NumberResponse `json:"numbers"`
	Linkable          []NumberResponse `json:"linkable"`
}

type NumberLinkRequest struct {
	Number string `json:"number" example:"5511965467700"`
}

type NumberLinkConfirmRequest struct {
	Number string `json:"number" example:"5511965467700"`
	Code   string `json:"code" example:"83569"`
}

type LocationResponse struct {
	Kind    string `json:"kind"`
	Key     string `json:"key"`
	Name    string `json:"name"`
	Region  string `json:"region,omitempty"`
	Country string `json:"country,omitempty"`
}

type TargetingOptionResponse struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Path        []string `json:"path"`
	AudienceMin int64    `json:"audienceMin"`
	AudienceMax int64    `json:"audienceMax"`
}

type ReachRequest struct {
	Targeting  advertising.Targeting        `json:"targeting"`
	Placements advertising.Placements       `json:"placements"`
	Goal       advertising.OptimizationGoal `json:"goal"`
}

type ReachResponse struct {
	Lower int64 `json:"lower"`
	Upper int64 `json:"upper"`
	Ready bool  `json:"ready"`
}

type ProductSetResponse struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	ProductCount int64  `json:"productCount"`
}

type CatalogResponse struct {
	ID          string               `json:"id"`
	Name        string               `json:"name"`
	ProductSets []ProductSetResponse `json:"productSets"`
}

type AppResponse struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	StoreURLs []string `json:"storeUrls"`
	IconURL   string   `json:"iconUrl,omitempty"`
}

type PostResponse struct {
	ID          string     `json:"id"`
	Platform    string     `json:"platform"`
	Message     string     `json:"message,omitempty"`
	PictureURL  string     `json:"pictureUrl,omitempty"`
	Permalink   string     `json:"permalink,omitempty"`
	CreatedTime *time.Time `json:"createdTime,omitempty"`
}

type InstantExperienceResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type CreatePixelRequest struct {
	Name string `json:"name"`
}

// @Summary		Páginas que podem anunciar
// @Description	Páginas da conexão de anúncios, o número de WhatsApp vinculado a cada uma, os números do workspace iguais a ele (numbers) e os números do workspace que a página pode receber (linkable: oficiais do mesmo portfólio da página, ou não oficiais).
// @Tags			Anúncios
// @Produce		json
// @Param			id	path		string	true	"ID da conta de anúncios"
// @Success		200	{array}		PageResponse
// @Failure		409	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/accounts/{id}/pages [get]
func (h *Handler) Pages(w http.ResponseWriter, r *http.Request) {
	pages, err := h.d.Assets.Pages(r.Context(), workspaceOf(r), mux.Vars(r)["id"])
	if err != nil {
		writeError(w, err, "Failed to list pages")
		return
	}
	response.WriteSuccess(w, http.StatusOK, presentAll(pages, presentPage))
}

// @Summary		Buscar locais para segmentação
// @Tags			Anúncios
// @Produce		json
// @Param			id	path		string	true	"ID da conta de anúncios"
// @Param			q	query		string	true	"texto da busca (mínimo 2 letras)"
// @Success		200	{array}		LocationResponse
// @Security		BearerAuth
// @Router			/ads/accounts/{id}/locations [get]
func (h *Handler) Locations(w http.ResponseWriter, r *http.Request) {
	found, err := h.d.Assets.Locations(r.Context(), workspaceOf(r), mux.Vars(r)["id"], r.URL.Query().Get("q"))
	if err != nil {
		writeError(w, err, "Failed to search locations")
		return
	}
	response.WriteSuccess(w, http.StatusOK, presentAll(found, func(l advertising.RemoteLocation) LocationResponse {
		return LocationResponse{Kind: string(l.Kind), Key: l.Key, Name: l.Name, Region: l.Region, Country: l.Country}
	}))
}

// @Summary		Buscar interesses, comportamentos ou idiomas
// @Description	Opções de segmentação detalhada da Meta, com o tamanho estimado do público. Comportamentos podem ser listados sem busca.
// @Tags			Anúncios
// @Produce		json
// @Param			id		path		string	true	"ID da conta de anúncios"
// @Param			kind	query		string	true	"interests, behaviors ou languages"
// @Param			q		query		string	false	"texto da busca (mínimo 2 letras)"
// @Success		200		{array}		TargetingOptionResponse
// @Failure		422		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/accounts/{id}/targeting [get]
func (h *Handler) Targeting(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	found, err := h.d.Assets.Targeting(r.Context(), workspaceOf(r), mux.Vars(r)["id"], advertising.TargetingSearchKind(q.Get("kind")), q.Get("q"))
	if err != nil {
		writeError(w, err, "Failed to search targeting options")
		return
	}
	response.WriteSuccess(w, http.StatusOK, presentAll(found, func(o advertising.TargetingOption) TargetingOptionResponse {
		return TargetingOptionResponse{ID: o.ID, Name: o.Name, Path: nonNil(o.Path), AudienceMin: o.AudienceMin, AudienceMax: o.AudienceMax}
	}))
}

// @Summary		Estimar alcance do público
// @Description	Estimativa da Meta para o público e os posicionamentos. Sem locais, volta vazia com ready false.
// @Tags			Anúncios
// @Accept			json
// @Produce		json
// @Param			id		path		string			true	"ID da conta de anúncios"
// @Param			body	body		ReachRequest	true	"público, posicionamentos e meta de otimização"
// @Success		200		{object}	ReachResponse
// @Failure		409		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/accounts/{id}/reach-estimate [post]
func (h *Handler) Reach(w http.ResponseWriter, r *http.Request) {
	var req ReachRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	estimate, err := h.d.Assets.Reach(r.Context(), workspaceOf(r), mux.Vars(r)["id"], req.Targeting, req.Placements, req.Goal)
	if err != nil {
		writeError(w, err, "Failed to estimate the reach")
		return
	}
	response.WriteSuccess(w, http.StatusOK, ReachResponse{Lower: estimate.Lower, Upper: estimate.Upper, Ready: estimate.Ready})
}

// @Summary		Catálogos de produtos
// @Description	Catálogos do portfólio empresarial da conta, com os conjuntos de produtos.
// @Tags			Anúncios
// @Produce		json
// @Param			id	path		string	true	"ID da conta de anúncios"
// @Success		200	{array}		CatalogResponse
// @Failure		409	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/accounts/{id}/catalogs [get]
func (h *Handler) Catalogs(w http.ResponseWriter, r *http.Request) {
	catalogs, err := h.d.Assets.Catalogs(r.Context(), workspaceOf(r), mux.Vars(r)["id"])
	if err != nil {
		writeError(w, err, "Failed to list catalogs")
		return
	}
	response.WriteSuccess(w, http.StatusOK, presentAll(catalogs, func(c advertising.RemoteCatalog) CatalogResponse {
		return CatalogResponse{ID: c.ID, Name: c.Name, ProductSets: presentAll(c.ProductSets, func(s advertising.RemoteProductSet) ProductSetResponse {
			return ProductSetResponse{ID: s.ID, Name: s.Name, ProductCount: s.ProductCount}
		})}
	}))
}

// @Summary		Aplicativos da conta de anúncios
// @Tags			Anúncios
// @Produce		json
// @Param			id	path		string	true	"ID da conta de anúncios"
// @Success		200	{array}		AppResponse
// @Failure		409	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/accounts/{id}/apps [get]
func (h *Handler) Apps(w http.ResponseWriter, r *http.Request) {
	apps, err := h.d.Assets.Apps(r.Context(), workspaceOf(r), mux.Vars(r)["id"])
	if err != nil {
		writeError(w, err, "Failed to list apps")
		return
	}
	response.WriteSuccess(w, http.StatusOK, presentAll(apps, func(a advertising.RemoteApp) AppResponse {
		return AppResponse{ID: a.ID, Name: a.Name, StoreURLs: nonNil(a.StoreURLs), IconURL: a.IconURL}
	}))
}

// @Summary		Publicações da página para anunciar
// @Description	Publicações da página no Facebook ou mídias do Instagram vinculado, para usar como anúncio.
// @Tags			Anúncios
// @Produce		json
// @Param			id			path		string	true	"ID da conta de anúncios"
// @Param			pageId		path		string	true	"ID da página"
// @Param			platform	query		string	true	"facebook ou instagram"
// @Success		200			{array}		PostResponse
// @Failure		422			{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/accounts/{id}/pages/{pageId}/posts [get]
func (h *Handler) Posts(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	posts, err := h.d.Assets.Posts(r.Context(), workspaceOf(r), vars["id"], vars["pageId"], r.URL.Query().Get("platform"))
	if err != nil {
		writeError(w, err, "Failed to list posts")
		return
	}
	response.WriteSuccess(w, http.StatusOK, presentAll(posts, presentPost))
}

// @Summary		Publicação da página
// @Description	Uma publicação do Facebook ou do Instagram da página, para a prévia de um rascunho que usa publicação existente. Só lê; recusa publicações de outra página.
// @Tags			Anúncios
// @Produce		json
// @Param			id			path		string	true	"ID da conta de anúncios"
// @Param			pageId		path		string	true	"ID da página"
// @Param			postId		path		string	true	"ID da publicação ou da mídia do Instagram"
// @Param			platform	query		string	true	"facebook ou instagram"
// @Success		200			{object}	PostResponse
// @Failure		404			{object}	response.ErrorResponse
// @Failure		422			{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/accounts/{id}/pages/{pageId}/posts/{postId} [get]
func (h *Handler) Post(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	post, err := h.d.Assets.Post(r.Context(), workspaceOf(r), vars["id"], vars["pageId"], r.URL.Query().Get("platform"), vars["postId"])
	if err != nil {
		writeError(w, err, "Failed to load the post")
		return
	}
	response.WriteSuccess(w, http.StatusOK, presentPost(*post))
}

func presentPost(p advertising.RemotePost) PostResponse {
	return PostResponse{ID: p.ID, Platform: p.Platform, Message: p.Message, PictureURL: p.PictureURL, Permalink: p.Permalink, CreatedTime: p.CreatedTime}
}

// @Summary		Experiências instantâneas da página
// @Tags			Anúncios
// @Produce		json
// @Param			id		path		string	true	"ID da conta de anúncios"
// @Param			pageId	path		string	true	"ID da página"
// @Success		200		{array}		InstantExperienceResponse
// @Failure		422		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/accounts/{id}/pages/{pageId}/instant-experiences [get]
func (h *Handler) InstantExperiences(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	found, err := h.d.Assets.InstantExperiences(r.Context(), workspaceOf(r), vars["id"], vars["pageId"])
	if err != nil {
		writeError(w, err, "Failed to list instant experiences")
		return
	}
	response.WriteSuccess(w, http.StatusOK, presentAll(found, func(e advertising.RemoteInstantExperience) InstantExperienceResponse {
		return InstantExperienceResponse{ID: e.ID, Name: e.Name}
	}))
}

// @Summary		Pixels da conta de anúncios
// @Tags			Anúncios
// @Produce		json
// @Param			id	path		string	true	"ID da conta de anúncios"
// @Success		200	{array}		advertising.Pixel
// @Failure		409	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/accounts/{id}/pixels [get]
func (h *Handler) Pixels(w http.ResponseWriter, r *http.Request) {
	pixels, err := h.d.Assets.Pixels(r.Context(), workspaceOf(r), mux.Vars(r)["id"])
	if err != nil {
		writeError(w, err, "Failed to list pixels")
		return
	}
	response.WriteSuccess(w, http.StatusOK, nonNil(pixels))
}

// @Summary		Criar pixel
// @Tags			Anúncios
// @Accept			json
// @Produce		json
// @Param			id		path		string				true	"ID da conta de anúncios"
// @Param			body	body		CreatePixelRequest	true	"nome do pixel"
// @Success		201		{object}	advertising.Pixel
// @Failure		422		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/accounts/{id}/pixels [post]
func (h *Handler) CreatePixel(w http.ResponseWriter, r *http.Request) {
	var req CreatePixelRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	pixel, err := h.d.Assets.CreatePixel(r.Context(), workspaceOf(r), mux.Vars(r)["id"], req.Name)
	if err != nil {
		writeError(w, err, "Failed to create the pixel")
		return
	}
	response.WriteSuccess(w, http.StatusCreated, pixel)
}

func presentPage(p adsuc.PromotablePage) PageResponse {
	return PageResponse{
		PageID: p.Page.PageID, Name: p.Page.Name, PictureURL: p.Page.PictureURL, WhatsAppNumber: p.Page.WhatsAppNumber,
		InstagramUserID: p.Page.InstagramUserID, InstagramUsername: p.Page.InstagramUsername, CanAdvertise: p.Page.CanAdvertise,
		LeadTermsAccepted: p.Page.LeadTermsAccepted,
		Numbers:           presentAll(p.Numbers, presentNumber),
		Linkable:          presentAll(p.Linkable, presentNumber),
	}
}

func presentNumber(n advertising.WorkspaceNumber) NumberResponse {
	return NumberResponse{Kind: string(n.Kind), Label: n.Label, Number: n.Number}
}

// @Summary		Pedir código para vincular WhatsApp à página
// @Description	Pede à Meta que envie, por WhatsApp, o código que vincula o número à página. O número precisa estar em linkable da página (oficial do mesmo portfólio, ou não oficial). O código chega no app do WhatsApp do número; números em coexistência recebem no app do celular, não na caixa de entrada do Vozko. 422 com number not_linkable para um número que a página não pode receber; 409 page_link_refused se a Meta não enviar o código.
// @Tags			Anúncios
// @Accept			json
// @Produce		json
// @Param			id		path	string				true	"ID da conta de anúncios"
// @Param			pageId	path	string				true	"ID da página na Meta"
// @Param			body	body	NumberLinkRequest	true	"número a vincular"
// @Success		204
// @Failure		409	{object}	response.ErrorResponse
// @Failure		422	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/accounts/{id}/pages/{pageId}/whatsapp-link/code [post]
func (h *Handler) RequestNumberLink(w http.ResponseWriter, r *http.Request) {
	var req NumberLinkRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	vars := mux.Vars(r)
	if err := h.d.Assets.RequestNumberLink(r.Context(), workspaceOf(r), vars["id"], vars["pageId"], req.Number); err != nil {
		writeError(w, err, "Failed to request the WhatsApp link code")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// @Summary		Confirmar vínculo de WhatsApp com a página
// @Description	Envia à Meta o código recebido no WhatsApp e devolve a página atualizada. 422 com code invalid para um código fora do formato (4 a 8 dígitos); 409 page_link_refused quando a Meta não confirma o vínculo. A Meta pode levar alguns instantes para mostrar o número em whatsAppNumber; a publicação só libera quando ele aparece.
// @Tags			Anúncios
// @Accept			json
// @Produce		json
// @Param			id		path		string						true	"ID da conta de anúncios"
// @Param			pageId	path		string						true	"ID da página na Meta"
// @Param			body	body		NumberLinkConfirmRequest	true	"número e código"
// @Success		200		{object}	PageResponse
// @Failure		409		{object}	response.ErrorResponse
// @Failure		422		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/accounts/{id}/pages/{pageId}/whatsapp-link [post]
func (h *Handler) ConfirmNumberLink(w http.ResponseWriter, r *http.Request) {
	var req NumberLinkConfirmRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	vars := mux.Vars(r)
	page, err := h.d.Assets.ConfirmNumberLink(r.Context(), workspaceOf(r), vars["id"], vars["pageId"], req.Number, req.Code)
	if err != nil {
		writeError(w, err, "Failed to confirm the WhatsApp link")
		return
	}
	response.WriteSuccess(w, http.StatusOK, presentPage(*page))
}

// @Summary		Orçamento diário mínimo
// @Description	Mínimo diário que a Meta aceita para um conjunto de anúncios desta conta com a meta de otimização informada, em unidades menores da moeda da conta. Com lance manual, informe bidAmount. Contas só de leitura recebem 409 account_read_only.
// @Tags			Anúncios
// @Produce		json
// @Param			id			path		string	true	"ID da conta de anúncios"
// @Param			goal		query		string	true	"meta de otimização, como LINK_CLICKS"
// @Param			bidAmount	query		int		false	"lance manual em unidades menores da moeda da conta"
// @Success		200			{object}	BudgetMinimumResponse
// @Failure		409			{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/accounts/{id}/budget-minimum [get]
func (h *Handler) BudgetMinimum(w http.ResponseWriter, r *http.Request) {
	bid, err := intQuery(r, "bidAmount")
	if err != nil {
		writeError(w, err, "Failed to read the minimum budget")
		return
	}
	goal := advertising.OptimizationGoal(strings.TrimSpace(r.URL.Query().Get("goal")))
	if goal == "" {
		writeError(w, advertising.FieldError("goal", "required"), "Failed to read the minimum budget")
		return
	}
	minimum, err := h.d.Assets.BudgetMinimum(r.Context(), workspaceOf(r), mux.Vars(r)["id"], goal, int64(bid))
	if err != nil {
		writeError(w, err, "Failed to read the minimum budget")
		return
	}
	response.WriteSuccess(w, http.StatusOK, presentBudgetMinimum(&minimum))
}
