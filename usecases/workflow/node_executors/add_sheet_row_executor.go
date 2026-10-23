package node_executors

import (
	"context"
	"errors"
	"log"
	"sort"
	"strings"

	"vozko/domain/googleaccount"
	"vozko/domain/googlesheet"
	"vozko/domain/workflow"
	gsuc "vozko/usecases/googlesheet"
)

const (
	AddSheetRowSheetKey  = "sheet_id"
	AddSheetRowValuesKey = "values"
	addSheetRowWritten   = "sucesso"
	addSheetRowRefused   = "erro"
)

type SheetRowSink interface {
	Enqueue(ctx context.Context, in gsuc.EnqueueInput) (*gsuc.EnqueueResult, error)
}

type addSheetRowExecutor struct {
	rows SheetRowSink
}

func NewAddSheetRowExecutor(rows SheetRowSink) workflow.NodeExecutor {
	return &addSheetRowExecutor{rows: rows}
}

func (e *addSheetRowExecutor) Definition() workflow.NodeDefinition {
	return workflow.NodeDefinition{
		Type:         workflow.NodeTypeActionAddSheetRow,
		TestBehavior: workflow.NodeTestExternalCall,
		Category:     workflow.NodeCategoryAction,
		Scopes:       []workflow.NodeScope{workflow.NodeScopeShared, workflow.NodeScopeVoice},
		Label:        "Adicionar linha na planilha",
		Description:  "Registra uma linha em uma planilha do Google Planilhas criada no painel, com as colunas preenchidas por variáveis do fluxo.",
		Icon:         "Table",
		Guidance: workflow.NodeGuidance{
			When: "Para registrar pedidos, leads, agendamentos ou qualquer dado do fluxo em uma planilha do Google Planilhas.",
			Behavior: "A linha é validada pelos tipos das colunas e entra na fila de gravação; a saída 'sucesso' significa que ela foi aceita e será gravada em instantes, na ordem. " +
				"Colunas preenchidas pelo sistema (telefone do lead, data, link da conversa) não entram no mapeamento. Uma variável sem valor leva à saída 'erro', nunca a uma célula com {{...}}.",
			Examples: []string{
				"config: {\"sheet_id\":\"<id>\",\"values\":{\"produto\":\"{{var.produto}}\",\"quantidade\":\"{{var.quantidade}}\"}}",
			},
		},
		Outputs: []workflow.HandleDefinition{
			{ID: addSheetRowWritten, Label: "Sucesso"},
			{ID: addSheetRowRefused, Label: "Erro", Optional: true},
		},
		OutputKeys: []workflow.OutputKeyDefinition{
			{Key: "row_id", Description: "ID da linha na fila de gravação", Label: "ID da linha", Type: workflow.VariableTypeText},
			{Key: "sheet_id", Description: "ID da planilha", Label: "ID da planilha", Type: workflow.VariableTypeText},
			{Key: "sheet_title", Description: "Título da planilha", Label: "Título da planilha", Type: workflow.VariableTypeText},
			{Key: "sheet_url", Description: "Link da planilha no Google", Label: "Link da planilha", Type: workflow.VariableTypeText},
			{Key: "duplicate", Description: "true quando este passo já tinha registrado a linha e ela não foi repetida", Label: "Linha já registrada", Type: workflow.VariableTypeBoolean},
			{Key: "error", Description: "Descrição da recusa", Label: "Recusa", Type: workflow.VariableTypeText},
			{Key: "error_code", Description: "Código da recusa", Label: "Código da recusa", Type: workflow.VariableTypeText},
		},
		DefaultConfig: map[string]interface{}{AddSheetRowSheetKey: "", AddSheetRowValuesKey: map[string]interface{}{}},
		ConfigSchema: []workflow.ConfigField{
			{Key: AddSheetRowSheetKey, Label: "Planilha", Type: "select", OptionsSource: "google_sheets", Required: true},
			{Key: AddSheetRowValuesKey, Label: "Colunas", Type: "sheet_columns", Required: true,
				Description: "O valor de cada coluna. Aceita variáveis do fluxo {{...}}."},
		},
	}
}

func (e *addSheetRowExecutor) Execute(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
	if ctx == nil || ctx.Run == nil || ctx.Node == nil {
		return nil, workflow.ErrNodeConfigMissing
	}
	if e.rows == nil {
		return sheetRowRefused(ctx, googlesheet.CodeInternal, "integração Google Planilhas indisponível no servidor"), nil
	}
	values, missing := e.values(ctx)
	if len(missing) > 0 {
		return sheetRowRefused(ctx, googlesheet.CodeUnresolvedVariable, "variáveis sem valor: "+strings.Join(missing, ", ")), nil
	}
	sheetID, _ := ctx.Node.Config[AddSheetRowSheetKey].(string)
	origin := googlesheet.Origin{
		Kind: googlesheet.OriginWorkflow, WorkflowID: ctx.Run.WorkflowID, RunID: ctx.Run.ID,
		EntryID: ctx.Run.EntryID, EntryType: ctx.Run.EntryType,
	}
	if ctx.Run.OnLead() {
		origin.LeadID = ctx.Run.EntryID
	}
	callCtx, release := ctx.CallContext()
	defer release()
	result, err := e.rows.Enqueue(callCtx, gsuc.EnqueueInput{
		WorkspaceID:    ctx.Run.WorkspaceID,
		SheetID:        strings.TrimSpace(sheetID),
		Values:         values,
		IdempotencyKey: googlesheet.KeyForNode(ctx.Run.ID, ctx.Run.NodeVisit()),
		Origin:         origin,
	})
	if err != nil {
		code := sheetRowCode(err)
		if code == googlesheet.CodeInternal && canRetryNode(ctx) {
			log.Printf("[workflow][node:%s][run:%s] add_sheet_row will retry with the same key: %v", ctx.Node.ID, ctx.Run.ID, err)
			return nil, err
		}
		return sheetRowRefused(ctx, code, err.Error()), nil
	}
	output := map[string]interface{}{
		"row_id":      result.RowID,
		"sheet_id":    result.Sheet.ID,
		"sheet_title": result.Sheet.Title,
		"sheet_url":   result.Sheet.URL(),
		"duplicate":   result.Duplicate,
	}
	return routeOutput(&workflow.NodeResult{Output: output}, outgoingEdges(ctx), addSheetRowWritten), nil
}

func (e *addSheetRowExecutor) values(ctx *workflow.NodeContext) (map[string]any, []string) {
	raw, _ := ctx.Node.Config[AddSheetRowValuesKey].(map[string]interface{})
	values := make(map[string]any, len(raw))
	var missing []string
	for key, value := range raw {
		text, ok := value.(string)
		if !ok {
			text = workflow.TemplateText(value)
		}
		resolved, unresolved := workflow.InterpolateStrict(text, ctx.State, nil)
		missing = append(missing, unresolved...)
		values[key] = strings.TrimSpace(resolved)
	}
	sort.Strings(missing)
	return values, missing
}

func sheetRowCode(err error) string {
	if errors.Is(err, googleaccount.ErrNotConnected) {
		return googleaccount.ErrorCode(err)
	}
	return googlesheet.ErrorCode(err)
}

func sheetRowRefused(ctx *workflow.NodeContext, code, message string) *workflow.NodeResult {
	output := map[string]interface{}{"error": message, "error_code": code}
	return routeOutput(&workflow.NodeResult{Output: output}, outgoingEdges(ctx), addSheetRowRefused)
}
