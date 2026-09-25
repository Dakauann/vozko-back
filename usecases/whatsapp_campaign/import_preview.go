package whatsapp_campaign_usecase

import (
	"context"
	"fmt"
	"strings"

	"vozko/domain/balance"
	"vozko/domain/lead"
	"vozko/domain/media"
	"vozko/domain/sheet"
	tmpl "vozko/domain/whatsapp/template"
	wc "vozko/domain/whatsapp_campaign"
)

type ImportPreviewDeps struct {
	Files     media.ReadMediaUseCase
	Templates tmpl.WorkspaceTemplatesUseCase
	Prices    tmpl.TemplateCostReader
	Balance   balance.BalanceReader
}

type importPreview struct{ deps ImportPreviewDeps }

func NewImportPreviewUseCase(deps ImportPreviewDeps) wc.ImportPreviewUseCase {
	return &importPreview{deps: deps}
}

func (uc *importPreview) Preview(ctx context.Context, req wc.ImportRequest) (*wc.ImportPreview, error) {
	template, err := uc.deps.Templates.Get(req.WorkspaceID, req.TemplateID)
	if err != nil {
		return nil, err
	}
	file, err := uc.deps.Files.Read(ctx, req.WorkspaceID, req.MediaID)
	if err != nil {
		return nil, err
	}
	rows := sheet.Parse(file.Data)
	if len(rows) < 2 {
		return nil, wc.ErrImportEmpty
	}
	variables := template.ParameterCount()
	headers := rows[0].Cells
	mapping := req.Mapping
	if strings.TrimSpace(mapping.Number) == "" {
		mapping = wc.DefaultMapping(headers, variables)
	}
	columns, err := resolveColumns(headers, mapping, variables)
	if err != nil {
		return nil, err
	}
	if len(rows)-1 > wc.MaxCampaignPhoneNumbers {
		return nil, wc.ErrCampaignPhoneNumbersTooMany
	}

	preview := &wc.ImportPreview{Headers: headers, Mapping: mapping, Variables: variables, TotalRows: len(rows) - 1, IssueCounts: map[string]int{}}
	seen := map[string]int{}
	for _, row := range rows[1:] {
		input, issue := readRow(row, columns, variables, seen)
		if issue != nil {
			preview.IssueCounts[issue.Reason]++
			if len(preview.Issues) < wc.MaxPreviewIssues {
				preview.Issues = append(preview.Issues, *issue)
			}
			continue
		}
		seen[input.Number] = row.Line
		preview.Rows = append(preview.Rows, input)
	}
	preview.ValidRows = len(preview.Rows)
	return preview, uc.price(req.WorkspaceID, template, preview)
}

type importColumns struct {
	number    int
	name      int
	variables []int
}

func resolveColumns(headers []string, mapping wc.ColumnMapping, variables int) (importColumns, error) {
	index := func(name string) int {
		for i, h := range headers {
			if strings.EqualFold(strings.TrimSpace(h), strings.TrimSpace(name)) && strings.TrimSpace(name) != "" {
				return i
			}
		}
		return -1
	}
	cols := importColumns{number: index(mapping.Number), name: index(mapping.Name)}
	if cols.number < 0 {
		return cols, wc.ErrImportNumberColumn
	}
	if len(mapping.Variables) != variables {
		return cols, wc.ErrImportVariableCount
	}
	for _, v := range mapping.Variables {
		i := index(v)
		if i < 0 {
			return cols, wc.ErrImportVariableCount
		}
		cols.variables = append(cols.variables, i)
	}
	return cols, nil
}

func readRow(row sheet.Row, cols importColumns, variables int, seen map[string]int) (wc.PhoneInput, *wc.ImportIssue) {
	cell := func(i int) string {
		if i < 0 || i >= len(row.Cells) {
			return ""
		}
		return strings.TrimSpace(row.Cells[i])
	}
	raw := cell(cols.number)
	number := lead.NormalizeNumber(lead.NormalizeRawNumber(raw))
	if number == "" {
		return wc.PhoneInput{}, &wc.ImportIssue{Line: row.Line, Reason: wc.IssueInvalidNumber, Value: raw}
	}
	if _, dup := seen[number]; dup {
		return wc.PhoneInput{}, &wc.ImportIssue{Line: row.Line, Reason: wc.IssueDuplicate, Value: raw}
	}
	values := make([]string, 0, variables)
	for n, i := range cols.variables {
		v := cell(i)
		if v == "" {
			return wc.PhoneInput{}, &wc.ImportIssue{Line: row.Line, Reason: wc.IssueMissingVariable, Variable: n + 1}
		}
		values = append(values, v)
	}
	return wc.PhoneInput{Number: number, Name: cell(cols.name), Variables: values}, nil
}

func (uc *importPreview) price(workspaceID string, template *tmpl.Template, preview *wc.ImportPreview) error {
	category, err := template.BillingCategory()
	if err != nil {
		return fmt.Errorf("template category: %w", err)
	}
	unit, err := uc.deps.Prices.GetTemplateCostMicros(workspaceID, category)
	if err != nil {
		return fmt.Errorf("template price: %w", err)
	}
	balance, err := uc.deps.Balance.GetBalance(workspaceID)
	if err != nil {
		return fmt.Errorf("balance: %w", err)
	}
	preview.UnitCostMicros = unit
	preview.CostMicros = unit * int64(preview.ValidRows)
	preview.BalanceMicros = balance
	preview.Affordable = unit > 0 && balance >= preview.CostMicros
	return nil
}
