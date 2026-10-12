package studio

import (
	"encoding/json"
	"os"
	"testing"
)

type textShadowFixture struct {
	DefaultShadow Shadow          `json:"defaultShadow"`
	TextShadow    TextShadowGuide `json:"textShadow"`
}

func TestTheShadowDefaultAndTheTextShadowGuideMatchTheEditor(t *testing.T) {
	raw, err := os.ReadFile("testdata/editor_text_shadow.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture textShadowFixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.DefaultShadow != DefaultShadow {
		t.Errorf("default shadow differs: editor %+v, backend %+v", fixture.DefaultShadow, DefaultShadow)
	}
	if fixture.TextShadow != TextShadow {
		t.Errorf("text shadow guide differs: editor %+v, backend %+v", fixture.TextShadow, TextShadow)
	}
}
