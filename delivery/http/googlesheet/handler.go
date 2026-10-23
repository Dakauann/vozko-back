package googlesheet

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/domain/googleaccount"
	"vozko/domain/googlesheet"
	"vozko/domain/shared"
	"vozko/infra/http/middleware"
	gsuc "vozko/usecases/googlesheet"
)

const maxRequestBody = 256 << 10

type Sheets interface {
	ListRows(ctx context.Context, workspaceID, id string, pagination shared.Pagination) (*shared.PaginatedResult[*googlesheet.Row], error)
	Create(ctx context.Context, in gsuc.CreateInput) (*googlesheet.Sheet, error)
	Get(ctx context.Context, workspaceID, id string) (*googlesheet.Sheet, error)
	List(ctx context.Context, in googlesheet.ListInput) (*shared.PaginatedResult[*googlesheet.Sheet], error)
	UpdateDetails(ctx context.Context, in gsuc.DetailsInput) (*googlesheet.Sheet, error)
	EditColumns(ctx context.Context, in gsuc.ColumnsInput) (*googlesheet.Sheet, error)
	Delete(ctx context.Context, workspaceID, id string) error
	Health(ctx context.Context, workspaceID, id string) (*gsuc.HealthView, error)
	RetryProblems(ctx context.Context, workspaceID, id string) (int, error)
	Reactivate(ctx context.Context, in gsuc.ReactivateInput) (*googlesheet.Sheet, error)
}

func (h *Handler) ListRows(w http.ResponseWriter, r *http.Request) {
	page, err := h.sheets.ListRows(r.Context(), middleware.GetWorkspaceID(r), mux.Vars(r)["id"], httpx.ParsePagination(r.URL.Query()))
	if err != nil {
		writeError(w, err)
		return
	}
	items := make([]RowResponse, 0, len(page.Items))
	for _, row := range page.Items {
		items = append(items, RowResponse{ID: row.ID, Values: row.Cells, Status: string(row.Status), RowNumber: row.RowNumber, Origin: string(row.Origin.Kind), CreatedAt: row.CreatedAt, WrittenAt: row.WrittenAt, ErrorCode: row.ErrorCode})
	}
	response.WriteSuccess(w, http.StatusOK, shared.PaginatedResult[RowResponse]{Items: items, Page: page.Page, PageSize: page.PageSize, TotalItems: page.TotalItems, TotalPages: page.TotalPages})
}

type Rows interface {
	Enqueue(ctx context.Context, in gsuc.EnqueueInput) (*gsuc.EnqueueResult, error)
}

type Handler struct {
	sheets Sheets
	rows   Rows
}

func NewHandler(sheets Sheets, rows Rows) *Handler {
	if sheets == nil || rows == nil {
		panic("googlesheet http: the handler requires sheets and rows")
	}
	return &Handler{sheets: sheets, rows: rows}
}

// @Summary		Catálogo das colunas de planilha
// @Description	Tipos de coluna, valores preenchidos pelo sistema (com o tipo que cada um exige), limites, códigos de erro das linhas e motivos de atenção. A tela de colunas usa este catálogo em vez de repetir as regras.
// @Tags			Google Planilhas
// @Produce		json
// @Success		200	{object}	CatalogResponse
// @Security		BearerAuth
// @Router			/google-sheets/catalog [get]
func (h *Handler) Catalog(w http.ResponseWriter, _ *http.Request) {
	response.WriteSuccess(w, http.StatusOK, catalog())
}

// @Summary		Listar planilhas
// @Tags			Google Planilhas
// @Produce		json
// @Param			search		query		string	false	"Busca pelo título"
// @Param			page		query		int		false	"Página"
// @Param			pageSize	query		int		false	"Tamanho da página"
// @Success		200			{object}	SheetListResponse
// @Security		BearerAuth
// @Router			/google-sheets [get]
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, err := h.sheets.List(r.Context(), googlesheet.ListInput{
		WorkspaceID: middleware.GetWorkspaceID(r),
		Search:      strings.TrimSpace(q.Get("search")),
		Pagination:  httpx.ParsePagination(q),
	})
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toListResponse(page))
}

