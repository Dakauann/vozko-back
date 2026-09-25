package copilottools

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"vozko/domain/copilot"
	"vozko/domain/media"
	"vozko/domain/rag"
	"vozko/domain/tools"
	"vozko/domain/workspace"
)

type KnowledgeWriteDeps struct {
	Create    rag.CreateKnowledgeBaseUseCase
	Access    rag.KnowledgeBaseAccessUseCase
	Documents rag.ScopedDocumentsUseCase
	Media     media.GetMediaUseCase
}

func knowledgeWriteMeta() copilot.Meta {
	return copilot.Meta{Mutating: true, Resource: workspace.ResourceKnowledgeBases, Action: workspace.ActionCreate}
}

func knowledgeViewerOf(cc copilot.Context) rag.Viewer {
	return rag.Viewer{WorkspaceID: cc.WorkspaceID, Departments: cc.Departments}
}

type createKnowledgeBaseArgs struct {
	Name         string `json:"name" req:"true" desc:"nome da base (até 120 caracteres)"`
	Description  string `json:"description" desc:"do que a base trata"`
	DepartmentID string `json:"department_id" desc:"departamento (list_departments); só quando o usuário tem mais de um" id:"true"`
}

type createKnowledgeBaseTool struct{ deps KnowledgeWriteDeps }

func NewCreateKnowledgeBaseTool(deps KnowledgeWriteDeps) copilot.Tool {
	return &createKnowledgeBaseTool{deps: deps}
}

func (t *createKnowledgeBaseTool) Meta() copilot.Meta { return knowledgeWriteMeta() }

func (t *createKnowledgeBaseTool) Definition() tools.Definition {
	return definition("create_knowledge_base",
		"Cria uma base de conhecimento vazia, com a configuração padrão. Para colocar conteúdo, use add_knowledge_document "+
			"com um arquivo anexado. Só depois da aprovação do usuário.", createKnowledgeBaseArgs{})
}

func (t *createKnowledgeBaseTool) Describe(_ context.Context, _ copilot.Context, args map[string]interface{}) []copilot.Field {
	var a createKnowledgeBaseArgs
	bindArgs(args, &a)
	fields := []copilot.Field{{Key: "name", Value: strings.TrimSpace(a.Name)}}
	if d := strings.TrimSpace(a.Description); d != "" {
		fields = append(fields, copilot.Field{Key: "description", Value: d})
	}
	return fields
}

func (t *createKnowledgeBaseTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a createKnowledgeBaseArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	scoped, err := creationContext(ctx, a.DepartmentID)
	if err != nil {
		return knowledgeWriteFailure("create_knowledge_base", err)
	}
	kb, err := t.deps.Create.Execute(scoped, rag.CreateKnowledgeBaseInput{
		WorkspaceID: cc.WorkspaceID,
		Name:        strings.TrimSpace(a.Name),
		Description: strings.TrimSpace(a.Description),
		Config:      rag.DefaultKnowledgeBaseConfig(),
	})
	if err != nil {
		return knowledgeWriteFailure("create_knowledge_base", err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"knowledge_base_id": kb.ID, "name": kb.Name}}
}

type addKnowledgeDocumentArgs struct {
	KnowledgeBaseID string `json:"knowledge_base_id" req:"true" desc:"id exato de list_knowledge_bases ou create_knowledge_base" id:"true"`
	MediaID         string `json:"media_id" req:"true" desc:"media_id do arquivo anexado na conversa (PDF, DOCX, Markdown, HTML, JSON ou texto)" id:"true"`
	Name            string `json:"name" desc:"nome do documento; omita para usar o nome do arquivo"`
}

type addKnowledgeDocumentTool struct{ deps KnowledgeWriteDeps }

func NewAddKnowledgeDocumentTool(deps KnowledgeWriteDeps) copilot.Tool {
	return &addKnowledgeDocumentTool{deps: deps}
}

func (t *addKnowledgeDocumentTool) Meta() copilot.Meta { return knowledgeWriteMeta() }

