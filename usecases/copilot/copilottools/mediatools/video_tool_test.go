package mediatools

import (
	"testing"

	"vozko/usecases/copilot/copilottools"
)

func TestAListOfObjectsDescribesItsItemFields(t *testing.T) {
	def := copilottools.Definition("render_video", "x", renderVideoArgs{})
	scenes := def.Parameters["scenes"]
	if scenes.Type != "array" || scenes.Items == nil || scenes.Items.Type != "object" {
		t.Fatalf("scenes %+v", scenes)
	}
	if scenes.Items.Properties["seconds"].Type != "number" || scenes.Items.Properties["media_id"].Type != "string" {
		t.Fatalf("item fields %+v", scenes.Items.Properties)
	}
	if len(scenes.Items.Required) != 2 {
		t.Fatalf("required %v", scenes.Items.Required)
	}
	if ids := def.Parameters["music_media_id"]; ids.Type != "string" || ids.Items != nil {
		t.Fatalf("scalar %+v", ids)
	}
}