// @Summary		Criar planilha
// @Description	Cria a planilha no Google Drive da conta conectada ao Google Planilhas, com o cabeçalho, os formatos e as validações das colunas, e a registra no workspace.
// @Tags			Google Planilhas
// @Accept			json
// @Produce		json
// @Param			request	body		CreateSheetRequest	true	"Planilha"
// @Success		201		{object}	SheetEnvelope
// @Failure		400		{object}	response.ErrorResponse
// @Failure		412		{object}	response.ErrorResponse
// @Failure		429		{object}	response.ErrorResponse
// @Failure		502		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/google-sheets [post]
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateSheetRequest
	if !decode(w, r, &req) {
		return
	}
	userID := ""
	if claims := middleware.GetClaims(r); claims != nil {
		userID = claims.UserID
	}
	sheet, err := h.sheets.Create(r.Context(), gsuc.CreateInput{
		WorkspaceID: middleware.GetWorkspaceID(r), UserID: userID, Title: req.Title, TabName: req.TabName,
		Description: req.Description, TimeZone: req.TimeZone, Columns: inputs(req.Columns),
	})
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusCreated, SheetEnvelope{Sheet: toSheetResponse(sheet)})
}

// @Summary		Consultar planilha
// @Tags			Google Planilhas
// @Produce		json
// @Param			id	path		string	true	"Planilha"
// @Success		200	{object}	SheetEnvelope
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/google-sheets/{id} [get]
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	sheet, err := h.sheets.Get(r.Context(), middleware.GetWorkspaceID(r), mux.Vars(r)["id"])
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, SheetEnvelope{Sheet: toSheetResponse(sheet)})
}

// @Summary		Editar título e descrição
// @Description	Muda o nome e a descrição da planilha no painel. O arquivo no Google Drive mantém o nome que tem lá.
// @Tags			Google Planilhas
// @Accept			json
// @Produce		json
// @Param			id		path		string					true	"Planilha"
// @Param			request	body		UpdateDetailsRequest	true	"Alterações"
// @Success		200		{object}	SheetEnvelope
// @Failure		409		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/google-sheets/{id} [patch]
func (h *Handler) UpdateDetails(w http.ResponseWriter, r *http.Request) {
	var req UpdateDetailsRequest
	if !decode(w, r, &req) {
		return
	}
	sheet, err := h.sheets.UpdateDetails(r.Context(), gsuc.DetailsInput{
		WorkspaceID: middleware.GetWorkspaceID(r), ID: mux.Vars(r)["id"], Version: req.Version, Title: req.Title, Description: req.Description,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, SheetEnvelope{Sheet: toSheetResponse(sheet)})
}

// @Summary		Editar colunas
// @Description	Adiciona colunas ao fim, renomeia cabeçalhos, muda instruções e obrigatoriedade, arquiva colunas e acrescenta opções. O cabeçalho no Google é atualizado na mesma operação. Tipo e chave de uma coluna não mudam.
// @Tags			Google Planilhas
// @Accept			json
// @Produce		json
// @Param			id		path		string				true	"Planilha"
// @Param			request	body		EditColumnsRequest	true	"Alterações"
// @Success		200		{object}	SheetEnvelope
// @Failure		400		{object}	response.ErrorResponse
// @Failure		409		{object}	response.ErrorResponse
// @Failure		502		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/google-sheets/{id}/columns [patch]
func (h *Handler) EditColumns(w http.ResponseWriter, r *http.Request) {
	var req EditColumnsRequest
	if !decode(w, r, &req) {
		return
	}
	sheet, err := h.sheets.EditColumns(r.Context(), gsuc.ColumnsInput{
		WorkspaceID: middleware.GetWorkspaceID(r), ID: mux.Vars(r)["id"], Version: req.Version, Edit: req.edit(),
	})
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, SheetEnvelope{Sheet: toSheetResponse(sheet)})
}

