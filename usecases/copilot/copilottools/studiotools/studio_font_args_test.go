package studiotools

import (
	"strings"
	"testing"

	"vozko/domain/studio"
	"vozko/domain/tools"
)

func TestBothEditorsOfferEveryFontGroupedByCategory(t *testing.T) {
	for _, def := range []struct {
		name  string
		props map[string]tools.Parameter
	}{
		{"image", NewStudioEditImageTool(StudioDeps{}).Definition().Parameters["operations"].Items.Properties},
		{"video", NewStudioEditVideoTool(StudioDeps{}).Definition().Parameters["operations"].Items.Properties},
	} {
		font := def.props["font_id"]
		if strings.Join(font.Enum, ",") != strings.Join(studio.OrderedFonts(), ",") {
			t.Fatalf("%s offers fonts in another order than the voice groups: %v", def.name, font.Enum)
		}
		for _, want := range []string{"sem serifa: ", "serifadas: ", "de destaque: ", "manuscritas: ", "monoespaçadas: ", "pacifico", "creepster", "jetbrains-mono"} {
			if !strings.Contains(font.Description, want) {
				t.Errorf("%s font_id description misses %q: %s", def.name, want, font.Description)
			}
		}
	}
}
