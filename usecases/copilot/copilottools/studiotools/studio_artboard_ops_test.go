package studiotools

import (
	"context"
	"strings"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/studio"
)

func TestARefusedBatchTellsTheModelWhatWasAndWasNotApplied(t *testing.T) {
	outcome := `{"applied":[],"refused":[{"operation":1,"op":"update_layer","reason":"a camada dot3 está travada"}],"not_applied":[{"operation":2,"op":"add_text","reason":"não foi aplicada"}]}`
	editor := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{
		copilot.ScreenEdit: {Error: &copilot.ScreenError{Code: "operation_refused", Message: "Nada foi aplicado: 1 de 2 operações foram recusadas."}, Data: []byte(outcome)},
	}}
	edit := NewStudioEditImageTool(studioDeps(studio.KindImage, nil, nil))
	args := map[string]interface{}{"operations": []interface{}{
		map[string]interface{}{"op": "update_layer", "layer_ids": []interface{}{"door1", "dot3"}, "y": 0.2},
		map[string]interface{}{"op": "add_text", "text": "Oi"},
	}}
	res := edit.Execute(context.Background(), studioSession(copilot.StudioImage, editor), args)
	if res.Status != copilot.StatusError {
		t.Fatalf("a refused batch is an error for the model, got %+v", res)
	}
	for _, want := range []string{"Nada foi aplicado", `"applied":[]`, `"refused":[{"operation":1`, "dot3 está travada", `"not_applied"`} {
		if !strings.Contains(res.Message, want) {
			t.Fatalf("the model must read %q in the refusal, got %q", want, res.Message)
		}
	}
}

func TestARefusalWithoutAnOutcomeKeepsItsPlainMessage(t *testing.T) {
	editor := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{
		copilot.ScreenEdit: {Error: &copilot.ScreenError{Code: "invalid_batch", Message: "envie de 1 a 40 operações"}},
	}}
	edit := NewStudioEditImageTool(studioDeps(studio.KindImage, nil, nil))
	res := edit.Execute(context.Background(), studioSession(copilot.StudioImage, editor), map[string]interface{}{"operations": []interface{}{map[string]interface{}{"op": "add_text", "text": "Oi"}}})
	if res.Message != studioEditorRefused+": envie de 1 a 40 operações" {
		t.Fatalf("message = %q", res.Message)
	}
}

func TestImageEditCarriesArtboardPlacesAndArrangements(t *testing.T) {
	editor := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenEdit: {OK: true, Data: []byte(`{"applied":[1,2]}`)}}}
	edit := NewStudioEditImageTool(studioDeps(studio.KindImage, nil, nil))
	args := map[string]interface{}{"operations": []interface{}{
		map[string]interface{}{"op": "update_artboard", "artboard_id": "c2", "artboard_x": -500.0, "artboard_y": 2400.0},
		map[string]interface{}{"op": "arrange_artboards", "artboard_ids": []interface{}{"c3", "c1", "c2"}, "arrange_layout": "grid", "arrange_gap": 0.0, "arrange_order": "given", "arrange_align": "center", "arrange_columns": 2.0, "arrange_by_size": true},
	}}
	res := edit.Execute(context.Background(), studioSession(copilot.StudioImage, editor), args)
	if res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	cmd, _ := editor.sent(copilot.ScreenEdit)
	sent := renderJSON(cmd.Args)
	for _, want := range []string{`"artboard_x":-500`, `"artboard_y":2400`, `"artboard_ids":["c3","c1","c2"]`, `"arrange_layout":"grid"`, `"arrange_gap":0`, `"arrange_order":"given"`, `"arrange_align":"center"`, `"arrange_columns":2`, `"arrange_by_size":true`} {
		if !strings.Contains(sent, want) {
			t.Fatalf("the batch lost %s: %s", want, sent)
		}
	}
}

func TestArtboardPlacesAndArrangementsAreCheckedBeforeTheEditor(t *testing.T) {
	cases := map[string]map[string]interface{}{
		"arrange without a layout": {"op": "arrange_artboards", "arrange_gap": 10.0},
		"unknown layout":           {"op": "arrange_artboards", "arrange_layout": "diagonal"},
		"unknown order":            {"op": "arrange_artboards", "arrange_layout": "row", "arrange_order": "random"},
		"unknown alignment":        {"op": "arrange_artboards", "arrange_layout": "row", "arrange_align": "top"},
		"negative gap":             {"op": "arrange_artboards", "arrange_layout": "row", "arrange_gap": -1.0},
		"gap past the limit":       {"op": "arrange_artboards", "arrange_layout": "row", "arrange_gap": 10001.0},
		"no columns":               {"op": "arrange_artboards", "arrange_layout": "grid", "arrange_columns": 0.0},
		"artboard far to the left": {"op": "update_artboard", "artboard_x": -float64(studio.MaxArtboardCoordinate) - 1},
		"artboard far below":       {"op": "update_artboard", "artboard_y": float64(studio.MaxArtboardCoordinate) + 1},
	}
	for name, op := range cases {
		editor := &fakeEditor{}
		edit := NewStudioEditImageTool(studioDeps(studio.KindImage, nil, nil))
		res := edit.Execute(context.Background(), studioSession(copilot.StudioImage, editor), map[string]interface{}{"operations": []interface{}{op}})
		if res.Status != copilot.StatusError {
			t.Errorf("%s: must be refused, got %+v", name, res)
		}
		if len(editor.commands) > 0 {
			t.Errorf("%s: nothing may reach the editor", name)
		}
	}
}

func TestTheArtboardFamilyTeachesMovingArrangingAndSpreading(t *testing.T) {
	var family studioOpFamily
	for _, f := range studioImageFamilies {
		if f.name == "pranchetas" {
			family = f
		}
	}
	for _, want := range []string{"arrange_artboards", "artboard_x", "artboard_y", "arrange_layout", "arrange_by_size", "uma por prancheta"} {
		if !strings.Contains(family.guide, want) {
			t.Errorf("the artboard family must mention %s", want)
		}
	}
}
