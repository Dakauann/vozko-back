package choicestats_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/choicestats"
	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	"vozko/domain/whatsapp/template"
	wc "vozko/domain/whatsapp_campaign"
	"vozko/domain/workflow"
)

type countingStore struct {
	fakeStore
	rows    []choicestats.CountRow
	err     error
	queries []choicestats.CountQuery
}

func (s *countingStore) Counts(_ context.Context, q choicestats.CountQuery) ([]choicestats.CountRow, error) {
	s.queries = append(s.queries, q)
	return s.rows, s.err
}

type fakeWorkflows struct {
	wf  *workflow.Workflow
	err error
}

func (f fakeWorkflows) Get(workflow.Scope, string) (*workflow.Workflow, error) { return f.wf, f.err }

type fakeCampaigns struct{ c *wc.Campaign }

func (f fakeCampaigns) FindByID(string) (*wc.Campaign, error) {
	if f.c == nil {
		return nil, wc.ErrCampaignNotFound
	}
	return f.c, nil
}

type fakeUnofficial struct {
	c   *uwc.Campaign
	err error
}

func (f fakeUnofficial) Owned(context.Context, string, uw.DepartmentScope, string) (*uwc.Campaign, error) {
	return f.c, f.err
}

type fakeTemplates struct {
	t   *template.Template
	err error
}

func (f fakeTemplates) Get(string, string) (*template.Template, error) { return f.t, f.err }

func (f fakeTemplates) FindByID(string) (*template.Template, error) { return f.t, f.err }

var readerNow = time.Date(2026, 10, 22, 15, 0, 0, 0, time.UTC)

func canvasWorkflow() *workflow.Workflow {
	return &workflow.Workflow{ID: "wf-1", WorkspaceID: "ws", Graph: workflow.Graph{Nodes: []workflow.Node{
		{ID: "ask", Type: workflow.NodeTypeActionSendInteractive, Config: map[string]interface{}{
			"interactive_type": "buttons",
			"buttons":          `[{"Type":"reply","ID":"sim","Title":"Sim"},{"Type":"reply","ID":"nao","Title":"Não"}]`,
		}},
	}}}
}

func promoTemplate() *template.Template {
	return &template.Template{ID: "tpl-1", Components: []template.TemplateComponent{{Type: "BUTTONS", Buttons: []template.TemplateButton{
		{Type: "QUICK_REPLY", Text: "Quero"},
		{Type: "URL", Text: "Loja", URL: "https://loja.com"},
	}}}}
}

func newReader(t *testing.T, store *countingStore, mutate func(*ReaderDeps)) *Reader {
	t.Helper()
	deps := ReaderDeps{
		Store: store, Zones: &fakeZones{loc: saoPaulo},
		Workflows: fakeWorkflows{wf: canvasWorkflow()}, Campaigns: fakeCampaigns{c: &wc.Campaign{ID: "c-1", WorkspaceID: "ws", DepartmentID: "sales", TemplateID: "tpl-1", CreatedAt: readerNow.AddDate(0, 0, -3)}},
		Unofficial: fakeUnofficial{c: &uwc.Campaign{ID: "u-1", WorkspaceID: "ws", CreatedAt: readerNow.AddDate(0, 0, -2), Message: uwc.MessageSpec{Kind: uwc.KindMenu, Options: []uw.InteractiveOption{{ID: "a"}, {ID: "b"}}}}},
		Templates: fakeTemplates{t: promoTemplate()}, TemplateRepo: fakeTemplates{t: promoTemplate()},
		Now: func() time.Time { return readerNow },
	}
	if mutate != nil {
		mutate(&deps)
	}
	r, err := NewReader(deps)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestTheCanvasReadSumsEveryVersionAndMatchesTheDrawnHandles(t *testing.T) {
	store := &countingStore{rows: []choicestats.CountRow{
		{NodeID: "ask", OptionID: "", Shown: 10, Clicks: 8},
		{NodeID: "ask", OptionID: "sim", Shown: 10, Clicks: 6},
		{NodeID: "ask", OptionID: "nao", Shown: 10, Clicks: 2},
		{NodeID: "ask", OptionID: "velho", Shown: 3, Clicks: 3},
	}}
	report, err := newReader(t, store, nil).Workflow(context.Background(), workflow.Scope{WorkspaceID: "ws"}, "wf-1", "2026-09-23", "2026-10-21", nil)
	if err != nil {
		t.Fatal(err)
	}
	if report.From != "2026-09-23" || report.To != "2026-10-21" || report.Timezone != "BRT" || report.Version != nil {
		t.Fatalf("report = %+v", report)
	}
	if len(report.Nodes) != 1 || len(report.Nodes[0].Options) != 2 || report.Nodes[0].Options[0].OptionID != "sim" || report.Nodes[0].ClickRate != 8000 {
		t.Fatalf("nodes = %+v", report.Nodes)
	}
	q := store.queries[0]
	if q.WorkspaceID != "ws" || q.Kind != choicestats.SourceWorkflow || q.SourceID != "wf-1" || q.Version != nil ||
		!q.From.Equal(time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC)) || !q.To.Equal(time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("query = %+v", q)
	}

	version := 3
	if _, err := newReader(t, store, nil).Workflow(context.Background(), workflow.Scope{WorkspaceID: "ws"}, "wf-1", "2026-09-23", "2026-10-21", &version); err != nil {
		t.Fatal(err)
	}
	if store.queries[1].Version == nil || *store.queries[1].Version != 3 {
		t.Fatalf("version filter lost: %+v", store.queries[1])
	}
}

