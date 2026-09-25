package copilot

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type ChartType string

const (
	ChartBar           ChartType = "bar"
	ChartHorizontalBar ChartType = "horizontal_bar"
	ChartStackedBar    ChartType = "stacked_bar"
	ChartLine          ChartType = "line"
	ChartArea          ChartType = "area"
	ChartStackedArea   ChartType = "stacked_area"
	ChartPie           ChartType = "pie"
	ChartDonut         ChartType = "donut"
	ChartScatter       ChartType = "scatter"
	ChartRadar         ChartType = "radar"
	ChartTable         ChartType = "table"
)

func ChartTypes() []ChartType {
	return []ChartType{ChartBar, ChartHorizontalBar, ChartStackedBar, ChartLine, ChartArea, ChartStackedArea, ChartPie, ChartDonut, ChartScatter, ChartRadar, ChartTable}
}

func (t ChartType) valid() bool {
	for _, known := range ChartTypes() {
		if t == known {
			return true
		}
	}
	return false
}

func (t ChartType) singleSeries() bool { return t == ChartPie || t == ChartDonut }

const (
	MaxChartSeries    = 5
	MaxCategoryPoints = 60
	MaxOrderedPoints  = 400
	MaxPieSlices      = 5
	MaxTableRows      = 50
	MinRadarSpokes    = 3
	OtherCategory     = "__other__"
	InlineCategoryKey = "category"
)

var ErrInvalidChart = errors.New("copilot: invalid chart")

type ChartSeries struct {
	Key    string     `json:"key"`
	Label  string     `json:"label"`
	Kind   ColumnKind `json:"kind"`
	Values []*float64 `json:"values"`
}

type Chart struct {
	Type       ChartType     `json:"type"`
	Title      string        `json:"title"`
	Subtitle   string        `json:"subtitle,omitempty"`
	XLabel     string        `json:"xLabel"`
	XKind      ColumnKind    `json:"xKind"`
	Categories []string      `json:"categories,omitempty"`
	XValues    []*float64    `json:"xValues,omitempty"`
	Series     []ChartSeries `json:"series"`
}

type ChartRequest struct {
	Type       ChartType
	Title      string
	Subtitle   string
	X          string
	Y          []string
	SortBy     string
	Descending bool
	Limit      int
}

func invalidChart(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidChart, fmt.Sprintf(format, args...))
}

func BuildChart(d *Dataset, req ChartRequest) (Chart, error) {
	if !req.Type.valid() {
		return Chart{}, invalidChart("unknown chart type %q, use one of %s", req.Type, chartTypeList())
	}
	xIdx, err := d.columnIndex(req.X)
	if err != nil {
		return Chart{}, invalidChart("x: %v", err)
	}
	yIdx, err := seriesColumns(d, req)
	if err != nil {
		return Chart{}, err
	}
	rows, err := d.sortedRows(req.SortBy, req.Descending)
	if err != nil {
		return Chart{}, invalidChart("sort: %v", err)
	}
	if req.Type.singleSeries() && req.SortBy == "" {
		rows, _ = d.sortedRows(req.Y[0], true)
	}
	if req.Limit > 0 && req.Limit < len(rows) {
		rows = rows[:req.Limit]
	}
	if err := checkPointCount(req.Type, len(rows)); err != nil {
		return Chart{}, err
	}

	xCol := d.Columns[xIdx]
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = d.Title
	}
	c := Chart{Type: req.Type, Title: title, Subtitle: strings.TrimSpace(req.Subtitle), XLabel: labelOf(xCol), XKind: xCol.Kind}
	if req.Type == ChartScatter {
		if !xCol.Kind.Numeric() {
			return Chart{}, invalidChart("scatter needs a numeric x column, %q is %s", xCol.Key, xCol.Kind)
		}
		c.XValues = numericColumn(rows, xIdx)
	} else {
		c.Categories = make([]string, 0, len(rows))
		for _, row := range rows {
			c.Categories = append(c.Categories, cellText(row[xIdx]))
		}
	}
	for _, idx := range yIdx {
		col := d.Columns[idx]
		c.Series = append(c.Series, ChartSeries{Key: col.Key, Label: labelOf(col), Kind: col.Kind, Values: numericColumn(rows, idx)})
	}
	if req.Type.singleSeries() {
		foldPieTail(&c)
	}
	return c, nil
}

