package studiotools

import (
	"context"
	"strings"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/studio"
)

func TestStudioLookOffersTheWireframeGuidesAndDepthViews(t *testing.T) {
	look := NewStudioLookTool(studioDeps(studio.KindImage, nil, nil))
	param, ok := look.Definition().Parameters["view"]
	if !ok || strings.Join(param.Enum, ",") != "render,wireframe,guides,depth" {
		t.Fatalf("view param = %+v", param)
	}
	for _, view := range []string{"wireframe", "guides", "depth"} {
		editor := lookEditor()
		if res := look.Execute(context.Background(), studioSession(copilot.StudioImage, editor), map[string]interface{}{"view": view, "focus_id": "layer-1"}); res.Status != copilot.StatusOK {
			t.Fatalf("%s look = %+v", view, res)
		}
		cmd, _ := editor.sent(copilot.ScreenLook)
		if sent := renderJSON(cmd.Args); !strings.Contains(sent, `"view":"`+view+`"`) || !strings.Contains(sent, `"focus_id":"layer-1"`) {
			t.Fatalf("look args %s", sent)
		}
	}
}

func TestStudioLookRefusesAnUnknownViewAndAViewOfALibraryMedia(t *testing.T) {
	look := NewStudioLookTool(studioDeps(studio.KindImage, nil, nil))
	for _, args := range []map[string]interface{}{
		{"view": "xray"},
		{"view": "guides", "media_id": "5f0c7c1e-1d2a-4b8e-9d11-3a2b1c0d9e8f"},
	} {
		editor := lookEditor()
		if res := look.Execute(context.Background(), studioSession(copilot.StudioImage, editor), args); res.Status != copilot.StatusError {
			t.Fatalf("%v must be refused, got %+v", args, res)
		}
		if _, sent := editor.sent(copilot.ScreenLook); sent {
			t.Fatalf("%v reached the editor", args)
		}
	}
}
