package copilottools

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/copilot"
	"vozko/domain/crmfilter"
	"vozko/domain/customfield"
	"vozko/domain/lead"
	"vozko/domain/leadarea"
	"vozko/domain/shared"
)

const (
	northArea = "3c9e1a2b-4d5f-4a6b-8c7d-9e0f1a2b3c4d"
	someOwner = "4b1c8a52-0d9e-4f6a-9c3e-2a7d5b8e1f00"
)

type fakePages struct {
	viewer conversation.Viewer
	listed *lead.ListLeadsInput
	items  []*lead.LeadWithSummary
	err    error
}

func (f *fakePages) List(_ context.Context, a conversation.Viewer, in lead.ListLeadsInput) (*shared.PaginatedResult[*lead.LeadWithSummary], error) {
	f.viewer, f.listed = a, &in
	if f.err != nil {
		return nil, f.err
	}
	return &shared.PaginatedResult[*lead.LeadWithSummary]{Items: f.items, Page: in.Options.Pagination.Page, TotalItems: 12, TotalPages: 2}, nil
}

type fakeAreas struct {
	areas  []leadarea.Area
	err    error
	listed int
}

func (f *fakeAreas) List(context.Context, conversation.Viewer) ([]leadarea.Area, error) {
	f.listed++
	return f.areas, f.err
}

type fakeDefinitions struct {
	defs []*customfield.Definition
	err  error
}

func (f fakeDefinitions) ListByObject(string, customfield.ObjectType) ([]*customfield.Definition, error) {
	return f.defs, f.err
}

func sensitiveStance() fakeDefinitions {
	return fakeDefinitions{defs: []*customfield.Definition{
		{Key: "posicao", ObjectType: customfield.ObjectLead, Type: customfield.TypeSelect, Options: []string{"Positivo", "Negativo"}, Sensitive: true, LegalBasis: "consentimento"},
		{Key: "interesse", ObjectType: customfield.ObjectLead, Type: customfield.TypeSelect, Options: []string{"alto", "baixo"}},
	}}
}

func filterDeps() (LeadDeps, *fakeAreas) {
	areas := &fakeAreas{areas: []leadarea.Area{
		{ID: northArea, WorkspaceID: "ws-1", Name: "Território Norte"},
		{ID: "5d0f2b3c-4e6a-4b7c-8d9e-0f1a2b3c4d5e", WorkspaceID: "ws-1", Name: "Centro expandido"},
	}}
	return LeadDeps{Areas: areas, Definitions: sensitiveStance()}, areas
}

func onLeads(filter *crmfilter.Filter) copilot.Context {
	cc := member()
	cc.View = copilot.View{Surface: copilot.SurfaceLeads, LeadFilter: filter}
	return cc
}

func group(field crmfilter.Field, op crmfilter.Operator, values ...string) crmfilter.Group {
	return predicate(field, op, values...)
}

func TestLeadFilterArgumentsBecomeTheLeadFilter(t *testing.T) {
	screen := &crmfilter.Filter{Groups: []crmfilter.Group{group(crmfilter.FieldState, crmfilter.OpIn, "SP")}}
	cases := []struct {
		name string
		cc   copilot.Context
		args leadFilterArgs
		want []crmfilter.Group
	}{
		{"a city written with its UF", member(), leadFilterArgs{Cidade: "Campinas/SP"},
			[]crmfilter.Group{group(crmfilter.FieldCity, crmfilter.OpIn, "sp:campinas")}},
		{"a city key from the geo summary", member(), leadFilterArgs{Cidade: "sp:campinas"},
			[]crmfilter.Group{group(crmfilter.FieldCity, crmfilter.OpIn, "sp:campinas")}},
		{"bairros by name inside a city", member(), leadFilterArgs{Cidade: "Campinas", UF: "SP", Bairros: []string{"Centro", "Jd. Guanabara"}},
			[]crmfilter.Group{group(crmfilter.FieldDistrict, crmfilter.OpIn, "sp:campinas/centro", "sp:campinas/jardim guanabara")}},
		{"a bairro pair from the geo summary", member(), leadFilterArgs{Bairros: []string{"sp:campinas/cambui"}},
			[]crmfilter.Group{group(crmfilter.FieldDistrict, crmfilter.OpIn, "sp:campinas/cambui")}},
		{"an area by its name", member(), leadFilterArgs{Area: "territorio norte"},
			[]crmfilter.Group{group(crmfilter.FieldArea, crmfilter.OpIn, northArea)}},
		{"an area by its id", member(), leadFilterArgs{Area: northArea},
			[]crmfilter.Group{group(crmfilter.FieldArea, crmfilter.OpIn, northArea)}},
		{"the user's own leads", memberWithID(someOwner), leadFilterArgs{OwnerID: "me"},
			[]crmfilter.Group{group(crmfilter.FieldOwner, crmfilter.OpIn, someOwner)}},
		{"leads without an owner", member(), leadFilterArgs{OwnerID: "none"},
			[]crmfilter.Group{group(crmfilter.FieldOwner, crmfilter.OpIsEmpty)}},
		{"a member as owner", member(), leadFilterArgs{OwnerID: someOwner},
			[]crmfilter.Group{group(crmfilter.FieldOwner, crmfilter.OpIn, someOwner)}},
		{"text and memories", member(), leadFilterArgs{Query: "maria", HasMemory: true},
			[]crmfilter.Group{group(crmfilter.FieldQuery, crmfilter.OpContains, "maria"), group(crmfilter.FieldMemoryCategory, crmfilter.OpIsSet)}},
		{"the screen filter narrowed by a city", onLeads(screen), leadFilterArgs{UseScreenFilter: true, Cidade: "sp:campinas"},
			[]crmfilter.Group{group(crmfilter.FieldState, crmfilter.OpIn, "SP"), group(crmfilter.FieldCity, crmfilter.OpIn, "sp:campinas")}},
		{"a state alone", member(), leadFilterArgs{UF: "sp"},
			[]crmfilter.Group{group(crmfilter.FieldState, crmfilter.OpIn, "SP")}},
		{"a state by its name", member(), leadFilterArgs{UF: "Minas Gerais"},
			[]crmfilter.Group{group(crmfilter.FieldState, crmfilter.OpIn, "MG")}},
		{"bairro pairs inside a state", member(), leadFilterArgs{UF: "SP", Bairros: []string{"sp:campinas/cambui"}},
			[]crmfilter.Group{group(crmfilter.FieldDistrict, crmfilter.OpIn, "sp:campinas/cambui"), group(crmfilter.FieldState, crmfilter.OpIn, "SP")}},
		{"the leads page without a filter", onLeads(nil), leadFilterArgs{UseScreenFilter: true}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			deps, _ := filterDeps()
			got, err := leadFilterOf(context.Background(), tc.cc, deps, tc.args)
			if err != nil {
				t.Fatalf("leadFilterOf = %v", err)
			}
			if !reflect.DeepEqual(got.Groups, tc.want) {
				t.Fatalf("groups = %+v\nwant %+v", got.Groups, tc.want)
			}
		})
	}
}

