package studio

import (
	"slices"
	"unicode/utf8"
)

const LayerTable LayerType = "table"

const (
	MinTableFontSize    = 0.01
	MaxTableFontSize    = 0.3
	MinTableLineHeight  = 0.8
	MaxTableLineHeight  = 3
	MaxTableBorderWidth = 0.05
	MaxTablePadding     = 3
	MinTableColumnWidth = 0.01
	MaxTableColumnWidth = 100
	MinTableWeight      = 100
	MaxTableWeight      = 900
)

func TableAligns() []string { return []string{"left", "center", "right"} }

func TableBorders() []string { return []string{"all", "rows", "outer", "none"} }

type TableCell struct {
	Text string    `json:"text"`
	Runs []TextRun `json:"runs,omitempty"`
}

type TableColumn struct {
	Width float64 `json:"width"`
	Align string  `json:"align"`
}

type TableStyle struct {
	FontID       string  `json:"fontId"`
	FontSize     float64 `json:"fontSize"`
	LineHeight   float64 `json:"lineHeight"`
	Weight       int     `json:"weight"`
	TextColor    string  `json:"textColor"`
	Fill         string  `json:"fill"`
	HeaderFill   string  `json:"headerFill"`
	HeaderText   string  `json:"headerText"`
	HeaderWeight int     `json:"headerWeight"`
	Banding      bool    `json:"banding"`
	BandFill     string  `json:"bandFill"`
	Borders      string  `json:"borders"`
	BorderColor  string  `json:"borderColor"`
	BorderWidth  float64 `json:"borderWidth"`
	Padding      float64 `json:"padding"`
}

type TableSpec struct {
	Columns []TableColumn  `json:"columns"`
	Rows    [][]*TableCell `json:"rows"`
	Header  bool           `json:"header"`
	Style   TableStyle     `json:"style"`
}

var tableLayerFields = map[string]bool{
	"id": true, "type": true, "name": true, "transform": true, "hidden": true, "locked": true,
	"groupId": true, "table": true, "layoutItem": true,
}

func (l Layer) tableIssue() string {
	if !l.setsOnly(tableLayerFields) {
		return CodeInvalid
	}
	if l.Table == nil {
		return CodeRequired
	}
	return l.Table.Issue()
}

func (s TableSpec) Issue() string {
	if len(s.Columns) == 0 || len(s.Rows) == 0 {
		return CodeRequired
	}
	for _, column := range s.Columns {
		if !slices.Contains(TableAligns(), column.Align) {
			return CodeUnknown
		}
		if !within(column.Width, MinTableColumnWidth, MaxTableColumnWidth) {
			return CodeOutOfRange
		}
	}
	for _, row := range s.Rows {
		if len(row) != len(s.Columns) {
			return CodeInvalid
		}
		for _, cell := range row {
			if code := cell.issue(); code != "" {
				return code
			}
		}
	}
	return s.Style.issue()
}

func cellTextValid(text string) bool {
	if utf8.RuneCountInString(text) > MaxTextRunes {
		return false
	}
	for _, r := range text {
		if (r < 0x20 && r != '\n') || (r >= 0x7f && r <= 0x9f) {
			return false
		}
	}
	return true
}

func (c *TableCell) issue() string {
	if c == nil || !cellTextValid(c.Text) {
		return CodeInvalid
	}
	for _, run := range c.Runs {
		if run.FontSize != 0 {
			return CodeInvalid
		}
	}
	return Layer{Type: LayerText, Runs: c.Runs}.textRunsIssue(utf8.RuneCountInString(c.Text))
}

func tableWeightValid(weight int) bool {
	return weight >= MinTableWeight && weight <= MaxTableWeight && weight%100 == 0
}

func (s TableStyle) issue() string {
	switch {
	case !fonts[s.FontID]:
		return CodeUnknown
	case !slices.Contains(TableBorders(), s.Borders):
		return CodeUnknown
	}
	for _, color := range []string{s.TextColor, s.Fill, s.HeaderFill, s.HeaderText, s.BandFill, s.BorderColor} {
		if !validColor(color, true) {
			return CodeInvalid
		}
	}
	switch {
	case !tableWeightValid(s.Weight) || !tableWeightValid(s.HeaderWeight):
		return CodeOutOfRange
	case !within(s.FontSize, MinTableFontSize, MaxTableFontSize) || !within(s.LineHeight, MinTableLineHeight, MaxTableLineHeight):
		return CodeOutOfRange
	case !within(s.BorderWidth, 0, MaxTableBorderWidth) || !within(s.Padding, 0, MaxTablePadding):
		return CodeOutOfRange
	}
	return ""
}
