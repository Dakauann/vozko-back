package report_renderers

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"vozko/domain/address"
	"vozko/domain/customfield"
	"vozko/domain/lead"
	"vozko/domain/leadaction"
	"vozko/domain/report"
	"vozko/domain/workspace"
)

type fakeLeadExport struct {
	pages    [][]string
	leads    map[string]*lead.Lead
	defs     []*customfield.Definition
	afters   []string
	attached int
}

func (f *fakeLeadExport) Snapshot(_ context.Context, _, _, after string, _ int) ([]string, error) {
	f.afters = append(f.afters, after)
	index := len(f.afters) - 1
	if index >= len(f.pages) {
		return nil, nil
	}
	return f.pages[index], nil
}

func (f *fakeLeadExport) FindByIDs(_ string, ids []string) ([]*lead.Lead, error) {
	out := make([]*lead.Lead, 0, len(ids))
	for i := len(ids) - 1; i >= 0; i-- {
		if l, ok := f.leads[ids[i]]; ok {
			copied := *l
			out = append(out, &copied)
		}
	}
	return out, nil
}

func (f *fakeLeadExport) AttachContactDetails(_ context.Context, _ string, leads []*lead.Lead) error {
	f.attached += len(leads)
	return nil
}

func (f *fakeLeadExport) ListByObject(string, customfield.ObjectType) ([]*customfield.Definition, error) {
	return f.defs, nil
}

func (f *fakeLeadExport) Names(ids ...string) map[string]string {
	return map[string]string{"4b1c8a52-0d9e-4f6a-9c3e-2a7d5b8e1f00": "Marina"}
}

func exportSource() *fakeLeadExport {
	home := address.Postal{ZipCode: "30140071", Street: "Rua da Bahia", Number: "100", District: "Centro", City: "Belo Horizonte", State: "MG"}
	return &fakeLeadExport{
		pages: [][]string{{"l-1", "l-2"}, {"l-3"}},
		leads: map[string]*lead.Lead{
			"l-1": {ID: "l-1", Name: "Ana", Number: "5531987654321", Owner: "4b1c8a52-0d9e-4f6a-9c3e-2a7d5b8e1f00",
				CustomFields: map[string]any{"cor": "azul", "classificacao": "Positivo"},
				Addresses:    []lead.Address{{Primary: true, Postal: home}}},
			"l-2": {ID: "l-2", Name: "=cmd()", Number: "5531912345678"},
			"l-3": {ID: "l-3", Name: "Caio"},
		},
		defs: []*customfield.Definition{
			{Key: "cor", Label: "Cor", ObjectType: customfield.ObjectLead, Type: customfield.TypeText, Position: 1},
			{Key: "classificacao", Label: "Classificação", ObjectType: customfield.ObjectLead, Type: customfield.TypeSelect, Options: []string{"Positivo"}, Sensitive: true, Position: 0},
		},
	}
}

func leadsJob(t *testing.T, params LeadsParams) report.Job {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	return report.Job{WorkspaceID: "ws-1", RequestedBy: "u-1", Format: report.FormatCSV, Locale: "pt", Params: raw}
}

