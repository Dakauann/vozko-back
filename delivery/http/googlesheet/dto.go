package googlesheet

import (
	"time"

	"vozko/domain/googlesheet"
	"vozko/domain/shared"
	gsuc "vozko/usecases/googlesheet"
)

type ColumnRequest struct {
	Header      string   `json:"header" example:"Nome do cliente"`
	Type        string   `json:"type" example:"text" enums:"text,long_text,number,money,date,datetime,boolean,choice,email,phone,url"`
	Options     []string `json:"options,omitempty"`
	Required    bool     `json:"required"`
	Instruction string   `json:"instruction,omitempty" example:"Nome completo como o cliente informou"`
	Source      string   `json:"source,omitempty" example:"input" enums:"input,written_at,lead_name,lead_phone,lead_email,conversation_link,origin"`
}

func (c ColumnRequest) input() googlesheet.ColumnInput {
	return googlesheet.ColumnInput{
		Header:      c.Header,
		Type:        googlesheet.ColumnType(c.Type),
		Options:     c.Options,
		Required:    c.Required,
		Instruction: c.Instruction,
		Source:      googlesheet.Source(c.Source),
	}
}

func inputs(columns []ColumnRequest) []googlesheet.ColumnInput {
	out := make([]googlesheet.ColumnInput, len(columns))
	for i, c := range columns {
		out[i] = c.input()
	}
	return out
}

type CreateSheetRequest struct {
	Title       string          `json:"title" example:"Pedidos do site"`
	TabName     string          `json:"tabName,omitempty" example:"Pedidos"`
	Description string          `json:"description,omitempty" example:"Pedidos registrados pelo agente de vendas"`
	TimeZone    string          `json:"timeZone,omitempty" example:"America/Sao_Paulo"`
	Columns     []ColumnRequest `json:"columns"`
}

type UpdateDetailsRequest struct {
	Version     int     `json:"version" example:"3"`
	Title       *string `json:"title,omitempty"`
	Description *string `json:"description,omitempty"`
}

type ColumnUpdateRequest struct {
	Key         string   `json:"key" example:"valor"`
	Header      *string  `json:"header,omitempty"`
	Instruction *string  `json:"instruction,omitempty"`
	Required    *bool    `json:"required,omitempty"`
	Archived    *bool    `json:"archived,omitempty"`
	Options     []string `json:"options,omitempty"`
}

type EditColumnsRequest struct {
	Version int                   `json:"version" example:"3"`
	Add     []ColumnRequest       `json:"add,omitempty"`
	Update  []ColumnUpdateRequest `json:"update,omitempty"`
}

func (r EditColumnsRequest) edit() googlesheet.ColumnsEdit {
	edit := googlesheet.ColumnsEdit{Add: inputs(r.Add)}
	for _, u := range r.Update {
		edit.Update = append(edit.Update, googlesheet.ColumnUpdate{
			Key: u.Key, Header: u.Header, Instruction: u.Instruction, Required: u.Required, Archived: u.Archived, Options: u.Options,
		})
	}
	return edit
}

type ReactivateRequest struct {
	RepairHeader bool `json:"repairHeader" example:"true"`
}

type AddRowRequest struct {
	Values map[string]any `json:"values" swaggertype:"object"`
}

type RowResponse struct {
	ID        string            `json:"id"`
	Values    googlesheet.Cells `json:"values"`
	Status    string            `json:"status"`
	RowNumber int               `json:"rowNumber,omitempty"`
	Origin    string            `json:"origin"`
	CreatedAt time.Time         `json:"createdAt"`
	WrittenAt *time.Time        `json:"writtenAt,omitempty"`
	ErrorCode string            `json:"errorCode,omitempty"`
}

type ColumnResponse struct {
	Key         string   `json:"key" example:"nome_do_cliente"`
	Header      string   `json:"header" example:"Nome do cliente"`
	Type        string   `json:"type" example:"text"`
	Options     []string `json:"options,omitempty"`
	Required    bool     `json:"required"`
	Instruction string   `json:"instruction,omitempty"`
	Source      string   `json:"source" example:"input"`
	Archived    bool     `json:"archived"`
}

type SheetResponse struct {
	ID            string           `json:"id"`
	Title         string           `json:"title"`
	TabName       string           `json:"tabName"`
	Description   string           `json:"description,omitempty"`
	TimeZone      string           `json:"timeZone"`
	URL           string           `json:"url" example:"https://docs.google.com/spreadsheets/d/abc/edit#gid=0"`
	Status        string           `json:"status" example:"active" enums:"active,needs_attention"`
	Reason        string           `json:"reason,omitempty" example:"header_mismatch"`
	ReasonDetail  string           `json:"reasonDetail,omitempty"`
	Version       int              `json:"version"`
	Columns       []ColumnResponse `json:"columns"`
	CreatedAt     time.Time        `json:"createdAt"`
	UpdatedAt     time.Time        `json:"updatedAt"`
	LastWrittenAt *time.Time       `json:"lastWrittenAt,omitempty"`
}

