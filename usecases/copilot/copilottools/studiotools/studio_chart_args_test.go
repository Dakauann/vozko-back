package studiotools

import (
	"context"
	"strings"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/studio"
)

func chartOperation(op string, extra map[string]interface{}) map[string]interface{} {
	operation := map[string]interface{}{
		"op":           op,
		"chart_kind":   "barGrouped",
		"chart_labels": []interface{}{"Jan", "Fev"},
		"chart_series": []interface{}{
			map[string]interface{}{"name": "2025", "values": []interface{}{120.0, nil}},
			map[string]interface{}{"name": "2026", "values": []interface{}{140.0, 210.0}},
		},
		"chart_style":  map[string]interface{}{"palette": []interface{}{"#ff0066", "#2563eb"}, "legend": "top", "font_size": 0.06, "value_labels": true},
		"chart_format": map[string]interface{}{"locale": "pt-BR", "decimals": 1.0, "prefix": "R$ "},
	}
	for key, value := range extra {
		operation[key] = value
	}
	return operation
}

func TestStudioEditPassesChartOperationsInBothEditors(t *testing.T) {
	video := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenEdit: {OK: true}}}
	args := map[string]interface{}{"operations": []interface{}{
		chartOperation("add_chart", map[string]interface{}{"at_ms": 0.0, "duration_ms": 4000.0, "ref": "grafico"}),
		map[string]interface{}{"op": "update_chart_data", "clip_id": "@grafico", "chart_labels": []interface{}{"A", "B", "C"}, "chart_series": []interface{}{map[string]interface{}{"name": "x", "values": []interface{}{1.0, 2.0, 3.0}}}},
		map[string]interface{}{"op": "animate_chart", "clip_id": "@grafico", "chart_entrance": "grow", "chart_order": "category", "duration_ms": 800.0, "stagger_ms": 150.0, "easing": "easeOut"},
	}}
	if res := NewStudioEditVideoTool(studioDeps(studio.KindVideo, nil, nil)).Execute(context.Background(), studioSession(copilot.StudioVideo, video), args); res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	cmd, _ := video.sent(copilot.ScreenEdit)
	sent := renderJSON(cmd.Args)
	for _, want := range []string{`"op":"add_chart"`, `"chart_kind":"barGrouped"`, `"values":[120,null]`, `"palette":["#ff0066","#2563eb"]`, `"prefix":"R$ "`, `"op":"update_chart_data"`, `"chart_entrance":"grow"`, `"chart_order":"category"`, `"stagger_ms":150`} {
		if !strings.Contains(sent, want) {
			t.Fatalf("the batch lost %s: %s", want, sent)
		}
	}
	image := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenEdit: {OK: true}}}
	imageArgs := map[string]interface{}{"operations": []interface{}{
		chartOperation("add_chart", map[string]interface{}{"ref": "grafico", "w": 0.8}),
		map[string]interface{}{"op": "expand_chart", "layer_id": "@grafico"},
	}}
	if res := NewStudioEditImageTool(studioDeps(studio.KindImage, nil, nil)).Execute(context.Background(), studioSession(copilot.StudioImage, image), imageArgs); res.Status != copilot.StatusOK {
		t.Fatalf("result = %+v", res)
	}
	cmd, _ = image.sent(copilot.ScreenEdit)
	if sent := renderJSON(cmd.Args); !strings.Contains(sent, `"op":"expand_chart"`) || !strings.Contains(sent, `"chart_labels":["Jan","Fev"]`) {
		t.Fatalf("the image batch lost the chart: %s", sent)
	}
}

func TestStudioEditRefusesChartArgumentsTheEditorWouldRefuse(t *testing.T) {
	cases := map[string]map[string]interface{}{
		"a series shorter than the labels": {"op": "add_chart", "chart_kind": "bar", "chart_labels": []interface{}{"a", "b"}, "chart_series": []interface{}{map[string]interface{}{"values": []interface{}{1.0}}}},
		"an unknown kind":                  chartOperation("add_chart", map[string]interface{}{"chart_kind": "stream"}),
		"a named color":                    chartOperation("add_chart", map[string]interface{}{"chart_style": map[string]interface{}{"palette": []interface{}{"red"}}}),
		"a giant font":                     chartOperation("add_chart", map[string]interface{}{"chart_style": map[string]interface{}{"font_size": 0.5}}),
		"an unknown font":                  chartOperation("add_chart", map[string]interface{}{"chart_style": map[string]interface{}{"font_id": "comic"}}),
		"an unknown legend":                chartOperation("add_chart", map[string]interface{}{"chart_style": map[string]interface{}{"legend": "left"}}),
		"a hole past the ring":             chartOperation("add_chart", map[string]interface{}{"chart_style": map[string]interface{}{"hole": 0.95}}),
		"an unknown locale":                chartOperation("add_chart", map[string]interface{}{"chart_format": map[string]interface{}{"locale": "fr-FR"}}),
		"too many decimals":                chartOperation("add_chart", map[string]interface{}{"chart_format": map[string]interface{}{"decimals": 4.0}}),
		"a huge value":                     {"op": "add_chart", "chart_kind": "bar", "chart_labels": []interface{}{"a"}, "chart_series": []interface{}{map[string]interface{}{"values": []interface{}{2e12}}}},
		"a label with a line break":        {"op": "add_chart", "chart_kind": "bar", "chart_labels": []interface{}{"a\nb"}, "chart_series": []interface{}{map[string]interface{}{"values": []interface{}{1.0}}}},
		"an unknown entrance":              {"op": "animate_chart", "clip_id": "c-1", "chart_entrance": "spin"},
		"a blink":                          {"op": "animate_chart", "clip_id": "c-1", "chart_entrance": "fade", "duration_ms": 50.0},
		"a very long stagger":              {"op": "animate_chart", "clip_id": "c-1", "chart_entrance": "fade", "stagger_ms": 6000.0},
	}
	for name, op := range cases {
		editor := &fakeEditor{replies: map[copilot.ScreenCommandName]copilot.ScreenReply{copilot.ScreenEdit: {OK: true}}}
		res := NewStudioEditVideoTool(studioDeps(studio.KindVideo, nil, nil)).Execute(context.Background(), studioSession(copilot.StudioVideo, editor), map[string]interface{}{"operations": []interface{}{op}})
		if res.Status == copilot.StatusOK {
			t.Errorf("%s was accepted", name)
		}
		if _, sent := editor.sent(copilot.ScreenEdit); sent {
			t.Errorf("%s reached the editor", name)
		}
	}
}

func TestStudioChartSchemaOffersTheEditorFields(t *testing.T) {
	video := NewStudioEditVideoTool(StudioDeps{}).Definition().Parameters["operations"].Items.Properties
	image := NewStudioEditImageTool(StudioDeps{}).Definition().Parameters["operations"].Items.Properties
	for name, kind := range map[string]string{"chart_kind": "string", "chart_labels": "array", "chart_series": "array", "chart_style": "object", "chart_format": "object"} {
		if video[name].Type != kind || image[name].Type != kind {
			t.Fatalf("%s must be a %s in both tools, got %q and %q", name, kind, video[name].Type, image[name].Type)
		}
	}
	if _, ok := image["chart_entrance"]; ok {
		t.Fatal("the image tool offers a chart entrance, which only video clips have")
	}
	if video["chart_entrance"].Type != "string" || video["chart_order"].Type != "string" {
		t.Fatal("the video tool must offer chart_entrance and chart_order")
	}
}