// @Summary		Remover planilha
// @Description	Remove a planilha do workspace e descarta as linhas que ainda não foram gravadas. O arquivo continua no Google Drive.
// @Tags			Google Planilhas
// @Produce		json
// @Param			id	path		string	true	"Planilha"
// @Success		200	{object}	DeletedResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/google-sheets/{id} [delete]
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	if err := h.sheets.Delete(r.Context(), middleware.GetWorkspaceID(r), mux.Vars(r)["id"]); err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, DeletedResponse{Deleted: true})
}

// @Summary		Saúde da planilha
// @Description	Linhas aguardando, em envio, retidas, recusadas e gravadas nos últimos dias, com as linhas com problema mais recentes.
// @Tags			Google Planilhas
// @Produce		json
// @Param			id	path		string	true	"Planilha"
// @Success		200	{object}	HealthResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/google-sheets/{id}/health [get]
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	view, err := h.sheets.Health(r.Context(), middleware.GetWorkspaceID(r), mux.Vars(r)["id"])
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toHealthResponse(view))
}

// @Summary		Reenviar linhas com problema
// @Description	Devolve à fila as linhas retidas e recusadas da planilha. A planilha precisa estar ativa.
// @Tags			Google Planilhas
// @Produce		json
// @Param			id	path		string	true	"Planilha"
// @Success		200	{object}	RetryResponse
// @Failure		409	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/google-sheets/{id}/retry [post]
func (h *Handler) Retry(w http.ResponseWriter, r *http.Request) {
	requeued, err := h.sheets.RetryProblems(r.Context(), middleware.GetWorkspaceID(r), mux.Vars(r)["id"])
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, RetryResponse{Requeued: requeued})
}

// @Summary		Reativar planilha
// @Description	Confere o acesso, a aba e o cabeçalho no Google e volta a gravar. Com repairHeader=true reescreve o cabeçalho a partir das colunas do painel.
// @Tags			Google Planilhas
// @Accept			json
// @Produce		json
// @Param			id		path		string				true	"Planilha"
// @Param			request	body		ReactivateRequest	true	"Opções"
// @Success		200		{object}	SheetEnvelope
// @Failure		409		{object}	response.ErrorResponse
// @Failure		502		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/google-sheets/{id}/reactivate [post]
func (h *Handler) Reactivate(w http.ResponseWriter, r *http.Request) {
	var req ReactivateRequest
	if !decode(w, r, &req) {
		return
	}
	sheet, err := h.sheets.Reactivate(r.Context(), gsuc.ReactivateInput{
		WorkspaceID: middleware.GetWorkspaceID(r), ID: mux.Vars(r)["id"], RepairHeader: req.RepairHeader,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, SheetEnvelope{Sheet: toSheetResponse(sheet)})
}

// @Summary		Adicionar linha
// @Description	Valida os valores com as mesmas regras de agentes e fluxos e põe a linha na fila de gravação. O cabeçalho Idempotency-Key é obrigatório; repetir a chave devolve a mesma linha.
// @Tags			Google Planilhas
// @Accept			json
// @Produce		json
// @Param			id				path		string			true	"Planilha"
// @Param			Idempotency-Key	header		string			true	"Chave única da operação"
// @Param			request			body		AddRowRequest	true	"Valores por chave de coluna"
// @Success		202				{object}	AddRowResponse
// @Failure		400				{object}	response.ErrorResponse
// @Failure		422				{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/google-sheets/{id}/rows [post]
func (h *Handler) AddRow(w http.ResponseWriter, r *http.Request) {
	var req AddRowRequest
	if !decode(w, r, &req) {
		return
	}
	userID := ""
	if claims := middleware.GetClaims(r); claims != nil {
		userID = claims.UserID
	}
	clientKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if clientKey == "" {
		writeError(w, googlesheet.ErrIdempotencyKeyRequired)
		return
	}
	result, err := h.rows.Enqueue(r.Context(), gsuc.EnqueueInput{
		WorkspaceID:    middleware.GetWorkspaceID(r),
		SheetID:        mux.Vars(r)["id"],
		Values:         req.Values,
		IdempotencyKey: googlesheet.KeyForManual(userID, clientKey),
		Origin:         googlesheet.Origin{Kind: googlesheet.OriginManual, ActorID: userID},
	})
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusAccepted, AddRowResponse{RowID: result.RowID, Duplicate: result.Duplicate})
}

