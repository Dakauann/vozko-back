package report

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

const (
	CSVDelimiter = ";"
	CSVDecimal   = ","
	CSVNewline   = "\r\n"
	UTF8BOM      = "\ufeff"
)

type CSVCell struct {
	text    string
	isEmpty bool
}

func Text(value string) CSVCell {
	return CSVCell{text: value}
}

func Empty() CSVCell {
	return CSVCell{isEmpty: true}
}

func Int(value int64) CSVCell {
	return CSVCell{text: strconv.FormatInt(value, 10)}
}

func IntPtr(value *int64) CSVCell {
	if value == nil {
		return Empty()
	}
	return Int(*value)
}

func Number(value float64) CSVCell {
	return CSVCell{text: csvNumber(value, 2)}
}

func NumberPtr(value *float64) CSVCell {
	if value == nil {
		return Empty()
	}
	return Number(*value)
}

func Bool(value bool) CSVCell {
	if value {
		return CSVCell{text: "true"}
	}
	return CSVCell{text: "false"}
}

func csvNumber(value float64, decimals int) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return ""
	}
	var rendered string
	if value == math.Trunc(value) && math.Abs(value) < 1e15 {
		rendered = strconv.FormatFloat(value, 'f', -1, 64)
	} else {
		rendered = strconv.FormatFloat(value, 'f', decimals, 64)
		rendered = strings.TrimRight(rendered, "0")
		rendered = strings.TrimRight(rendered, ".")
	}
	return strings.ReplaceAll(rendered, ".", CSVDecimal)
}

var newlines = regexp.MustCompile(`[\n\r]`)

func SafeCSVText(value string) string {
	flattened := newlines.ReplaceAllString(strings.ReplaceAll(value, "\r\n", " "), " ")
	if flattened == "" {
		return flattened
	}
	switch flattened[0] {
	case '=', '+', '-', '@', '\t':
		return "'" + flattened
	}
	return flattened
}

func (c CSVCell) render() string {
	if c.isEmpty {
		return ""
	}
	text := SafeCSVText(c.text)
	needsQuotes := strings.Contains(text, CSVDelimiter) ||
		strings.ContainsAny(text, "\"\n\r") ||
		text != strings.TrimSpace(text)
	if !needsQuotes {
		return text
	}
	return `"` + strings.ReplaceAll(text, `"`, `""`) + `"`
}

type CSVSection struct {
	Title  string
	Header []string
	Rows   [][]CSVCell
}

func renderRow(cells []CSVCell) string {
	parts := make([]string, 0, len(cells))
	for _, cell := range cells {
		parts = append(parts, cell.render())
	}
	return strings.Join(parts, CSVDelimiter)
}

func renderHeader(header []string) string {
	cells := make([]CSVCell, 0, len(header))
	for _, value := range header {
		cells = append(cells, Text(value))
	}
	return renderRow(cells)
}

func BuildCSVDocument(sections []CSVSection, withBOM bool) string {
	blocks := make([]string, 0, len(sections))

	for _, section := range sections {
		lines := make([]string, 0, len(section.Rows)+2)
		if section.Title != "" {
			lines = append(lines, renderRow([]CSVCell{Text(section.Title)}))
		}
		if len(section.Header) > 0 {
			lines = append(lines, renderHeader(section.Header))
		}
		for _, row := range section.Rows {
			lines = append(lines, renderRow(row))
		}
		if len(lines) > 0 {
			blocks = append(blocks, strings.Join(lines, CSVNewline))
		}
	}

	body := strings.Join(blocks, CSVNewline+CSVNewline)
	text := ""
	if body != "" {
		text = body + CSVNewline
	}
	if withBOM {
		return UTF8BOM + text
	}
	return text
}

var filenameUnsafe = regexp.MustCompile(`[^a-zA-Z0-9\-_.]`)
var filenameDashes = regexp.MustCompile(`-+`)

func Filename(extension string, parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			continue
		}
		kept = append(kept, part)
	}
	slug := filenameUnsafe.ReplaceAllString(strings.Join(kept, "-"), "-")
	slug = filenameDashes.ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")
	if slug == "" {
		slug = "export"
	}
	return fmt.Sprintf("%s.%s", slug, extension)
}
