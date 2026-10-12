package studio

import (
	"encoding/json"
	"os"
	"testing"

	"vozko/domain/studio/library"
)

func TestEveryLibraryItemInsertsADocumentTheBackendAccepts(t *testing.T) {
	catalog, err := library.Default()
	if err != nil {
		t.Fatal(err)
	}
	wanted := fixtureInserts(catalog)
	for kind, path := range map[Kind]string{KindImage: "testdata/editor_library_image.json", KindVideo: "testdata/editor_library_video.json"} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := ValidateDocument(kind, raw); err != nil {
			t.Fatalf("%s: the editor inserted library items the backend refuses: %v", kind, err)
		}
		if got := insertedItems(t, kind, raw); got < wanted[kind] {
			t.Fatalf("%s: the fixture holds %d inserts for %d catalog items", kind, got, wanted[kind])
		}
	}
}

func fixtureInserts(catalog *library.Catalog) map[Kind]int {
	sampled := map[string]bool{}
	for _, category := range catalog.Categories {
		sampled[category.ID] = category.Fixture == "sample"
	}
	wanted := map[Kind]int{}
	seen := map[string]bool{}
	for _, item := range catalog.Items {
		for _, editor := range item.Editors {
			key := editor + "/" + item.Category + "/" + item.Subcategory
			if sampled[item.Category] && seen[key] {
				continue
			}
			seen[key] = true
			wanted[Kind(editor)]++
		}
	}
	return wanted
}

func insertedItems(t *testing.T, kind Kind, raw []byte) int {
	t.Helper()
	if kind == KindImage {
		var doc ImageDocument
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatal(err)
		}
		return len(doc.Artboards[0].Layers)
	}
	var doc VideoDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	starts := map[int64]bool{}
	for _, track := range doc.Tracks {
		for _, clip := range track.Clips {
			starts[clip.StartMS] = true
		}
	}
	return len(starts)
}
