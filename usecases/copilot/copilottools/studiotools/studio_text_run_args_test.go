package studiotools

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/studio"
	"vozko/usecases/copilot/copilottools"
)

func TestStudioEditPassesTextRunsOnVideo(t *testing.T) {
	editor := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenEdit: {OK: true}}}
	edit := NewStudioEditVideoTool(studioDeps(studio.KindVideo, nil, nil))
	args := map[string]interface{}{"operations": []interface{}{
		map[string]interface{}{"op": "style_text", "clip_id": "c-1", "char_start": 0.0, "char_end": 6.0, "fill": "#ffcc00", "font_weight": 800.0, "underline": false},
		map[string]interface{}{"op": "style_text", "clip_id": "c-1", "match": "off", "match_all": true, "caps": true, "run_clear": []interface{}{"fill", "letter_spacing"}},
	}}
	if res := edit.Execute(context.Background(), studioSession(copilot.StudioVideo, editor), args); res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	cmd, _ := editor.sent(copilot.ScreenEdit)
	sent := renderJSON(cmd.Args)
	for _, want := range []string{`"op":"style_text"`, `"clip_id":"c-1"`, `"char_start":0`, `"char_end":6`, `"fill":"#ffcc00"`, `"font_weight":800`, `"underline":false`, `"match":"off"`, `"match_all":true`, `"caps":true`, `"run_clear":["fill","letter_spacing"]`} {
		if !strings.Contains(sent, want) {
			t.Fatalf("the batch lost %s: %s", want, sent)
		}
	}
	for _, unset := range []string{`"italic"`, `"letter_spacing":`, `"font_size"`} {
		if strings.Contains(sent, unset) {
			t.Fatalf("an omitted %s was sent: %s", unset, sent)
		}
	}
}

func TestStudioEditPassesTextRunsOnImage(t *testing.T) {
	editor := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenEdit: {OK: true}}}
	edit := NewStudioEditImageTool(studioDeps(studio.KindImage, nil, nil))
	args := map[string]interface{}{"operations": []interface{}{
		map[string]interface{}{"op": "style_text", "layer_id": "title", "match": "Black Friday", "fill": "#facc15", "font_weight": 900.0, "caps": true},
		map[string]interface{}{"op": "style_text", "layer_id": "title", "char_start": 22.0, "char_end": 25.0, "underline": true, "run_clear": []interface{}{"font_id"}},
	}}
	if res := edit.Execute(context.Background(), studioSession(copilot.StudioImage, editor), args); res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	cmd, _ := editor.sent(copilot.ScreenEdit)
	sent := renderJSON(cmd.Args)
	for _, want := range []string{`"op":"style_text"`, `"layer_id":"title"`, `"match":"Black Friday"`, `"fill":"#facc15"`, `"font_weight":900`, `"caps":true`, `"char_start":22`, `"char_end":25`, `"underline":true`, `"run_clear":["font_id"]`} {
		if !strings.Contains(sent, want) {
			t.Fatalf("the batch lost %s: %s", want, sent)
		}
	}
	for _, unset := range []string{`"match_all"`, `"italic"`} {
		if strings.Contains(sent, unset) {
			t.Fatalf("an omitted %s was sent: %s", unset, sent)
		}
	}
}

func TestStudioTextRunSchema(t *testing.T) {
	for _, op := range []any{studioVideoOperation{}, studioImageOperation{}} {
		if !copilottools.Contains(opEnum(op), "style_text") {
			t.Fatalf("%T cannot run style_text", op)
		}
	}
	field, _ := reflect.TypeOf(studioTextRunArgs{}).FieldByName("RunClear")
	want := []string{"fill", "font_id", "font_weight", "font_size", "italic", "underline", "caps", "letter_spacing"}
	if got := copilottools.EnumValues(field); !slices.Equal(got, textRunClearFields()) || !slices.Equal(got, want) {
		t.Fatalf("run_clear enum = %v", got)
	}
	for _, edit := range []copilot.Tool{NewStudioEditVideoTool(studioDeps(studio.KindVideo, nil, nil)), NewStudioEditImageTool(studioDeps(studio.KindImage, nil, nil))} {
		clear := edit.Definition().Parameters["operations"].Items.Properties["run_clear"]
		if clear.Items == nil || !slices.Equal(clear.Items.Enum, want) {
			t.Fatalf("run_clear schema = %+v", clear)
		}
	}
}
