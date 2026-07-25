package handlers

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	"vozko/domain/rag"
	"vozko/infra/http/middleware"
)

type KnowledgeBaseHandler struct {
	createUseCase      rag.CreateKnowledgeBaseUseCase
	updateUseCase      rag.UpdateKnowledgeBaseUseCase
	deleteUseCase      rag.DeleteKnowledgeBaseUseCase
	getUseCase         rag.GetKnowledgeBaseUseCase
	listUseCase        rag.ListKnowledgeBasesUseCase
	createDocUseCase   rag.CreateDocumentUseCase
	deleteDocUseCase   rag.DeleteDocumentUseCase
	getDocUseCase      rag.GetDocumentUseCase
	listDocsUseCase    rag.ListDocumentsUseCase
	linkAgentUseCase   rag.LinkAgentKnowledgeBasesUseCase
	getAgentKBsUseCase rag.GetAgentKnowledgeBasesUseCase
	queryUseCase       rag.QueryKnowledgeBaseUseCase
}

func NewKnowledgeBaseHandler(
	createUC rag.CreateKnowledgeBaseUseCase,
	updateUC rag.UpdateKnowledgeBaseUseCase,
	deleteUC rag.DeleteKnowledgeBaseUseCase,
	getUC rag.GetKnowledgeBaseUseCase,
	listUC rag.ListKnowledgeBasesUseCase,
	createDocUC rag.CreateDocumentUseCase,
	deleteDocUC rag.DeleteDocumentUseCase,
	getDocUC rag.GetDocumentUseCase,
	listDocsUC rag.ListDocumentsUseCase,
	linkAgentUC rag.LinkAgentKnowledgeBasesUseCase,
	getAgentKBsUC rag.GetAgentKnowledgeBasesUseCase,
	queryUC rag.QueryKnowledgeBaseUseCase,
) *KnowledgeBaseHandler {
	return &KnowledgeBaseHandler{
		createUseCase:      createUC,
		updateUseCase:      updateUC,
		deleteUseCase:      deleteUC,
		getUseCase:         getUC,
		listUseCase:        listUC,
		createDocUseCase:   createDocUC,
		deleteDocUseCase:   deleteDocUC,
		getDocUseCase:      getDocUC,
		listDocsUseCase:    listDocsUC,
		linkAgentUseCase:   linkAgentUC,
		getAgentKBsUseCase: getAgentKBsUC,
		queryUseCase:       queryUC,
	}
}

type createKnowledgeBaseRequest struct {
	Name             string `json:"name"`
	Description      string `json:"description"`
	ChunkingStrategy string `json:"chunkingStrategy,omitempty"`
	ChunkSize        int    `json:"chunkSize,omitempty"`
	ChunkOverlap     int    `json:"chunkOverlap,omitempty"`
	EmbeddingModel   string `json:"embeddingModel,omitempty"`
}

type updateKnowledgeBaseRequest struct {
	Name             *string `json:"name,omitempty"`
	Description      *string `json:"description,omitempty"`
	ChunkingStrategy *string `json:"chunkingStrategy,omitempty"`
	ChunkSize        *int    `json:"chunkSize,omitempty"`
	ChunkOverlap     *int    `json:"chunkOverlap,omitempty"`
	EmbeddingModel   *string `json:"embeddingModel,omitempty"`
}

