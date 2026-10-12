package studio

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestTheEditorEaseDocumentPassesTheContract(t *testing.T) {
	raw, err := os.ReadFile("testdata/editor_ease.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateDocument(KindVideo, raw); err != nil {
		t.Fatalf("the editor saved eased keys the backend refuses: %v", err)
	}
	var doc VideoDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	keys := doc.Tracks[0].Clips[0].Keyframes
	if !keys.X[2].Smooth || keys.X[1].Smooth || !keys.Opacity[1].Smooth {
		t.Fatalf("smooth keys lost: %+v", keys.X)
	}
}

func TestOnlyNumericKeysCanBeSmooth(t *testing.T) {
	raw, err := os.ReadFile("testdata/editor_ease.json")
	if err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string][2]string{
		"a color key": {`{ "atMs": 400, "color": "#ffcc00", "easing": "linear" }`, `{ "atMs": 400, "color": "#ffcc00", "easing": "linear", "smooth": true }`},
		"a text flag": {`"easing": "linear", "smooth": true }`, `"easing": "linear", "smooth": "yes" }`},
	} {
		changed := strings.Replace(string(raw), change[0], change[1], 1)
		if changed == string(raw) {
			t.Fatalf("%s: the fixture changed", name)
		}
		if err := ValidateDocument(KindVideo, []byte(changed)); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}
