package studio

import (
	"encoding/json"
	"os"
	"testing"

	"vozko/domain/mediagen"
)

func value(v float64) *float64 { return &v }

func chartSpec(kind ChartKind) ChartSpec {
	return ChartSpec{
		Kind: kind,
		Data: ChartData{Labels: []string{"Jan", "Fev"}, Series: []ChartSeries{{Name: "Vendas", Values: []*float64{value(10), nil}}, {Name: "Meta", Values: []*float64{value(12), value(14)}}}},
		Style: ChartStyle{
			Palette: []string{"#2563eb", "#f97316"}, FontID: "inter", FontSize: 0.05, TextColor: "#111111", GridColor: "#11111133",
			Axes: true, Gridlines: true, Legend: "bottom", BarGap: 0.3, LineWidth: 0.008, Points: true, Hole: 0.55,
		},
		Format: ChartFormat{Locale: "pt-BR", Prefix: "R$ "},
	}
}

func chartLayer(spec ChartSpec) Layer {
	return Layer{ID: "chart", Type: LayerChart, Transform: box(), Chart: &spec}
}

func TestTheEditorChartDocumentsPassTheContract(t *testing.T) {
	for kind, name := range map[Kind]string{KindImage: "testdata/editor_chart.json", KindVideo: "testdata/editor_chart_motion.json"} {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := ValidateDocument(kind, raw); err != nil {
			t.Fatalf("the editor saved a chart document the backend refuses (%s): %v", name, err)
		}
	}
	raw, _ := os.ReadFile("testdata/editor_chart.json")
	var doc ImageDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	sales := doc.Artboards[0].Layers[0].Chart
	if sales == nil || sales.Kind != ChartBarGrouped || sales.Data.Series[0].Values[2] != nil || *sales.Data.Series[0].Values[0] != 1200.5 {
		t.Fatalf("the chart data was lost: %+v", sales)
	}
	raw, _ = os.ReadFile("testdata/editor_chart_motion.json")
	var film VideoDocument
	if err := json.Unmarshal(raw, &film); err != nil {
		t.Fatal(err)
	}
	if in := film.Tracks[0].Clips[0].ChartIn; in == nil || in.Style != ChartDraw || in.StaggerMS != 300 {
		t.Fatalf("the chart animation was lost: %+v", in)
	}
}

func TestEveryChartKindIsAccepted(t *testing.T) {
	for _, kind := range ChartKinds() {
		doc := imageDoc()
		doc.Layers = append(doc.Layers, chartLayer(chartSpec(kind)))
		if err := ValidateDocument(KindImage, mustJSON(t, doc)); err != nil {
			t.Fatalf("%s was refused: %v", kind, err)
		}
	}
}

func TestChartsRefuseWhatTheEditorRefuses(t *testing.T) {
	cases := map[string]struct {
		change func(*Layer)
		code   string
	}{
		"an unknown kind":           {func(l *Layer) { l.Chart.Kind = "stream" }, CodeUnknown},
		"rows that miss labels":     {func(l *Layer) { l.Chart.Data.Series[0].Values = []*float64{value(1)} }, CodeInvalid},
		"no rows":                   {func(l *Layer) { l.Chart.Data = ChartData{} }, CodeRequired},
		"a scatter without x":       {func(l *Layer) { l.Chart.Kind = ChartScatter; l.Chart.Data.Series = l.Chart.Data.Series[:1] }, CodeRequired},
		"a huge number":             {func(l *Layer) { l.Chart.Data.Series[0].Values[0] = value(2e12) }, CodeOutOfRange},
		"a label with a line break": {func(l *Layer) { l.Chart.Data.Labels[0] = "a\nb" }, CodeInvalid},
		"an empty palette":          {func(l *Layer) { l.Chart.Style.Palette = nil }, CodeInvalid},
		"a palette with a name":     {func(l *Layer) { l.Chart.Style.Palette = []string{"red"} }, CodeInvalid},
		"an unknown font":           {func(l *Layer) { l.Chart.Style.FontID = "comic" }, CodeUnknown},
		"an unknown legend":         {func(l *Layer) { l.Chart.Style.Legend = "left" }, CodeUnknown},
		"a giant font":              {func(l *Layer) { l.Chart.Style.FontSize = 0.5 }, CodeOutOfRange},
		"a hole past the ring":      {func(l *Layer) { l.Chart.Style.Hole = 0.95 }, CodeOutOfRange},
		"an unknown locale":         {func(l *Layer) { l.Chart.Format.Locale = "fr-FR" }, CodeUnknown},
		"too many decimals":         {func(l *Layer) { l.Chart.Format.Decimals = 4 }, CodeOutOfRange},
		"a long prefix":             {func(l *Layer) { l.Chart.Format.Prefix = "R$ R$ R$ R$ R$ R$ " }, CodeTooLarge},
		"a fill on the chart":       {func(l *Layer) { l.Fill = "#ff0000" }, CodeInvalid},
		"a flipped chart":           {func(l *Layer) { l.FlipX = true }, CodeInvalid},
		"a chart without its spec":  {func(l *Layer) { l.Chart = nil }, CodeRequired},
		"a spec on a plain shape":   {func(l *Layer) { l.Type = LayerShape; l.Shape = ShapeRect }, CodeInvalid},
	}
	for name, c := range cases {
		layer := chartLayer(chartSpec(ChartBarGrouped))
		c.change(&layer)
		doc := imageDoc()
		doc.Layers = append(doc.Layers, layer)
		if code := codeOf(t, ValidateDocument(KindImage, mustJSON(t, doc)))[FieldLayers]; code != c.code {
			t.Errorf("%s: got %q, want %q", name, code, c.code)
		}
	}
}

