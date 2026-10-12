package studiotools

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/studio"
	"vozko/usecases/copilot/copilottools"
)

func TestStudioEditPassesEntranceAndExitTransitions(t *testing.T) {
	editor := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenEdit: {OK: true}}}
	edit := NewStudioEditVideoTool(studioDeps(studio.KindVideo, nil, nil))
	args := map[string]interface{}{"operations": []interface{}{
		map[string]interface{}{"op": "entrance", "clip_id": "c-1", "transition_kind": "venetianBlinds", "angle": 90.0, "piece_size": 0.2, "softness": 0.3, "duration_ms": 700.0},
		map[string]interface{}{"op": "exit", "clip_ids": []interface{}{"c-1", "c-2"}, "transition_kind": "gradientWipe", "wipe_map": "clouds", "easing": "easeIn"},
		map[string]interface{}{"op": "add_transition", "clip_id": "c-1", "transition_kind": "gradientWipe", "wipe_map_media_id": "m-7", "reverse": true},
		map[string]interface{}{"op": "add_transition", "clip_id": "c-2", "transition_kind": "radialWipe", "origin_y": 1.0},
	}}
	if res := edit.Execute(context.Background(), studioSession(copilot.StudioVideo, editor), args); res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	cmd, _ := editor.sent(copilot.ScreenEdit)
	sent := renderJSON(cmd.Args)
	for _, want := range []string{
		`"op":"entrance"`, `"transition_kind":"venetianBlinds"`, `"piece_size":0.2`, `"softness":0.3`, `"op":"exit"`, `"wipe_map":"clouds"`,
		`"wipe_map_media_id":"m-7"`, `"transition_kind":"radialWipe"`, `"origin_y":1`,
	} {
		if !strings.Contains(sent, want) {
			t.Fatalf("the batch lost %s: %s", want, sent)
		}
	}
	bad := map[string]interface{}{"operations": []interface{}{map[string]interface{}{"op": "entrance", "clip_id": "c-1", "transition_kind": "gradientWipe", "wipe_map": "spiral"}}}
	if res := edit.Execute(context.Background(), studioSession(copilot.StudioVideo, &fakeEditor{}), bad); res.Status != copilot.StatusError {
		t.Fatalf("an unknown gradient map must be refused, got %+v", res)
	}
}

func TestStudioTransitionSchemaFollowsTheDocument(t *testing.T) {
	operation := reflect.TypeOf(studioVideoOperation{})
	kind, _ := operation.FieldByName("TransitionKind")
	for _, want := range []studio.TransitionKind{studio.TransitionGradientWipe, studio.TransitionVenetianBlinds, studio.TransitionBlockDissolve, studio.TransitionRadialWipe} {
		if !copilottools.Contains(copilottools.EnumValues(kind), string(want)) {
			t.Fatalf("transition_kind cannot reach %s", want)
		}
	}
	wipeMap, _ := operation.FieldByName("WipeMap")
	if wipeMap.Tag.Get("enum") != strings.Join(studio.GradientWipeMaps, ",") {
		t.Fatalf("wipe_map drifted from the document: %q", wipeMap.Tag.Get("enum"))
	}
}

func TestStudioEditGuideExplainsEntranceTransitions(t *testing.T) {
	description := NewStudioEditVideoTool(StudioDeps{}).Definition().Description
	for _, want := range []string{"entrance(clip_id ou clip_ids, effect? ou transition_kind?", "wipe_map", "piece_size", "softness", "gradientWipe", "venetianBlinds", "blockDissolve", "radialWipe"} {
		if !strings.Contains(description, want) {
			t.Fatalf("the video guide does not explain %s", want)
		}
	}
}
