package studiotools

import (
	"fmt"
	"strings"
	"testing"

	"vozko/domain/studio"
	"vozko/domain/tools"
)

func TestBothEditorsDescribeTheShadowFromTheSharedDefaultAndTextGuide(t *testing.T) {
	percent := func(share float64) string { return fmt.Sprintf("%g por cento", share*100) }
	for _, def := range []struct {
		name  string
		props map[string]tools.Parameter
	}{
		{"image", NewStudioEditImageTool(StudioDeps{}).Definition().Parameters["operations"].Items.Properties},
		{"video", NewStudioEditVideoTool(StudioDeps{}).Definition().Parameters["operations"].Items.Properties},
	} {
		shadow := def.props["shadow"]
		defaults := fmt.Sprintf("%s, blur %g, y %g", studio.DefaultShadow.Color, studio.DefaultShadow.Blur, studio.DefaultShadow.Y)
		if !strings.Contains(shadow.Description, defaults) {
			t.Errorf("%s shadow description misses the default %q: %s", def.name, defaults, shadow.Description)
		}
		fields := shadow.Items.Properties
		checks := map[string]string{
			"color": studio.TextShadow.Color,
			"blur":  percent(studio.TextShadow.BlurShare),
			"y":     percent(studio.TextShadow.OffsetShare),
		}
		for field, want := range checks {
			if !strings.Contains(fields[field].Description, want) {
				t.Errorf("%s shadow.%s description misses %q: %s", def.name, field, want, fields[field].Description)
			}
		}
	}
}
