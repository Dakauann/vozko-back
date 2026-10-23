package carriermessaging

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/domain/carriermessaging"
	"vozko/domain/conversation"
	"vozko/domain/rcs"
	"vozko/domain/sms"
	cmuc "vozko/usecases/carriermessaging"
)

type API interface {
	ListSenders(ctx context.Context, workspaceID string) ([]*rcs.Sender, error)
	GetSender(ctx context.Context, workspaceID, id string) (*rcs.Sender, error)
	CreateSender(ctx context.Context, workspaceID string, in cmuc.SenderInput) (*rcs.Sender, error)
	UpdateSender(ctx context.Context, workspaceID, id string, in cmuc.SenderInput) (*rcs.Sender, error)
	SubmitSender(ctx context.Context, workspaceID, id string) (*rcs.Sender, error)
	PauseSender(ctx context.Context, workspaceID, id string) (*rcs.Sender, error)
	ResumeSender(ctx context.Context, workspaceID, id string) (*rcs.Sender, error)
	DeleteSender(ctx context.Context, workspaceID, id string) error
	SuggestedDescription(sender rcs.Sender) string

	ListRoutes(ctx context.Context, workspaceID string) ([]*sms.Route, error)
	CreateRoute(ctx context.Context, workspaceID, brandPrefix string) (*sms.Route, error)
	UpdateRoute(ctx context.Context, workspaceID, id, brandPrefix string) (*sms.Route, error)
	PauseRoute(ctx context.Context, workspaceID, id string) (*sms.Route, error)
	ResumeRoute(ctx context.Context, workspaceID, id string) (*sms.Route, error)
	DeleteRoute(ctx context.Context, workspaceID, id string) error

	QuoteRCS(ctx context.Context, workspaceID string, content rcs.Content) (cmuc.RCSQuote, error)
	QuoteSMS(ctx context.Context, workspaceID, text, routeID string) (cmuc.SMSQuote, error)

	GetSettings(ctx context.Context, workspaceID string) (carriermessaging.Settings, error)
	PutSettings(ctx context.Context, workspaceID string, settings carriermessaging.Settings) (carriermessaging.Settings, error)
	ListSuppressions(ctx context.Context, workspaceID string, limit, offset int) ([]carriermessaging.Suppression, int, error)
	AddSuppression(ctx context.Context, workspaceID, phone string) error
	RemoveSuppression(ctx context.Context, workspaceID, phone string) error

	Deliver(ctx context.Context, req cmuc.DeliverRequest) (*carriermessaging.Message, error)
	ResolveLinkClick(ctx context.Context, token string, at time.Time) (string, error)
	ResolveMedia(ctx context.Context, workspaceID, mediaID string) (*rcs.Media, error)
	SetSenderInbox(ctx context.Context, workspaceID, id string, in cmuc.InboxSettings) (*rcs.Sender, error)
	SetRouteInbox(ctx context.Context, workspaceID, id string, in cmuc.InboxSettings) (*sms.Route, error)

	AdminListSenders(ctx context.Context, status *rcs.SenderStatus) ([]*rcs.Sender, error)
	AdminPauseSender(ctx context.Context, id string, reason rcs.PauseReason) (*rcs.Sender, error)
	AdminResumeSender(ctx context.Context, id string) (*rcs.Sender, error)
	AdminListRoutes(ctx context.Context) ([]*sms.Route, error)
	AdminAssignNumber(ctx context.Context, id, number string) (*sms.Route, error)
	AdminReleaseNumber(ctx context.Context, id string) (*sms.Route, error)
}

type Handler struct {
	api API
	now func() time.Time
}

func NewHandler(api API) *Handler {
	return &Handler{api: api, now: time.Now}
}

func (h *Handler) sender(w http.ResponseWriter, s *rcs.Sender, status int) {
	response.WriteSuccess(w, status, senderDTO(s, h.api.SuggestedDescription(*s)))
}

