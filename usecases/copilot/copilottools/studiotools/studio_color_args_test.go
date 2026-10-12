package studiotools

import (
	"context"
	"strings"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/studio"
	"vozko/usecases/copilot/copilottools"
)

func TestStudioEditPassesColorAndSpeedCurveOperations(t *testing.T) {
	editor := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenEdit: {OK: true}}}
	edit := NewStudioEditVideoTool(studioDeps(studio.KindVideo, nil, nil))
	args := map[string]interface{}{"operations": []interface{}{
		map[string]interface{}{
			"op": "set_color", "clip_id": "c-1",
			"curves":        map[string]interface{}{"master": []interface{}{map[string]interface{}{"x": 0.0, "y": 0.05}, map[string]interface{}{"x": 1.0, "y": 0.95}}},
			"curves_preset": "contrast",
			"hsl":           map[string]interface{}{"color": "red", "width": 50.0, "desaturate": 100.0},
			"lut_media_id":  "look-1", "lut_intensity": 0.6,
			"gradient_map": map[string]interface{}{"preset": "noite", "intensity": 0.5},
			"clear_color":  []interface{}{"curves"},
		},
		map[string]interface{}{"op": "animate", "clip_id": "c-1", "property": "exposure", "keys": []interface{}{map[string]interface{}{"at_ms": 0.0, "value": -1.0}, map[string]interface{}{"at_ms": 800.0, "value": 0.5}}},
		map[string]interface{}{"op": "set_speed_curve", "clip_id": "c-1", "speed_curve_preset": "hero", "smooth": true, "keep_duration": false},
		map[string]interface{}{"op": "set_speed_curve", "clip_id": "c-1", "speed_points": []interface{}{map[string]interface{}{"at": 0.0, "speed": 1.0}, map[string]interface{}{"at": 1.0, "speed": 0.3}}},
		map[string]interface{}{"op": "speed_ramp", "clip_id": "c-1", "speed": 0.4, "ramp_ms": 600.0, "ramp_mode": "smooth"},
		map[string]interface{}{"op": "set_speed", "clip_id": "c-1", "frame_mix": true},
		map[string]interface{}{"op": "set_speed_curve", "clip_id": "c-1", "clear_speed_curve": true},
	}}
	if res := edit.Execute(context.Background(), studioSession(copilot.StudioVideo, editor), args); res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	cmd, _ := editor.sent(copilot.ScreenEdit)
	sent := renderJSON(cmd.Args)
	for _, want := range []string{
		`"curves":{"master":[{"x":0,"y":0.05},{"x":1,"y":0.95}]}`, `"curves_preset":"contrast"`, `"hsl":{"color":"red","width":50,"desaturate":100}`,
		`"lut_media_id":"look-1"`, `"lut_intensity":0.6`, `"gradient_map":{"preset":"noite","intensity":0.5}`, `"clear_color":["curves"]`,
		`"property":"exposure"`, `"op":"set_speed_curve"`, `"speed_curve_preset":"hero"`, `"speed_points":[{"at":0,"speed":1},{"at":1,"speed":0.3}]`,
		`"ramp_mode":"smooth"`, `"frame_mix":true`, `"clear_speed_curve":true`,
	} {
		if !strings.Contains(sent, want) {
			t.Fatalf("the batch lost %s: %s", want, sent)
		}
	}
}

func TestStudioColorSchemaRefusesUnknownChoices(t *testing.T) {
	editor := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenEdit: {OK: true}}}
	edit := NewStudioEditVideoTool(studioDeps(studio.KindVideo, nil, nil))
	for name, op := range map[string]map[string]interface{}{
		"ramp mode":     {"op": "speed_ramp", "clip_id": "c-1", "speed": 0.5, "ramp_ms": 400.0, "ramp_mode": "wavy"},
		"curve preset":  {"op": "set_speed_curve", "clip_id": "c-1", "speed_curve_preset": "rocket"},
		"curves preset": {"op": "set_color", "clip_id": "c-1", "curves_preset": "neon"},
		"animated prop": {"op": "animate", "clip_id": "c-1", "property": "hue", "keys": []interface{}{}},
	} {
		res := edit.Execute(context.Background(), studioSession(copilot.StudioVideo, editor), map[string]interface{}{"operations": []interface{}{op}})
		if res.Status == copilot.StatusOK {
			t.Errorf("%s: an unknown choice was accepted", name)
		}
	}
}

func TestStudioColorSchemaOffersTheEditorFields(t *testing.T) {
	video := NewStudioEditVideoTool(StudioDeps{}).Definition().Parameters["operations"].Items.Properties
	image := NewStudioEditImageTool(StudioDeps{}).Definition().Parameters["operations"].Items.Properties
	for name, kind := range map[string]string{"curves": "object", "curves_preset": "string", "hsl": "object", "lut_media_id": "string", "lut_intensity": "number", "clear_color": "array", "gradient_map": "object"} {
		if video[name].Type != kind || image[name].Type != kind {
			t.Fatalf("%s must be a %s in both editors, got %q and %q", name, kind, video[name].Type, image[name].Type)
		}
	}
	for name, kind := range map[string]string{"ramp_mode": "string", "speed_points": "array", "speed_curve_preset": "string", "clear_speed_curve": "boolean", "frame_mix": "boolean"} {
		if video[name].Type != kind {
			t.Fatalf("%s must be a %s, got %q", name, kind, video[name].Type)
		}
	}
	if !copilottools.Contains(opEnum(studioVideoOperation{}), "set_speed_curve") {
		t.Fatal("the video tool does not offer set_speed_curve")
	}
	properties := strings.Join(video["property"].Enum, ",")
	for _, want := range []string{"exposure", "contrast", "highlights", "shadows", "whites", "blacks", "temperature", "tint", "saturation", "vibrance", "speed"} {
		if !copilottools.Contains(video["property"].Enum, want) {
			t.Fatalf("animate does not offer %s: %s", want, properties)
		}
	}
	if strings.Join(video["clear_color"].Items.Enum, ",") != "curves,hsl,lut,gradient_map" {
		t.Fatalf("clear_color schema = %+v", video["clear_color"])
	}
}

func TestStudioGuidesExplainColorAndSpeedCurves(t *testing.T) {
	videoGuide := NewStudioEditVideoTool(StudioDeps{}).Definition().Description
	for _, want := range []string{"set_speed_curve(", "speed_curve_preset", "ramp_mode", "frame_mix", "curves", "hsl", "lut_media_id", "gradient_map", "property exposure", "LUT"} {
		if !strings.Contains(videoGuide, want) {
			t.Fatalf("the video guide does not explain %s", want)
		}
	}
	imageGuide := NewStudioEditImageTool(StudioDeps{}).Definition().Description
	for _, want := range []string{"curves", "hsl", "lut_media_id", "clear_color"} {
		if !strings.Contains(imageGuide, want) {
			t.Fatalf("the image guide does not explain %s", want)
		}
	}
}
