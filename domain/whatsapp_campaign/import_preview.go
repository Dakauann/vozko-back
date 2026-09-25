package whatsapp_campaign

import (
	"context"
	"errors"
	"strconv"
	"strings"
)

const (
	IssueInvalidNumber   = "invalid_number"
	IssueMissingVariable = "missing_variable"
	IssueDuplicate       = "duplicate"
	MaxPreviewIssues     = 50
)

var (
	ErrImportEmpty         = errors.New("whatsapp campaign import: the file has no rows")
	ErrImportNumberColumn  = errors.New("whatsapp campaign import: the number column was not found")
	ErrImportVariableCount = errors.New("whatsapp campaign import: the mapping does not give one column per template variable")
)

type ColumnMapping struct {
	Number    string   `json:"number"`
	Name      string   `json:"name,omitempty"`
	Variables []string `json:"variables,omitempty"`
}

type ImportRequest struct {
	WorkspaceID string
	MediaID     string
	TemplateID  string
	Mapping     ColumnMapping
}

type ImportIssue struct {
	Line     int    `json:"line"`
	Reason   string `json:"reason"`
	Variable int    `json:"variable,omitempty"`
	Value    string `json:"value,omitempty"`
}

type ImportPreview struct {
	Headers        []string       `json:"headers"`
	Mapping        ColumnMapping  `json:"mapping"`
	Variables      int            `json:"variables"`
	TotalRows      int            `json:"totalRows"`
	ValidRows      int            `json:"validRows"`
	IssueCounts    map[string]int `json:"issueCounts"`
	Issues         []ImportIssue  `json:"issues"`
	UnitCostMicros int64          `json:"unitCostMicros"`
	CostMicros     int64          `json:"costMicros"`
	BalanceMicros  int64          `json:"balanceMicros"`
	Affordable     bool           `json:"affordable"`
	Rows           []PhoneInput   `json:"-"`
}

type ImportPreviewUseCase interface {
	Preview(ctx context.Context, req ImportRequest) (*ImportPreview, error)
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