// @Summary		Remetentes RCS
// @Description	Lista os remetentes RCS (agentes verificados, um por marca) do workspace.
// @Tags			RCS e SMS
// @Produce		json
// @Success		200	{object}	SendersResponse
// @Router			/rcs/senders [get]
func (h *Handler) ListSenders(w http.ResponseWriter, r *http.Request) {
	ws, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	senders, err := h.api.ListSenders(r.Context(), ws)
	if err != nil {
		writeError(w, err, "list senders")
		return
	}
	out := SendersResponse{Senders: make([]SenderDTO, 0, len(senders))}
	for _, s := range senders {
		out.Senders = append(out.Senders, senderDTO(s, h.api.SuggestedDescription(*s)))
	}
	response.WriteSuccess(w, http.StatusOK, out)
}

// @Summary		Remetente RCS
// @Tags			RCS e SMS
// @Produce		json
// @Param			id	path		string	true	"Remetente"
// @Success		200	{object}	SenderDTO
// @Router			/rcs/senders/{id} [get]
func (h *Handler) GetSender(w http.ResponseWriter, r *http.Request) {
	ws, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	s, err := h.api.GetSender(r.Context(), ws, mux.Vars(r)["id"])
	if err != nil {
		writeError(w, err, "get sender")
		return
	}
	h.sender(w, s, http.StatusOK)
}

// @Summary		Criar rascunho de remetente RCS
// @Tags			RCS e SMS
// @Accept			json
// @Produce		json
// @Param			body	body		SenderRequest	true	"Dados da marca"
// @Success		201		{object}	SenderDTO
// @Router			/rcs/senders [post]
func (h *Handler) CreateSender(w http.ResponseWriter, r *http.Request) {
	ws, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	var body SenderRequest
	if !httpx.DecodeStrictJSON(w, r, &body) {
		return
	}
	s, err := h.api.CreateSender(r.Context(), ws, body.input())
	if err != nil {
		writeError(w, err, "create sender")
		return
	}
	h.sender(w, s, http.StatusCreated)
}

// @Summary		Editar rascunho de remetente RCS
// @Tags			RCS e SMS
// @Accept			json
// @Produce		json
// @Param			id		path		string			true	"Remetente"
// @Param			body	body		SenderRequest	true	"Dados da marca"
// @Success		200		{object}	SenderDTO
// @Router			/rcs/senders/{id} [patch]
func (h *Handler) UpdateSender(w http.ResponseWriter, r *http.Request) {
	ws, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	var body SenderRequest
	if !httpx.DecodeStrictJSON(w, r, &body) {
		return
	}
	s, err := h.api.UpdateSender(r.Context(), ws, mux.Vars(r)["id"], body.input())
	if err != nil {
		writeError(w, err, "update sender")
		return
	}
	h.sender(w, s, http.StatusOK)
}

func (h *Handler) senderAction(action func(context.Context, string, string) (*rcs.Sender, error), subject string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ws, ok := httpx.RequireWorkspace(w, r)
		if !ok {
			return
		}
		s, err := action(r.Context(), ws, mux.Vars(r)["id"])
		if err != nil {
			writeError(w, err, subject)
			return
		}
		h.sender(w, s, http.StatusOK)
	}
}

// @Summary		Enviar remetente RCS para aprovação
// @Description	Valida a marca e o CNPJ e registra o remetente no provedor; depois as operadoras pedem a confirmação da marca por e-mail.
// @Tags			RCS e SMS
// @Produce		json
// @Param			id	path		string	true	"Remetente"
// @Success		200	{object}	SenderDTO
// @Router			/rcs/senders/{id}/submit [post]
func (h *Handler) SubmitSender(w http.ResponseWriter, r *http.Request) {
	h.senderAction(h.api.SubmitSender, "submit sender")(w, r)
}

// @Summary		Pausar remetente RCS
// @Tags			RCS e SMS
// @Produce		json
// @Param			id	path		string	true	"Remetente"
// @Success		200	{object}	SenderDTO
// @Router			/rcs/senders/{id}/pause [post]
func (h *Handler) PauseSender(w http.ResponseWriter, r *http.Request) {
	h.senderAction(h.api.PauseSender, "pause sender")(w, r)
}

