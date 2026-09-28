package campaign

import (
	"errors"
	"strconv"
	"strings"

	"vozko/domain/sheet"
)

const (
	IssueInvalidNumber   = "invalid_number"
	IssueMissingVariable = "missing_variable"
	IssueDuplicate       = "duplicate"
	MaxImportIssues      = 50
)

var (
	ErrImportEmpty         = errors.New("campaign import: the file has no rows")
	ErrImportNumberColumn  = errors.New("campaign import: the number column was not found")
	ErrImportVariableCount = errors.New("campaign import: the mapping does not give one column per message variable")
)

type ColumnMapping struct {
	Number    string   `json:"number"`
	Name      string   `json:"name,omitempty"`
	Variables []string `json:"variables,omitempty"`
}

type ImportIssue struct {
	Line     int    `json:"line"`
	Reason   string `json:"reason"`
	Variable int    `json:"variable,omitempty"`
	Value    string `json:"value,omitempty"`
}

type ImportRow struct {
	Number    string
	Name      string
	Variables []string
}

type ImportResult struct {
	Headers     []string       `json:"headers"`
	Mapping     ColumnMapping  `json:"mapping"`
	Variables   int            `json:"variables"`
	TotalRows   int            `json:"totalRows"`
	ValidRows   int            `json:"validRows"`
	IssueCounts map[string]int `json:"issueCounts"`
	Issues      []ImportIssue  `json:"issues"`
	Rows        []ImportRow    `json:"-"`
}

type NumberNormalizer func(raw string) string

func ReadImport(rows []sheet.Row, mapping ColumnMapping, variables int, normalize NumberNormalizer) (*ImportResult, error) {
	if len(rows) < 2 {
		return nil, ErrImportEmpty
	}
	headers := rows[0].Cells
	if strings.TrimSpace(mapping.Number) == "" {
		mapping = DefaultMapping(headers, variables)
	}
	columns, err := resolveColumns(headers, mapping, variables)
	if err != nil {
		return nil, err
	}
	result := &ImportResult{Headers: headers, Mapping: mapping, Variables: variables, TotalRows: len(rows) - 1, IssueCounts: map[string]int{}}
	seen := map[string]struct{}{}
	for _, row := range rows[1:] {
		parsed, issue := columns.read(row, normalize, seen)
		if issue != nil {
			result.IssueCounts[issue.Reason]++
			if len(result.Issues) < MaxImportIssues {
				result.Issues = append(result.Issues, *issue)
			}
			continue
		}
		seen[parsed.Number] = struct{}{}
		result.Rows = append(result.Rows, parsed)
	}
	result.ValidRows = len(result.Rows)
	return result, nil
}

func DefaultMapping(headers []string, variables int) ColumnMapping {
	var m ColumnMapping
	byVariable := map[int]string{}
	for _, h := range headers {
		key := strings.ToLower(strings.TrimSpace(h))
		switch key {
		case "number", "numero", "número", "telefone", "phone", "celular", "whatsapp":
			if m.Number == "" {
				m.Number = h
			}
		case "name", "nome":
			if m.Name == "" {
				m.Name = h
			}
		}
		if strings.HasPrefix(key, "var") {
			if n, err := strconv.Atoi(strings.TrimPrefix(key, "var")); err == nil && n >= 1 {
				byVariable[n] = h
			}
		}
	}
	for i := 1; i <= variables; i++ {
		m.Variables = append(m.Variables, byVariable[i])
	}
	return m
}

type importColumns struct {
	number    int
	name      int
	variables []int
}

func resolveColumns(headers []string, mapping ColumnMapping, variables int) (importColumns, error) {
	index := func(name string) int {
		name = strings.TrimSpace(name)
		if name == "" {
			return -1
		}
		for i, h := range headers {
			if strings.EqualFold(strings.TrimSpace(h), name) {
				return i
			}
		}
		return -1
	}
	cols := importColumns{number: index(mapping.Number), name: index(mapping.Name)}
	if cols.number < 0 {
		return cols, ErrImportNumberColumn
	}
	if len(mapping.Variables) != variables {
		return cols, ErrImportVariableCount
	}
	for _, v := range mapping.Variables {
		i := index(v)
		if i < 0 {
			return cols, ErrImportVariableCount
		}
		cols.variables = append(cols.variables, i)
	}
	return cols, nil
}

func (cols importColumns) read(row sheet.Row, normalize NumberNormalizer, seen map[string]struct{}) (ImportRow, *ImportIssue) {
	cell := func(i int) string {
		if i < 0 || i >= len(row.Cells) {
			return ""
		}
		return strings.TrimSpace(row.Cells[i])
	}
	raw := cell(cols.number)
	number := normalize(raw)
	if number == "" {
		return ImportRow{}, &ImportIssue{Line: row.Line, Reason: IssueInvalidNumber, Value: raw}
	}
	if _, dup := seen[number]; dup {
		return ImportRow{}, &ImportIssue{Line: row.Line, Reason: IssueDuplicate, Value: raw}
	}
	values := make([]string, 0, len(cols.variables))
	for n, i := range cols.variables {
		v := cell(i)
		if v == "" {
			return ImportRow{}, &ImportIssue{Line: row.Line, Reason: IssueMissingVariable, Variable: n + 1}
		}
		values = append(values, v)
	}
	return ImportRow{Number: number, Name: cell(cols.name), Variables: values}, nil
}
