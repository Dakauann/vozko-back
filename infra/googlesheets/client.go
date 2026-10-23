package googlesheets

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"vozko/domain/googlesheet"
)

const (
	requestTimeout  = 30 * time.Second
	maxResponseBody = 32 << 20
)

type Endpoints struct {
	Sheets string
	Drive  string
}

var GoogleEndpoints = Endpoints{
	Sheets: "https://sheets.googleapis.com/v4/spreadsheets",
	Drive:  "https://www.googleapis.com/drive/v3/files",
}

type Client struct {
	endpoints Endpoints
	http      *http.Client
}

var _ googlesheet.SheetsAPI = (*Client)(nil)

func New(endpoints Endpoints) *Client {
	if endpoints == (Endpoints{}) {
		endpoints = GoogleEndpoints
	}
	return &Client{endpoints: endpoints, http: &http.Client{Timeout: requestTimeout}}
}

type gridProperties struct {
	RowCount       int `json:"rowCount,omitempty"`
	ColumnCount    int `json:"columnCount,omitempty"`
	FrozenRowCount int `json:"frozenRowCount,omitempty"`
}

type sheetProperties struct {
	SheetID        int64          `json:"sheetId"`
	Title          string         `json:"title"`
	GridProperties gridProperties `json:"gridProperties"`
}

type spreadsheetBody struct {
	SpreadsheetID string `json:"spreadsheetId,omitempty"`
	Properties    struct {
		Title    string `json:"title"`
		Locale   string `json:"locale,omitempty"`
		TimeZone string `json:"timeZone,omitempty"`
	} `json:"properties"`
	Sheets []struct {
		Properties sheetProperties `json:"properties"`
	} `json:"sheets"`
}

func (c *Client) CreateSpreadsheet(ctx context.Context, token string, spec googlesheet.SpreadsheetSpec) (googlesheet.CreatedSpreadsheet, error) {
	var body spreadsheetBody
	body.Properties.Title = spec.Title
	body.Properties.Locale = spec.Locale
	body.Properties.TimeZone = spec.TimeZone
	body.Sheets = []struct {
		Properties sheetProperties `json:"properties"`
	}{{Properties: sheetProperties{Title: spec.TabName, GridProperties: gridProperties{
		RowCount: 1000, ColumnCount: len(spec.Columns) + 1, FrozenRowCount: 1,
	}}}}
	var created spreadsheetBody
	if err := c.do(ctx, http.MethodPost, c.endpoints.Sheets, token, body, &created); err != nil {
		return googlesheet.CreatedSpreadsheet{}, err
	}
	if created.SpreadsheetID == "" || len(created.Sheets) == 0 {
		return googlesheet.CreatedSpreadsheet{}, fmt.Errorf("googlesheets: create returned no spreadsheet")
	}
	out := googlesheet.CreatedSpreadsheet{SpreadsheetID: created.SpreadsheetID, TabID: created.Sheets[0].Properties.SheetID}
	target := googlesheet.Target{SpreadsheetID: out.SpreadsheetID, TabID: out.TabID, TabName: spec.TabName}
	requests := append(columnRequests(target.TabID, spec.Columns), protectHeader(target.TabID))
	if err := c.batchUpdate(ctx, token, target.SpreadsheetID, requests); err != nil {
		return out, err
	}
	return out, nil
}

func (c *Client) TrashSpreadsheet(ctx context.Context, token, spreadsheetID string) error {
	return c.do(ctx, http.MethodPatch, c.endpoints.Drive+"/"+url.PathEscape(spreadsheetID), token, map[string]any{"trashed": true}, nil)
}

func (c *Client) ApplyColumns(ctx context.Context, token string, target googlesheet.Target, columns []googlesheet.Column) error {
	info, err := c.TabInfo(ctx, token, target)
	if err != nil {
		return err
	}
	var requests []map[string]any
	if missing := len(columns) + 1 - info.Columns; missing > 0 {
		requests = append(requests, map[string]any{"appendDimension": map[string]any{
			"sheetId": target.TabID, "dimension": "COLUMNS", "length": missing,
		}})
	}
	return c.batchUpdate(ctx, token, target.SpreadsheetID, append(requests, columnRequests(target.TabID, columns)...))
}

