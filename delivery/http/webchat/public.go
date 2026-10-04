package webchat

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	wcdomain "vozko/domain/webchat"
	"vozko/infra/http/middleware"
	wcuc "vozko/usecases/webchat"
)

const (
	PublicPrefix    = "/public/webchat"
	LoaderPath      = PublicPrefix + "/loader.js"
	maxVisitorBody  = 16 << 10
	uploadOverhead  = 64 << 10
	streamLifetime  = 30 * time.Minute
	heartbeatEvery  = 20 * time.Second
	loaderMaxAge    = "public, max-age=300"
	assetsImmutable = "public, max-age=31536000, immutable"
)

//go:embed assets/*
var assets embed.FS

var frameTemplate = template.Must(template.ParseFS(assets, "assets/frame.html"))

var assetVersion = contentVersion("frame.js", "frame.css")

func contentVersion(names ...string) string {
	digest := sha256.New()
	for _, name := range names {
		data, err := assets.ReadFile(path.Join("assets", name))
		if err != nil {
			panic(err)
		}
		digest.Write(data)
	}
	return hex.EncodeToString(digest.Sum(nil))[:12]
}

type PublicHandler struct {
	visitors  *wcuc.VisitorService
	stream    wcdomain.EventStream
	apiOrigin string
}

func NewPublicHandler(visitors *wcuc.VisitorService, stream wcdomain.EventStream, apiOrigin string) *PublicHandler {
	return &PublicHandler{visitors: visitors, stream: stream, apiOrigin: strings.TrimRight(apiOrigin, "/")}
}

type ChallengeRequest struct {
	ParentOrigin string `json:"parentOrigin"`
}

type SessionRequest struct {
	Token          string `json:"token"`
	ChallengeToken string `json:"challengeToken"`
	Nonce          string `json:"nonce"`
	Identity       string `json:"identity"`
	ParentOrigin   string `json:"parentOrigin"`
	Locale         string `json:"locale"`
}

type IntakeRequest struct {
	Name    string `json:"name"`
	Email   string `json:"email"`
	Phone   string `json:"phone"`
	Consent bool   `json:"consent"`
	Website string `json:"website"`
}

type MessageRequest struct {
	ClientMessageID string `json:"clientMessageId"`
	Text            string `json:"text"`
	SelectionID     string `json:"selectionId"`
	PageURL         string `json:"pageUrl"`
}

type TypingRequest struct {
	Typing bool `json:"typing"`
}

func (h *PublicHandler) sameOrigin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		site := r.Header.Get("Sec-Fetch-Site")
		origin := strings.TrimRight(r.Header.Get("Origin"), "/")
		allowed := site == "same-origin" || (site == "" && origin != "" && origin == h.apiOrigin)
		if !allowed {
			response.WriteCodedError(w, http.StatusForbidden, "cross_site", "this endpoint only answers the chat frame")
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		next(w, r)
	}
}

func (h *PublicHandler) session(w http.ResponseWriter, r *http.Request) (*wcuc.Session, bool) {
	token, found := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !found || strings.TrimSpace(token) == "" {
		writeError(w, wcdomain.ErrVisitorTokenInvalid)
		return nil, false
	}
	s, err := h.visitors.Authenticate(r.Context(), strings.TrimSpace(token), middleware.GetClientIP(r))
	if err != nil {
		writeError(w, err)
		return nil, false
	}
	return s, true
}

// @Summary		Script do WebChat
// @Description	Script que o site inclui com <script async src=".../public/webchat/loader.js" data-key="CHAVE_PUBLICA">. Desenha o botão e abre o chat num iframe servido por esta API. Para identificar um cliente logado, defina antes window.VozkoChat = { identity: "<JWT HS256 gerado no seu servidor>" }.
// @Tags			WebChat
// @Produce		application/javascript
// @Success		200
// @Router			/public/webchat/loader.js [get]
func (h *PublicHandler) Loader(w http.ResponseWriter, r *http.Request) {
	h.serveAsset(w, "loader.js", loaderMaxAge)
}

// @Summary		Arquivos do WebChat
// @Description	JavaScript e CSS da janela do chat.
// @Tags			WebChat
// @Param			file	path	string	true	"frame.js ou frame.css"
// @Success		200
// @Failure		404
// @Router			/public/webchat/assets/{file} [get]
func (h *PublicHandler) Asset(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["file"]
	if name != "frame.js" && name != "frame.css" {
		http.NotFound(w, r)
		return
	}
	cacheControl := "no-cache"
	if r.URL.Query().Get("v") == assetVersion {
		cacheControl = assetsImmutable
	}
	h.serveAsset(w, name, cacheControl)
}

