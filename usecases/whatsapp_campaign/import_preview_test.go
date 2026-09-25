package whatsapp_campaign_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/media"
	"vozko/domain/shared"
	tmpl "vozko/domain/whatsapp/template"
	wc "vozko/domain/whatsapp_campaign"
)

type importFiles map[string]string

func (f importFiles) Read(_ context.Context, workspaceID, mediaID string) (*media.Content, error) {
	body, ok := f[workspaceID+"|"+mediaID]
	if !ok {
		return nil, media.ErrMediaNotFound
	}
	return &media.Content{Name: "lista.csv", Data: []byte(body)}, nil
}

type importTemplates struct{ granted bool }

func (t importTemplates) List(string, tmpl.ListInput) (*shared.PaginatedResult[*tmpl.Template], error) {
	return nil, nil
}
func (t importTemplates) Create(string, string, tmpl.CreateTemplateInput) (*tmpl.CreateTemplateOutput, error) {
	return nil, nil
}
func (t importTemplates) Get(_ string, id string) (*tmpl.Template, error) {
	if !t.granted {
		return nil, tmpl.ErrTemplateAccessDenied
	}
	return &tmpl.Template{ID: id, Status: tmpl.TemplateStatusApproved, Category: "MARKETING",
		Components: []tmpl.TemplateComponent{{Type: "BODY", Text: "Oi {{1}}, cupom {{2}}"}}}, nil
}

type importPrices struct{}

func (importPrices) GetTemplateCostMicros(string, string) (int64, error) { return 66_667, nil }

type importBalance int64

func (b importBalance) GetBalance(string) (int64, error) { return int64(b), nil }

const importFile = "numero;nome;var1;var2\n" +
	"5584994409624;Maria;Maria;BF10\n" +
	"(84) 99440-9625;João;João;BF10\n" +
	"123;Ana;Ana;BF10\n" +
	"5584994409624;Maria de novo;Maria;BF10\n" +
	"5584994409626;Pedro;Pedro;\n"

func previewUseCase(balance int64, granted bool) wc.ImportPreviewUseCase {
	return NewImportPreviewUseCase(ImportPreviewDeps{
		Files:     importFiles{"ws1|m1": importFile},
		Templates: importTemplates{granted: granted},
		Prices:    importPrices{},
		Balance:   importBalance(balance),
	})
}

func TestImportPreviewReportsEveryProblemWithItsLine(t *testing.T) {
	p, err := previewUseCase(1_000_000, true).Preview(context.Background(), wc.ImportRequest{WorkspaceID: "ws1", MediaID: "m1", TemplateID: "t1"})
	if err != nil {
		t.Fatal(err)
	}
	if p.TotalRows != 5 || p.ValidRows != 2 || p.Variables != 2 {
		t.Fatalf("preview = %+v", p)
	}
	if p.IssueCounts[wc.IssueInvalidNumber] != 1 || p.IssueCounts[wc.IssueDuplicate] != 1 || p.IssueCounts[wc.IssueMissingVariable] != 1 {
		t.Fatalf("counts = %v", p.IssueCounts)
	}
	lines := map[int]string{}
	for _, i := range p.Issues {
		lines[i.Line] = i.Reason
	}
	if lines[4] != wc.IssueInvalidNumber || lines[5] != wc.IssueDuplicate || lines[6] != wc.IssueMissingVariable {
		t.Fatalf("issues = %+v", p.Issues)
	}
	if p.Rows[1].Number != "5584994409625" || p.Rows[1].Variables[1] != "BF10" {
		t.Fatalf("rows = %+v", p.Rows)
	}
}

func TestImportPreviewEstimatesCostAgainstTheBalance(t *testing.T) {
	p, _ := previewUseCase(100_000, true).Preview(context.Background(), wc.ImportRequest{WorkspaceID: "ws1", MediaID: "m1", TemplateID: "t1"})
	if p.CostMicros != 2*66_667 || p.Affordable {
		t.Fatalf("cost %d affordable %v", p.CostMicros, p.Affordable)
	}
}

func TestImportPreviewOnlyReadsTheWorkspacesFileAndTemplate(t *testing.T) {
	if _, err := previewUseCase(0, true).Preview(context.Background(), wc.ImportRequest{WorkspaceID: "ws2", MediaID: "m1", TemplateID: "t1"}); !errors.Is(err, media.ErrMediaNotFound) {
		t.Fatalf("foreign file: %v", err)
	}
	if _, err := previewUseCase(0, false).Preview(context.Background(), wc.ImportRequest{WorkspaceID: "ws1", MediaID: "m1", TemplateID: "t1"}); !errors.Is(err, tmpl.ErrTemplateAccessDenied) {
		t.Fatalf("foreign template: %v", err)
	}
}

func TestImportPreviewHonoursAnExplicitMapping(t *testing.T) {
	file := "fone,cliente,cupom\n5584994409624,Maria,BF10\n"
	uc := NewImportPreviewUseCase(ImportPreviewDeps{
		Files: importFiles{"ws1|m1": file}, Templates: importTemplates{granted: true}, Prices: importPrices{}, Balance: importBalance(1_000_000),
	})
	p, err := uc.Preview(context.Background(), wc.ImportRequest{WorkspaceID: "ws1", MediaID: "m1", TemplateID: "t1",
		Mapping: wc.ColumnMapping{Number: "fone", Name: "cliente", Variables: []string{"cliente", "cupom"}}})
	if err != nil || p.ValidRows != 1 || p.Rows[0].Variables[0] != "Maria" {
		t.Fatalf("preview %+v err %v", p, err)
	}
	if _, err := uc.Preview(context.Background(), wc.ImportRequest{WorkspaceID: "ws1", MediaID: "m1", TemplateID: "t1",
		Mapping: wc.ColumnMapping{Number: "fone", Variables: []string{"cupom"}}}); !errors.Is(err, wc.ErrImportVariableCount) {
		t.Fatalf("short mapping: %v", err)
	}
}
