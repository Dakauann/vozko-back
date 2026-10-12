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

func TestStudioEditPassesLayoutAndTimingOperations(t *testing.T) {
	editor := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenEdit: {OK: true}}}
	edit := NewStudioEditVideoTool(studioDeps(studio.KindVideo, nil, nil))
	args := map[string]interface{}{"operations": []interface{}{
		map[string]interface{}{"op": "align_clips", "clip_ids": []interface{}{"c-1", "c-2"}, "frame_align": "center", "relative_to": "frame", "at_ms": 400.0},
		map[string]interface{}{"op": "distribute_clips", "clip_ids": []interface{}{"c-1", "c-2", "c-3"}, "axis": "vertical"},
		map[string]interface{}{"op": "sequence_clips", "clip_ids": []interface{}{"c-3", "c-1"}, "gap_ms": -250.0, "transition_kind": "dissolve", "duration_ms": 300.0},
		map[string]interface{}{"op": "stagger_clips", "clip_ids": []interface{}{"c-1", "c-2"}, "step_ms": 120.0},
		map[string]interface{}{"op": "set_speed", "clip_id": "c-1", "speed": 0.5, "protect_edges_ms": 400.0},
	}}
	if res := edit.Execute(context.Background(), studioSession(copilot.StudioVideo, editor), args); res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	cmd, _ := editor.sent(copilot.ScreenEdit)
	sent := renderJSON(cmd.Args)
	for _, want := range []string{
		`"op":"align_clips"`, `"frame_align":"center"`, `"relative_to":"frame"`, `"at_ms":400`,
		`"op":"distribute_clips"`, `"axis":"vertical"`,
		`"op":"sequence_clips"`, `"gap_ms":-250`, `"transition_kind":"dissolve"`, `"duration_ms":300`,
		`"op":"stagger_clips"`, `"step_ms":120`, `"protect_edges_ms":400`,
	} {
		if !strings.Contains(sent, want) {
			t.Fatalf("the batch lost %s: %s", want, sent)
		}
	}
}

func TestStudioLayoutSchemaOffersTheEditorChoices(t *testing.T) {
	operation := reflect.TypeOf(studioVideoOperation{})
	align, _ := operation.FieldByName("FrameAlign")
	if got := strings.Join(copilottools.EnumValues(align), ","); got != "left,center,right,top,middle,bottom" {
		t.Fatalf("frame_align drifted from the editor alignments: %s", got)
	}
	axis, _ := operation.FieldByName("Axis")
	if got := strings.Join(copilottools.EnumValues(axis), ","); got != "horizontal,vertical" {
		t.Fatalf("axis drifted from the editor axes: %s", got)
	}
	relative, _ := operation.FieldByName("RelativeTo")
	if got := strings.Join(copilottools.EnumValues(relative), ","); got != "selection,frame" {
		t.Fatalf("relative_to drifted from the editor choices: %s", got)
	}
	ops := opEnum(studioVideoOperation{})
	for _, name := range []string{"align_clips", "distribute_clips", "sequence_clips", "stagger_clips"} {
		if !copilottools.Contains(ops, name) {
			t.Fatalf("the video tool does not offer %s", name)
		}
	}
}

func TestStudioEditGuideExplainsLayoutAndTiming(t *testing.T) {
	description := NewStudioEditVideoTool(StudioDeps{}).Definition().Description
	for _, want := range []string{"align_clips(", "distribute_clips(", "sequence_clips(", "stagger_clips(", "protect_edges_ms"} {
		if !strings.Contains(description, want) {
			t.Fatalf("the video guide does not explain %s", want)
		}
	}
}