func (h *PublicHandler) serveAsset(w http.ResponseWriter, name, cacheControl string) {
	data, err := assets.ReadFile(path.Join("assets", name))
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	contentType := "application/javascript; charset=utf-8"
	if strings.HasSuffix(name, ".css") {
		contentType = "text/css; charset=utf-8"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", cacheControl)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cross-Origin-Resource-Policy", "cross-origin")
	_, _ = w.Write(data)
}

// @Summary		Janela do WebChat
// @Description	Página do chat carregada pelo script dentro de um iframe. Só pode ser exibida nos sites permitidos do chat (Content-Security-Policy frame-ancestors). Chat pausado ou removido responde 404.
// @Tags			WebChat
// @Produce		html
// @Param			publicKey	path	string	true	"chave pública do chat"
// @Success		200
// @Failure		404
// @Router			/public/webchat/{publicKey}/frame [get]
func (h *PublicHandler) Frame(w http.ResponseWriter, r *http.Request) {
	widget, err := h.visitors.WidgetForFrame(r.Context(), mux.Vars(r)["publicKey"])
	if err != nil {
		http.NotFound(w, r)
		return
	}
	header := w.Header()
	header.Set("Content-Type", "text/html; charset=utf-8")
	header.Set("Cache-Control", "no-store")
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Referrer-Policy", "no-referrer")
	header.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
	header.Set("Content-Security-Policy", strings.Join([]string{
		"default-src 'none'",
		"script-src 'self'",
		"style-src 'self'",
		"img-src 'self' https: data: blob:",
		"connect-src 'self'",
		"form-action 'none'",
		"base-uri 'none'",
		"frame-ancestors " + widget.FrameAncestors(),
	}, "; "))
	if err := frameTemplate.Execute(w, map[string]string{
		"PublicKey": widget.PublicKey,
		"Prefix":    PublicPrefix,
		"Name":      widget.Name,
		"Accent":    widget.AccentColor,
		"Position":  string(widget.Position),
		"Label":     widget.LauncherLabel,
		"Version":   assetVersion,
	}); err != nil {
		http.Error(w, "frame unavailable", http.StatusInternalServerError)
	}
}

// @Summary		Desafio anti-robô do WebChat
// @Description	Devolve um desafio de prova de trabalho (SHA-256 com bits zerados no início) que o chat resolve antes de abrir uma sessão anônima. Vale 2 minutos e uma única vez. Só responde à janela do chat (mesma origem).
// @Tags			WebChat
// @Accept			json
// @Produce		json
// @Param			publicKey	path		string				true	"chave pública do chat"
// @Param			body		body		ChallengeRequest	true	"origem do site onde o chat está"
// @Success		200			{object}	ChallengeResponse
// @Failure		403			{object}	response.CodedErrorResponse
// @Failure		404			{object}	response.CodedErrorResponse
// @Router			/public/webchat/{publicKey}/challenge [post]
func (h *PublicHandler) Challenge(w http.ResponseWriter, r *http.Request) {
	var req ChallengeRequest
	if !decodeBody(w, r, maxVisitorBody, &req) {
		return
	}
	challenge, err := h.visitors.Challenge(r.Context(), mux.Vars(r)["publicKey"], req.ParentOrigin)
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toChallengeResponse(challenge))
}

// @Summary		Abrir sessão do visitante
// @Description	Abre ou retoma a sessão de um visitante. Com token válido retoma o mesmo visitante; sem ele exige o desafio resolvido (challengeToken e nonce). Com identity (JWT assinado pelo site) o visitante é o cliente identificado, em qualquer aparelho. Devolve o token da sessão (30 dias, enviado como Authorization: Bearer), a aparência do chat e o que falta no formulário inicial. Limites: novas sessões por IP e por chat (429 rate_limited).
// @Tags			WebChat
// @Accept			json
// @Produce		json
// @Param			publicKey	path		string			true	"chave pública do chat"
// @Param			body		body		SessionRequest	true	"token, desafio ou identidade"
// @Success		200			{object}	SessionResponse
// @Failure		400			{object}	response.CodedErrorResponse
// @Failure		401			{object}	response.CodedErrorResponse
// @Failure		403			{object}	response.CodedErrorResponse
// @Failure		429			{object}	response.CodedErrorResponse
// @Router			/public/webchat/{publicKey}/session [post]
func (h *PublicHandler) StartSession(w http.ResponseWriter, r *http.Request) {
	var req SessionRequest
	if !decodeBody(w, r, maxVisitorBody, &req) {
		return
	}
	view, err := h.visitors.StartSession(r.Context(), wcuc.StartSessionInput{
		PublicKey:      mux.Vars(r)["publicKey"],
		Token:          req.Token,
		ChallengeToken: req.ChallengeToken,
		Nonce:          req.Nonce,
		Identity:       req.Identity,
		Client: wcuc.Client{
			ParentOrigin: req.ParentOrigin,
			IP:           middleware.GetClientIP(r),
			UserAgent:    r.UserAgent(),
			Locale:       req.Locale,
		},
	})
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toSessionResponse(view))
}

