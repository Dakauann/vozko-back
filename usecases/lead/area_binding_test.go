package lead_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/lead"
	"vozko/domain/leadarea"
)

func TestTheLeadListResolvesAreasBeforeCompiling(t *testing.T) {
	var seen []lead.ListLeadsInput
	areas := &mapAreas{updated: time.Unix(100, 0)}
	pages, err := NewPages(PageDeps{
		Leads: pageQueries{seen: &seen}, Contacts: &pageContacts{}, Permissions: mapViewer,
		Definitions: &fakeDefinitions{defs: leadDefinitions()}, Names: namesOf{}, Zones: fixedZones{loc: time.UTC}, Areas: areas,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pages.List(context.Background(), operator(), lead.ListLeadsInput{WorkspaceID: cmdWorkspace, Filter: areaOnly()}); err != nil {
		t.Fatal(err)
	}
	if _, bound := seen[0].Filter.Groups[0].Predicates[0].BoundKind(); !bound || areas.calls != 1 {
		t.Fatalf("the list must receive the resolved area, got %+v", seen[0].Filter)
	}
	areas.err = leadarea.ErrNotFound
	if _, err := pages.List(context.Background(), operator(), lead.ListLeadsInput{WorkspaceID: cmdWorkspace, Filter: areaOnly()}); !errors.Is(err, leadarea.ErrNotFound) {
		t.Fatalf("a deleted area in the table = %v, want ErrNotFound", err)
	}
}

func TestWithoutAnAreaDirectoryAnAreaFilterIsRefused(t *testing.T) {
	var seen []lead.ListLeadsInput
	pages, err := NewPages(PageDeps{
		Leads: pageQueries{seen: &seen}, Contacts: &pageContacts{}, Permissions: mapViewer,
		Definitions: &fakeDefinitions{defs: leadDefinitions()}, Names: namesOf{}, Zones: fixedZones{loc: time.UTC},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pages.List(context.Background(), operator(), lead.ListLeadsInput{WorkspaceID: cmdWorkspace, Filter: areaOnly()}); !errors.Is(err, lead.ErrLeadFilterInvalid) {
		t.Fatalf("an area filter without the directory = %v, want ErrLeadFilterInvalid", err)
	}
	if len(seen) != 0 {
		t.Fatal("a refused area filter lists nothing")
	}
	if _, err := pages.List(context.Background(), operator(), lead.ListLeadsInput{WorkspaceID: cmdWorkspace}); err != nil {
		t.Fatalf("a filter without areas still lists: %v", err)
	}
}

func TestLeadSectionsResolveAreasBeforeCompiling(t *testing.T) {
	reader := &sectionReader{}
	areas := &mapAreas{updated: time.Unix(100, 0)}
	sections, err := NewSections(SectionDeps{
		Reader: reader, Permissions: mapViewer, Definitions: &fakeDefinitions{defs: leadDefinitions()},
		Names: namesOf{}, Zones: fixedZones{loc: time.UTC}, Areas: areas,
		Caching: SectionCaching{Memo: &sectionMemo{values: map[string][]byte{}}, Gate: &sectionGate{}, Versions: &sectionVersions{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sections.Places(context.Background(), operator(), areaOnly()); err != nil {
		t.Fatal(err)
	}
	if _, bound := reader.queries[0].Filter.Groups[0].Predicates[0].BoundKind(); !bound {
		t.Fatalf("the section must receive the resolved area, got %+v", reader.queries[0].Filter)
	}
}

func TestASectionReadsAgainWhenAnAreaIsReshapedEvenIfTheGenerationStays(t *testing.T) {
	reader := &sectionReader{}
	areas := &mapAreas{updated: time.Unix(100, 0)}
	versions := &sectionVersions{generation: 7}
	sections, err := NewSections(SectionDeps{
		Reader: reader, Permissions: mapViewer, Definitions: &fakeDefinitions{defs: leadDefinitions()},
		Names: namesOf{}, Zones: fixedZones{loc: time.UTC}, Areas: areas,
		Caching: SectionCaching{Memo: &sectionMemo{values: map[string][]byte{}}, Gate: &sectionGate{}, Versions: versions},
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := sections.Places(context.Background(), operator(), areaOnly()); err != nil {
			t.Fatal(err)
		}
	}
	if len(reader.queries) != 1 {
		t.Fatalf("the same area twice reads once, read %d", len(reader.queries))
	}
	areas.updated = time.Unix(200, 0)
	if _, err := sections.Places(context.Background(), operator(), areaOnly()); err != nil {
		t.Fatal(err)
	}
	if len(reader.queries) != 2 || versions.generation != 7 {
		t.Fatalf("a reshaped area reads again without a generation bump: %d reads, generation %d", len(reader.queries), versions.generation)
	}
}
