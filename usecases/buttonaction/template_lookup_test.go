package buttonaction_usecase

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"

	"vozko/domain/buttonaction"
	"vozko/domain/shared"
	"vozko/domain/whatsapp/template"
)

func (f *fakeTemplates) FindByName(workspaceID, name, language string) ([]*template.Template, error) {
	want := buttonaction.NewTemplateRef(name, language)
	var out []*template.Template
	if workspaceID != ws {
		return out, nil
	}
	for _, id := range sortedIDs(f.byID) {
		if buttonaction.RefOf(f.byID[id]) == want {
			out = append(out, f.byID[id])
		}
	}
	return out, nil
}

func sortedIDs(byID map[string]*template.Template) []string {
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

type pagedTemplates struct {
	*fakeTemplates
	lookupErr error
	lookups   int
}

func (p *pagedTemplates) List(workspaceID string, input template.ListInput) (*shared.PaginatedResult[*template.Template], error) {
	out := &shared.PaginatedResult[*template.Template]{}
	for _, id := range sortedIDs(p.byID) {
		candidate := p.byID[id]
		if workspaceID == ws && candidate.Language == input.Language && strings.Contains(strings.ToLower(candidate.Name), strings.ToLower(input.Search)) {
			out.Items = append(out.Items, candidate)
		}
	}
	if size := input.Options.Pagination.PageSize; size > 0 && len(out.Items) > size {
		out.Items = out.Items[:size]
	}
	return out, nil
}

func (p *pagedTemplates) FindByName(workspaceID, name, language string) ([]*template.Template, error) {
	p.lookups++
	if p.lookupErr != nil {
		return nil, p.lookupErr
	}
	return p.fakeTemplates.FindByName(workspaceID, name, language)
}

func withPagedTemplates(t *testing.T, f *fixture) (*Service, *pagedTemplates) {
	t.Helper()
	paged := &pagedTemplates{fakeTemplates: f.templates}
	deps := f.svc.deps
	deps.Templates = paged
	svc, err := NewService(deps)
	if err != nil {
		t.Fatal(err)
	}
	return svc, paged
}

func addSubstringNamesakes(f *fixture, count int) {
	for i := 0; i < count; i++ {
		namesake := promoTemplate()
		namesake.ID = fmt.Sprintf("a-%03d", i)
		namesake.Name = fmt.Sprintf("promo_outubro_%03d", i)
		f.templates.byID[namesake.ID] = namesake
	}
}

func promoKey() TemplateKey {
	return TemplateKey{WorkspaceID: ws, TemplateName: "promo_outubro", Language: "pt_BR"}
}

func TestResolveFindsTheExactTemplateBeyondAHundredNamesakes(t *testing.T) {
	f := newFixture(t)
	addSubstringNamesakes(f, 150)
	svc, paged := withPagedTemplates(t, f)

	got, err := svc.ResolveAction(ctx, ResolveInput{TemplateKey: promoKey(), ButtonIndex: 1})
	if err != nil {
		t.Fatal(err)
	}
	if got.Reason == buttonaction.ReasonTemplateNotFound || got.Template != buttonaction.RefOf(promoTemplate()) {
		t.Fatalf("got %+v; the exact template sits after 150 substring matches", got)
	}
	if paged.lookups != 1 {
		t.Fatalf("exact lookups = %d", paged.lookups)
	}
}

func TestResolveMatchesTheNameWithoutCase(t *testing.T) {
	f := newFixture(t)
	svc, _ := withPagedTemplates(t, f)
	got, err := svc.ResolveAction(ctx, ResolveInput{TemplateKey: TemplateKey{WorkspaceID: ws, TemplateName: "  PROMO_Outubro ", Language: " pt_BR "}, ButtonIndex: 1})
	if err != nil || got.Template != buttonaction.RefOf(promoTemplate()) {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestResolveOfTwoExactNamesakesWithDifferentButtonsIsAmbiguous(t *testing.T) {
	f := newFixture(t)
	addSubstringNamesakes(f, 150)
	other := promoTemplate()
	other.ID = "z-other-waba"
	other.Name = "Promo_Outubro"
	other.Components[1].Buttons[1].Text = "Parar"
	f.templates.byID[other.ID] = other
	svc, _ := withPagedTemplates(t, f)

	got, err := svc.ResolveAction(ctx, ResolveInput{TemplateKey: promoKey(), ButtonIndex: 1})
	if err != nil || got.Reason != buttonaction.ReasonTemplateAmbiguous {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestResolveOfTwoExactNamesakesWithTheSameButtonsUsesEither(t *testing.T) {
	f := newFixture(t)
	other := promoTemplate()
	other.ID = "z-other-waba"
	f.templates.byID[other.ID] = other
	svc, _ := withPagedTemplates(t, f)

	got, err := svc.ResolveAction(ctx, ResolveInput{TemplateKey: promoKey(), ButtonIndex: 1})
	if err != nil || got.Reason == buttonaction.ReasonTemplateAmbiguous || got.Template != buttonaction.RefOf(promoTemplate()) {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestResolveWithOnlySubstringNamesakesIsNotFound(t *testing.T) {
	f := newFixture(t)
	delete(f.templates.byID, templateID)
	addSubstringNamesakes(f, 3)
	svc, _ := withPagedTemplates(t, f)

	got, err := svc.ResolveAction(ctx, ResolveInput{TemplateKey: promoKey(), ButtonIndex: 1})
	if err != nil || got.Reason != buttonaction.ReasonTemplateNotFound {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestResolveFailsClosedWhenTheExactLookupFails(t *testing.T) {
	f := newFixture(t)
	svc, paged := withPagedTemplates(t, f)
	paged.lookupErr = errors.New("database down")

	if _, err := svc.ResolveAction(ctx, ResolveInput{TemplateKey: promoKey(), ButtonIndex: 1}); !errors.Is(err, paged.lookupErr) {
		t.Fatalf("got %v, want the lookup error", err)
	}
	if _, err := svc.TemplateExit(ctx, promoKey()); !errors.Is(err, paged.lookupErr) {
		t.Fatalf("template exit: got %v, want the lookup error", err)
	}
}