func TestTheReadsRefuseWhatTheyCannotVerify(t *testing.T) {
	zero := 0
	cases := []struct {
		name string
		read func(*Reader) error
		want error
	}{
		{"a period past the retention", func(r *Reader) error {
			_, err := r.Workflow(context.Background(), workflow.Scope{}, "wf-1", "2024-01-01", "2026-10-21", nil)
			return err
		}, choicestats.ErrInvalidPeriod},
		{"a workflow read without a period", func(r *Reader) error {
			_, err := r.Workflow(context.Background(), workflow.Scope{}, "wf-1", "", "", nil)
			return err
		}, choicestats.ErrInvalidPeriod},
		{"version zero", func(r *Reader) error {
			_, err := r.Workflow(context.Background(), workflow.Scope{}, "wf-1", "2026-10-01", "2026-10-21", &zero)
			return err
		}, choicestats.ErrInvalidVersion},
		{"a hidden campaign", func(r *Reader) error {
			_, err := r.WhatsAppCampaign(context.Background(), wc.ReportAccess{WorkspaceID: "ws", AllowDepartment: func(string) bool { return false }}, "c-1", "", "")
			return err
		}, wc.ErrCampaignNotFound},
		{"another workspace's campaign", func(r *Reader) error {
			_, err := r.WhatsAppCampaign(context.Background(), wc.ReportAccess{WorkspaceID: "other", AllowDepartment: func(string) bool { return true }}, "c-1", "", "")
			return err
		}, wc.ErrCampaignNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.read(newReader(t, &countingStore{}, nil)); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}

	unreadable := newReader(t, &countingStore{err: errors.New("db down")}, nil)
	if _, err := unreadable.Template(context.Background(), "ws", "tpl-1", "", ""); !errors.Is(err, choicestats.ErrUnavailable) {
		t.Fatalf("unreadable counters = %v, an unreadable body must never read as zeros", err)
	}
	noZone := newReader(t, &countingStore{}, func(d *ReaderDeps) { d.Zones = &fakeZones{err: errors.New("config down")} })
	if _, err := noZone.Template(context.Background(), "ws", "tpl-1", "", ""); !errors.Is(err, choicestats.ErrUnavailable) {
		t.Fatalf("no zone = %v", err)
	}
	if _, err := NewReader(ReaderDeps{}); !errors.Is(err, ErrReaderUnwired) {
		t.Fatalf("an unwired reader = %v", err)
	}
}

func TestCampaignAndTemplateReadsDefaultToTheirLifetimeAndListTheirOptions(t *testing.T) {
	store := &countingStore{rows: []choicestats.CountRow{{OptionID: "", Shown: 4, Clicks: 1}, {OptionID: "template_button:0", Shown: 4, Clicks: 1}}}
	reader := newReader(t, store, nil)

	official, err := reader.WhatsAppCampaign(context.Background(), wc.ReportAccess{WorkspaceID: "ws", AllowDepartment: func(id string) bool { return id == "sales" }}, "c-1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if official.From != nil || official.To != nil || store.queries[0].From != time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC) || official.Prompt.Sends != 4 || len(official.Prompt.Options) != 2 {
		t.Fatalf("official = %+v", official)
	}
	if store.queries[0].Kind != choicestats.SourceWhatsAppCampaign || store.queries[0].SourceID != "c-1" {
		t.Fatalf("query = %+v", store.queries[0])
	}
	if url := official.Prompt.Options[1]; url.OptionID != "template_url:1" || url.Tracked == nil || *url.Tracked {
		t.Fatalf("an untracked url must say so, got %+v", url)
	}

	unofficial, err := reader.UnofficialCampaign(context.Background(), "ws", uw.DepartmentScope{}, "u-1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if unofficial.From != nil || store.queries[1].From != time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC) || len(unofficial.Prompt.Options) != 2 || unofficial.Prompt.Options[0].OptionID != "a" || unofficial.Prompt.Options[0].Tracked != nil {
		t.Fatalf("unofficial = %+v", unofficial)
	}

	tpl, err := reader.Template(context.Background(), "ws", "tpl-1", "2026-10-01", "2026-10-21")
	if err != nil {
		t.Fatal(err)
	}
	if tpl.From == nil || *tpl.From != "2026-10-01" || tpl.To == nil || *tpl.To != "2026-10-21" || store.queries[2].TemplateID != "tpl-1" || store.queries[2].Kind != "" {
		t.Fatalf("template = %+v, query = %+v", tpl, store.queries[2])
	}

	missing := newReader(t, store, func(d *ReaderDeps) { d.Templates = fakeTemplates{err: template.ErrTemplateAccessDenied} })
	if _, err := missing.Template(context.Background(), "ws", "tpl-1", "", ""); !errors.Is(err, template.ErrTemplateNotFound) {
		t.Fatalf("a template of another workspace = %v", err)
	}
}

func TestWithoutUnofficialCampaignsThereIsNoUnofficialCampaignToRead(t *testing.T) {
	reader := newReader(t, &countingStore{}, func(d *ReaderDeps) { d.Unofficial = nil })
	if _, err := reader.UnofficialCampaign(context.Background(), "ws", uw.DepartmentScope{}, "u-1", "", ""); !errors.Is(err, uwc.ErrCampaignNotFound) {
		t.Fatalf("err = %v", err)
	}
}