// @Summary		Retomar remetente RCS
// @Tags			RCS e SMS
// @Produce		json
// @Param			id	path		string	true	"Remetente"
// @Success		200	{object}	SenderDTO
// @Router			/rcs/senders/{id}/resume [post]
func (h *Handler) ResumeSender(w http.ResponseWriter, r *http.Request) {
	h.senderAction(h.api.ResumeSender, "resume sender")(w, r)
}

// @Summary		Excluir rascunho de remetente RCS
// @Tags			RCS e SMS
// @Param			id	path	string	true	"Remetente"
// @Success		204
// @Router			/rcs/senders/{id} [delete]
func (h *Handler) DeleteSender(w http.ResponseWriter, r *http.Request) {
	ws, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	if err := h.api.DeleteSender(r.Context(), ws, mux.Vars(r)["id"]); err != nil {
		writeError(w, err, "delete sender")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func routesResponse(routes []*sms.Route, withWorkspace bool) RoutesResponse {
	out := RoutesResponse{Routes: make([]RouteDTO, 0, len(routes))}
	for _, route := range routes {
		dto := routeDTO(route)
		if withWorkspace {
			dto.WorkspaceID = route.WorkspaceID
		}
		out.Routes = append(out.Routes, dto)
	}
	return out
}

// @Summary		Remetentes de SMS
// @Description	Lista os remetentes de SMS do workspace: o número compartilhado da plataforma ou um número dedicado, com o nome da marca que abre cada SMS.
// @Tags			RCS e SMS
// @Produce		json
// @Success		200	{object}	RoutesResponse
// @Router			/sms/routes [get]
func (h *Handler) ListRoutes(w http.ResponseWriter, r *http.Request) {
	ws, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	routes, err := h.api.ListRoutes(r.Context(), ws)
	if err != nil {
		writeError(w, err, "list routes")
		return
	}
	response.WriteSuccess(w, http.StatusOK, routesResponse(routes, false))
}

// @Summary		Criar remetente de SMS
// @Tags			RCS e SMS
// @Accept			json
// @Produce		json
// @Param			body	body		RouteRequest	true	"Nome da marca no SMS"
// @Success		201		{object}	RouteDTO
// @Router			/sms/routes [post]
func (h *Handler) CreateRoute(w http.ResponseWriter, r *http.Request) {
	ws, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	var body RouteRequest
	if !httpx.DecodeStrictJSON(w, r, &body) {
		return
	}
	route, err := h.api.CreateRoute(r.Context(), ws, body.BrandPrefix)
	if err != nil {
		writeError(w, err, "create route")
		return
	}
	response.WriteSuccess(w, http.StatusCreated, routeDTO(route))
}

// @Summary		Editar remetente de SMS
// @Tags			RCS e SMS
// @Accept			json
// @Produce		json
// @Param			id		path		string			true	"Remetente de SMS"
// @Param			body	body		RouteRequest	true	"Nome da marca no SMS"
// @Success		200		{object}	RouteDTO
// @Router			/sms/routes/{id} [patch]
func (h *Handler) UpdateRoute(w http.ResponseWriter, r *http.Request) {
	ws, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	var body RouteRequest
	if !httpx.DecodeStrictJSON(w, r, &body) {
		return
	}
	route, err := h.api.UpdateRoute(r.Context(), ws, mux.Vars(r)["id"], body.BrandPrefix)
	if err != nil {
		writeError(w, err, "update route")
		return
	}
	response.WriteSuccess(w, http.StatusOK, routeDTO(route))
}

func (h *Handler) routeAction(action func(context.Context, string, string) (*sms.Route, error), subject string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ws, ok := httpx.RequireWorkspace(w, r)
		if !ok {
			return
		}
		route, err := action(r.Context(), ws, mux.Vars(r)["id"])
		if err != nil {
			writeError(w, err, subject)
			return
		}
		response.WriteSuccess(w, http.StatusOK, routeDTO(route))
	}
}

// @Summary		Pausar remetente de SMS
// @Tags			RCS e SMS
// @Produce		json
// @Param			id	path		string	true	"Remetente de SMS"
// @Success		200	{object}	RouteDTO
// @Router			/sms/routes/{id}/pause [post]
func (h *Handler) PauseRoute(w http.ResponseWriter, r *http.Request) {
	h.routeAction(h.api.PauseRoute, "pause route")(w, r)
}

// @Summary		Retomar remetente de SMS
// @Tags			RCS e SMS
// @Produce		json
// @Param			id	path		string	true	"Remetente de SMS"
// @Success		200	{object}	RouteDTO
// @Router			/sms/routes/{id}/resume [post]
func (h *Handler) ResumeRoute(w http.ResponseWriter, r *http.Request) {
	h.routeAction(h.api.ResumeRoute, "resume route")(w, r)
}

// @Summary		Excluir remetente de SMS
// @Tags			RCS e SMS
// @Param			id	path	string	true	"Remetente de SMS"
// @Success		204
// @Router			/sms/routes/{id} [delete]
func (h *Handler) DeleteRoute(w http.ResponseWriter, r *http.Request) {
	ws, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	if err := h.api.DeleteRoute(r.Context(), ws, mux.Vars(r)["id"]); err != nil {
		writeError(w, err, "delete route")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// @Summary		Conferir mensagem RCS
// @Description	Valida a mensagem do compositor e devolve o tipo, a classe de cobrança, as variáveis, os problemas com o caminho de cada um e o preço.
// @Tags			RCS e SMS
// @Accept			json
// @Produce		json
// @Param			body	body		ContentCheckRequest	true	"Mensagem"
// @Success		200		{object}	ContentCheckResponse
// @Router			/rcs/content/check [post]
func (h *Handler) CheckContent(w http.ResponseWriter, r *http.Request) {
	ws, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	var body ContentCheckRequest
	if !httpx.DecodeJSON(w, r, &body) {
		return
	}
	q, err := h.api.QuoteRCS(r.Context(), ws, body.Content)
	if err != nil {
		writeError(w, err, "check content")
		return
	}
	response.WriteSuccess(w, http.StatusOK, contentCheckResponse(q))
}

// @Summary		Conferir SMS
// @Description	Mede o SMS já com o nome da marca: codificação, partes, caracteres restantes, problemas e preço.
// @Tags			RCS e SMS
// @Accept			json
// @Produce		json
// @Param			body	body		TextCheckRequest	true	"Texto"
// @Success		200		{object}	TextCheckResponse
// @Router			/sms/text/check [post]
func (h *Handler) CheckText(w http.ResponseWriter, r *http.Request) {
	ws, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	var body TextCheckRequest
	if !httpx.DecodeStrictJSON(w, r, &body) {
		return
	}
	q, err := h.api.QuoteSMS(r.Context(), ws, body.Text, body.RouteID)
	if err != nil {
		writeError(w, err, "check text")
		return
	}
	response.WriteSuccess(w, http.StatusOK, textCheckResponse(q))
}

// @Summary		Remover acentos do SMS
// @Description	Troca letras acentuadas pelas sem acento para o SMS caber em menos partes.
// @Tags			RCS e SMS
// @Accept			json
// @Produce		json
// @Param			body	body		ToGSMRequest	true	"Texto"
// @Success		200		{object}	ToGSMResponse
// @Router			/sms/text/to-gsm [post]
func (h *Handler) ToGSM(w http.ResponseWriter, r *http.Request) {
	if _, ok := httpx.RequireWorkspace(w, r); !ok {
		return
	}
	var body ToGSMRequest
	if !httpx.DecodeStrictJSON(w, r, &body) {
		return
	}
	response.WriteSuccess(w, http.StatusOK, ToGSMResponse{Text: sms.ToGSM(body.Text)})
}

// @Summary		Regras de envio de RCS e SMS
// @Description	Horário de envio de marketing, limite por contato e janela de atribuição de conversões.
// @Tags			RCS e SMS
// @Produce		json
// @Success		200	{object}	carriermessaging.Settings
// @Router			/carrier/settings [get]
func (h *Handler) GetSettings(w http.ResponseWriter, r *http.Request) {
	ws, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	s, err := h.api.GetSettings(r.Context(), ws)
	if err != nil {
		writeError(w, err, "get settings")
		return
	}
	response.WriteSuccess(w, http.StatusOK, s)
}

// @Summary		Salvar regras de envio de RCS e SMS
// @Tags			RCS e SMS
// @Accept			json
// @Produce		json
// @Param			body	body		carriermessaging.Settings	true	"Regras"
// @Success		200		{object}	carriermessaging.Settings
// @Router			/carrier/settings [put]
func (h *Handler) PutSettings(w http.ResponseWriter, r *http.Request) {
	ws, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	var body carriermessaging.Settings
	if !httpx.DecodeStrictJSON(w, r, &body) {
		return
	}
	s, err := h.api.PutSettings(r.Context(), ws, body)
	if err != nil {
		writeError(w, err, "put settings")
		return
	}
	response.WriteSuccess(w, http.StatusOK, s)
}

// @Summary		Contatos que saíram do RCS e SMS
// @Tags			RCS e SMS
// @Produce		json
// @Param			limit	query		int	false	"Itens por página"
// @Param			offset	query		int	false	"Deslocamento"
// @Success		200		{object}	SuppressionsResponse
// @Router			/carrier/suppressions [get]
func (h *Handler) ListSuppressions(w http.ResponseWriter, r *http.Request) {
	ws, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	limit, offset := boundedInt(r.URL.Query().Get("limit"), 50, 1, 200), boundedInt(r.URL.Query().Get("offset"), 0, 0, 1<<30)
	items, total, err := h.api.ListSuppressions(r.Context(), ws, limit, offset)
	if err != nil {
		writeError(w, err, "list suppressions")
		return
	}
	out := SuppressionsResponse{Items: make([]SuppressionDTO, 0, len(items)), Total: total}
	for _, s := range items {
		out.Items = append(out.Items, SuppressionDTO{Phone: s.Phone, Source: string(s.Source), CreatedAt: s.CreatedAt})
	}
	response.WriteSuccess(w, http.StatusOK, out)
}

func boundedInt(raw string, fallback, min, max int) int {
	n, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return clamp(n, min, max)
}

func clamp(n, min, max int) int {
	if n < min {
		return min
	}
	if n > max {
		return max
	}
	return n
}

// @Summary		Tirar contato do RCS e SMS de marketing
// @Tags			RCS e SMS
// @Accept			json
// @Param			body	body	SuppressionRequest	true	"Telefone"
// @Success		204
// @Router			/carrier/suppressions [post]
func (h *Handler) AddSuppression(w http.ResponseWriter, r *http.Request) {
	ws, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	var body SuppressionRequest
	if !httpx.DecodeStrictJSON(w, r, &body) {
		return
	}
	if err := h.api.AddSuppression(r.Context(), ws, body.Phone); err != nil {
		writeError(w, err, "add suppression")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// @Summary		Devolver contato ao RCS e SMS de marketing
// @Tags			RCS e SMS
// @Param			phone	path	string	true	"Telefone"
// @Success		204
// @Router			/carrier/suppressions/{phone} [delete]
func (h *Handler) RemoveSuppression(w http.ResponseWriter, r *http.Request) {
	ws, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	if err := h.api.RemoveSuppression(r.Context(), ws, mux.Vars(r)["phone"]); err != nil {
		writeError(w, err, "remove suppression")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// @Summary		Envio de teste
// @Description	Envia a mensagem para um número de teste. Até 20 por dia são gratuitos; não passam pelas regras de marketing.
// @Tags			RCS e SMS
// @Accept			json
// @Produce		json
// @Param			body	body		TestSendRequest	true	"Mensagem de teste"
// @Success		200		{object}	MessageDTO
// @Router			/carrier/test-sends [post]
func (h *Handler) TestSend(w http.ResponseWriter, r *http.Request) {
	ws, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	person, ok := httpx.RequirePerson(w, r)
	if !ok {
		return
	}
	var body TestSendRequest
	if !httpx.DecodeStrictJSON(w, r, &body) {
		return
	}
	msg, err := h.api.Deliver(r.Context(), cmuc.DeliverRequest{
		WorkspaceID: ws, Channel: carriermessaging.Channel(body.Channel), SenderID: body.SenderID, Phone: body.Phone,
		RunKind: carriermessaging.RunTest, Content: body.Content, Text: body.Text, Vars: body.Vars,
		SentBy: conversation.SentByPerson(person.UserID),
	})
	if err != nil {
		writeError(w, err, "test send")
		return
	}
	response.WriteSuccess(w, http.StatusOK, messageDTO(msg))
}

// @Summary		Link rastreado de botão
// @Description	Registra o clique no botão de link de uma mensagem RCS e redireciona para o endereço original.
// @Tags			RCS e SMS
// @Param			token	path	string	true	"Token assinado do link"
// @Success		302
// @Failure		404
// @Router			/public/carrier/r/{token} [get]
func (h *Handler) FollowLink(w http.ResponseWriter, r *http.Request) {
	target, err := h.api.ResolveLinkClick(r.Context(), mux.Vars(r)["token"], h.now())
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	http.Redirect(w, r, target, http.StatusFound)
}

type MediaRequest struct {
	MediaID string `json:"media_id"`
}

// @Summary		Preparar mídia da biblioteca para RCS
// @Description	Confere o arquivo da biblioteca de mídia e devolve tipo, formato e tamanho prontos para um cartão ou mensagem RCS.
// @Tags			RCS e SMS
// @Accept			json
// @Produce		json
// @Param			body	body		MediaRequest	true	"Arquivo da biblioteca"
// @Success		200		{object}	rcs.Media
// @Router			/rcs/media [post]
func (h *Handler) ResolveMedia(w http.ResponseWriter, r *http.Request) {
	ws, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	var body MediaRequest
	if !httpx.DecodeStrictJSON(w, r, &body) {
		return
	}
	m, err := h.api.ResolveMedia(r.Context(), ws, body.MediaID)
	if err != nil {
		writeError(w, err, "resolve media")
		return
	}
	response.WriteSuccess(w, http.StatusOK, m)
}

// @Summary		Caixa de entrada do remetente RCS
// @Description	Define o departamento, o funil e o agente de IA ou fluxo que atendem as conversas RCS deste remetente.
// @Tags			RCS e SMS
// @Accept			json
// @Produce		json
// @Param			id		path		string			true	"Remetente"
// @Param			body	body		InboxRequest	true	"Caixa de entrada"
// @Success		200		{object}	SenderDTO
// @Router			/rcs/senders/{id}/inbox [put]
func (h *Handler) SetSenderInbox(w http.ResponseWriter, r *http.Request) {
	ws, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	var body InboxRequest
	if !httpx.DecodeStrictJSON(w, r, &body) {
		return
	}
	s, err := h.api.SetSenderInbox(r.Context(), ws, mux.Vars(r)["id"], body.domain())
	if err != nil {
		writeError(w, err, "sender inbox")
		return
	}
	h.sender(w, s, http.StatusOK)
}

// @Summary		Caixa de entrada do remetente de SMS
// @Description	Define o departamento, o funil e o agente de IA ou fluxo que atendem as conversas de SMS deste remetente.
// @Tags			RCS e SMS
// @Accept			json
// @Produce		json
// @Param			id		path		string			true	"Remetente de SMS"
// @Param			body	body		InboxRequest	true	"Caixa de entrada"
// @Success		200		{object}	RouteDTO
// @Router			/sms/routes/{id}/inbox [put]
func (h *Handler) SetRouteInbox(w http.ResponseWriter, r *http.Request) {
	ws, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	var body InboxRequest
	if !httpx.DecodeStrictJSON(w, r, &body) {
		return
	}
	route, err := h.api.SetRouteInbox(r.Context(), ws, mux.Vars(r)["id"], body.domain())
	if err != nil {
		writeError(w, err, "route inbox")
		return
	}
	response.WriteSuccess(w, http.StatusOK, routeDTO(route))
}

// @Summary		Canais de RCS e SMS disponíveis
// @Description	Diz quais canais já estão disponíveis. Um canal com launched false está em breve: as rotas dele não existem e envios por ele são recusados com carrier_channel_coming_soon.
// @Tags			RCS e SMS
// @Produce		json
// @Success		200	{object}	ChannelsResponse
// @Router			/carrier/channels [get]
func (h *Handler) ListChannels(w http.ResponseWriter, _ *http.Request) {
	response.WriteSuccess(w, http.StatusOK, channelsResponse())
}
