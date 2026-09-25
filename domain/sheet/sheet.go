package sheet

import (
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
)

const bom = "\xEF\xBB\xBF"

type Row struct {
	Line  int
	Cells []string
}

func Parse(data []byte) []Row {
	lines := strings.Split(decode(data), "\n")
	delimiter := rune(0)
	var rows []Row
	for i, line := range lines {
		trimmed := strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if trimmed == "" {
			continue
		}
		if delimiter == 0 {
			delimiter = DetectDelimiter(trimmed)
		}
		rows = append(rows, Row{Line: i + 1, Cells: SplitCells(trimmed, delimiter)})
	}
	return rows
}

func DetectDelimiter(line string) rune {
	switch {
	case strings.Contains(line, "\t"):
		return '\t'
	case strings.Contains(line, ";"):
		return ';'
	}
	return ','
}

func SplitCells(line string, delimiter rune) []string {
	var cells []string
	var current strings.Builder
	quoted := false
	runes := []rune(line)
	for i := 0; i < len(runes); i++ {
		ch := runes[i]
		switch {
		case ch == '"' && quoted && i+1 < len(runes) && runes[i+1] == '"':
			current.WriteRune('"')
			i++
		case ch == '"':
			quoted = !quoted
		case ch == delimiter && !quoted:
			cells = append(cells, strings.TrimSpace(current.String()))
			current.Reset()
		default:
			current.WriteRune(ch)
		}
	}
	return append(cells, strings.TrimSpace(current.String()))
}

func decode(data []byte) string {
	text := string(data)
	if !utf8.ValidString(text) {
		if decoded, err := charmap.Windows1252.NewDecoder().String(text); err == nil {
			text = decoded
		}
	}
	return strings.TrimPrefix(text, bom)
}
