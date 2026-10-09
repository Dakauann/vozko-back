package lead_usecase

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"vozko/domain/cache"
	"vozko/domain/crmfilter"
	"vozko/domain/customfield"
	"vozko/domain/lead"
	"vozko/domain/leadmap"
)

type sectionMemo struct {
	mu     sync.Mutex
	values map[string][]byte
	keys   []string
}

func (m *sectionMemo) Remember(ctx context.Context, key string, _ time.Duration, compute func(context.Context) ([]byte, error)) ([]byte, error) {
	m.mu.Lock()
	m.keys = append(m.keys, key)
	cached, ok := m.values[key]
	m.mu.Unlock()
	if ok {
		return cached, nil
	}
	value, err := compute(ctx)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.values[key] = value
	m.mu.Unlock()
	return value, nil
}

type sectionGate struct {
	acquired int
	busy     bool
}

func (g *sectionGate) Acquire(context.Context) (func(), error) {
	if g.busy {
		return nil, cache.ErrGateBusy
	}
	g.acquired++
	return func() {}, nil
}

type sectionVersions struct {
	generation int
	err        error
}

func (v *sectionVersions) Version(string) (string, error) {
	if v.err != nil {
		return "", v.err
	}
	return strconv.Itoa(v.generation), nil
}

func (v *sectionVersions) Bump(string) error { v.generation++; return nil }

type sectionReader struct {
	queries  []lead.SectionQuery
	summary  lead.SummarySection
	facets   lead.FacetsSection
	places   lead.PlacesSection
	failWith error
}

func (r *sectionReader) ReadSummary(_ context.Context, q lead.SectionQuery) (*lead.SummarySection, error) {
	r.queries = append(r.queries, q)
	if r.failWith != nil {
		return nil, r.failWith
	}
	out := r.summary
	return &out, nil
}

func (r *sectionReader) ReadFacets(_ context.Context, q lead.SectionQuery) (*lead.FacetsSection, error) {
	r.queries = append(r.queries, q)
	if r.failWith != nil {
		return nil, r.failWith
	}
	out := r.facets
	out.Owners = append([]lead.OwnerCount(nil), r.facets.Owners...)
	return &out, nil
}

func (r *sectionReader) ReadPlaces(_ context.Context, q lead.SectionQuery) (*lead.PlacesSection, error) {
	r.queries = append(r.queries, q)
	if r.failWith != nil {
		return nil, r.failWith
	}
	out := r.places
	return &out, nil
}

type fixedZones struct {
	loc *time.Location
	err error
}

func (z fixedZones) Location(context.Context, string, string) (*time.Location, error) {
	return z.loc, z.err
}

type namesOf map[string]string

func (n namesOf) Names(ids ...string) map[string]string {
	out := map[string]string{}
	for _, id := range ids {
		if name, ok := n[id]; ok {
			out[id] = name
		}
	}
	return out
}

type sectionRig struct {
	reader   *sectionReader
	memo     *sectionMemo
	gate     *sectionGate
	versions *sectionVersions
	sections *Sections
}

func classifiedDefinitions() []*customfield.Definition {
	defs := leadDefinitions()
	defs[2].Role = customfield.RoleClassification
	return defs
}