func TestLeadFilterArgumentsRefuseWhatCannotBeApplied(t *testing.T) {
	sensitiveScreen := &crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{
		{Field: crmfilter.FieldCustom, Key: "posicao", Operator: crmfilter.OpEquals, Values: []string{"Positivo"}},
	}}}}
	cases := []struct {
		name string
		cc   copilot.Context
		args leadFilterArgs
		want string
	}{
		{"a city without its state", member(), leadFilterArgs{Cidade: "Campinas"}, "UF"},
		{"a bairro without its city", member(), leadFilterArgs{Bairros: []string{"Centro"}}, "cidade"},
		{"an unknown state", member(), leadFilterArgs{UF: "XX"}, "uf"},
		{"an unknown area", member(), leadFilterArgs{Area: "Zona Sul"}, "Território Norte"},
		{"an owner that is not a member id", member(), leadFilterArgs{OwnerID: "maria"}, "owner_id"},
		{"the screen filter away from the leads page", member(), leadFilterArgs{UseScreenFilter: true}, "tela de Leads"},
		{"a screen filter on a sensitive field", onLeads(sensitiveScreen), leadFilterArgs{UseScreenFilter: true}, "sensível"},
		{"too many bairros", member(), leadFilterArgs{Cidade: "sp:campinas", Bairros: manyBairros(crmfilter.MaxDistrictPairs + 1)}, "bairros"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			deps, _ := filterDeps()
			_, err := leadFilterOf(context.Background(), tc.cc, deps, tc.args)
			if !errors.Is(err, errInvalidArgs) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want an invalid argument naming %q", err, tc.want)
			}
		})
	}
}

func TestLeadFilterArgumentsFailClosedWithoutTheirPorts(t *testing.T) {
	if _, err := leadFilterOf(context.Background(), member(), LeadDeps{}, leadFilterArgs{Area: "Território Norte"}); err == nil {
		t.Fatal("an area cannot be resolved without the area directory")
	}
	screen := &crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{
		{Field: crmfilter.FieldCustom, Key: "interesse", Operator: crmfilter.OpEquals, Values: []string{"alto"}},
	}}}}
	if _, err := leadFilterOf(context.Background(), onLeads(screen), LeadDeps{}, leadFilterArgs{UseScreenFilter: true}); err == nil {
		t.Fatal("a custom field cannot be checked for sensitivity without the field definitions")
	}
	failing := LeadDeps{Definitions: fakeDefinitions{err: errors.New("db down")}}
	if _, err := leadFilterOf(context.Background(), onLeads(screen), failing, leadFilterArgs{UseScreenFilter: true}); err == nil {
		t.Fatal("a failed definition read refuses")
	}
}

func manyBairros(n int) []string {
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, "Bairro "+strings.Repeat("x", i%7+1)+string(rune('a'+i%26))+string(rune('a'+(i/26)%26)))
	}
	return out
}

func memberWithID(userID string) copilot.Context {
	cc := member()
	cc.UserID = userID
	return cc
}

func TestEveryFilterArgumentCountsAsAFilter(t *testing.T) {
	for name, args := range map[string]leadFilterArgs{
		"query": {Query: "maria"}, "memories": {HasMemory: true}, "city": {Cidade: "sp:campinas"}, "state": {UF: "SP"},
		"bairros": {Bairros: []string{"sp:campinas/cambui"}}, "area": {Area: northArea}, "owner": {OwnerID: "me"}, "screen": {UseScreenFilter: true},
	} {
		if !args.given() {
			t.Fatalf("%s is not counted as a filter", name)
		}
	}
	if (leadFilterArgs{Bairros: []string{" "}}).given() || (leadFilterArgs{}).given() {
		t.Fatal("blank arguments are no filter")
	}
}
