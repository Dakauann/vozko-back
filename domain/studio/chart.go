package studio

import (
	"slices"

	"vozko/domain/mediagen"
)

const LayerChart LayerType = "chart"

type ChartKind string

const (
	ChartBar           ChartKind = "bar"
	ChartBarHorizontal ChartKind = "barHorizontal"
	ChartBarGrouped    ChartKind = "barGrouped"
	ChartBarStacked    ChartKind = "barStacked"
	ChartLine          ChartKind = "line"
	ChartLineMulti     ChartKind = "lineMulti"
	ChartArea          ChartKind = "area"
	ChartAreaStacked   ChartKind = "areaStacked"
	ChartPie           ChartKind = "pie"
	ChartDonut         ChartKind = "donut"
	ChartScatter       ChartKind = "scatter"
)

const (
	MinChartFontSize   = 0.02
	MaxChartFontSize   = 0.2
	MinChartLineWidth  = 0.001
	MaxChartLineWidth  = 0.05
	MaxChartBarGap     = 0.9
	MinChartHole       = 0.1
	MaxChartHole       = 0.9
	MaxChartLabelRunes = 200
	MaxChartNameRunes  = 120

	MinChartAnimationMS = 100
	MaxChartAnimationMS = 10_000
	MaxChartStaggerMS   = 5_000
)

func ChartKinds() []ChartKind {
	return []ChartKind{ChartBar, ChartBarHorizontal, ChartBarGrouped, ChartBarStacked, ChartLine, ChartLineMulti, ChartArea, ChartAreaStacked, ChartPie, ChartDonut, ChartScatter}
}

func ChartLegends() []string { return []string{"none", "top", "bottom", "right"} }

type ChartSeries struct {
	Name   string     `json:"name"`
	Values []*float64 `json:"values"`
}

type ChartData struct {
	Labels []string      `json:"labels"`
	Series []ChartSeries `json:"series"`
}

type ChartStyle struct {
	Palette     []string `json:"palette"`
	FontID      string   `json:"fontId"`
	FontSize    float64  `json:"fontSize"`
	TextColor   string   `json:"textColor"`
	GridColor   string   `json:"gridColor"`
	Axes        bool     `json:"axes"`
	Gridlines   bool     `json:"gridlines"`
	Legend      string   `json:"legend"`
	ValueLabels bool     `json:"valueLabels"`
	BarRadius   float64  `json:"barRadius"`
	BarGap      float64  `json:"barGap"`
	LineWidth   float64  `json:"lineWidth"`
	Smooth      bool     `json:"smooth"`
	Points      bool     `json:"points"`
	Hole        float64  `json:"hole"`
}

type ChartFormat struct {
	Locale   string `json:"locale"`
	Decimals int    `json:"decimals"`
	Prefix   string `json:"prefix,omitempty"`
	Suffix   string `json:"suffix,omitempty"`
	Percent  bool   `json:"percent"`
}

type ChartSpec struct {
	Kind   ChartKind   `json:"kind"`
	Data   ChartData   `json:"data"`
	Style  ChartStyle  `json:"style"`
	Format ChartFormat `json:"format"`
}

var chartLayerFields = map[string]bool{
	"id": true, "type": true, "name": true, "transform": true, "hidden": true, "locked": true,
	"groupId": true, "chart": true, "layoutItem": true,
}

func MinChartSeries(kind ChartKind) int {
	if kind == ChartScatter {
		return 2
	}
	return 1
}

func (l Layer) chartIssue() string {
	if !l.setsOnly(chartLayerFields) {
		return CodeInvalid
	}
	if l.Chart == nil {
		return CodeRequired
	}
	return l.Chart.Issue()
}

func (s ChartSpec) Issue() string {
	if !slices.Contains(ChartKinds(), s.Kind) {
		return CodeUnknown
	}
	if code := s.Data.issue(s.Kind); code != "" {
		return code
	}
	if code := s.Style.issue(); code != "" {
		return code
	}
	return s.Format.issue()
}

func (d ChartData) issue(kind ChartKind) string {
	if len(d.Labels) == 0 || len(d.Series) < MinChartSeries(kind) {
		return CodeRequired
	}
	for _, label := range d.Labels {
		if code := plainTextIssue(label, MaxChartLabelRunes); code != "" {
			return code
		}
	}
	for _, series := range d.Series {
		if code := plainTextIssue(series.Name, MaxChartNameRunes); code != "" {
			return code
		}
		if len(series.Values) != len(d.Labels) {
			return CodeInvalid
		}
		for _, value := range series.Values {
			if value != nil && !within(*value, MinCounterValue, MaxCounterValue) {
				return CodeOutOfRange
			}
		}
	}
	return ""
}

func (s ChartStyle) issue() string {
	if len(s.Palette) == 0 {
		return CodeInvalid
	}
	for _, color := range s.Palette {
		if !validColor(color, true) {
			return CodeInvalid
		}
	}
	switch {
	case !fonts[s.FontID]:
		return CodeUnknown
	case !validColor(s.TextColor, true) || !validColor(s.GridColor, true):
		return CodeInvalid
	case !slices.Contains(ChartLegends(), s.Legend):
		return CodeUnknown
	case !within(s.FontSize, MinChartFontSize, MaxChartFontSize) || !within(s.BarRadius, 0, 1) || !within(s.BarGap, 0, MaxChartBarGap):
		return CodeOutOfRange
	case !within(s.LineWidth, MinChartLineWidth, MaxChartLineWidth) || !within(s.Hole, MinChartHole, MaxChartHole):
		return CodeOutOfRange
	}
	return ""
}

func (f ChartFormat) issue() string {
	switch {
	case !slices.Contains(CounterLocales(), f.Locale):
		return CodeUnknown
	case f.Decimals < 0 || f.Decimals > MaxCounterDecimals:
		return CodeOutOfRange
	}
	if code := affixIssue(f.Prefix); code != "" {
		return code
	}
	return affixIssue(f.Suffix)
}

type ChartAnimationStyle string

const (
	ChartGrow  ChartAnimationStyle = "grow"
	ChartDraw  ChartAnimationStyle = "draw"
	ChartSweep ChartAnimationStyle = "sweep"
	ChartFade  ChartAnimationStyle = "fade"
)

func ChartAnimationStyles() []ChartAnimationStyle {
	return []ChartAnimationStyle{ChartGrow, ChartDraw, ChartSweep, ChartFade}
}

func ChartAnimationOrders() []string { return []string{"series", "category"} }

type ChartAnimator struct {
	Style      ChartAnimationStyle `json:"style"`
	Order      string              `json:"order"`
	DurationMS int64               `json:"durationMs"`
	StaggerMS  int64               `json:"staggerMs"`
	Easing     mediagen.Easing     `json:"easing"`
}

func (a *ChartAnimator) issue() string {
	switch {
	case a == nil:
		return ""
	case !slices.Contains(ChartAnimationStyles(), a.Style) || !slices.Contains(ChartAnimationOrders(), a.Order) || !a.Easing.Known():
		return CodeUnknown
	case a.DurationMS < MinChartAnimationMS || a.DurationMS > MaxChartAnimationMS || a.StaggerMS < 0 || a.StaggerMS > MaxChartStaggerMS:
		return CodeOutOfRange
	}
	return ""
}

func (c Clip) chartInIssue() string {
	if c.ChartIn == nil {
		return ""
	}
	if c.Type != ClipOverlay || c.Layer == nil || c.Layer.Type != LayerChart {
		return CodeInvalid
	}
	return c.ChartIn.issue()
}
