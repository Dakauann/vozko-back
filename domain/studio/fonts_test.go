package studio

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"
)

type fixtureFont struct {
	ID       string `json:"id"`
	Family   string `json:"family"`
	Category string `json:"category"`
}

func TestFontsMatchTheEditorList(t *testing.T) {
	raw, err := os.ReadFile("testdata/studio_fonts.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Fonts []fixtureFont `json:"fonts"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Fonts) != len(Fonts()) {
		t.Fatalf("the editor lists %d fonts, the backend %d", len(fixture.Fonts), len(Fonts()))
	}
	for _, font := range fixture.Fonts {
		if got := FontCategory(font.ID); got != font.Category {
			t.Errorf("font %s: category %q, editor says %q", font.ID, got, font.Category)
		}
	}
}

func TestEveryFontCategoryHoldsFonts(t *testing.T) {
	for _, category := range FontCategories {
		if len(FontsIn(category)) == 0 {
			t.Errorf("category %s has no font", category)
		}
	}
	all := []string{}
	for _, category := range FontCategories {
		all = append(all, FontsIn(category)...)
	}
	sort.Strings(all)
	want := Fonts()
	sort.Strings(want)
	if len(all) != len(want) {
		t.Fatalf("categories cover %d of %d fonts", len(all), len(want))
	}
	for i := range all {
		if all[i] != want[i] {
			t.Fatalf("categories cover %v, fonts are %v", all, want)
		}
	}
}

func TestFontsAreListedByVoiceSoNoFontWinsByItsName(t *testing.T) {
	for _, category := range FontCategories {
		if group := FontsIn(category); len(group) > 2 && sort.StringsAreSorted(group) {
			t.Errorf("category %s is listed alphabetically: %v", category, group)
		}
	}
	condensed := map[string]bool{"anton": true, "bebas-neue": true, "oswald": true, "staatliches": true}
	if first := FontsIn(FontDisplay)[0]; condensed[first] {
		t.Errorf("display fonts open with the condensed %s", first)
	}
	ordered := OrderedFonts()
	var grouped []string
	for _, category := range FontCategories {
		grouped = append(grouped, FontsIn(category)...)
	}
	if strings.Join(ordered, ",") != strings.Join(grouped, ",") {
		t.Fatalf("ordered fonts %v differ from the groups %v", ordered, grouped)
	}
}

func TestATextLayerAcceptsANewFontAndRefusesAnUnknownOne(t *testing.T) {
	layer := Layer{ID: "t", Type: LayerText, Text: "Oi", FontID: "pacifico", FontSize: 0.05, Fill: "#000000", Transform: box()}
	if code := layer.issue(); code != "" {
		t.Fatalf("pacifico refused: %s", code)
	}
	layer.FontID = "comic-sans"
	if code := layer.issue(); code != CodeUnknown {
		t.Fatalf("an unknown font gave %q", code)
	}
}