func seriesColumns(d *Dataset, req ChartRequest) ([]int, error) {
	if len(req.Y) == 0 {
		return nil, invalidChart("name at least one numeric y column")
	}
	if len(req.Y) > MaxChartSeries {
		return nil, invalidChart("at most %d series per chart", MaxChartSeries)
	}
	if req.Type.singleSeries() && len(req.Y) != 1 {
		return nil, invalidChart("%s shows exactly one series", req.Type)
	}
	out := make([]int, 0, len(req.Y))
	for _, key := range req.Y {
		idx, err := d.columnIndex(key)
		if err != nil {
			return nil, invalidChart("y: %v", err)
		}
		col := d.Columns[idx]
		if !col.Kind.Numeric() {
			return nil, invalidChart("y column %q is %s, not a number", key, col.Kind)
		}
		if req.Type != ChartTable && len(out) > 0 && d.Columns[out[0]].Kind != col.Kind {
			return nil, invalidChart("series %q (%s) and %q (%s) do not share a scale, chart them separately", d.Columns[out[0]].Key, d.Columns[out[0]].Kind, key, col.Kind)
		}
		out = append(out, idx)
	}
	return out, nil
}

func checkPointCount(t ChartType, n int) error {
	switch t {
	case ChartLine, ChartArea, ChartStackedArea, ChartScatter:
		if n > MaxOrderedPoints {
			return invalidChart("%d points exceed the %d a %s can show, aggregate first or pass limit", n, MaxOrderedPoints, t)
		}
	case ChartTable:
		if n > MaxTableRows {
			return invalidChart("%d rows exceed the %d a table shows, pass sort_by and limit", n, MaxTableRows)
		}
	case ChartPie, ChartDonut:
	default:
		if n > MaxCategoryPoints {
			return invalidChart("%d categories exceed the %d a %s can show, pass sort_by and limit for a top N", n, MaxCategoryPoints, t)
		}
	}
	if t == ChartRadar && n < MinRadarSpokes {
		return invalidChart("a radar needs at least %d categories", MinRadarSpokes)
	}
	return nil
}

func foldPieTail(c *Chart) {
	if len(c.Categories) <= MaxPieSlices {
		return
	}
	keep := MaxPieSlices - 1
	values := c.Series[0].Values
	var other float64
	for _, v := range values[keep:] {
		if v != nil {
			other += *v
		}
	}
	c.Categories = append(c.Categories[:keep], OtherCategory)
	c.Series[0].Values = append(values[:keep], &other)
}

func numericColumn(rows [][]any, idx int) []*float64 {
	out := make([]*float64, len(rows))
	for i, row := range rows {
		if v, ok := row[idx].(float64); ok {
			out[i] = &v
		}
	}
	return out
}

func cellText(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	}
	return fmt.Sprint(v)
}

func labelOf(c Column) string {
	if strings.TrimSpace(c.Label) != "" {
		return c.Label
	}
	return c.Key
}

func chartTypeList() string {
	names := make([]string, 0, len(ChartTypes()))
	for _, t := range ChartTypes() {
		names = append(names, string(t))
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

type InlineSeries struct {
	Label  string
	Values []float64
}

func InlineDataset(title string, categories []string, series []InlineSeries, kind ColumnKind) (*Dataset, error) {
	if !kind.Numeric() {
		kind = ColumnNumber
	}
	if len(categories) == 0 {
		return nil, fmt.Errorf("%w: no categories", ErrInvalidDataset)
	}
	cols := []Column{{Key: InlineCategoryKey, Label: "", Kind: ColumnText}}
	for i, s := range series {
		if len(s.Values) != len(categories) {
			return nil, fmt.Errorf("%w: series %q has %d values for %d categories", ErrInvalidDataset, s.Label, len(s.Values), len(categories))
		}
		cols = append(cols, Column{Key: "s" + strconv.Itoa(i+1), Label: s.Label, Kind: kind})
	}
	d := NewDataset(title, cols)
	for i, category := range categories {
		row := []any{category}
		for _, s := range series {
			row = append(row, s.Values[i])
		}
		if err := d.AddRow(row...); err != nil {
			return nil, err
		}
	}
	return d, nil
}