func toSheetResponse(s *googlesheet.Sheet) SheetResponse {
	columns := make([]ColumnResponse, len(s.Columns))
	for i, c := range s.Columns {
		columns[i] = ColumnResponse{
			Key: c.Key, Header: c.Header, Type: string(c.Type), Options: c.Options, Required: c.Required,
			Instruction: c.Instruction, Source: string(c.Source), Archived: c.Archived,
		}
	}
	return SheetResponse{
		ID: s.ID, Title: s.Title, TabName: s.TabName, Description: s.Description, TimeZone: s.TimeZone, URL: s.URL(),
		Status: string(s.Status), Reason: string(s.Reason), ReasonDetail: s.ReasonDetail, Version: s.Version,
		Columns: columns, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt, LastWrittenAt: s.LastWrittenAt,
	}
}

type SheetEnvelope struct {
	Sheet SheetResponse `json:"sheet"`
}

type SheetListResponse struct {
	Items      []SheetResponse `json:"items"`
	Page       int             `json:"page"`
	PageSize   int             `json:"pageSize"`
	TotalItems int64           `json:"totalItems"`
	TotalPages int             `json:"totalPages"`
}

func toListResponse(page *shared.PaginatedResult[*googlesheet.Sheet]) SheetListResponse {
	items := make([]SheetResponse, len(page.Items))
	for i, s := range page.Items {
		items[i] = toSheetResponse(s)
	}
	return SheetListResponse{Items: items, Page: page.Page, PageSize: page.PageSize, TotalItems: page.TotalItems, TotalPages: page.TotalPages}
}

type ProblemRowResponse struct {
	ID          string    `json:"id"`
	Status      string    `json:"status" enums:"held,failed"`
	ErrorCode   string    `json:"errorCode" example:"google_sheet_write_access_lost"`
	ErrorDetail string    `json:"errorDetail,omitempty"`
	Attempts    int       `json:"attempts"`
	Origin      string    `json:"origin" enums:"agent,workflow,copilot,manual"`
	CreatedAt   time.Time `json:"createdAt"`
}

type HealthResponse struct {
	Sheet           SheetResponse        `json:"sheet"`
	Pending         int                  `json:"pending"`
	InFlight        int                  `json:"inFlight"`
	Held            int                  `json:"held"`
	Failed          int                  `json:"failed"`
	Written         int                  `json:"written"`
	WrittenCapped   bool                 `json:"writtenCapped"`
	OldestPendingAt *time.Time           `json:"oldestPendingAt,omitempty"`
	Problems        []ProblemRowResponse `json:"problems"`
}

func toHealthResponse(view *gsuc.HealthView) HealthResponse {
	h := view.Health
	problems := make([]ProblemRowResponse, len(h.Problems))
	for i, row := range h.Problems {
		problems[i] = ProblemRowResponse{
			ID: row.ID, Status: string(row.Status), ErrorCode: row.ErrorCode, ErrorDetail: row.ErrorDetail,
			Attempts: row.Attempts, Origin: string(row.Origin.Kind), CreatedAt: row.CreatedAt,
		}
	}
	return HealthResponse{
		Sheet: toSheetResponse(view.Sheet), Pending: h.Pending, InFlight: h.InFlight, Held: h.Held, Failed: h.Failed,
		Written: h.Written, WrittenCapped: h.Written >= googlesheet.HealthCountCap, OldestPendingAt: h.OldestPendingAt, Problems: problems,
	}
}

type RetryResponse struct {
	Requeued int `json:"requeued"`
}

type AddRowResponse struct {
	RowID     string `json:"rowId"`
	Duplicate bool   `json:"duplicate"`
}

type DeletedResponse struct {
	Deleted bool `json:"deleted" example:"true"`
}

type SourceResponse struct {
	Source string `json:"source" example:"lead_phone"`
	Type   string `json:"type" example:"phone"`
}

type CatalogResponse struct {
	Types             []string         `json:"types"`
	Sources           []SourceResponse `json:"sources"`
	DefaultTimeZone   string           `json:"defaultTimeZone"`
	MaxHeader         int              `json:"maxHeader"`
	MaxInstruction    int              `json:"maxInstruction"`
	MaxOption         int              `json:"maxOption"`
	RowErrorCodes     []string         `json:"rowErrorCodes"`
	AttentionReasons  []string         `json:"attentionReasons"`
	WrittenRetainDays int              `json:"writtenRetainDays"`
}

func catalog() CatalogResponse {
	out := CatalogResponse{
		DefaultTimeZone: googlesheet.DefaultTimeZone, MaxHeader: googlesheet.MaxHeaderRunes,
		MaxInstruction: googlesheet.MaxInstructionRunes, MaxOption: googlesheet.MaxOptionRunes,
		RowErrorCodes:     googlesheet.RowErrorCodes(),
		WrittenRetainDays: int(gsuc.WrittenRowRetention.Hours() / 24),
	}
	for _, t := range googlesheet.ColumnTypes() {
		out.Types = append(out.Types, string(t))
	}
	for _, s := range googlesheet.SystemSources() {
		t, _ := s.ColumnType()
		out.Sources = append(out.Sources, SourceResponse{Source: string(s), Type: string(t)})
	}
	for _, r := range googlesheet.AttentionReasons() {
		out.AttentionReasons = append(out.AttentionReasons, string(r))
	}
	return out
}
