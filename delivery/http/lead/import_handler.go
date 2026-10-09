package lead

import (
	"context"
	"encoding/csv"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	leaddomain "vozko/domain/lead"
	"vozko/domain/leadimport"
	lead_usecase "vozko/usecases/lead"
)

const (
	importMultipartOverhead = 1 << 20
	rejectionsPage          = 5000
)

type Imports interface {
	Upload(ctx context.Context, a lead_usecase.Actor, in lead_usecase.UploadInput) (*leadimport.Job, error)
	Detail(ctx context.Context, a lead_usecase.Actor, id string) (lead_usecase.ImportDetail, error)
	List(ctx context.Context, a lead_usecase.Actor) ([]leadimport.Job, error)
	Catalog(ctx context.Context, a lead_usecase.Actor) (lead_usecase.ImportCatalog, error)
	DryRun(ctx context.Context, a lead_usecase.Actor, id string, s leadimport.Settings) (*leadimport.Job, error)
	Start(ctx context.Context, a lead_usecase.Actor, id string) (*leadimport.Job, error)
	Rejections(ctx context.Context, a lead_usecase.Actor, id string, after int64, limit int) ([]leadimport.IssueRow, error)
}

func (h *LeadHandler) importActor(w http.ResponseWriter, r *http.Request) (lead_usecase.Actor, bool) {
	if h.imports == nil {
		response.WriteErrorWithCode(w, http.StatusServiceUnavailable, leadimport.ErrorCode(leadimport.ErrUnavailable), "A importação de leads não está disponível neste servidor", nil)
		return lead_usecase.Actor{}, false
	}
	return h.requestActor(w, r)
}

func writeImportError(w http.ResponseWriter, a lead_usecase.Actor, importID string, err error) {
	if writeImportRefusal(w, err) {
		return
	}
	attrs := []any{"workspace_id", a.WorkspaceID, "user_id", a.UserID}
	if importID != "" {
		attrs = append(attrs, "import_id", importID)
	}
	slog.Error("lead import: request failed", append(attrs, "error", err)...)
	response.WriteError(w, http.StatusInternalServerError, "A importação não pôde ser concluída", nil)
}

func writeImportRefusal(w http.ResponseWriter, err error) bool {
	code := leadimport.ErrorCode(err)
	var refusal *leadimport.PermissionError
	var mapping *leadimport.MappingError
	switch {
	case errors.As(err, &refusal):
		response.WriteErrorWithCode(w, http.StatusForbidden, code, err.Error(), map[string]string{"permission": refusal.Permission()})
	case errors.As(err, &mapping):
		expected := map[string]string{"rule": mapping.Rule, "field": mapping.Field}
		if mapping.Column >= 0 {
			expected["column"] = strconv.Itoa(mapping.Column)
		}
		response.WriteErrorWithCode(w, http.StatusBadRequest, code, err.Error(), expected)
	case errors.Is(err, leadimport.ErrNotFound), errors.Is(err, leadimport.ErrFileUnavailable):
		response.WriteErrorWithCode(w, http.StatusNotFound, code, err.Error(), nil)
	case errors.Is(err, leadimport.ErrRunning), errors.Is(err, leadimport.ErrNotReady):
		response.WriteErrorWithCode(w, http.StatusConflict, code, err.Error(), nil)
	case errors.Is(err, leadimport.ErrFileTooLarge), errors.Is(err, leadimport.ErrTooManyRows):
		response.WriteErrorWithCode(w, http.StatusRequestEntityTooLarge, code, err.Error(), map[string]string{
			"maxBytes": strconv.Itoa(leadimport.MaxFileBytes), "maxRows": strconv.Itoa(leadimport.MaxRows)})
	case errors.Is(err, leadimport.ErrFileEmpty), errors.Is(err, leadimport.ErrUnsupportedFile):
		response.WriteErrorWithCode(w, http.StatusBadRequest, code, err.Error(), nil)
	default:
		return false
	}
	return true
}

func (h *LeadHandler) importView(w http.ResponseWriter, r *http.Request, a lead_usecase.Actor, detail lead_usecase.ImportDetail, status int) {
	catalog, err := h.imports.Catalog(r.Context(), a)
	if err != nil {
		var importID string
		if detail.Job != nil {
			importID = detail.Job.ID
		}
		writeImportError(w, a, importID, err)
		return
	}
	response.WriteSuccess(w, status, toLeadImportResponse(detail, catalog))
}