// @Summary		Enviar formulário inicial
// @Description	Nome, e-mail, telefone e aceite da política de privacidade, conforme o chat pede. Um telefone liga o visitante ao lead com o mesmo número. O campo website precisa ficar vazio.
// @Tags			WebChat
// @Accept			json
// @Produce		json
// @Param			body	body		IntakeRequest	true	"respostas"
// @Success		200		{object}	VisitorStateResponse
// @Failure		422		{object}	response.CodedErrorResponse
// @Security		BearerAuth
// @Router			/public/webchat/session/intake [post]
func (h *PublicHandler) Intake(w http.ResponseWriter, r *http.Request) {
	s, ok := h.session(w, r)
	if !ok {
		return
	}
	var req IntakeRequest
	if !decodeBody(w, r, maxVisitorBody, &req) {
		return
	}
	if req.Website != "" {
		response.WriteSuccess(w, http.StatusOK, VisitorStateResponse{})
		return
	}
	state, err := h.visitors.SubmitIntake(r.Context(), s, wcdomain.IntakeAnswers{
		Name: req.Name, Email: req.Email, Phone: req.Phone, Consent: req.Consent,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toVisitorStateResponse(state))
}

// @Summary		Histórico do visitante
// @Description	Mensagens da conversa do visitante, da mais antiga à mais nova, só com o que o cliente pode ver (sem notas internas, ferramentas ou dados de atendentes). before e after (RFC 3339) paginam; options são os botões ainda em aberto.
// @Tags			WebChat
// @Produce		json
// @Param			before	query		string	false	"mensagens antes deste instante"
// @Param			after	query		string	false	"mensagens depois deste instante"
// @Success		200		{object}	HistoryResponse
// @Security		BearerAuth
// @Router			/public/webchat/session/messages [get]
func (h *PublicHandler) History(w http.ResponseWriter, r *http.Request) {
	s, ok := h.session(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	view, err := h.visitors.History(r.Context(), s, wcuc.HistoryInput{
		Before: parseInstant(query.Get("before")),
		After:  parseInstant(query.Get("after")),
	})
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toHistoryResponse(view))
}

func parseInstant(raw string) *time.Time {
	if raw == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return nil
	}
	return &t
}

// @Summary		Enviar mensagem do visitante
// @Description	Texto (até 2.000 caracteres) ou a escolha de um botão oferecido (selectionId). clientMessageId (8 a 64 caracteres) torna o envio idempotente: repetir o mesmo id não duplica a mensagem nem a resposta automática. O agente ou fluxo do chat responde pela sessão em tempo real. Limites por visitante e por IP (429 rate_limited).
// @Tags			WebChat
// @Accept			json
// @Produce		json
// @Param			body	body		MessageRequest	true	"mensagem"
// @Success		201		{object}	MessageResponse
// @Failure		409		{object}	response.CodedErrorResponse
// @Failure		422		{object}	response.CodedErrorResponse
// @Failure		429		{object}	response.CodedErrorResponse
// @Security		BearerAuth
// @Router			/public/webchat/session/messages [post]
func (h *PublicHandler) Send(w http.ResponseWriter, r *http.Request) {
	s, ok := h.session(w, r)
	if !ok {
		return
	}
	var req MessageRequest
	if !decodeBody(w, r, maxVisitorBody, &req) {
		return
	}
	message, err := h.visitors.Send(r.Context(), s, wcuc.SendInput{
		ClientMessageID: req.ClientMessageID,
		Text:            req.Text,
		SelectionID:     req.SelectionID,
		PageURL:         req.PageURL,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusCreated, toMessageResponse(*message))
}

// @Summary		Enviar arquivo do visitante
// @Description	multipart/form-data com file, clientMessageId e pageUrl. Só quando o chat aceita anexos: JPEG, PNG, WebP, GIF ou PDF, até 10 MB. O tipo é conferido pelo conteúdo, não pelo nome.
// @Tags			WebChat
// @Accept			multipart/form-data
// @Produce		json
// @Param			file			formData	file	true	"arquivo"
// @Param			clientMessageId	formData	string	true	"id do envio"
// @Success		201				{object}	MessageResponse
// @Failure		403				{object}	response.CodedErrorResponse
// @Failure		413				{object}	response.CodedErrorResponse
// @Failure		415				{object}	response.CodedErrorResponse
// @Security		BearerAuth
// @Router			/public/webchat/session/media [post]
func (h *PublicHandler) Upload(w http.ResponseWriter, r *http.Request) {
	s, ok := h.session(w, r)
	if !ok {
		return
	}
	if !s.Widget.AllowAttachments {
		writeError(w, wcdomain.ErrAttachmentsDisabled)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, wcdomain.MaxAttachmentBytes+uploadOverhead)
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		writeError(w, wcdomain.ErrAttachmentTooLarge)
		return
	}
	defer r.MultipartForm.RemoveAll()
	file, header, err := r.FormFile("file")
	if err != nil {
		response.WriteCodedError(w, http.StatusBadRequest, "invalid_body", "file is required")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, wcdomain.MaxAttachmentBytes+1))
	if err != nil {
		writeError(w, wcdomain.ErrAttachmentTooLarge)
		return
	}
	message, err := h.visitors.Upload(r.Context(), s, wcuc.UploadInput{
		ClientMessageID: r.FormValue("clientMessageId"),
		FileName:        header.Filename,
		Data:            data,
		PageURL:         r.FormValue("pageUrl"),
	})
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusCreated, toMessageResponse(*message))
}

