package crmtools

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/google/uuid"

	"vozko/domain/copilot"
	"vozko/domain/googleaccount"
	"vozko/domain/googlesheet"
	"vozko/domain/shared"
	"vozko/domain/tools"
	"vozko/domain/workspace"
	"vozko/usecases/copilot/copilottools"
	gsuc "vozko/usecases/googlesheet"
)

type GoogleSheets interface {
	List(ctx context.Context, in googlesheet.ListInput) (*shared.PaginatedResult[*googlesheet.Sheet], error)
	Create(ctx context.Context, in gsuc.CreateInput) (*googlesheet.Sheet, error)
}

type GoogleSheetRows interface {
	Enqueue(ctx context.Context, in gsuc.EnqueueInput) (*gsuc.EnqueueResult, error)
	Preview(ctx context.Context, workspaceID, sheetID string, values map[string]any, origin googlesheet.Origin) (*googlesheet.Sheet, googlesheet.Cells, error)
}

const googleSheetListLimit = 50

type listGoogleSheetsArgs struct {
	Search string `json:"search" desc:"parte do título da planilha"`
}

type listGoogleSheetsTool struct{ sheets GoogleSheets }

func NewListGoogleSheetsTool(sheets GoogleSheets) copilot.Tool {
	return &listGoogleSheetsTool{sheets: sheets}
}

func (t *listGoogleSheetsTool) Meta() copilot.Meta {
	return copilot.Meta{Resource: workspace.ResourceGoogleSheets, Action: workspace.ActionRead}
}

func (t *listGoogleSheetsTool) Definition() tools.Definition {
	return copilottools.Definition("list_google_sheets",
		"Lista as planilhas do Google Planilhas do workspace com as colunas de cada uma (chave, cabeçalho, tipo, opções, obrigatória e quem preenche) e o estado de gravação.",
		listGoogleSheetsArgs{})
}

func (t *listGoogleSheetsTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a listGoogleSheetsArgs
	if err := copilottools.DecodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	page, err := t.sheets.List(ctx, googlesheet.ListInput{
		WorkspaceID: cc.WorkspaceID, Search: strings.TrimSpace(a.Search), Pagination: shared.Pagination{Page: 1, PageSize: googleSheetListLimit},
	})
	if err != nil {
		return sheetToolFailure("list_google_sheets", err)
	}
	sheets := make([]map[string]interface{}, 0, len(page.Items))
	for _, s := range page.Items {
		columns := make([]map[string]interface{}, 0, len(s.Columns))
		for _, c := range s.Columns {
			if c.Archived {
				continue
			}
			columns = append(columns, map[string]interface{}{
				"key": c.Key, "header": c.Header, "type": c.Type, "options": c.Options, "required": c.Required, "filled_by": c.Source,
			})
		}
		sheets = append(sheets, map[string]interface{}{
			"id": s.ID, "title": s.Title, "status": s.Status, "reason": s.Reason, "url": s.URL(), "columns": columns,
		})
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"sheets": sheets, "total": page.TotalItems}}
}

type googleSheetColumnArgs struct {
	Header      string   `json:"header" req:"true" desc:"cabeçalho da coluna"`
	Type        string   `json:"type" req:"true" enum:"text,long_text,number,money,date,datetime,boolean,choice,email,phone,url" desc:"tipo dos valores"`
	Options     []string `json:"options" desc:"opções de uma coluna choice"`
	Required    bool     `json:"required" desc:"true quando toda linha precisa deste valor"`
	Instruction string   `json:"instruction" desc:"o que agentes de IA devem escrever nesta coluna"`
	Source      string   `json:"source" enum:"input,written_at,lead_name,lead_phone,lead_email,conversation_link,origin" desc:"input quando agentes e fluxos preenchem; os demais são preenchidos pelo sistema"`
}

type createGoogleSheetArgs struct {
	Title       string                  `json:"title" req:"true" desc:"título da planilha"`
	Description string                  `json:"description" desc:"para que a planilha serve; os agentes leem isto"`
	Columns     []googleSheetColumnArgs `json:"columns" req:"true" desc:"colunas na ordem da planilha"`
}

func (a createGoogleSheetArgs) inputs() []googlesheet.ColumnInput {
	out := make([]googlesheet.ColumnInput, len(a.Columns))
	for i, c := range a.Columns {
		out[i] = googlesheet.ColumnInput{
			Header: c.Header, Type: googlesheet.ColumnType(c.Type), Options: c.Options, Required: c.Required,
			Instruction: c.Instruction, Source: googlesheet.Source(c.Source),
		}
	}
	return out
}

type createGoogleSheetTool struct{ sheets GoogleSheets }

func NewCreateGoogleSheetTool(sheets GoogleSheets) copilot.Tool {
	return &createGoogleSheetTool{sheets: sheets}
}

func (t *createGoogleSheetTool) Meta() copilot.Meta {
	return copilot.Meta{Mutating: true, Resource: workspace.ResourceGoogleSheets, Action: workspace.ActionCreate}
}

func (t *createGoogleSheetTool) Definition() tools.Definition {
	return copilottools.Definition("create_google_sheet",
		"Cria uma planilha no Google Drive da conta conectada ao Google Planilhas, com as colunas pedidas, pronta para agentes e fluxos registrarem linhas. Só depois da aprovação do usuário.",
		createGoogleSheetArgs{})
}