func decode(w http.ResponseWriter, r *http.Request, into any) bool {
	decoder := json.NewDecoder(io.LimitReader(r.Body, maxRequestBody))
	decoder.UseNumber()
	if err := decoder.Decode(into); err != nil {
		response.WriteErrorWithCode(w, http.StatusBadRequest, "invalid_body", "Corpo da requisição inválido", nil)
		return false
	}
	return true
}

func refusal(status int, code, message string) response.Refusal {
	return response.Refusal{Status: status, Code: code, Message: message}
}

var refusals = []response.ErrorRefusal{
	{Err: googleaccount.ErrNotConnected, Refusal: refusal(http.StatusPreconditionFailed, googleaccount.CodeNotConnected, "Conecte o Google Planilhas antes")},
	{Err: googlesheet.ErrSheetNotFound, Refusal: refusal(http.StatusNotFound, googlesheet.CodeSheetNotFound, "Planilha não encontrada")},
	{Err: googlesheet.ErrStaleSheet, Refusal: refusal(http.StatusConflict, googlesheet.CodeStaleSheet, "A planilha mudou; recarregue e tente de novo")},
	{Err: googlesheet.ErrSheetNotActive, Refusal: refusal(http.StatusConflict, googlesheet.CodeSheetNotActive, "A planilha precisa de atenção antes de receber linhas")},
	{Err: googlesheet.ErrAccountChanged, Refusal: refusal(http.StatusConflict, googlesheet.CodeAccountChanged, "A conta Google conectada não é a que criou a planilha")},
	{Err: googlesheet.ErrGoogleBusy, Refusal: refusal(http.StatusTooManyRequests, googlesheet.CodeGoogleBusy, "O limite do Google para este minuto foi atingido; tente de novo em um minuto")},
	{Err: googlesheet.ErrInvalidSheet, Refusal: refusal(http.StatusBadRequest, googlesheet.CodeInvalidSheet, "Definição de planilha inválida")},
	{Err: googlesheet.ErrIdempotencyKeyRequired, Refusal: refusal(http.StatusBadRequest, googlesheet.CodeIdempotencyKeyRequired, "Informe o cabeçalho Idempotency-Key")},
	{Err: googlesheet.ErrRowTooLarge, Refusal: refusal(http.StatusRequestEntityTooLarge, googlesheet.CodeRowTooLarge, "A linha é grande demais para uma gravação")},
}

type valueRefusal struct {
	Column string `json:"column"`
	Header string `json:"header"`
	Detail string `json:"detail,omitempty"`
}

type googleRefusal struct {
	Failure string `json:"failure"`
	Detail  string `json:"detail,omitempty"`
}

func writeError(w http.ResponseWriter, err error) {
	var valueErr *googlesheet.ValueError
	if errors.As(err, &valueErr) {
		response.WriteErrorWithDetails(w, http.StatusUnprocessableEntity, googlesheet.ErrorCode(err), valueErr.Error(),
			valueRefusal{Column: valueErr.Column, Header: valueErr.Header, Detail: valueErr.Detail})
		return
	}
	var googleErr *gsuc.GoogleRefusal
	if errors.As(err, &googleErr) {
		response.WriteErrorWithDetails(w, http.StatusBadGateway, googleErr.Failure.Code(), "O Google recusou a operação",
			googleRefusal{Failure: string(googleErr.Failure.Kind), Detail: googleErr.Failure.Detail})
		return
	}
	if response.WriteErrorRefusal(w, err, refusals) {
		return
	}
	log.Printf("[googlesheet] request failed: %v", err)
	response.WriteErrorWithCode(w, http.StatusInternalServerError, googlesheet.CodeInternal, "Não foi possível concluir a operação com a planilha", nil)
}

func RefusalCodes() []string {
	return append(response.RefusalCodes(response.ErrorRefusalsOf(refusals)), googlesheet.CodeInternal, "invalid_body")
}