func newSectionRig(t *testing.T, perms fakePermissions) *sectionRig {
	t.Helper()
	saoPaulo, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		t.Skip("no time zone database")
	}
	rig := &sectionRig{
		reader:   &sectionReader{summary: lead.SummarySection{Total: 9}, facets: lead.FacetsSection{Owners: []lead.OwnerCount{{Owner: cmdUser, Count: 3}}}},
		memo:     &sectionMemo{values: map[string][]byte{}},
		gate:     &sectionGate{},
		versions: &sectionVersions{generation: 4},
	}
	rig.sections, err = NewSections(SectionDeps{
		Reader:      rig.reader,
		Permissions: perms,
		Definitions: &fakeDefinitions{defs: classifiedDefinitions()},
		Names:       namesOf{cmdUser: "Marina"},
		Zones:       fixedZones{loc: saoPaulo},
		Caching:     SectionCaching{Memo: rig.memo, Gate: rig.gate, Versions: rig.versions, TTL: time.Minute},
		Now:         func() time.Time { return time.Date(2026, time.October, 9, 1, 30, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	return rig
}

func TestSections_SummaryCountsTheWorkspaceDayOnceAndIsMemoised(t *testing.T) {
	rig := newSectionRig(t, fakePermissions{"leads:read": true})
	first, err := rig.sections.Summary(context.Background(), operator(), crmfilter.Filter{})
	if err != nil {
		t.Fatalf("Summary() error = %v", err)
	}
	if first.Total != 9 {
		t.Fatalf("Summary() = %+v", first)
	}
	if _, err := rig.sections.Summary(context.Background(), operator(), crmfilter.Filter{}); err != nil {
		t.Fatal(err)
	}
	if len(rig.reader.queries) != 1 || rig.gate.acquired != 1 {
		t.Fatalf("a repeated section read %d times through %d gate slots, want once", len(rig.reader.queries), rig.gate.acquired)
	}
	today := rig.reader.queries[0].Today
	if today.Day() != 8 || today.Month() != time.October {
		t.Fatalf("today = %v, want the 8th in the workspace time zone (01:30 UTC is still the 8th in Sao Paulo)", today)
	}
	rig.versions.generation++
	if _, err := rig.sections.Summary(context.Background(), operator(), crmfilter.Filter{}); err != nil {
		t.Fatal(err)
	}
	if len(rig.reader.queries) != 2 {
		t.Fatal("a lead write moves the generation and the section is read again")
	}
}

func TestSections_KeysSeparateViewerTiersAndSections(t *testing.T) {
	operatorRig := newSectionRig(t, fakePermissions{"leads:read": true})
	managerRig := newSectionRig(t, withSensitive(withAddresses(fakePermissions{"leads:read": true})))
	if _, err := operatorRig.sections.Facets(context.Background(), operator(), crmfilter.Filter{}); err != nil {
		t.Fatal(err)
	}
	if _, err := managerRig.sections.Facets(context.Background(), operator(), crmfilter.Filter{}); err != nil {
		t.Fatal(err)
	}
	if _, err := managerRig.sections.Places(context.Background(), operator(), crmfilter.Filter{}); err != nil {
		t.Fatal(err)
	}
	if operatorRig.memo.keys[0] == managerRig.memo.keys[0] {
		t.Fatal("a viewer without the sensitive tier must never share a memo with one that has it")
	}
	if managerRig.memo.keys[0] == managerRig.memo.keys[1] {
		t.Fatal("two sections must not share a memo")
	}
}

func TestSections_FacetsCarryOwnerNamesAndOnlyAVisibleClassification(t *testing.T) {
	operatorRig := newSectionRig(t, fakePermissions{"leads:read": true})
	got, err := operatorRig.sections.Facets(context.Background(), operator(), crmfilter.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Owners) != 1 || got.Owners[0].Name != "Marina" {
		t.Fatalf("owners = %+v, want the name resolved", got.Owners)
	}
	if key := operatorRig.reader.queries[0].ClassificationKey; key != "" {
		t.Fatalf("a sensitive classification was asked for a viewer who cannot read it: %q", key)
	}

	managerRig := newSectionRig(t, withSensitive(fakePermissions{"leads:read": true}))
	if _, err := managerRig.sections.Facets(context.Background(), operator(), crmfilter.Filter{}); err != nil {
		t.Fatal(err)
	}
	if key := managerRig.reader.queries[0].ClassificationKey; key != "classificacao" {
		t.Fatalf("classification key = %q, want the visible classification", key)
	}
}

func TestSections_FacetsColoredByCountTheChosenSelectField(t *testing.T) {
	rig := newSectionRig(t, fakePermissions{"leads:read": true})
	if _, err := rig.sections.FacetsColoredBy(context.Background(), operator(), crmfilter.Filter{}, " Interesse "); err != nil {
		t.Fatal(err)
	}
	if key := rig.reader.queries[0].ClassificationKey; key != "interesse" {
		t.Fatalf("classification key = %q, want the field the map is coloured by", key)
	}
	if _, err := rig.sections.Facets(context.Background(), operator(), crmfilter.Filter{}); err != nil {
		t.Fatal(err)
	}
	if len(rig.memo.keys) != 2 || rig.memo.keys[0] == rig.memo.keys[1] {
		t.Fatalf("memo keys = %v, want the coloured facets apart from the plain ones", rig.memo.keys)
	}
}

func TestSections_FacetsColoredByRefusesFieldsTheViewerCannotColourBy(t *testing.T) {
	rig := newSectionRig(t, fakePermissions{"leads:read": true})
	cases := []struct {
		key  string
		want error
	}{
		{"cor", leadmap.ErrColorByInvalid},
		{"sumido", leadmap.ErrColorByInvalid},
		{"classificacao", leadmap.ErrColorByForbidden},
	}
	for _, c := range cases {
		if _, err := rig.sections.FacetsColoredBy(context.Background(), operator(), crmfilter.Filter{}, c.key); !errors.Is(err, c.want) {
			t.Fatalf("FacetsColoredBy(%q) error = %v, want %v", c.key, err, c.want)
		}
	}
	if len(rig.reader.queries) != 0 {
		t.Fatal("a refused colour field must never reach the reader")
	}
	manager := newSectionRig(t, withSensitive(fakePermissions{"leads:read": true}))
	if _, err := manager.sections.FacetsColoredBy(context.Background(), operator(), crmfilter.Filter{}, "classificacao"); err != nil {
		t.Fatalf("a viewer with the sensitive tier colours by a sensitive select field: %v", err)
	}
}

func TestSections_FacetsColoredByNothingKeepsTheClassification(t *testing.T) {
	rig := newSectionRig(t, withSensitive(fakePermissions{"leads:read": true}))
	if _, err := rig.sections.FacetsColoredBy(context.Background(), operator(), crmfilter.Filter{}, ""); err != nil {
		t.Fatal(err)
	}
	if key := rig.reader.queries[0].ClassificationKey; key != "classificacao" {
		t.Fatalf("classification key = %q, want the visible classification", key)
	}
}

func TestSections_Refusals(t *testing.T) {
	sensitive := crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{
		{Field: crmfilter.FieldCustom, Key: "classificacao", Operator: crmfilter.OpEquals, Values: []string{"positivo"}},
	}}}}
	rig := newSectionRig(t, fakePermissions{"leads:read": true})
	if _, err := rig.sections.Summary(context.Background(), operator(), sensitive); !errors.Is(err, customfield.ErrFilterSensitive) {
		t.Fatalf("a sensitive predicate without the permission = %v, want ErrFilterSensitive", err)
	}
	if len(rig.reader.queries) != 0 {
		t.Fatal("a refused filter must not reach the database")
	}

	if _, err := newSectionRig(t, fakePermissions{}).sections.Summary(context.Background(), operator(), crmfilter.Filter{}); !errors.Is(err, lead.ErrLeadForbidden) {
		t.Fatalf("without leads:read = %v", err)
	}

	busy := newSectionRig(t, fakePermissions{"leads:read": true})
	busy.gate.busy = true
	if _, err := busy.sections.Places(context.Background(), operator(), crmfilter.Filter{}); !errors.Is(err, cache.ErrGateBusy) {
		t.Fatalf("a busy gate = %v, want ErrGateBusy", err)
	}

	broken := newSectionRig(t, fakePermissions{"leads:read": true})
	broken.reader.failWith = errors.New("statement timeout")
	if _, err := broken.sections.Facets(context.Background(), operator(), crmfilter.Filter{}); err == nil {
		t.Fatal("a failed read must fail the section")
	}

	unknownKey := crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{
		{Field: crmfilter.FieldCustom, Key: "inexistente", Operator: crmfilter.OpEquals, Values: []string{"x"}},
	}}}}
	if _, err := rig.sections.Facets(context.Background(), operator(), unknownKey); !errors.Is(err, lead.ErrLeadFilterInvalid) {
		t.Fatalf("an unknown custom key = %v, want ErrLeadFilterInvalid", err)
	}

	for name, drop := range map[string]func(*SectionDeps){
		"reader":      func(d *SectionDeps) { d.Reader = nil },
		"permissions": func(d *SectionDeps) { d.Permissions = nil },
		"definitions": func(d *SectionDeps) { d.Definitions = nil },
		"names":       func(d *SectionDeps) { d.Names = nil },
		"zones":       func(d *SectionDeps) { d.Zones = nil },
		"memo":        func(d *SectionDeps) { d.Caching.Memo = nil },
		"gate":        func(d *SectionDeps) { d.Caching.Gate = nil },
		"versions":    func(d *SectionDeps) { d.Caching.Versions = nil },
	} {
		deps := SectionDeps{
			Reader: &sectionReader{}, Permissions: fakePermissions{}, Definitions: &fakeDefinitions{}, Names: namesOf{}, Zones: fixedZones{loc: time.UTC},
			Caching: SectionCaching{Memo: &sectionMemo{}, Gate: &sectionGate{}, Versions: &sectionVersions{}},
		}
		drop(&deps)
		if _, err := NewSections(deps); err == nil {
			t.Fatalf("sections without %s must not be built", name)
		}
	}
}

func TestSections_AnUnreadableGenerationStillAnswersThroughTheGate(t *testing.T) {
	rig := newSectionRig(t, fakePermissions{"leads:read": true})
	rig.versions.err = errors.New("redis down")
	if _, err := rig.sections.Summary(context.Background(), operator(), crmfilter.Filter{}); err != nil {
		t.Fatalf("Summary() error = %v", err)
	}
	if rig.gate.acquired != 1 || len(rig.memo.keys) != 0 {
		t.Fatalf("without a generation the section is computed behind the gate and never memoised, gate %d memo %d", rig.gate.acquired, len(rig.memo.keys))
	}
}
