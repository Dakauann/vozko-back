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

func TestStudioEditPassesCountersAndTextPaths(t *testing.T) {
	editor := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenEdit: {OK: true}}}
	edit := NewStudioEditVideoTool(studioDeps(studio.KindVideo, nil, nil))
	args := map[string]interface{}{"operations": []interface{}{
		map[string]interface{}{"op": "set_counter", "clip_id": "c-1", "counter_from": 0.0, "counter_to": 10000.0, "count_start_ms": 0.0, "count_ms": 1500.0, "decimals": 0.0, "counter_locale": "pt-BR", "grouping": false, "prefix": "R$ ", "suffix": ""},
		map[string]interface{}{"op": "animate", "clip_id": "c-1", "property": "counter", "keys": []interface{}{map[string]interface{}{"at_ms": 0.0, "value": 0.0}, map[string]interface{}{"at_ms": 900.0, "value": 500.0, "easing": "easeOut"}}},
		map[string]interface{}{"op": "set_text_path", "clip_id": "c-2", "path_shape": "custom", "path": "M0 0.8 C0.3 0.1 0.7 0.1 1 0.8", "path_offset": 0.0, "path_align": "start", "upright": false, "path_reverse": true},
		map[string]interface{}{"op": "animate", "clip_id": "c-2", "property": "pathOffset", "keys": []interface{}{map[string]interface{}{"at_ms": 0.0, "value": -0.5}, map[string]interface{}{"at_ms": 900.0, "value": 1.5}}},
		map[string]interface{}{"op": "remove_counter", "clip_id": "c-1"},
		map[string]interface{}{"op": "remove_text_path", "clip_id": "c-2"},
	}}
	if res := edit.Execute(context.Background(), studioSession(copilot.StudioVideo, editor), args); res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	cmd, _ := editor.sent(copilot.ScreenEdit)
	sent := renderJSON(cmd.Args)
	for _, want := range []string{`"op":"set_counter"`, `"counter_from":0`, `"counter_to":10000`, `"count_start_ms":0`, `"count_ms":1500`, `"decimals":0`, `"counter_locale":"pt-BR"`, `"grouping":false`, `"prefix":"R$ "`, `"suffix":""`, `"property":"counter"`, `"op":"set_text_path"`, `"path_shape":"custom"`, `"path":"M0 0.8 C0.3 0.1 0.7 0.1 1 0.8"`, `"path_offset":0`, `"path_align":"start"`, `"upright":false`, `"path_reverse":true`, `"property":"pathOffset"`, `"op":"remove_counter"`, `"op":"remove_text_path"`} {
		if !strings.Contains(sent, want) {
			t.Fatalf("the batch lost %s: %s", want, sent)
		}
	}
	if strings.Contains(sent, `"bend"`) {
		t.Fatalf("an omitted bend was sent: %s", sent)
	}
}

func TestStudioTextMotionSchemaFollowsTheDocument(t *testing.T) {
	property, _ := reflect.TypeOf(studioVideoOperation{}).FieldByName("Property")
	allowed := strings.Split(property.Tag.Get("enum"), ",")
	lanes := reflect.TypeOf(studio.ClipKeyframes{})
	for _, field := range []string{"Counter", "PathOffset"} {
		lane, _ := lanes.FieldByName(field)
		name := strings.Split(lane.Tag.Get("json"), ",")[0]
		if name == "" || !copilottools.Contains(allowed, name) {
			t.Fatalf("animate cannot reach the text property %s", field)
		}
	}
	enums := []struct {
		args  any
		field string
		want  []string
	}{
		{studioCounterArgs{}, "CounterLocale", studio.CounterLocales()},
		{studioTextPathArgs{}, "PathShape", studio.TextPathShapes()},
		{studioTextPathArgs{}, "PathAlign", studio.TextPathAligns()},
	}
	for _, e := range enums {
		field, _ := reflect.TypeOf(e.args).FieldByName(e.field)
		if got := strings.Split(field.Tag.Get("enum"), ","); !slices.Equal(got, e.want) {
			t.Fatalf("%s drifted from the document: %v", e.field, got)
		}
	}
	for _, op := range []string{"set_counter", "remove_counter", "set_text_path", "remove_text_path"} {
		if !copilottools.Contains(opEnum(studioVideoOperation{}), op) {
			t.Fatalf("the video edit cannot run %s", op)
		}
	}
}
