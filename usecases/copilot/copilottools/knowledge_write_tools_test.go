package copilottools

import (
	"context"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/media"
	"vozko/domain/rag"
	wd "vozko/domain/workspace/workspace_department"
)

const knownKnowledgeBase = "0a1b2c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d"

type fakeKnowledgeCreate struct {
	input rag.CreateKnowledgeBaseInput
	scope wd.CreationScope
}

func (f *fakeKnowledgeCreate) Execute(ctx context.Context, in rag.CreateKnowledgeBaseInput) (*rag.KnowledgeBase, error) {
	f.input = in
	f.scope, _ = wd.GetCreationScope(ctx)
	return &rag.KnowledgeBase{ID: knownKnowledgeBase, Name: in.Name}, nil
}

type fakeKnowledgeAccess struct{}

func (fakeKnowledgeAccess) Owned(_ context.Context, viewer rag.Viewer, id string) (*rag.KnowledgeBase, error) {
	if viewer.WorkspaceID != "ws-1" || id != knownKnowledgeBase {
		return nil, rag.ErrKnowledgeBaseAccessDenied
	}
	return &rag.KnowledgeBase{ID: id, Name: "Políticas"}, nil
}

type fakeScopedDocuments struct {
	viewer rag.Viewer
	input  rag.CreateDocumentInput
}

func (f *fakeScopedDocuments) Add(_ context.Context, viewer rag.Viewer, in rag.CreateDocumentInput) (*rag.Document, error) {
	f.viewer, f.input = viewer, in
	if viewer.WorkspaceID != "ws-1" || in.KnowledgeBaseID != knownKnowledgeBase {
		return nil, rag.ErrKnowledgeBaseAccessDenied
	}
	if in.MediaID != knownSheet {
		return nil, media.ErrMediaNotFound
	}
	return &rag.Document{ID: "doc-1", Name: "trocas.pdf", Status: rag.DocumentStatusPending}, nil
}

var sheetMedia = fakeChatMedia{knownSheet: {ID: knownSheet, WorkspaceID: "ws-1", URL: "https://files.test/ws-1/trocas.pdf"}}

func knowledgeWriteDeps() (KnowledgeWriteDeps, *fakeKnowledgeCreate, *fakeScopedDocuments) {
	create, docs := &fakeKnowledgeCreate{}, &fakeScopedDocuments{}
	return KnowledgeWriteDeps{Create: create, Access: fakeKnowledgeAccess{}, Documents: docs, Media: sheetMedia}, create, docs
}

func TestKnowledgeWriteToolsNeedKnowledgeCreate(t *testing.T) {
	deps, _, _ := knowledgeWriteDeps()
	for _, tool := range []copilot.Tool{NewCreateKnowledgeBaseTool(deps), NewAddKnowledgeDocumentTool(deps)} {
		if m := tool.Meta(); m.Resource != "knowledge_bases" || m.Action != "create" || !m.Mutating {
			t.Fatalf("%s meta = %+v", tool.Definition().Name, m)
		}
	}
}

func TestCreateKnowledgeBaseResolvesTheDepartmentAsTheUser(t *testing.T) {
	deps, create, _ := knowledgeWriteDeps()
	cc := member()
	res := NewCreateKnowledgeBaseTool(deps).Execute(creating(cc), cc, map[string]interface{}{
		"name": " Políticas ", "department_id": knownDepartment, "workspace_id": "ws-2",
	})
	if res.Status != copilot.StatusOK {
		t.Fatalf("status %s: %s", res.Status, res.Message)
	}
	if create.input.WorkspaceID != "ws-1" || create.input.Name != "Políticas" || create.input.Config != rag.DefaultKnowledgeBaseConfig() {
		t.Fatalf("input = %+v", create.input)
	}
	if create.scope.UserID != "u-1" || create.scope.RequestedDepartmentID != knownDepartment {
		t.Fatalf("scope = %+v", create.scope)
	}
}

func TestCreateKnowledgeBaseFailsClosedWithoutACreationScope(t *testing.T) {
	deps, create, _ := knowledgeWriteDeps()
	res := NewCreateKnowledgeBaseTool(deps).Execute(context.Background(), member(), map[string]interface{}{"name": "Políticas"})
	if res.Status != copilot.StatusDenied || create.input.Name != "" {
		t.Fatalf("result = %+v", res)
	}
}

func TestAddKnowledgeDocumentGoesThroughTheUsersScope(t *testing.T) {
	deps, _, docs := knowledgeWriteDeps()
	cc := member()
	res := NewAddKnowledgeDocumentTool(deps).Execute(context.Background(), cc, map[string]interface{}{"knowledge_base_id": knownKnowledgeBase, "media_id": knownSheet})
	if res.Status != copilot.StatusOK || docs.viewer.Departments != cc.Departments || docs.input.MediaID != knownSheet {
		t.Fatalf("result %+v viewer %+v", res, docs.viewer)
	}
}

func TestAddKnowledgeDocumentRefusesForeignBasesAndFiles(t *testing.T) {
	deps, _, _ := knowledgeWriteDeps()
	cc := member()
	cc.WorkspaceID = "ws-2"
	if res := NewAddKnowledgeDocumentTool(deps).Execute(context.Background(), cc, map[string]interface{}{"knowledge_base_id": knownKnowledgeBase, "media_id": knownSheet}); res.Status != copilot.StatusDenied {
		t.Fatalf("foreign base = %+v", res)
	}
	if res := NewAddKnowledgeDocumentTool(deps).Execute(context.Background(), member(), map[string]interface{}{"knowledge_base_id": knownKnowledgeBase, "media_id": knownCampaign}); res.Status != copilot.StatusError {
		t.Fatalf("unknown file = %+v", res)
	}
	if res := NewAddKnowledgeDocumentTool(deps).Execute(context.Background(), member(), map[string]interface{}{"knowledge_base_id": "Políticas", "media_id": knownSheet}); res.Status != copilot.StatusError {
		t.Fatalf("invented id = %+v", res)
	}
}

func TestAddKnowledgeDocumentApprovalNamesBaseAndFile(t *testing.T) {
	deps, _, _ := knowledgeWriteDeps()
	fields := NewAddKnowledgeDocumentTool(deps).(copilot.Describer).Describe(context.Background(), member(), map[string]interface{}{"knowledge_base_id": knownKnowledgeBase, "media_id": knownSheet})
	if fieldValue(fields, "knowledgeBase") != "Políticas" || fieldValue(fields, "file") != "trocas.pdf" {
		t.Fatalf("fields = %+v", fields)
	}
}
