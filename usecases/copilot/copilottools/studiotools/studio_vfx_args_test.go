package studiotools

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/studio"
)

func TestStudioEditPassesVisualEffects(t *testing.T) {
	editor := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenEdit: {OK: true}}}
	edit := NewStudioEditVideoTool(studioDeps(studio.KindVideo, nil, nil))
	args := map[string]interface{}{"operations": []interface{}{
		map[string]interface{}{"op": "add_effect_clip", "ref": "fx", "vfx_kind": "lightLeak", "at_ms": 500.0, "duration_ms": 1500.0, "vfx_seed": 0.0, "vfx_params": map[string]interface{}{"amount": 0.0, "color": "#ffaa00"}},
		map[string]interface{}{"op": "add_vfx", "ref": "g", "clip_id": "c-1", "vfx_kind": "grain", "vfx_params": map[string]interface{}{"size": 2.0}},
		map[string]interface{}{"op": "update_vfx", "clip_id": "c-1", "vfx_id": "@g", "vfx_hidden": true, "at_ms": 0.0},
		map[string]interface{}{"op": "animate_vfx", "clip_id": "@fx", "vfx_property": "amount", "keys": []interface{}{map[string]interface{}{"at_ms": 0.0, "value": 0.0}}},
		map[string]interface{}{"op": "move_vfx", "clip_id": "c-1", "vfx_id": "@g", "to_index": 0.0},
		map[string]interface{}{"op": "remove_vfx", "clip_id": "c-1", "vfx_id": "@g"},
	}}
	if res := edit.Execute(context.Background(), studioSession(copilot.StudioVideo, editor), args); res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	cmd, _ := editor.sent(copilot.ScreenEdit)
	sent := renderJSON(cmd.Args)
	for _, want := range []string{`"op":"add_effect_clip"`, `"vfx_kind":"lightLeak"`, `"vfx_seed":0`, `"amount":0`, `"color":"#ffaa00"`, `"vfx_id":"@g"`, `"vfx_hidden":true`, `"vfx_property":"amount"`, `"op":"move_vfx"`, `"op":"remove_vfx"`} {
		if !strings.Contains(sent, want) {
			t.Fatalf("the batch lost %s: %s", want, sent)
		}
	}
}

func sortedJoin(values []string) string {
	sorted := append([]string(nil), values...)
	sort.Strings(sorted)
	return strings.Join(sorted, ",")
}

func TestTheVfxSchemaFollowsTheDocument(t *testing.T) {
	fields := reflect.TypeOf(studioVfxArgs{})
	kinds, _ := fields.FieldByName("VfxKind")
	properties, _ := fields.FieldByName("VfxProperty")
	if kinds.Tag.Get("enum") != sortedJoin(studio.VfxKinds()) {
		t.Fatalf("vfx_kind drifted: %q", kinds.Tag.Get("enum"))
	}
	if properties.Tag.Get("enum") != sortedJoin(studio.VfxParams()) {
		t.Fatalf("vfx_property drifted: %q", properties.Tag.Get("enum"))
	}
	params := reflect.TypeOf(studioVfxParamsArgs{})
	names := []string{}
	for i := 0; i < params.NumField(); i++ {
		names = append(names, strings.Split(params.Field(i).Tag.Get("json"), ",")[0])
	}
	if sortedJoin(names) != sortedJoin(append(studio.VfxParams(), studio.VfxColorNames()...)) {
		t.Fatalf("vfx_params drifted: %v", names)
	}
}

func TestTheVfxArgumentsAreCheckedBeforeTheEditorSeesThem(t *testing.T) {
	cases := map[string]struct {
		op   studioVideoOperation
		want string
	}{
		"a twirl angle in range": {op: studioVideoOperation{Op: "add_vfx", studioVfxArgs: studioVfxArgs{VfxKind: "twirl", VfxParams: &studioVfxParamsArgs{Angle: floatArg(-900)}}}},
		"a glow size out of range": {
			op:   studioVideoOperation{Op: "add_vfx", studioVfxArgs: studioVfxArgs{VfxKind: "glow", VfxParams: &studioVfxParamsArgs{Size: floatArg(0)}}},
			want: "vfx_params.size de glow vai de 1 a 200",
		},
		"a parameter of another kind": {
			op:   studioVideoOperation{Op: "add_vfx", studioVfxArgs: studioVfxArgs{VfxKind: "mosaic", VfxParams: &studioVfxParamsArgs{Levels: floatArg(4)}}},
			want: "vfx_params.levels não vale para mosaic",
		},
		"a color the kind does not paint": {
			op:   studioVideoOperation{Op: "add_vfx", studioVfxArgs: studioVfxArgs{VfxKind: "mosaic", VfxParams: &studioVfxParamsArgs{Color3: "#ffffff"}}},
			want: "vfx_params.color3 não é uma cor de mosaic; use #rrggbb nas cores do tipo",
		},
		"a spectrum without its track": {
			op:   studioVideoOperation{Op: "add_vfx", studioVfxArgs: studioVfxArgs{VfxKind: "audioSpectrum"}},
			want: "audioSpectrum precisa de vfx_track_id, a faixa de áudio que as barras mostram",
		},
		"a waveform effect clip with its track": {op: studioVideoOperation{Op: "add_effect_clip", studioVfxArgs: studioVfxArgs{VfxKind: "audioWaveform", VfxTrackID: "music"}}},
		"a track on a glow": {
			op:   studioVideoOperation{Op: "add_vfx", studioVfxArgs: studioVfxArgs{VfxKind: "glow", VfxTrackID: "music"}},
			want: "vfx_track_id vale só para audioSpectrum e audioWaveform",
		},
		"an echo effect clip": {
			op:   studioVideoOperation{Op: "add_effect_clip", studioVfxArgs: studioVfxArgs{VfxKind: "echo"}},
			want: "echo repete o próprio clipe e não vale num clipe de efeito; use add_vfx no clipe que se move",
		},
	}
	for name, c := range cases {
		if got := c.op.argsIssue(); got != c.want {
			t.Errorf("%s: got %q, want %q", name, got, c.want)
		}
	}
}

func floatArg(v float64) *float64 { return &v }
