package googlesheets

import (
	"vozko/domain/googlesheet"
)

var numberFormats = map[googlesheet.ColumnType]map[string]any{
	googlesheet.TypeNumber:   {"type": "NUMBER", "pattern": "#,##0.##########"},
	googlesheet.TypeMoney:    {"type": "CURRENCY", "pattern": "\"R$\" #,##0.00"},
	googlesheet.TypeDate:     {"type": "DATE", "pattern": "dd/mm/yyyy"},
	googlesheet.TypeDateTime: {"type": "DATE_TIME", "pattern": "dd/mm/yyyy hh:mm"},
}

func columnRequests(tabID int64, columns []googlesheet.Column) []map[string]any {
	header := []map[string]any{headerCell(googlesheet.RefHeader)}
	for _, col := range columns {
		header = append(header, headerCell(col.Header))
	}
	requests := []map[string]any{
		{"updateCells": map[string]any{
			"range":  gridRange(tabID, 0, 1, 0, len(columns)+1),
			"rows":   []map[string]any{{"values": header}},
			"fields": "userEnteredValue,userEnteredFormat.textFormat.bold",
		}},
		{"updateDimensionProperties": map[string]any{
			"range":      map[string]any{"sheetId": tabID, "dimension": "COLUMNS", "startIndex": 0, "endIndex": 1},
			"properties": map[string]any{"hiddenByUser": true},
			"fields":     "hiddenByUser",
		}},
	}
	for i, col := range columns {
		index := i + 1
		body := gridRange(tabID, 1, 0, index, index+1)
		if format, ok := numberFormats[col.Type]; ok {
			requests = append(requests, map[string]any{"repeatCell": map[string]any{
				"range":  body,
				"cell":   map[string]any{"userEnteredFormat": map[string]any{"numberFormat": format}},
				"fields": "userEnteredFormat.numberFormat",
			}})
		}
		if rule := validationRule(col); rule != nil {
			requests = append(requests, map[string]any{"setDataValidation": map[string]any{"range": body, "rule": rule}})
		}
	}
	return requests
}

func validationRule(col googlesheet.Column) map[string]any {
	switch {
	case col.Archived:
		return nil
	case col.Type == googlesheet.TypeBoolean:
		return map[string]any{"condition": map[string]any{"type": "BOOLEAN"}, "strict": true}
	case col.Type == googlesheet.TypeChoice:
		values := make([]map[string]any, len(col.Options))
		for i, option := range col.Options {
			values[i] = map[string]any{"userEnteredValue": option}
		}
		return map[string]any{"condition": map[string]any{"type": "ONE_OF_LIST", "values": values}, "strict": true, "showCustomUi": true}
	}
	return nil
}

func protectHeader(tabID int64) map[string]any {
	return map[string]any{"addProtectedRange": map[string]any{"protectedRange": map[string]any{
		"range":       gridRange(tabID, 0, 1, 0, 0),
		"description": "Cabeçalho usado pelas gravações automáticas",
		"warningOnly": true,
	}}}
}

func headerCell(text string) map[string]any {
	return map[string]any{
		"userEnteredValue":  map[string]any{"stringValue": text},
		"userEnteredFormat": map[string]any{"textFormat": map[string]any{"bold": true}},
	}
}

func gridRange(tabID int64, startRow, endRow, startColumn, endColumn int) map[string]any {
	r := map[string]any{"sheetId": tabID, "startRowIndex": startRow}
	if endRow > 0 {
		r["endRowIndex"] = endRow
	}
	if endColumn > 0 {
		r["startColumnIndex"] = startColumn
		r["endColumnIndex"] = endColumn
	}
	return r
}