func TestLeadsPolicyIsTheCatalogRequirementOfTheExportCapability(t *testing.T) {
	r := NewLeadsRenderer(exportSource())
	cases := []struct {
		params LeadsParams
		tier   string
	}{
		{LeadsParams{SnapshotID: "s"}, "basic"},
		{LeadsParams{SnapshotID: "s", Addresses: true}, "addresses"},
		{LeadsParams{SnapshotID: "s", Addresses: true, Sensitive: true}, "addresses+sensitive"},
	}
	for _, tc := range cases {
		want, err := workspace.CapabilitiesRequire(leadaction.ActionExport.Requirements(leadaction.Params{Addresses: tc.params.Addresses, Sensitive: tc.params.Sensitive}, false))
		if err != nil {
			t.Fatal(err)
		}
		policy, err := r.Policy(leadsJob(t, tc.params))
		if err != nil || !reflect.DeepEqual(policy.Required, want) || policy.Tier != tc.tier || policy.RequesterOnly {
			t.Errorf("%+v policy = %+v, %v", tc.params, policy, err)
		}
	}
	basic, _ := r.Policy(leadsJob(t, LeadsParams{SnapshotID: "s"}))
	keys := map[string]bool{}
	for _, p := range basic.Required {
		keys[p.Key()] = true
	}
	for _, key := range []string{"leads:read", "leads:export", "reports:create", "reports:read"} {
		if !keys[key] {
			t.Fatalf("the export policy lost %s: %v", key, basic.Required)
		}
	}
	if !r.Internal() {
		t.Fatal("the leads kind is only created by the lead export action")
	}
	if _, err := r.Policy(report.Job{Params: json.RawMessage(`{}`)}); !errors.Is(err, report.ErrNoPolicy) {
		t.Fatalf("a job without a snapshot = %v", err)
	}
}

func TestLeadsRenderWalksTheSnapshotAndProjectsThePermittedColumns(t *testing.T) {
	source := exportSource()
	renderer := NewLeadsRenderer(source)
	renderer.page = 2
	artifact, err := renderer.Render(context.Background(), leadsJob(t, LeadsParams{SnapshotID: "s"}), func(int) {})
	if err != nil {
		t.Fatal(err)
	}
	if artifact.RowCount != 3 || !reflect.DeepEqual(source.afters, []string{"", "l-2"}) || source.attached != 3 {
		t.Fatalf("rows = %d, afters = %v, attached = %d", artifact.RowCount, source.afters, source.attached)
	}
	text := string(artifact.Data)
	lines := strings.Split(strings.TrimPrefix(text, report.UTF8BOM), report.CSVNewline)
	if !strings.HasPrefix(lines[0], "ID;Nome;Número") || !strings.HasSuffix(lines[0], ";Cor") {
		t.Fatalf("header = %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "l-1;Ana;5531987654321") || !strings.Contains(lines[1], "Marina") || !strings.Contains(lines[1], "Centro;Belo Horizonte;MG") {
		t.Fatalf("first row = %q", lines[1])
	}
	if strings.Contains(text, "Rua da Bahia") || strings.Contains(text, "30140071") || strings.Contains(text, "Positivo") || strings.Contains(text, "Classificação") {
		t.Fatalf("a basic export leaked a street, a CEP or a sensitive value:\n%s", text)
	}
	if !strings.Contains(lines[2], "'=cmd()") {
		t.Fatalf("a formula was not neutralised: %q", lines[2])
	}
}

func TestLeadsRenderWithEveryTier(t *testing.T) {
	artifact, err := NewLeadsRenderer(exportSource()).Render(context.Background(), leadsJob(t, LeadsParams{SnapshotID: "s", Addresses: true, Sensitive: true}), func(int) {})
	if err != nil {
		t.Fatal(err)
	}
	text := string(artifact.Data)
	if !strings.Contains(text, "Rua da Bahia") || !strings.Contains(text, "30140071") || !strings.Contains(text, "Positivo") || !strings.Contains(text, "Classificação") {
		t.Fatalf("a full export misses columns:\n%s", text)
	}
}

func TestLeadsRenderOfAnEmptySnapshot(t *testing.T) {
	source := exportSource()
	source.pages = nil
	if _, err := NewLeadsRenderer(source).Render(context.Background(), leadsJob(t, LeadsParams{SnapshotID: "s"}), func(int) {}); !errors.Is(err, report.ErrEmptyResult) {
		t.Fatalf("an empty snapshot = %v", err)
	}
	if _, err := NewLeadsRenderer(nil).Render(context.Background(), leadsJob(t, LeadsParams{SnapshotID: "s"}), func(int) {}); !errors.Is(err, report.ErrNoRenderer) {
		t.Fatalf("a renderer without its source = %v", err)
	}
}