func chartFilm(in *ChartAnimator) VideoDocument {
	doc := videoDoc()
	layer := chartLayer(chartSpec(ChartBar))
	layer.Transform = Transform{X: 0.5, Y: 0.5, W: 1, H: 1, Opacity: 1}
	doc.Tracks[1].Clips[0].Layer = &layer
	doc.Tracks[1].Clips[0].ChartIn = in
	return doc
}

func TestChartAnimationIsOnlyForChartOverlays(t *testing.T) {
	grow := &ChartAnimator{Style: ChartGrow, Order: "category", DurationMS: 800, StaggerMS: 120, Easing: mediagen.EaseOut}
	if err := ValidateDocument(KindVideo, mustJSON(t, chartFilm(grow))); err != nil {
		t.Fatalf("a chart overlay with an entrance was refused: %v", err)
	}
	cases := map[string]struct {
		change func(*VideoDocument)
		code   string
	}{
		"a text overlay": {func(d *VideoDocument) { text := videoDoc().Tracks[1].Clips[0].Layer; d.Tracks[1].Clips[0].Layer = text }, CodeInvalid},
		"an image clip":  {func(d *VideoDocument) { d.Tracks[0].Clips[0].ChartIn = grow }, CodeInvalid},
		"an unknown style": {func(d *VideoDocument) {
			d.Tracks[1].Clips[0].ChartIn = &ChartAnimator{Style: "spin", Order: "series", DurationMS: 800, Easing: mediagen.EaseOut}
		}, CodeUnknown},
		"an unknown order": {func(d *VideoDocument) {
			d.Tracks[1].Clips[0].ChartIn = &ChartAnimator{Style: ChartFade, Order: "row", DurationMS: 800, Easing: mediagen.EaseOut}
		}, CodeUnknown},
		"an unknown easing": {func(d *VideoDocument) {
			d.Tracks[1].Clips[0].ChartIn = &ChartAnimator{Style: ChartFade, Order: "series", DurationMS: 800, Easing: "wobbly"}
		}, CodeUnknown},
		"a blink": {func(d *VideoDocument) {
			d.Tracks[1].Clips[0].ChartIn = &ChartAnimator{Style: ChartFade, Order: "series", DurationMS: 50, Easing: mediagen.EaseOut}
		}, CodeOutOfRange},
		"a very long stagger": {func(d *VideoDocument) {
			d.Tracks[1].Clips[0].ChartIn = &ChartAnimator{Style: ChartFade, Order: "series", DurationMS: 800, StaggerMS: 6000, Easing: mediagen.EaseOut}
		}, CodeOutOfRange},
	}
	for name, c := range cases {
		doc := chartFilm(grow)
		c.change(&doc)
		if code := codeOf(t, ValidateDocument(KindVideo, mustJSON(t, doc)))[FieldTracks]; code != c.code {
			t.Errorf("%s: got %q, want %q", name, code, c.code)
		}
	}
}
