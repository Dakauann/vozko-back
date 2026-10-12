package studiotools

import (
	"context"
	"strings"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/studio"
	"vozko/usecases/copilot/copilottools"
)

func TestStudioEditPassesBehaviorOperations(t *testing.T) {
	editor := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenEdit: {OK: true}}}
	edit := NewStudioEditVideoTool(studioDeps(studio.KindVideo, nil, nil))
	args := map[string]interface{}{"operations": []interface{}{
		map[string]interface{}{"op": "add_behavior", "ref": "shake", "clip_id": "c-1", "property": "x", "behavior_kind": "wiggle", "behavior": map[string]interface{}{"frequency": 3.0, "amount": 0.02, "seed": 7.0}},
		map[string]interface{}{"op": "add_behavior", "clip_id": "c-1", "property": "scale", "behavior_kind": "audioReact", "audio_track_id": "t-9", "behavior": map[string]interface{}{"amount": 0.2, "floor_db": -40.0, "ceiling_db": -10.0}},
		map[string]interface{}{"op": "update_behavior", "clip_id": "c-1", "behavior_id": "@shake", "behavior": map[string]interface{}{"mode": "pingpong", "side": "both"}},
		map[string]interface{}{"op": "bake_behavior", "clip_id": "c-1", "behavior_id": "@shake"},
		map[string]interface{}{"op": "remove_behavior", "clip_id": "c-1", "behavior_id": "b-2"},
	}}
	if res := edit.Execute(context.Background(), studioSession(copilot.StudioVideo, editor), args); res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	cmd, _ := editor.sent(copilot.ScreenEdit)
	sent := renderJSON(cmd.Args)
	for _, want := range []string{
		`"op":"add_behavior"`, `"behavior_kind":"wiggle"`, `"frequency":3`, `"amount":0.02`, `"seed":7`,
		`"behavior_kind":"audioReact"`, `"audio_track_id":"t-9"`, `"floor_db":-40`, `"ceiling_db":-10`,
		`"op":"update_behavior"`, `"behavior_id":"@shake"`, `"mode":"pingpong"`, `"side":"both"`,
		`"op":"bake_behavior"`, `"op":"remove_behavior"`, `"behavior_id":"b-2"`,
	} {
		if !strings.Contains(sent, want) {
			t.Fatalf("the batch lost %s: %s", want, sent)
		}
	}
}

func TestStudioBehaviorSchemaOffersTheEditorFields(t *testing.T) {
	ops := opEnum(studioVideoOperation{})
	for _, name := range []string{"add_behavior", "update_behavior", "remove_behavior", "bake_behavior"} {
		if !copilottools.Contains(ops, name) {
			t.Fatalf("the video tool does not offer %s", name)
		}
	}
	properties := NewStudioEditVideoTool(StudioDeps{}).Definition().Parameters["operations"].Items.Properties
	for name, kind := range map[string]string{"behavior_id": "string", "behavior_kind": "string", "behavior": "object", "audio_track_id": "string"} {
		if properties[name].Type != kind {
			t.Fatalf("%s must be a %s, got %q", name, kind, properties[name].Type)
		}
	}
	params := properties["behavior"].Items.Properties
	for _, name := range []string{"frequency", "amount", "seed", "mode", "side", "rate", "amplitude", "decay", "floor_db", "ceiling_db"} {
		if _, ok := params[name]; !ok {
			t.Fatalf("behavior is missing %s", name)
		}
	}
	if kinds := strings.Join(properties["behavior_kind"].Enum, ","); kinds != strings.Join(studio.BehaviorKinds(), ",") {
		t.Fatalf("behavior kinds differ from the contract: %s", kinds)
	}
}

func TestStudioBehaviorArgumentsAreChecked(t *testing.T) {
	edit := NewStudioEditVideoTool(studioDeps(studio.KindVideo, nil, nil))
	valid := map[string]interface{}{"op": "add_behavior", "clip_id": "c-1", "property": "x", "behavior_kind": "loop", "behavior": map[string]interface{}{"mode": "offset", "side": "in"}}
	if err := edit.(copilot.Validator).Validate(context.Background(), studioSession(copilot.StudioVideo, &fakeEditor{}), map[string]interface{}{"operations": []interface{}{valid}}); err != nil {
		t.Fatalf("a valid behavior was refused: %v", err)
	}
	cases := map[string]map[string]interface{}{
		"an unknown kind":         {"op": "add_behavior", "clip_id": "c-1", "property": "x", "behavior_kind": "spin"},
		"a fast wiggle":           {"op": "add_behavior", "clip_id": "c-1", "property": "x", "behavior_kind": "wiggle", "behavior": map[string]interface{}{"frequency": 90.0}},
		"a strong overshoot":      {"op": "add_behavior", "clip_id": "c-1", "property": "scale", "behavior_kind": "overshoot", "behavior": map[string]interface{}{"amplitude": 3.0}},
		"a ceiling under floor":   {"op": "add_behavior", "clip_id": "c-1", "property": "scale", "behavior_kind": "audioReact", "behavior": map[string]interface{}{"floor_db": -10.0, "ceiling_db": -20.0}},
		"a seed past the range":   {"op": "add_behavior", "clip_id": "c-1", "property": "x", "behavior_kind": "wiggle", "behavior": map[string]interface{}{"seed": 1000000.0}},
		"an unknown loop mode":    {"op": "add_behavior", "clip_id": "c-1", "property": "x", "behavior_kind": "loop", "behavior": map[string]interface{}{"mode": "bounce"}},
		"a key influence too big": {"op": "ease_keys", "clip_id": "c-1", "side": "both", "influence": 120.0},
	}
	for name, op := range cases {
		if err := edit.(copilot.Validator).Validate(context.Background(), studioSession(copilot.StudioVideo, &fakeEditor{}), map[string]interface{}{"operations": []interface{}{op}}); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestStudioEditGuideExplainsBehaviors(t *testing.T) {
	description := NewStudioEditVideoTool(StudioDeps{}).Definition().Description
	for _, want := range []string{"add_behavior(", "update_behavior(", "remove_behavior(", "bake_behavior(", "wiggle", "loop", "drift", "overshoot", "audioReact", "uma por quadro", "behaviors"} {
		if !strings.Contains(description, want) {
			t.Fatalf("the video guide does not explain %s", want)
		}
	}
}