func (t *createGoogleSheetTool) Describe(_ context.Context, _ copilot.Context, args map[string]interface{}) []copilot.Field {
	var a createGoogleSheetArgs
	copilottools.BindArgs(args, &a)
	headers := make([]string, len(a.Columns))
	for i, c := range a.Columns {
		headers[i] = strings.TrimSpace(c.Header) + " (" + c.Type + ")"
	}
	return []copilot.Field{
		{Key: "title", Value: strings.TrimSpace(a.Title)},
		{Key: "columns", Value: strings.Join(headers, ", ")},
	}
}

func (t *createGoogleSheetTool) Validate(_ context.Context, cc copilot.Context, args map[string]interface{}) error {
	a, err := copilottools.ValidateArgs[createGoogleSheetArgs](nil, cc, args)
	if err != nil {
		return err
	}
	return googlesheet.CheckDefinition(googlesheet.NewSheetInput{Title: a.Title, Description: a.Description, Columns: a.inputs()})
}

func (t *createGoogleSheetTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a createGoogleSheetArgs
	if err := copilottools.DecodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	sheet, err := t.sheets.Create(ctx, gsuc.CreateInput{
		WorkspaceID: cc.WorkspaceID, UserID: cc.UserID, Title: a.Title, Description: a.Description, Columns: a.inputs(),
	})
	if err != nil {
		return sheetToolFailure("create_google_sheet", err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"created": true, "id": sheet.ID, "title": sheet.Title, "url": sheet.URL()}}
}

type googleSheetCellArgs struct {
	Column string `json:"column" req:"true" desc:"chave da coluna, como list_google_sheets mostra"`
	Value  string `json:"value" req:"true" desc:"valor da célula"`
}

type addGoogleSheetRowArgs struct {
	SheetID string                `json:"sheet_id" req:"true" desc:"ID da planilha"`
	Values  []googleSheetCellArgs `json:"values" req:"true" desc:"valores por coluna; colunas preenchidas pelo sistema ficam de fora"`
}

func (a addGoogleSheetRowArgs) values() map[string]any {
	out := make(map[string]any, len(a.Values))
	for _, cell := range a.Values {
		out[strings.TrimSpace(cell.Column)] = cell.Value
	}
	return out
}

type addGoogleSheetRowTool struct{ rows GoogleSheetRows }

func NewAddGoogleSheetRowTool(rows GoogleSheetRows) copilot.Tool {
	return &addGoogleSheetRowTool{rows: rows}
}

func (t *addGoogleSheetRowTool) Meta() copilot.Meta {
	return copilot.Meta{Mutating: true, Resource: workspace.ResourceGoogleSheets, Action: workspace.ActionUpdate}
}

func (t *addGoogleSheetRowTool) Definition() tools.Definition {
	return copilottools.Definition("add_google_sheet_row",
		"Adiciona uma linha a uma planilha do Google Planilhas do workspace. Use as chaves das colunas de list_google_sheets. Só depois da aprovação do usuário.",
		addGoogleSheetRowArgs{})
}

func copilotOrigin(cc copilot.Context) googlesheet.Origin {
	return googlesheet.Origin{Kind: googlesheet.OriginCopilot, ActorID: cc.UserID}
}

func (t *addGoogleSheetRowTool) Describe(_ context.Context, _ copilot.Context, args map[string]interface{}) []copilot.Field {
	var a addGoogleSheetRowArgs
	copilottools.BindArgs(args, &a)
	fields := []copilot.Field{{Key: "sheet_id", Value: a.SheetID}}
	for _, cell := range a.Values {
		fields = append(fields, copilot.Field{Key: strings.TrimSpace(cell.Column), Value: cell.Value})
	}
	return fields
}

func (t *addGoogleSheetRowTool) Validate(ctx context.Context, cc copilot.Context, args map[string]interface{}) error {
	a, err := copilottools.ValidateArgs[addGoogleSheetRowArgs](nil, cc, args)
	if err != nil {
		return err
	}
	_, _, err = t.rows.Preview(ctx, cc.WorkspaceID, strings.TrimSpace(a.SheetID), a.values(), copilotOrigin(cc))
	return err
}

func (t *addGoogleSheetRowTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a addGoogleSheetRowArgs
	if err := copilottools.DecodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	key := cc.ProposalID
	if key == "" {
		key = uuid.NewString()
	}
	result, err := t.rows.Enqueue(ctx, gsuc.EnqueueInput{
		WorkspaceID: cc.WorkspaceID, SheetID: strings.TrimSpace(a.SheetID), Values: a.values(),
		IdempotencyKey: googlesheet.KeyForCopilot(key), Origin: copilotOrigin(cc),
	})
	if err != nil {
		return sheetToolFailure("add_google_sheet_row", err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"queued": true, "row_id": result.RowID, "sheet": result.Sheet.Title, "repeated": result.Duplicate}}
}

func sheetToolFailure(tool string, err error) copilot.Result {
	var valueErr *googlesheet.ValueError
	switch {
	case errors.As(err, &valueErr):
		return copilot.Result{Status: copilot.StatusError, Message: fmt.Sprintf("coluna %q recusada (%s): %s", valueErr.Header, googlesheet.ErrorCode(err), valueErr.Detail)}
	case errors.Is(err, googleaccount.ErrNotConnected):
		return copilot.Result{Status: copilot.StatusError, Message: "o Google Planilhas não está conectado; conecte em Integrações › Google Planilhas"}
	case googlesheet.ErrorCode(err) != googlesheet.CodeInternal:
		return copilot.Result{Status: copilot.StatusError, Message: googlesheet.ErrorCode(err) + ": " + err.Error()}
	}
	log.Printf("[copilot] %s failed: %v", tool, err)
	return copilot.Result{Status: copilot.StatusError, Message: "falha ao concluir a operação com a planilha"}
}