type createDocumentRequest struct {
	Name     string            `json:"name"`
	Type     string            `json:"type"`
	Content  string            `json:"content"`
	MediaID  string            `json:"mediaId,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type linkAgentRequest struct {
	KnowledgeBaseIDs []string `json:"knowledgeBaseIds"`
}

type queryRequest struct {
	Query            string   `json:"query"`
	KnowledgeBaseIDs []string `json:"knowledgeBaseIds,omitempty"`
	MaxResults       int      `json:"maxResults,omitempty"`
	MinScore         float32  `json:"minScore,omitempty"`
}

func (h *KnowledgeBaseHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createKnowledgeBaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{"body": "invalid JSON"})
		return
	}

	if strings.TrimSpace(req.Name) == "" {
		response.WriteValidationError(w, map[string]string{"name": "required"})
		return
	}

	config := rag.DefaultKnowledgeBaseConfig()
	if req.ChunkingStrategy != "" {
		strategy := rag.ChunkingStrategy(strings.ToLower(req.ChunkingStrategy))
		if !strategy.IsValid() {
			response.WriteValidationError(w, map[string]string{"chunkingStrategy": "invalid strategy"})
			return
		}
		config.ChunkingStrategy = strategy
	}
	if req.ChunkSize > 0 {
		config.ChunkSize = req.ChunkSize
	}
	if req.ChunkOverlap >= 0 {
		config.ChunkOverlap = req.ChunkOverlap
	}
	if req.EmbeddingModel != "" {
		config.EmbeddingModel = req.EmbeddingModel
	}

	input := rag.CreateKnowledgeBaseInput{
		WorkspaceID: middleware.GetWorkspaceID(r),
		Name:        strings.TrimSpace(req.Name),
		Description: strings.TrimSpace(req.Description),
		Config:      config,
	}

	result, err := h.createUseCase.Execute(r.Context(), input)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusCreated, result)
}

func (h *KnowledgeBaseHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if id == "" {
		response.WriteError(w, http.StatusBadRequest, "Knowledge base ID is required", nil)
		return
	}

	if !h.verifyOwnership(w, id, r) {
		return
	}

	var req updateKnowledgeBaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{"body": "invalid JSON"})
		return
	}

	var configUpdate *rag.KnowledgeBaseConfig
	if req.ChunkingStrategy != nil || req.ChunkSize != nil || req.ChunkOverlap != nil || req.EmbeddingModel != nil {
		existing, err := h.getUseCase.Execute(r.Context(), id)
		if err != nil {
			h.handleDomainError(w, err)
			return
		}
		cfg := existing.Config
		if req.ChunkingStrategy != nil {
			strategy := rag.ChunkingStrategy(strings.ToLower(*req.ChunkingStrategy))
			if !strategy.IsValid() {
				response.WriteValidationError(w, map[string]string{"chunkingStrategy": "invalid strategy"})
				return
			}
			cfg.ChunkingStrategy = strategy
		}
		if req.ChunkSize != nil {
			cfg.ChunkSize = *req.ChunkSize
		}
		if req.ChunkOverlap != nil {
			cfg.ChunkOverlap = *req.ChunkOverlap
		}
		if req.EmbeddingModel != nil {
			cfg.EmbeddingModel = *req.EmbeddingModel
		}
		configUpdate = &cfg
	}

	input := rag.UpdateKnowledgeBaseInput{
		Name:        req.Name,
		Description: req.Description,
		Config:      configUpdate,
	}

	result, err := h.updateUseCase.Execute(r.Context(), id, input)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, result)
}

func (h *KnowledgeBaseHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if id == "" {
		response.WriteError(w, http.StatusBadRequest, "Knowledge base ID is required", nil)
		return
	}

	if !h.verifyOwnership(w, id, r) {
		return
	}

	if err := h.deleteUseCase.Execute(r.Context(), id); err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, map[string]bool{"deleted": true})
}

func (h *KnowledgeBaseHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if id == "" {
		response.WriteError(w, http.StatusBadRequest, "Knowledge base ID is required", nil)
		return
	}

	if !h.verifyOwnership(w, id, r) {
		return
	}

	result, err := h.getUseCase.Execute(r.Context(), id)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	if !canAccessDepartment(r, result.DepartmentID) {
		response.WriteError(w, http.StatusForbidden, "You don't have access to this campaign", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, result)
}

func (h *KnowledgeBaseHandler) List(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("pageSize"))

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	workspaceID := middleware.GetWorkspaceID(r)

	filter := middleware.GetDepartmentFilter(r)
	deptIDs := departmentFilterIDs(r)
	if shouldReturnEmptyDepartmentList(r) || (filter != nil && filter.ShouldFilter() && len(deptIDs) != 1) {
		response.WritePaginated(w, http.StatusOK, []*rag.KnowledgeBase{}, response.PaginationMeta{
			Page:       page,
			PageSize:   pageSize,
			TotalPages: 0,
			TotalItems: 0,
		})
		return
	}

	var departmentID *string
	if len(deptIDs) == 1 {
		departmentID = &deptIDs[0]
	}

	result, err := h.listUseCase.Execute(r.Context(), workspaceID, departmentID, page, pageSize)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WritePaginated(w, http.StatusOK, result.Items, response.PaginationMeta{
		Page:       result.Page,
		PageSize:   result.PageSize,
		TotalPages: result.TotalPages,
		TotalItems: result.Total,
	})
}

func (h *KnowledgeBaseHandler) CreateDocument(w http.ResponseWriter, r *http.Request) {
	kbID := mux.Vars(r)["id"]
	if kbID == "" {
		response.WriteError(w, http.StatusBadRequest, "Knowledge base ID is required", nil)
		return
	}

	if !h.verifyOwnership(w, kbID, r) {
		return
	}

	var req createDocumentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{"body": "invalid JSON"})
		return
	}

	if strings.TrimSpace(req.Name) == "" {
		response.WriteValidationError(w, map[string]string{"name": "required"})
		return
	}
	if strings.TrimSpace(req.Content) == "" && strings.TrimSpace(req.MediaID) == "" {
		response.WriteValidationError(w, map[string]string{"content": "content or mediaId is required"})
		return
	}

	docType := rag.DocumentType(strings.ToLower(req.Type))
	if !docType.IsValid() {
		docType = rag.DocumentTypeText
	}

	input := rag.CreateDocumentInput{
		KnowledgeBaseID: kbID,
		Name:            strings.TrimSpace(req.Name),
		Type:            docType,
		Content:         req.Content,
		MediaID:         strings.TrimSpace(req.MediaID),
		Metadata:        req.Metadata,
	}

	result, err := h.createDocUseCase.Execute(r.Context(), input)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusCreated, result)
}

func (h *KnowledgeBaseHandler) UploadDocument(w http.ResponseWriter, r *http.Request) {
	kbID := mux.Vars(r)["id"]
	if kbID == "" {
		response.WriteError(w, http.StatusBadRequest, "Knowledge base ID is required", nil)
		return
	}

	if !h.verifyOwnership(w, kbID, r) {
		return
	}

	const maxUploadSize = 200 << 20
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)

	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Upload too large (max 200MB total)", nil)
		return
	}
	defer r.MultipartForm.RemoveAll()

	fileHeaders := r.MultipartForm.File["files"]
	if len(fileHeaders) == 0 {
		fileHeaders = r.MultipartForm.File["file"]
	}
	if len(fileHeaders) == 0 {
		response.WriteError(w, http.StatusBadRequest, "At least one file is required (field: 'files' or 'file')", nil)
		return
	}

	if len(fileHeaders) > rag.MaxDocumentsPerKnowledgeBase {
		response.WriteError(w, http.StatusBadRequest, fmt.Sprintf("Too many files (max %d per request)", rag.MaxDocumentsPerKnowledgeBase), nil)
		return
	}

	extTypeMap := map[string]rag.DocumentType{
		"pdf":  rag.DocumentTypePDF,
		"docx": rag.DocumentTypeDocx,
		"doc":  rag.DocumentTypeDocx,
		"md":   rag.DocumentTypeMarkdown,
		"html": rag.DocumentTypeHTML,
		"htm":  rag.DocumentTypeHTML,
		"json": rag.DocumentTypeJSON,
	}

	type uploadResult struct {
		Name    string      `json:"name"`
		Success bool        `json:"success"`
		Error   string      `json:"error,omitempty"`
		Doc     interface{} `json:"document,omitempty"`
	}

	results := make([]uploadResult, 0, len(fileHeaders))

	for _, header := range fileHeaders {
		file, err := header.Open()
		if err != nil {
			results = append(results, uploadResult{Name: header.Filename, Error: "failed to open file"})
			continue
		}

		data, err := io.ReadAll(file)
		file.Close()
		if err != nil {
			results = append(results, uploadResult{Name: header.Filename, Error: "failed to read file"})
			continue
		}

		if len(data) == 0 {
			results = append(results, uploadResult{Name: header.Filename, Error: "file is empty"})
			continue
		}

		if len(data) > 10<<20 {
			results = append(results, uploadResult{Name: header.Filename, Error: "file exceeds 10MB limit"})
			continue
		}

		filename := header.Filename
		if filename == "" {
			filename = "upload"
		}

		ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(filename), "."))
		docType, ok := extTypeMap[ext]
		if !ok {
			docType = rag.DocumentTypeText
		}

		encoded := base64.StdEncoding.EncodeToString(data)

		input := rag.CreateDocumentInput{
			KnowledgeBaseID: kbID,
			Name:            filename,
			Type:            docType,
			Content:         encoded,
			Metadata: map[string]string{
				"originalSize": fmt.Sprintf("%d", len(data)),
				"encoding":     "base64",
			},
		}

		result, err := h.createDocUseCase.Execute(r.Context(), input)
		if err != nil {
			errMsg := "upload failed"
			if errors.Is(err, rag.ErrMaxDocumentsReached) {
				errMsg = "document limit reached (max 30)"
			}
			results = append(results, uploadResult{Name: filename, Error: errMsg})
			continue
		}

		results = append(results, uploadResult{Name: filename, Success: true, Doc: result})
	}

	if len(fileHeaders) == 1 && len(results) == 1 && results[0].Success {
		response.WriteSuccess(w, http.StatusCreated, results[0].Doc)
		return
	}

	response.WriteSuccess(w, http.StatusCreated, map[string]interface{}{
		"results": results,
		"total":   len(results),
	})
}

func (h *KnowledgeBaseHandler) DeleteDocument(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	kbID := vars["id"]
	docID := vars["documentId"]

	if kbID == "" || docID == "" {
		response.WriteError(w, http.StatusBadRequest, "Knowledge base ID and document ID are required", nil)
		return
	}

	if !h.verifyOwnership(w, kbID, r) {
		return
	}

	doc, err := h.getDocUseCase.Execute(r.Context(), docID)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	if doc.KnowledgeBaseID != kbID {
		response.WriteError(w, http.StatusForbidden, "Document does not belong to this knowledge base", nil)
		return
	}

	if err := h.deleteDocUseCase.Execute(r.Context(), docID); err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, map[string]bool{"deleted": true})
}

func (h *KnowledgeBaseHandler) GetDocument(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	kbID := vars["id"]
	docID := vars["documentId"]

	if kbID == "" || docID == "" {
		response.WriteError(w, http.StatusBadRequest, "Knowledge base ID and document ID are required", nil)
		return
	}

	if !h.verifyOwnership(w, kbID, r) {
		return
	}

	doc, err := h.getDocUseCase.Execute(r.Context(), docID)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	if doc.KnowledgeBaseID != kbID {
		response.WriteError(w, http.StatusForbidden, "Document does not belong to this knowledge base", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, doc)
}

func (h *KnowledgeBaseHandler) ListDocuments(w http.ResponseWriter, r *http.Request) {
	kbID := mux.Vars(r)["id"]
	if kbID == "" {
		response.WriteError(w, http.StatusBadRequest, "Knowledge base ID is required", nil)
		return
	}

	if !h.verifyOwnership(w, kbID, r) {
		return
	}

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("pageSize"))

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	result, err := h.listDocsUseCase.Execute(r.Context(), kbID, page, pageSize)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WritePaginated(w, http.StatusOK, result.Items, response.PaginationMeta{
		Page:       result.Page,
		PageSize:   result.PageSize,
		TotalPages: result.TotalPages,
		TotalItems: result.Total,
	})
}

func (h *KnowledgeBaseHandler) LinkToAgent(w http.ResponseWriter, r *http.Request) {
	agentID := mux.Vars(r)["agentId"]
	if agentID == "" {
		response.WriteError(w, http.StatusBadRequest, "Agent ID is required", nil)
		return
	}

	var req linkAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{"body": "invalid JSON"})
		return
	}

	input := rag.LinkAgentKnowledgeBasesInput{
		AgentID:          agentID,
		KnowledgeBaseIDs: req.KnowledgeBaseIDs,
	}

	if err := h.linkAgentUseCase.Execute(r.Context(), input); err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, map[string]bool{"linked": true})
}

func (h *KnowledgeBaseHandler) GetAgentKnowledgeBases(w http.ResponseWriter, r *http.Request) {
	agentID := mux.Vars(r)["agentId"]
	if agentID == "" {
		response.WriteError(w, http.StatusBadRequest, "Agent ID is required", nil)
		return
	}

	result, err := h.getAgentKBsUseCase.Execute(r.Context(), agentID)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{"knowledgeBases": result})
}

func (h *KnowledgeBaseHandler) Query(w http.ResponseWriter, r *http.Request) {
	var req queryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{"body": "invalid JSON"})
		return
	}

	if strings.TrimSpace(req.Query) == "" {
		response.WriteValidationError(w, map[string]string{"query": "required"})
		return
	}

	if len(req.KnowledgeBaseIDs) == 0 {
		response.WriteValidationError(w, map[string]string{"knowledgeBaseIds": "at least one knowledge base ID is required"})
		return
	}

	maxResults := req.MaxResults
	if maxResults <= 0 {
		maxResults = 5
	}
	if maxResults > 20 {
		maxResults = 20
	}

	minScore := req.MinScore
	if minScore <= 0 {
		minScore = 0.3
	}

	input := rag.QueryInput{
		KnowledgeBaseIDs: req.KnowledgeBaseIDs,
		Query:            strings.TrimSpace(req.Query),
		TopK:             maxResults,
		MinScore:         minScore,
		IncludeMetadata:  true,
	}

	result, err := h.queryUseCase.Execute(r.Context(), input)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, result)
}

func (h *KnowledgeBaseHandler) verifyOwnership(w http.ResponseWriter, kbID string, r *http.Request) bool {
	kb, err := h.getUseCase.Execute(r.Context(), kbID)
	if err != nil {
		h.handleDomainError(w, err)
		return false
	}

	if kb.WorkspaceID != middleware.GetWorkspaceID(r) {
		response.WriteError(w, http.StatusForbidden, "You don't have access to this knowledge base", nil)
		return false
	}

	return true
}

func (h *KnowledgeBaseHandler) handleDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, rag.ErrKnowledgeBaseNotFound):
		response.WriteError(w, http.StatusNotFound, "Knowledge base not found", nil)
	case errors.Is(err, rag.ErrDocumentNotFound):
		response.WriteError(w, http.StatusNotFound, "Document not found", nil)
	case errors.Is(err, rag.ErrKnowledgeBaseNameRequired):
		response.WriteValidationError(w, map[string]string{"name": "required"})
	case errors.Is(err, rag.ErrKnowledgeBaseNameTooLong):
		response.WriteValidationError(w, map[string]string{"name": "must not exceed 120 characters"})
	case errors.Is(err, rag.ErrDocumentNameRequired):
		response.WriteValidationError(w, map[string]string{"name": "required"})
	case errors.Is(err, rag.ErrDocumentContentRequired):
		response.WriteValidationError(w, map[string]string{"content": "content or mediaId is required"})
	case errors.Is(err, rag.ErrMaxKnowledgeBasesReached):
		response.WriteError(w, http.StatusForbidden, err.Error(), nil)
	case errors.Is(err, rag.ErrMaxDocumentsReached):
		response.WriteError(w, http.StatusForbidden, err.Error(), nil)
	default:
		response.WriteError(w, http.StatusInternalServerError, "Internal server error", nil)
	}
}
