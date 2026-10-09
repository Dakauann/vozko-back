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
	var rows []Row
	_ = Each(data, func(r Row) error {
		rows = append(rows, r)
		return nil
	})
	return rows
}

func Each(data []byte, fn func(Row) error) error {
	text := decode(data)
	delimiter := rune(0)
	for number := 1; text != ""; number++ {
		line := text
		if cut := strings.IndexByte(text, '\n'); cut >= 0 {
			line, text = text[:cut], text[cut+1:]
		} else {
			text = ""
		}
		trimmed := strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if trimmed == "" {
			continue
		}
		if delimiter == 0 {
			delimiter = DetectDelimiter(trimmed)
		}
		if err := fn(Row{Line: number, Cells: SplitCells(trimmed, delimiter)}); err != nil {
			return err
		}
	}
	return nil
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
	return strings.TrimPrefix(DecodeLegacyText(string(data)), bom)
}

func DecodeLegacyText(text string) string {
	if utf8.ValidString(text) {
		return text
	}
	if decoded, err := charmap.Windows1252.NewDecoder().String(text); err == nil {
		return decoded
	}
	return text
}