// @Summary		Pedir atendimento humano
// @Description	Disponível quando o chat oferece falar com uma pessoa: pausa o agente ou fluxo e passa a conversa para a fila do departamento do chat.
// @Tags			WebChat
// @Success		204
// @Failure		403	{object}	response.CodedErrorResponse
// @Failure		404	{object}	response.CodedErrorResponse
// @Security		BearerAuth
// @Router			/public/webchat/session/handoff [post]
func (h *PublicHandler) RequestHuman(w http.ResponseWriter, r *http.Request) {
	s, ok := h.session(w, r)
	if !ok {
		return
	}
	if err := h.visitors.RequestHuman(r.Context(), s); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// @Summary		Visitante digitando
// @Description	Mostra aos atendentes que o visitante está digitando.
// @Tags			WebChat
// @Accept			json
// @Param			body	body	TypingRequest	true	"digitando ou não"
// @Success		204
// @Security		BearerAuth
// @Router			/public/webchat/session/typing [post]
func (h *PublicHandler) Typing(w http.ResponseWriter, r *http.Request) {
	s, ok := h.session(w, r)
	if !ok {
		return
	}
	var req TypingRequest
	if !decodeBody(w, r, maxVisitorBody, &req) {
		return
	}
	if err := h.visitors.Typing(r.Context(), s, req.Typing); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// @Summary		Eventos em tempo real do visitante
// @Description	Server-Sent Events lidos com fetch (o token vai no cabeçalho Authorization, nunca na URL).
// @Description
// @Description	| evento | dados |
// @Description	|---|---|
// @Description	| message | { kind: "message", message: { id, author: visitor, team ou assistant, text, media, options, createdAt } } |
// @Description	| typing | { kind: "typing", typing } |
// @Description	| status | { kind: "status", state: human ou blocked } |
// @Description	| ping | a cada 20 s |
// @Description
// @Description	A conexão fecha depois de 30 minutos; o chat reconecta e busca o que perdeu com GET .../messages?after=. No máximo 3 conexões por visitante (429).
// @Tags			WebChat
// @Produce		text/event-stream
// @Success		200
// @Failure		401	{object}	response.CodedErrorResponse
// @Failure		429	{object}	response.CodedErrorResponse
// @Security		BearerAuth
// @Router			/public/webchat/session/stream [get]
func (h *PublicHandler) Stream(w http.ResponseWriter, r *http.Request) {
	s, ok := h.session(w, r)
	if !ok {
		return
	}
	events, cancel, err := h.stream.Subscribe(s.Visitor.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	defer cancel()

	controller := http.NewResponseController(w)
	header := w.Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-store")
	header.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	if err := writeEvent(w, controller, "ping", map[string]bool{"ok": true}); err != nil {
		return
	}

	heartbeat := time.NewTicker(heartbeatEvery)
	defer heartbeat.Stop()
	lifetime := time.NewTimer(streamLifetime)
	defer lifetime.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-lifetime.C:
			return
		case <-heartbeat.C:
			if err := writeEvent(w, controller, "ping", map[string]bool{"ok": true}); err != nil {
				return
			}
		case event, open := <-events:
			if !open {
				return
			}
			if err := writeEvent(w, controller, string(event.Kind), event); err != nil {
				return
			}
		}
	}
}

func writeEvent(w http.ResponseWriter, controller *http.ResponseController, name string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := io.WriteString(w, "event: "+name+"\ndata: "+string(data)+"\n\n"); err != nil {
		return err
	}
	if err := controller.Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return err
	}
	return nil
}
