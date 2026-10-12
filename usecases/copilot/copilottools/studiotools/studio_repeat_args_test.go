package studiotools

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/studio"
	"vozko/usecases/copilot/copilottools"
)

func TestStudioEditRefusesImageRepeatsOutOfRange(t *testing.T) {
	repeat := func(op string, shape map[string]interface{}) map[string]interface{} {
		target := map[string]interface{}{"op": op, "repeat": shape}
		if op == "repeat_layers" {
			target["layer_ids"] = []interface{}{"l-1"}
		} else {
			target["group_id"] = "g-1"
		}
		return target
	}
	validate := func(op map[string]interface{}) error {
		return NewStudioEditImageTool(studioDeps(studio.KindImage, nil, nil)).(copilot.Validator).Validate(context.Background(), studioSession(copilot.StudioImage, okEditor()), map[string]interface{}{"operations": []interface{}{op}})
	}
	for name, op := range map[string]map[string]interface{}{
		"one radial position": repeat("repeat_layers", map[string]interface{}{"mode": "radial", "count": 1.0}),
		"a radius too far":    repeat("repeat_layers", map[string]interface{}{"mode": "radial", "radius": 5000.0}),
		"a sweep past a turn": repeat("repeat_layers", map[string]interface{}{"mode": "radial", "sweep": 400.0}),
		"a start angle":       repeat("repeat_layers", map[string]interface{}{"mode": "radial", "start_angle": 270.0}),
		"no rows":             repeat("repeat_layers", map[string]interface{}{"mode": "grid", "rows": 0.0}),
		"a gap too wide":      repeat("repeat_layers", map[string]interface{}{"mode": "grid", "gap_x": 9000.0}),
		"a brick offset":      repeat("repeat_layers", map[string]interface{}{"mode": "grid", "layout": "brick-row", "offset": 2.0}),
		"a mirror angle":      repeat("repeat_layers", map[string]interface{}{"mode": "mirror", "angle": 200.0}),
		"an unknown mode":     repeat("repeat_layers", map[string]interface{}{"mode": "spiral"}),
		"an unknown side":     repeat("repeat_layers", map[string]interface{}{"mode": "mirror", "side": "left"}),
		"set_repeat too":      repeat("set_repeat", map[string]interface{}{"radius": 0.5}),
	} {
		if err := validate(op); !errors.Is(err, copilottools.ErrInvalidArgs) {
			t.Fatalf("%s: err = %v", name, err)
		}
	}
	for name, op := range map[string]map[string]interface{}{
		"a radial ring":   repeat("repeat_layers", map[string]interface{}{"mode": "radial", "count": 12.0, "radius": 300.0, "sweep": 180.0, "start_angle": -90.0}),
		"a brick grid":    repeat("repeat_layers", map[string]interface{}{"mode": "grid", "rows": 3.0, "columns": 4.0, "gap_x": -20.0, "layout": "brick-row", "offset": 0.5}),
		"a mirror":        repeat("repeat_layers", map[string]interface{}{"mode": "mirror", "angle": 90.0, "gap": -10.0}),
		"a partial patch": repeat("set_repeat", map[string]interface{}{"gap_y": 40.0}),
	} {
		if err := validate(op); err != nil {
			t.Fatalf("%s: err = %v", name, err)
		}
	}
}
