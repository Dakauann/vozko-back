package studiotools

import (
	"reflect"
	"strings"
	"testing"

	"vozko/domain/studio"
	"vozko/usecases/copilot/copilottools"
)

func TestOrganizeColorsMatchTheDocumentLabels(t *testing.T) {
	want := strings.Join(append(studio.LabelColors(), "none"), ",")
	for _, name := range []string{"MarkerColor", "LabelColor"} {
		field, _ := reflect.TypeOf(studioOrganizeArgs{}).FieldByName(name)
		if got := field.Tag.Get("enum"); got != want {
			t.Errorf("%s enum %q, want %q", name, got, want)
		}
	}
}

func TestOrganizeOperationsAreChecked(t *testing.T) {
	zero := 0
	cases := []struct {
		name string
		op   studioVideoOperation
		want string
	}{
		{"a long note", studioVideoOperation{Op: "add_marker", studioOrganizeArgs: studioOrganizeArgs{Note: strings.Repeat("a", studio.MaxMarkerNoteRunes+1)}}, "note"},
		{"beat every zero", studioVideoOperation{Op: "mark_beats", studioOrganizeArgs: studioOrganizeArgs{BeatEvery: &zero}}, "beat_every"},
		{"an unknown unset", studioVideoOperation{Op: "update_marker", studioOrganizeArgs: studioOrganizeArgs{Unset: []string{"color"}}}, "unset"},
		{"an unknown label", studioVideoOperation{Op: "update_clip", studioOrganizeArgs: studioOrganizeArgs{LabelColor: "teal"}}, "label_color"},
		{"a preset without id", studioVideoOperation{Op: "apply_animation_preset", ClipID: "c-1"}, "preset_id"},
		{"an unknown timing", studioVideoOperation{Op: "apply_animation_preset", studioPresetArgs: studioPresetArgs{PresetID: "builtin.popIn", PresetTiming: "slow"}}, "preset_timing"},
	}
	for _, c := range cases {
		msg := operationIssue(reflect.ValueOf(c.op))
		if !strings.Contains(msg, c.want) {
			t.Errorf("%s: got %q, want a message about %s", c.name, msg, c.want)
		}
	}
	ok := studioVideoOperation{Op: "mark_beats", ClipID: "song", studioOrganizeArgs: studioOrganizeArgs{MarkerColor: "red"}}
	if msg := operationIssue(reflect.ValueOf(ok)); msg != "" {
		t.Errorf("a valid mark_beats was refused: %s", msg)
	}
}

func TestTheVideoEditToolOffersOrganizationAndPresets(t *testing.T) {
	ops := opEnum(studioVideoOperation{})
	for _, name := range []string{"mark_beats", "apply_animation_preset", "add_marker", "update_marker"} {
		if !copilottools.Contains(ops, name) {
			t.Errorf("op %s missing", name)
		}
	}
	for _, phrase := range []string{"mark_beats(", "apply_animation_preset(", "guide", "shy"} {
		if !strings.Contains(studioVideoEditGuide, phrase) {
			t.Errorf("guide does not mention %s", phrase)
		}
	}
}