func (t *addKnowledgeDocumentTool) Definition() tools.Definition {
	return definition("add_knowledge_document",
		"Adiciona um arquivo anexado a uma base de conhecimento. O arquivo é processado em segundo plano e fica pesquisável em "+
			"alguns minutos. Só depois da aprovação do usuário.", addKnowledgeDocumentArgs{})
}

func (t *addKnowledgeDocumentTool) Describe(ctx context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	var a addKnowledgeDocumentArgs
	bindArgs(args, &a)
	base := "base desconhecida ou sem acesso"
	if kb, err := t.deps.Access.Owned(ctx, knowledgeViewerOf(cc), a.KnowledgeBaseID); err == nil {
		base = kb.Name
	}
	file := "anexo desconhecido"
	if m, err := t.deps.Media.GetMedia(cc.WorkspaceID, strings.TrimSpace(a.MediaID)); err == nil {
		file = m.DisplayName()
	}
	fields := []copilot.Field{{Key: "knowledgeBase", Value: base}, {Key: "file", Value: file}}
	if n := strings.TrimSpace(a.Name); n != "" {
		fields = append(fields, copilot.Field{Key: "name", Value: n})
	}
	return fields
}

func (t *addKnowledgeDocumentTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a addKnowledgeDocumentArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	kbID, err := knownID(a.KnowledgeBaseID, "knowledge_base_id", "list_knowledge_bases")
	if err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	mediaID, err := knownID(a.MediaID, "media_id", "anexos da conversa")
	if err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	doc, err := t.deps.Documents.Add(ctx, knowledgeViewerOf(cc), rag.CreateDocumentInput{
		KnowledgeBaseID: kbID,
		MediaID:         mediaID,
		Name:            strings.TrimSpace(a.Name),
	})
	if err != nil {
		return knowledgeWriteFailure("add_knowledge_document", err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"document_id": doc.ID, "name": doc.Name, "status": doc.Status}}
}

func knowledgeWriteFailure(tool string, err error) copilot.Result {
	switch {
	case errors.Is(err, errInvalidArgs):
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	case errors.Is(err, media.ErrMediaNotFound):
		return copilot.Result{Status: copilot.StatusError, Message: "anexo desconhecido; peça ao usuário para anexar o arquivo na conversa"}
	case errors.Is(err, media.ErrMediaTooLarge):
		return copilot.Result{Status: copilot.StatusError, Message: "o arquivo é grande demais; envie pela tela da base de conhecimento"}
	case errors.Is(err, rag.ErrMaxKnowledgeBasesReached):
		return copilot.Result{Status: copilot.StatusError, Message: "o workspace já tem o máximo de bases de conhecimento"}
	case errors.Is(err, rag.ErrMaxDocumentsReached), errors.Is(err, rag.ErrMaxTotalSizeReached):
		return copilot.Result{Status: copilot.StatusError, Message: "a base atingiu o limite: " + err.Error()}
	case errors.Is(err, rag.ErrKnowledgeBaseNameRequired), errors.Is(err, rag.ErrKnowledgeBaseNameTooLong):
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	return knowledgeFailure(tool, err)
}

func (t *createKnowledgeBaseTool) Validate(_ context.Context, cc copilot.Context, args map[string]interface{}) error {
	_, err := validateArgs[createKnowledgeBaseArgs](nil, cc, args)
	return err
}

func (t *addKnowledgeDocumentTool) Validate(ctx context.Context, cc copilot.Context, args map[string]interface{}) error {
	a, err := validateArgs[addKnowledgeDocumentArgs](nil, cc, args)
	if err != nil {
		return err
	}
	if _, err := t.deps.Access.Owned(ctx, knowledgeViewerOf(cc), a.KnowledgeBaseID); err != nil {
		return fmt.Errorf("%w: base de conhecimento não encontrada ou sem acesso; use list_knowledge_bases", errInvalidArgs)
	}
	if _, err := t.deps.Media.GetMedia(cc.WorkspaceID, a.MediaID); err != nil {
		return fmt.Errorf("%w: anexo desconhecido; peça ao usuário para anexar o arquivo na conversa", errInvalidArgs)
	}
	return nil
}