// @Summary		Enviar uma planilha de leads
// @Description	Primeiro passo da importação: recebe a planilha (multipart com o campo `file`, CSV ou TSV de até 20 MB e 200.000 linhas) ou o `mediaId` de um arquivo que já está na biblioteca (JSON `{mediaId}`, o caminho do Elo e das campanhas; exige também `media:read`, e só arquivos que aparecem na biblioteca servem, nunca a planilha guardada de outra importação). O arquivo vai para o armazenamento de mídias sem aparecer na biblioteca e é apagado com a importação após 7 dias. A resposta traz as colunas lidas, até 5 linhas de amostra, a sugestão de destino de cada coluna (`preview.columns[].field`, vazio quando nenhuma serve) e o catálogo de campos (`fields`): número de WhatsApp, até quatro telefones de contato (`phone:mobile`, `phone:landline`, `phone:work`, `phone:message`, `phone:other`), nome, apelido, e-mail, nascimento, CEP, logradouro, número, complemento, bairro, cidade, UF, latitude e longitude, responsável por e-mail, data e finalidade do consentimento, cada campo personalizado de lead (`custom_field:<chave>`) e o vínculo familiar (`relative_number` com `relation_kind`). Cada campo diz se você pode mapeá-lo (`allowed`) e qual permissão exige (`requires`); `options` diz se você pode completar leads existentes (`fillEmpty`, exige `leads:update`) e abrir conversas (`seedInbox`, `seedConversations`). Só quem enviou vê a importação. Cada pessoa guarda no máximo 5 envios ainda não importados (enviados, simulados ou que falharam antes de começar); um envio novo apaga o mais antigo deles, com o arquivo. O envio passa pelo mesmo limite de frequência dos uploads de mídia (429). Códigos: 400 `lead_import_empty`, `lead_import_unsupported_file`; 403 `lead_import_forbidden` com `expected.permission` (`media:read`); 404 `lead_import_file_unavailable`; 413 `lead_import_file_too_large`, `lead_import_too_many_rows`.
// @Tags			Leads
// @Accept			mpfd
// @Accept			json
// @Produce		json
// @Param			file		formData	file							false	"Planilha CSV ou TSV"
// @Param			request		body		lead.CreateLeadImportRequest	false	"Arquivo da biblioteca de mídias"
// @Success		201			{object}	lead.LeadImportResponse
// @Failure		400			{object}	response.ErrorResponse
// @Failure		403			{object}	response.ErrorResponse
// @Failure		404			{object}	response.ErrorResponse
// @Failure		413			{object}	response.ErrorResponse
// @Failure		429			{object}	response.ErrorResponse
// @Failure		500			{object}	response.ErrorResponse
// @Failure		503			{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/leads/imports [post]
func (h *LeadHandler) CreateImport(w http.ResponseWriter, r *http.Request) {
	a, ok := h.importActor(w, r)
	if !ok {
		return
	}
	in, ok := importUpload(w, r)
	if !ok {
		return
	}
	job, err := h.imports.Upload(r.Context(), a, in)
	if err != nil {
		writeImportError(w, a, "", err)
		return
	}
	h.importView(w, r, a, lead_usecase.ImportDetail{Job: job}, http.StatusCreated)
}

func importUpload(w http.ResponseWriter, r *http.Request) (lead_usecase.UploadInput, bool) {
	mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mediaType != "multipart/form-data" {
		var req CreateLeadImportRequest
		if !httpx.DecodeStrictJSON(w, r, &req) {
			return lead_usecase.UploadInput{}, false
		}
		if strings.TrimSpace(req.MediaID) == "" {
			response.WriteErrorWithCode(w, http.StatusBadRequest, httpx.CodeInvalidBody, "send the sheet as the multipart field file or a mediaId", nil)
			return lead_usecase.UploadInput{}, false
		}
		return lead_usecase.UploadInput{MediaID: req.MediaID, FileName: req.FileName}, true
	}
	r.Body = http.MaxBytesReader(w, r.Body, leadimport.MaxFileBytes+importMultipartOverhead)
	file, header, err := r.FormFile("file")
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeImportRefusal(w, leadimport.ErrFileTooLarge)
			return lead_usecase.UploadInput{}, false
		}
		response.WriteErrorWithCode(w, http.StatusBadRequest, httpx.CodeInvalidBody, "send the sheet as the multipart field file", nil)
		return lead_usecase.UploadInput{}, false
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, leadimport.MaxFileBytes+1))
	if err != nil {
		response.WriteErrorWithCode(w, http.StatusBadRequest, httpx.CodeInvalidBody, "the sheet could not be read", nil)
		return lead_usecase.UploadInput{}, false
	}
	if len(data) > leadimport.MaxFileBytes {
		writeImportRefusal(w, leadimport.ErrFileTooLarge)
		return lead_usecase.UploadInput{}, false
	}
	return lead_usecase.UploadInput{Data: data, FileName: header.Filename}, true
}

