package copilottools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/rag"
)

type fakeKBList struct {
	calls      int
	department *string
	page       int
}

func (f *fakeKBList) Execute(_ context.Context, workspaceID string, departmentID *string, page, pageSize int) (*rag.KnowledgeBaseListOutput, error) {
	f.calls++
	f.department, f.page = departmentID, page
	return &rag.KnowledgeBaseListOutput{Items: []*rag.KnowledgeBase{{ID: "kb1", Name: "Preços", DocumentCount: 3}}, Total: 1, Page: page, TotalPages: 1}, nil
}

type fakeScopedQuery struct {
	viewer rag.Viewer
	input  rag.QueryInput
	err    error
}

func (f *fakeScopedQuery) Execute(_ context.Context, viewer rag.Viewer, input rag.QueryInput) (*rag.QueryOutput, error) {
	f.viewer, f.input = viewer, input
	if f.err != nil {
		return nil, f.err
	}
	return &rag.QueryOutput{Results: []rag.QueryResult{{Content: strings.Repeat("a", knowledgeExcerptRunes+50), Score: 0.8123, DocumentName: "tabela.pdf", KnowledgeBaseID: "kb1"}}, TotalFound: 1}, nil
}

func knowledgeDeps(list *fakeKBList, query *fakeScopedQuery) KnowledgeDeps {
	return KnowledgeDeps{List: list, Query: query}
}

func TestKnowledgeToolsNeedKnowledgeBaseRead(t *testing.T) {
	deps := knowledgeDeps(&fakeKBList{}, &fakeScopedQuery{})
	for _, tool := range []copilot.Tool{NewListKnowledgeBasesTool(deps), NewSearchKnowledgeTool(deps)} {
		if m := tool.Meta(); m.Resource != "knowledge_bases" || m.Action != "read" || m.Mutating {
			t.Fatalf("%s meta = %+v", tool.Definition().Name, m)
		}
	}
}

func TestListKnowledgeBasesStaysInsideTheMembersDepartment(t *testing.T) {
	list := &fakeKBList{}
	res := NewListKnowledgeBasesTool(knowledgeDeps(list, &fakeScopedQuery{})).Execute(context.Background(), member(), nil)
	if res.Status != copilot.StatusOK || list.department == nil || *list.department != knownDepartment {
		t.Fatalf("status %s, department %v", res.Status, list.department)
	}
}

func TestListKnowledgeBasesAsksWhichDepartmentWhenTheMemberHasSeveral(t *testing.T) {
	list := &fakeKBList{}
	cc := memberOf(knownDepartment, "6a1b2c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d")
	res := NewListKnowledgeBasesTool(knowledgeDeps(list, &fakeScopedQuery{})).Execute(context.Background(), cc, nil)
	if res.Status != copilot.StatusError || list.calls != 0 {
		t.Fatalf("status %s after %d lists", res.Status, list.calls)
	}
}

func TestListKnowledgeBasesLetsAnOwnerSeeTheWholeWorkspace(t *testing.T) {
	list := &fakeKBList{}
	res := NewListKnowledgeBasesTool(knowledgeDeps(list, &fakeScopedQuery{})).Execute(context.Background(), ownerOn(copilot.View{}), nil)
	if res.Status != copilot.StatusOK || list.department != nil {
		t.Fatalf("status %s, department %v", res.Status, list.department)
	}
}

func TestSearchKnowledgeQueriesAsTheViewerAndTrimsExcerpts(t *testing.T) {
	query := &fakeScopedQuery{}
	cc := member()
	res := NewSearchKnowledgeTool(knowledgeDeps(&fakeKBList{}, query)).Execute(context.Background(), cc, map[string]interface{}{
		"query": "desconto à vista", "knowledge_base_ids": []interface{}{"kb1"},
	})
	if res.Status != copilot.StatusOK {
		t.Fatalf("status = %s: %s", res.Status, res.Message)
	}
	if query.viewer.WorkspaceID != "ws-1" || query.viewer.Departments != cc.Departments || query.input.TopK != knowledgeResults {
		t.Fatalf("viewer %+v input %+v", query.viewer, query.input)
	}
	b, _ := json.Marshal(res.Data)
	if !strings.Contains(string(b), "tabela.pdf") || strings.Contains(string(b), strings.Repeat("a", knowledgeExcerptRunes+1)) {
		t.Fatalf("data = %s", b)
	}
}

func TestSearchKnowledgeReportsAForbiddenBaseAsDenied(t *testing.T) {
	query := &fakeScopedQuery{err: rag.ErrKnowledgeBaseAccessDenied}
	res := NewSearchKnowledgeTool(knowledgeDeps(&fakeKBList{}, query)).Execute(context.Background(), member(), map[string]interface{}{
		"query": "x", "knowledge_base_ids": []interface{}{"kb9"},
	})
	if res.Status != copilot.StatusDenied {
		t.Fatalf("status = %s", res.Status)
	}
}

func TestSearchKnowledgeNeedsQueryAndBases(t *testing.T) {
	query := &fakeScopedQuery{}
	tool := NewSearchKnowledgeTool(knowledgeDeps(&fakeKBList{}, query))
	for _, args := range []map[string]interface{}{{"query": "x"}, {"knowledge_base_ids": []interface{}{"kb1"}}} {
		if res := tool.Execute(context.Background(), member(), args); res.Status != copilot.StatusError {
			t.Fatalf("args %v: status %s", args, res.Status)
		}
	}
	if query.viewer.WorkspaceID != "" {
		t.Fatal("queried with missing arguments")
	}
}