type appendResponse struct {
	Updates struct {
		UpdatedRange string `json:"updatedRange"`
		UpdatedRows  int    `json:"updatedRows"`
	} `json:"updates"`
}

var firstRowPattern = regexp.MustCompile(`![A-Z]+(\d+)`)

func (c *Client) AppendRows(ctx context.Context, token string, target googlesheet.Target, rows [][]any) (googlesheet.AppendResult, error) {
	a1 := quotedTab(target.TabName) + "!A1"
	query := url.Values{
		"valueInputOption":        {"RAW"},
		"insertDataOption":        {"INSERT_ROWS"},
		"includeValuesInResponse": {"false"},
	}
	endpoint := c.endpoints.Sheets + "/" + url.PathEscape(target.SpreadsheetID) + "/values/" + url.PathEscape(a1) + ":append?" + query.Encode()
	var out appendResponse
	if err := c.do(ctx, http.MethodPost, endpoint, token, map[string]any{"range": a1, "majorDimension": "ROWS", "values": rows}, &out); err != nil {
		return googlesheet.AppendResult{}, err
	}
	match := firstRowPattern.FindStringSubmatch(out.Updates.UpdatedRange)
	if match == nil {
		return googlesheet.AppendResult{}, fmt.Errorf("googlesheets: append returned the range %q", out.Updates.UpdatedRange)
	}
	first, _ := strconv.Atoi(match[1])
	return googlesheet.AppendResult{FirstRow: first, Rows: out.Updates.UpdatedRows}, nil
}

func (c *Client) ReadRange(ctx context.Context, token string, target googlesheet.Target, a1 string) ([][]any, error) {
	query := url.Values{"majorDimension": {"ROWS"}, "valueRenderOption": {"UNFORMATTED_VALUE"}}
	endpoint := c.endpoints.Sheets + "/" + url.PathEscape(target.SpreadsheetID) + "/values/" + url.PathEscape(quotedTab(target.TabName)+"!"+a1) + "?" + query.Encode()
	var out struct {
		Values [][]any `json:"values"`
	}
	if err := c.do(ctx, http.MethodGet, endpoint, token, nil, &out); err != nil {
		return nil, err
	}
	return out.Values, nil
}

func (c *Client) TabInfo(ctx context.Context, token string, target googlesheet.Target) (googlesheet.TabInfo, error) {
	endpoint := c.endpoints.Sheets + "/" + url.PathEscape(target.SpreadsheetID) + "?" + url.Values{"fields": {"sheets.properties(sheetId,title,gridProperties)"}}.Encode()
	var out spreadsheetBody
	if err := c.do(ctx, http.MethodGet, endpoint, token, nil, &out); err != nil {
		return googlesheet.TabInfo{}, err
	}
	for _, sheet := range out.Sheets {
		if sheet.Properties.SheetID == target.TabID {
			return googlesheet.TabInfo{
				Title:   sheet.Properties.Title,
				Rows:    sheet.Properties.GridProperties.RowCount,
				Columns: sheet.Properties.GridProperties.ColumnCount,
			}, nil
		}
	}
	return googlesheet.TabInfo{}, &APIError{Status: http.StatusNotFound, Reason: "tab_missing", Message: "a aba da planilha foi removida"}
}

func (c *Client) batchUpdate(ctx context.Context, token, spreadsheetID string, requests []map[string]any) error {
	if len(requests) == 0 {
		return nil
	}
	endpoint := c.endpoints.Sheets + "/" + url.PathEscape(spreadsheetID) + ":batchUpdate"
	return c.do(ctx, http.MethodPost, endpoint, token, map[string]any{"requests": requests}, nil)
}

func quotedTab(name string) string {
	return "'" + name + "'"
}

func (c *Client) do(ctx context.Context, method, endpoint, token string, body, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("googlesheets: encode request: %w", err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.http.Do(req)
	if err != nil {
		return &TransportError{Err: err}
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxResponseBody))
	if err != nil {
		return &TransportError{Err: err}
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return newAPIError(res, raw)
	}
	if out == nil || len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("googlesheets: decode %s: %w", strings.SplitN(endpoint, "?", 2)[0], err)
	}
	return nil
}