// @Summary		Listar as suas importações de leads
// @Description	Devolve as importações que você enviou e que ainda não expiraram (7 dias), da mais nova para a mais antiga, no máximo 20, com o andamento e as contagens de cada uma (sem a prévia nem o catálogo de campos: abra cada uma por GET /leads/imports/{id}). Nunca lista importações de outra pessoa: `mine` é opcional e aceita a forma sem valor (`?mine`) ou `true` (outro valor dá 400 `lead_import_only_mine`). `limits` traz os limites do servidor para a tela de envio: tamanho máximo do arquivo em bytes e em MB, linhas por arquivo, conversas roteirizadas por importação, dias de guarda e envios ainda não importados guardados por pessoa.
// @Tags			Leads
// @Produce		json
// @Param			mine	query		bool	false	"Só as suas importações (sem valor ou true)"
// @Success		200		{object}	lead.LeadImportListResponse
// @Failure		400		{object}	response.ErrorResponse
// @Failure		403		{object}	response.ErrorResponse
// @Failure		500		{object}	response.ErrorResponse
// @Failure		503		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/leads/imports [get]
func (h *LeadHandler) ListImports(w http.ResponseWriter, r *http.Request) {
	a, ok := h.importActor(w, r)
	if !ok {
		return
	}
	if values, given := r.URL.Query()["mine"]; given && (len(values) != 1 || (values[0] != "" && values[0] != "true")) {
		response.WriteErrorWithCode(w, http.StatusBadRequest, leadimport.ErrorCode(leadimport.ErrOnlyMine), leadimport.ErrOnlyMine.Error(), map[string]string{"mine": "true"})
		return
	}
	jobs, err := h.imports.List(r.Context(), a)
	if err != nil {
		writeImportError(w, a, "", err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toLeadImportListResponse(jobs, leadimport.CurrentLimits()))
}

// @Summary		Ver uma importação de leads
// @Description	Devolve a importação para quem a enviou: a prévia e o catálogo de campos, o mapeamento escolhido (`settings`), as contagens da simulação (`dryRun`), o andamento (`status`, `stage`, `processed` de `totalRows`) e o resultado (`result`: criados, completados, sem mudança, pulados, recusados, com conflito, bloqueados, endereços adicionados e posicionados, endereços existentes completados em `addressesFilled`, vínculos de família planejados e criados, linhas sem nenhuma coluna de endereço preenchida em `noAddress` (uma linha com endereço inválido não entra aqui, ela aparece em `issues`; `noAddress` fica de fora nas importações anteriores a essa contagem), e `issues` por motivo), mais o resultado da abertura de conversas (`seed`: conversas enfileiradas em `queued`; `unconfirmed` conta as que podem ou não ter chegado à fila antes de um reinício do servidor e nunca são enviadas de novo). As conversas são enfileiradas em lotes e cada lote é registrado antes de sair, para que um reinício nunca abra a mesma conversa duas vezes. Quando a importação termina (`done`), `placement` diz onde estão agora os endereços principais que ela trouxe ou completou, contando só leads que não foram apagados, com as mesmas regras do mapa: no mapa (posição de casa ou rua), aproximados (CEP amplo, bairro ou cidade), aguardando posição, não encontrados, sem cota de geocodificação no mês e recusados pelo provedor. Um endereço completado por uma importação posterior passa a contar nela. Importações anteriores a essa contagem não trazem `placement`. A importação continua no servidor mesmo com a janela fechada; consulte esta rota para acompanhar. Outra pessoa recebe 404. Depois de 7 dias a importação, o arquivo e as linhas recusadas são apagados.
// @Tags			Leads
// @Produce		json
// @Param			id	path		string	true	"ID da importação"
// @Success		200	{object}	lead.LeadImportResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Failure		503	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/leads/imports/{id} [get]
func (h *LeadHandler) GetImport(w http.ResponseWriter, r *http.Request) {
	a, ok := h.importActor(w, r)
	if !ok {
		return
	}
	id := mux.Vars(r)["id"]
	detail, err := h.imports.Detail(r.Context(), a, id)
	if err != nil {
		writeImportError(w, a, id, err)
		return
	}
	h.importView(w, r, a, detail, http.StatusOK)
}

// @Summary		Simular uma importação de leads
// @Description	Grava o mapeamento das colunas (`columns[]{index, field}`, colunas ausentes ou com `field` vazio são ignoradas) e a regra para leads que já existem (`onExisting`: `fill_empty`, o padrão, só completa o que está vazio e nunca troca um valor existente, como um nome dado pela equipe, um endereço ou um pino; `skip` não toca em leads existentes), e roda no servidor uma simulação sem gravar nada. Acompanhe por GET /leads/imports/{id}: em `analyzed`, `dryRun` traz quantos leads seriam criados, completados (com endereço, telefone ou campo novo), mantidos, pulados, recusados e com conflito, e quantos vínculos de família seriam criados. Cada coluna exige sua permissão (403 `lead_import_forbidden` com `expected.permission`): responsável exige `leads:assign`, campo sensível exige `leads:read_sensitive`, endereço e coordenadas exigem `leads:read_addresses`, vínculo familiar e `fill_empty` exigem `leads:update`. Mapeamentos inválidos dão 400 `lead_import_mapping_invalid` com `expected.rule` (`unknown_column`, `unknown_field`, `repeated`, `too_many_phones`, `identity_required`, `needs_pair`, `policy`, `script`). Só uma importação roda por vez no workspace (409 `lead_import_running`). `seedInbox` abre uma conversa no atendimento para cada número importado (exige `unofficial_whatsapp_instances:send`; sem ela, a importação segue e `seed.error` explica).
// @Tags			Leads
// @Accept			json
// @Produce		json
// @Param			id		path		string							true	"ID da importação"
// @Param			request	body		lead.LeadImportDryRunRequest	true	"Mapeamento e regras"
// @Success		202		{object}	lead.LeadImportResponse
// @Failure		400		{object}	response.ErrorResponse
// @Failure		403		{object}	response.ErrorResponse
// @Failure		404		{object}	response.ErrorResponse
// @Failure		409		{object}	response.ErrorResponse
// @Failure		503		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/leads/imports/{id}/dry-run [post]
func (h *LeadHandler) DryRunImport(w http.ResponseWriter, r *http.Request) {
	a, ok := h.importActor(w, r)
	if !ok {
		return
	}
	var req LeadImportDryRunRequest
	if !httpx.DecodeStrictJSON(w, r, &req) {
		return
	}
	policy, ok := leaddomain.ParseExistingPolicy(req.OnExisting)
	if !ok {
		policy = leaddomain.ExistingPolicy(req.OnExisting)
	}
	settings := leadimport.Settings{Policy: policy, SeedInbox: req.SeedInbox, Script: req.SeedConversations.toDomain()}
	for _, c := range req.Columns {
		settings.Columns = append(settings.Columns, leadimport.Column{Index: c.Index, Field: c.Field})
	}
	id := mux.Vars(r)["id"]
	job, err := h.imports.DryRun(r.Context(), a, id, settings)
	if err != nil {
		writeImportError(w, a, id, err)
		return
	}
	h.importView(w, r, a, lead_usecase.ImportDetail{Job: job}, http.StatusAccepted)
}

// @Summary		Iniciar uma importação de leads
// @Description	Começa a importação com o mapeamento da última simulação concluída (409 `lead_import_not_ready` antes dela). As permissões são conferidas de novo aqui e no servidor que executa. A importação roda em lotes de 500 linhas, cada lote numa transação, e continua do último lote gravado se o servidor reiniciar ou se um erro passageiro (banco fora do ar, tempo esgotado) interromper; depois de 3 tentativas ela para com `failureCode` `stalled`. Permissão retirada, arquivo apagado ou mapeamento que deixou de servir param na hora (`forbidden`, `file_unavailable`, `internal`); uma importação da rota antiga interrompida no meio para com `interrupted`. Leads novos entram com origem `import`; leads existentes só ganham o que estava vazio (o nome que veio do canal, como o perfil do WhatsApp, é trocado pelo da planilha; um nome dado pela equipe ou por outra importação nunca), e cada mudança aumenta a `version` do lead (a importação é a exceção documentada ao `If-Match`, porque nunca troca um valor existente). Telefones novos entram como contato; um endereço entra como principal quando o lead não tem nenhum, com posição exata quando a linha traz latitude e longitude válidas dentro do Brasil, e pendente para o mapa nos outros casos; quando o lead já tem endereço, só as partes vazias do principal são completadas (uma parte diferente vira conflito e nada muda) e um pino manual nunca é trocado. Telefones compartilhados por mais de 5.000 leads não servem para achar a pessoa: a linha sem WhatsApp é recusada com `contact_ambiguous`. Um vínculo de família que já existe entre os dois é informado com `relation_exists`. Um lead criado sem um campo personalizado obrigatório que você vê é informado com `custom_field_required`. Os vínculos de família são resolvidos depois das linhas: `relative_number` é o WhatsApp de um lead do arquivo ou da base e `relation_kind` é o que a pessoa da linha é desse familiar (por exemplo `filha`). Acompanhe por GET /leads/imports/{id}.
// @Tags			Leads
// @Produce		json
// @Param			id	path		string	true	"ID da importação"
// @Success		202	{object}	lead.LeadImportResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		409	{object}	response.ErrorResponse
// @Failure		503	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/leads/imports/{id}/start [post]
func (h *LeadHandler) StartImport(w http.ResponseWriter, r *http.Request) {
	a, ok := h.importActor(w, r)
	if !ok {
		return
	}
	id := mux.Vars(r)["id"]
	job, err := h.imports.Start(r.Context(), a, id)
	if err != nil {
		writeImportError(w, a, id, err)
		return
	}
	h.importView(w, r, a, lead_usecase.ImportDetail{Job: job}, http.StatusAccepted)
}

// @Summary		Baixar as linhas recusadas de uma importação
// @Description	CSV (separado por ponto e vírgula, UTF-8 com BOM) com uma linha por problema: número da linha na planilha, motivo legível no idioma pedido (`locale` pt, en, es ou de; padrão pt), código do motivo, coluna e se a linha foi importada mesmo assim (um campo inválido é descartado e o resto da linha entra). Não repete nenhum dado da planilha. Só quem enviou a importação pode baixar; o arquivo deixa de existir com a importação, 7 dias depois do envio.
// @Tags			Leads
// @Produce		text/csv
// @Param			id		path		string	true	"ID da importação"
// @Param			locale	query		string	false	"Idioma dos motivos"	Enums(pt, en, es, de)
// @Success		200		{file}		binary
// @Failure		403		{object}	response.ErrorResponse
// @Failure		404		{object}	response.ErrorResponse
// @Failure		503		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/leads/imports/{id}/rejections [get]
func (h *LeadHandler) ImportRejections(w http.ResponseWriter, r *http.Request) {
	a, ok := h.importActor(w, r)
	if !ok {
		return
	}
	id := mux.Vars(r)["id"]
	first, err := h.imports.Rejections(r.Context(), a, id, 0, rejectionsPage)
	if err != nil {
		writeImportError(w, a, id, err)
		return
	}
	locale := httpx.RequestLocale(r)
	columns := leadimport.RejectionColumns(locale)
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="importacao-`+id+`-linhas.csv"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "\xEF\xBB\xBF")
	out := csv.NewWriter(w)
	out.Comma = ';'
	_ = out.Write([]string{columns.Line, columns.Reason, columns.Code, columns.Field, columns.Imported})
	page := first
	for len(page) > 0 {
		for _, row := range page {
			imported := columns.Yes
			if row.Rejected {
				imported = columns.No
			}
			_ = out.Write([]string{strconv.Itoa(row.Line), leadimport.ReasonLabel(row.Reason, locale), string(row.Reason), row.Field, imported})
		}
		out.Flush()
		if len(page) < rejectionsPage {
			return
		}
		if page, err = h.imports.Rejections(r.Context(), a, id, page[len(page)-1].Seq, rejectionsPage); err != nil {
			slog.Error("lead import: the rejections download stopped", "import_id", id, "workspace_id", a.WorkspaceID, "error", err)
			return
		}
	}
}
