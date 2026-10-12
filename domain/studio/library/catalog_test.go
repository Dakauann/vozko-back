package library

import (
	"regexp"
	"testing"
)

var itemID = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

func mustCatalog(t *testing.T) *Catalog {
	t.Helper()
	c, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func ids(items []Item) []string {
	out := make([]string, len(items))
	for i, item := range items {
		out[i] = item.ID
	}
	return out
}

func TestTheEmbeddedCatalogIsComplete(t *testing.T) {
	c := mustCatalog(t)
	categories := map[string]Category{}
	for _, category := range c.Categories {
		categories[category.ID] = category
		for _, locale := range Locales {
			if category.Names[locale] == "" {
				t.Errorf("category %s has no %s name", category.ID, locale)
			}
		}
	}
	for _, item := range c.Items {
		if !itemID.MatchString(item.ID) {
			t.Errorf("item id %q is not a lowercase token", item.ID)
		}
		category, ok := categories[item.Category]
		if !ok || category.Panel != item.Panel {
			t.Errorf("item %s points at category %q in panel %q", item.ID, item.Category, item.Panel)
		}
		for _, locale := range Locales {
			if item.Names[locale] == "" {
				t.Errorf("item %s has no %s name", item.ID, locale)
			}
			for _, slot := range item.Slots {
				if slot.Samples[locale] == "" {
					t.Errorf("item %s slot %s has no %s sample", item.ID, slot.ID, locale)
				}
			}
		}
		if len(item.Editors) == 0 || item.Ratio <= 0 {
			t.Errorf("item %s has no editor or no ratio", item.ID)
		}
		for _, editor := range item.Editors {
			if editor != "image" && editor != "video" {
				t.Errorf("item %s names editor %q", item.ID, editor)
			}
			if len(item.Colors[editor]) == 0 && item.Category != "controls" && !item.Sound() && item.Kind != KindAsset && !(editor == "video" && item.Frames()) {
				t.Errorf("item %s lists no colors for %s", item.ID, editor)
			}
		}
		if item.Kind == "clips" && item.Supports("image") {
			t.Errorf("item %s inserts clips, which the image editor cannot hold", item.ID)
		}
		if !contains(Kinds, item.Kind) {
			t.Errorf("item %s has kind %q", item.ID, item.Kind)
		}
	}
}

func TestSearchFoldsAccentsAndMatchesEnglishToo(t *testing.T) {
	c := mustCatalog(t)
	if got := ids(c.Search(Query{Text: "coracao", Locale: "pt-BR"})); len(got) == 0 || got[0] != "shape-heart" {
		t.Fatalf("coracao = %v", got)
	}
	if got := ids(c.Search(Query{Text: "heart", Locale: "pt"})); len(got) == 0 || got[0] != "shape-heart" {
		t.Fatalf("an English word must find the Portuguese item: %v", got)
	}
	if got := ids(c.Search(Query{Text: "SETA dupla", Locale: "pt"})); len(got) == 0 || got[0] != "shape-double-arrow" {
		t.Fatalf("every word must match: %v", got)
	}
	if got := ids(c.Search(Query{Text: "Herz", Locale: "de"})); len(got) == 0 || got[0] != "shape-heart" {
		t.Fatalf("German names are searchable: %v", got)
	}
}

func TestSearchFiltersByEditorAndCategory(t *testing.T) {
	c := mustCatalog(t)
	for _, item := range c.Search(Query{Text: "nulo", Editor: "image", Locale: "pt"}) {
		if item.Category == "controls" {
			t.Fatalf("the image editor has no nulls: %s", item.ID)
		}
	}
	if got := ids(c.Search(Query{Text: "nulo", Editor: "video", Locale: "pt"})); len(got) == 0 || got[0] != "control-null" {
		t.Fatalf("nulo in video = %v", got)
	}
	icons := c.Search(Query{Category: "icons", Editor: "image"})
	if len(icons) == 0 {
		t.Fatal("the icons category is empty")
	}
	for _, item := range icons {
		if item.Category != "icons" {
			t.Fatalf("category filter leaked %s", item.ID)
		}
	}
}

func TestTheNameOutranksATagOrAnIDWord(t *testing.T) {
	name, terms := Item{ID: "shape-star", Names: Names{"pt": "Estrela", "en": "Star"}}.terms("pt")
	if rank, ok := MatchRank("estrela", name, terms); !ok || rank != 0 {
		t.Fatalf("exact name rank = %d %v", rank, ok)
	}
	if rank, ok := MatchRank("estr", name, terms); !ok || rank != 1 {
		t.Fatalf("name prefix rank = %d %v", rank, ok)
	}
	if rank, ok := MatchRank("star", name, terms); !ok || rank != 2 {
		t.Fatalf("english word rank = %d %v", rank, ok)
	}
	if _, ok := MatchRank("lua", name, terms); ok {
		t.Fatal("an unrelated word matched")
	}
}

func TestClosestSuggestsNearbyIDs(t *testing.T) {
	c := mustCatalog(t)
	if got := c.Closest("shape-hart", "image", 3); len(got) != 3 || got[0] != "shape-heart" {
		t.Fatalf("closest = %v", got)
	}
	for _, id := range c.Closest("null", "image", 5) {
		if id == "control-null" {
			t.Fatal("suggestions must belong to the editor")
		}
	}
}

const sharedBody = `{"category":"icons","panel":"elements","editors":["image","video"],"kind":"layers","ratio":1,"names":{"pt":"Bolota","en":"Acorn"},"tags":{"en":["nut"]},"colors":{"image":["#111111"]},"source":{"set":"phosphor","license":"MIT"}}`

func TestParseExpandsSharedBodiesInOrder(t *testing.T) {
	raw := `{"version":2,"shared":[` + sharedBody + `],"items":[{"id":"icon-acorn-regular","subcategory":"regular","base":0},{"id":"own","category":"controls","panel":"elements","editors":["video"],"kind":"clips","ratio":1,"names":{"pt":"Nulo"},"colors":{},"source":{"set":"vozko","license":"own"}},{"id":"icon-acorn-bold","subcategory":"bold","base":0,"names":{"pt":"Bolota (negrito)","en":"Acorn (bold)"}}]}`
	c, err := Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(c.Items); len(got) != 3 || got[0] != "icon-acorn-regular" || got[1] != "own" || got[2] != "icon-acorn-bold" {
		t.Fatalf("items = %v", got)
	}
	bold, ok := c.Find("icon-acorn-bold")
	if !ok || bold.Subcategory != "bold" || bold.Category != "icons" || bold.Names.In("en") != "Acorn (bold)" || bold.Tags["en"][0] != "nut" || bold.Source.Set != "phosphor" {
		t.Fatalf("bold = %+v", bold)
	}
	if regular, _ := c.Find("icon-acorn-regular"); regular.Names.In("en") != "Acorn" {
		t.Fatalf("a variant's own name leaked into the shared body: %+v", regular)
	}
	if found := c.Search(Query{Text: "acorn"}); len(found) != 2 {
		t.Fatalf("search = %v", ids(found))
	}
}

func TestParseRefusesABrokenCatalog(t *testing.T) {
	for name, raw := range map[string]string{
		"not json":          `{`,
		"old version":       `{"version":1,"items":[{"id":"a"}]}`,
		"repeated id":       `{"version":2,"items":[{"id":"a"},{"id":"a"}]}`,
		"empty":             `{"version":2,"items":[]}`,
		"missing body":      `{"version":2,"shared":[` + sharedBody + `],"items":[{"id":"a","base":1}]}`,
		"negative body":     `{"version":2,"shared":[` + sharedBody + `],"items":[{"id":"a","base":-1}]}`,
		"body and own data": `{"version":2,"shared":[` + sharedBody + `],"items":[{"id":"a","base":0,"category":"emoji"}]}`,
		"body with an id":   `{"version":2,"shared":[{"id":"x","category":"icons"}],"items":[{"id":"a","base":0}]}`,
	} {
		if _, err := Parse([]byte(raw)); err == nil {
			t.Fatalf("%s: parsed", name)
		}
	}
}

func TestTextHelpers(t *testing.T) {
	if got := Fold("  Coração ÁGUA "); got != "coracao agua" {
		t.Fatalf("fold = %q", got)
	}
	if got := IDWords("shape-doubleArrow"); len(got) != 3 || got[1] != "double" || got[2] != "arrow" {
		t.Fatalf("words = %v", got)
	}
	for raw, want := range map[string]string{"pt-BR": "pt", "en_US": "en", "DE": "de", "fr": "pt", "": "pt"} {
		if got := Locale(raw); got != want {
			t.Fatalf("locale %q = %q", raw, got)
		}
	}
}

func BenchmarkSearchTheWholeCatalog(b *testing.B) {
	c, err := Default()
	if err != nil {
		b.Fatal(err)
	}
	c.Search(Query{Text: "warm", Locale: "pt"})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Search(Query{Text: "coração", Locale: "pt"})
	}
}
