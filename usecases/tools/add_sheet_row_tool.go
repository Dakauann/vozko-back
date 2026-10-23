package tools_usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"vozko/domain/googleaccount"
	"vozko/domain/googlesheet"
	"vozko/domain/tools"
	gsuc "vozko/usecases/googlesheet"
)

const (
	AddSheetRowToolName       = "add_sheet_row"
	GoogleSheetsOptionsSource = "google_sheets"
	addSheetRowConfigKey      = "sheet_id"
	addSheetRowWhenKey        = "when_to_use"
)

const (
	addSheetRowUnavailable = "Esta planilha está indisponível. Não chame esta ferramenta."
	addSheetRowNoChat      = "Não foi possível identificar a conversa atual. Esta ferramenta só funciona durante um atendimento."
	addSheetRowNotActive   = "A planilha está pausada até alguém revisá-la no painel. Não prometa o registro; diga que a equipe vai verificar."
	addSheetRowNoGoogle    = "O Google Planilhas não está conectado. Não prometa o registro; diga que a equipe vai verificar."
	addSheetRowFailed      = "Não foi possível registrar agora. Não prometa o registro; diga que a equipe vai verificar."
)

var columnParamTypes = map[googlesheet.ColumnType]string{
	googlesheet.TypeText:     "string",
	googlesheet.TypeLongText: "string",
	googlesheet.TypeNumber:   "number",
	googlesheet.TypeMoney:    "number",
	googlesheet.TypeDate:     "date",
	googlesheet.TypeDateTime: "datetime",
	googlesheet.TypeBoolean:  "boolean",
	googlesheet.TypeChoice:   "string",
	googlesheet.TypeEmail:    "email",
	googlesheet.TypePhone:    "phone",
	googlesheet.TypeURL:      "string",
}

type SheetRows interface {
	Get(ctx context.Context, workspaceID, id string) (*googlesheet.Sheet, error)
	Enqueue(ctx context.Context, in gsuc.EnqueueInput) (*gsuc.EnqueueResult, error)
	Preview(ctx context.Context, workspaceID, sheetID string, values map[string]any, origin googlesheet.Origin) (*googlesheet.Sheet, googlesheet.Cells, error)
}

type addSheetRowTool struct {
	sheets SheetRows
}

func NewAddSheetRowTool(sheets SheetRows) tools.Handler {
	if sheets == nil {
		return nil
	}
	return &addSheetRowTool{sheets: sheets}
}

func (t *addSheetRowTool) Definition() tools.Definition {
	return tools.Definition{
		Name:               AddSheetRowToolName,
		DisplayName:        "Registrar em planilha",
		DisplayDescription: "Adiciona uma linha a uma planilha do Google Planilhas criada no painel. Cada planilha escolhida vira uma ferramenta própria do agente, com as colunas da planilha como campos.",
		Description:        "Registra uma linha em uma planilha do Google Planilhas.",
		ConfigSchema: map[string]tools.ConfigParameter{
			addSheetRowConfigKey: {
				Type:               "string",
				Description:        "Planilha do Google Planilhas criada no painel.",
				DisplayName:        "Planilha",
				DisplayDescription: "Planilha em que o agente registra as linhas.",
				OptionsSource:      GoogleSheetsOptionsSource,
				Required:           true,
			},
			addSheetRowWhenKey: {
				Type:               "string",
				Description:        "Quando o agente deve registrar uma linha nesta planilha.",
				DisplayName:        "Quando registrar",
				DisplayDescription: "Ex.: quando o cliente confirmar o pedido. Sem isto, vale a descrição da planilha.",
			},
		},
		RequiredConfig: []string{addSheetRowConfigKey},
		RequiresConfig: true,
		BindingKey:     addSheetRowConfigKey,
		Visibility:     []tools.ToolVisibility{tools.VisibilityMessaging},
		Category:       tools.CategoryAgentAction,
	}
}

func (t *addSheetRowTool) DefinitionWithContext(ctx tools.ToolContext) tools.Definition {
	def := t.Definition()
	sheet, err := t.sheets.Get(context.Background(), ctx.WorkspaceID, configString(ctx.Config, addSheetRowConfigKey))
	if err != nil || sheet.Writable() != nil {
		def.Description = addSheetRowUnavailable
		return def
	}
	when := configString(ctx.Config, addSheetRowWhenKey)
	if when == "" {
		when = sheet.Description
	}
	def.Description = fmt.Sprintf("Registra uma linha na planilha %q.", sheet.Title)
	if when != "" {
		def.Description += " Quando usar: " + when
	}
	def.Description += "\n\nPreencha só com o que o cliente informou ou que está claro na conversa; nunca invente valores. Campos que o sistema preenche (telefone, data, link) não aparecem aqui."
	def.Parameters = map[string]tools.Parameter{}
	def.Required = nil
	for _, col := range sheet.InputColumns() {
		param := tools.Parameter{Type: columnParamTypes[col.Type], Description: col.Header}
		if col.Instruction != "" {
			param.Description += ". " + col.Instruction
		}
		if col.Type == googlesheet.TypeChoice {
			param.Enum = col.Options
		}
		def.Parameters[col.Key] = param
		if col.Required {
			def.Required = append(def.Required, col.Key)
		}
	}
	return def
}

func (t *addSheetRowTool) Execute(ctx context.Context, params map[string]interface{}) (tools.ExecutionResult, error) {
	return t.ExecuteWithConfig(ctx, nil, params)
}

func (t *addSheetRowTool) ExecuteWithConfig(ctx context.Context, config map[string]interface{}, params map[string]interface{}) (tools.ExecutionResult, error) {
	origin, ok := sheetRowOrigin(ctx, config)
	if !ok {
		return refusal(addSheetRowNoChat), nil
	}
	callID := tools.CallIDFrom(ctx)
	if callID == "" {
		callID = uuid.NewString()
	}
	result, err := t.sheets.Enqueue(ctx, gsuc.EnqueueInput{
		WorkspaceID:    configString(config, "__workspace_id"),
		SheetID:        configString(config, addSheetRowConfigKey),
		Values:         params,
		IdempotencyKey: googlesheet.KeyForTool(origin.EntryID, callID, params),
		Origin:         origin,
	})
	if err != nil {
		return sheetRowRefusal(err), nil
	}
	return tools.ExecutionResult{
		Result:            fmt.Sprintf("Linha registrada na planilha %q.", result.Sheet.Title),
		ContextUpdateText: "Registro feito na planilha " + result.Sheet.Title,
	}, nil
}

func (t *addSheetRowTool) Preview(ctx context.Context, config map[string]interface{}, params map[string]interface{}) (tools.ExecutionResult, error) {
	origin, ok := sheetRowOrigin(ctx, config)
	if !ok {
		return refusal(addSheetRowNoChat), nil
	}
	sheet, cells, err := t.sheets.Preview(ctx, configString(config, "__workspace_id"), configString(config, addSheetRowConfigKey), params, origin)
	if err != nil {
		return sheetRowRefusal(err), nil
	}
	shown, _ := json.Marshal(map[string]any{"simulacao": true, "planilha": sheet.Title, "linha": cells})
	return tools.ExecutionResult{Result: string(shown)}, nil
}

func sheetRowOrigin(ctx context.Context, config map[string]interface{}) (googlesheet.Origin, bool) {
	origin := googlesheet.Origin{
		Kind:      googlesheet.OriginAgent,
		AgentID:   agentIDOf(ctx, config),
		LeadID:    configString(config, "__lead_id"),
		EntryID:   configString(config, "__entry_id"),
		EntryType: configString(config, "__entry_type"),
	}
	ok := configString(config, "__workspace_id") != "" && origin.EntryID != "" && origin.EntryType != "" && origin.AgentID != ""
	return origin, ok
}

func sheetRowRefusal(err error) tools.ExecutionResult {
	var valueErr *googlesheet.ValueError
	text := addSheetRowFailed
	switch {
	case errors.As(err, &valueErr):
		text = fmt.Sprintf("Campo %q recusado: %s. Corrija e chame de novo; se faltar informação, pergunte ao cliente.", valueErr.Header, refusalReason(valueErr))
	case errors.Is(err, googlesheet.ErrSheetNotActive), errors.Is(err, googlesheet.ErrAccountChanged):
		text = addSheetRowNotActive
	case errors.Is(err, googlesheet.ErrSheetNotFound):
		text = addSheetRowUnavailable
	case errors.Is(err, googleaccount.ErrNotConnected):
		text = addSheetRowNoGoogle
	}
	result := refusal(text)
	result.ErrorCode = googlesheet.ErrorCode(err)
	if errors.Is(err, googleaccount.ErrNotConnected) {
		result.ErrorCode = googleaccount.ErrorCode(err)
	}
	return result
}

func refusalReason(err *googlesheet.ValueError) string {
	reasons := map[error]string{
		googlesheet.ErrUnknownColumn:         "não existe nesta planilha",
		googlesheet.ErrColumnArchived:        "não é mais usado",
		googlesheet.ErrSystemColumn:          "é preenchido pelo sistema",
		googlesheet.ErrRequiredColumnMissing: "é obrigatório",
		googlesheet.ErrValueNotInOptions:     "valor fora das opções",
		googlesheet.ErrValueTooLong:          "texto longo demais",
	}
	reason := "valor inválido"
	for sentinel, text := range reasons {
		if errors.Is(err, sentinel) {
			reason = text
		}
	}
	if err.Detail != "" {
		reason += " (" + strings.TrimSpace(err.Detail) + ")"
	}
	return reason
}

var (
	_ tools.ContextualHandler = (*addSheetRowTool)(nil)
	_ tools.Previewer         = (*addSheetRowTool)(nil)
)

func (t *addSheetRowTool) ValidateBinding(ctx context.Context, workspaceID string, config map[string]interface{}) error {
	_, err := t.sheets.Get(ctx, workspaceID, configString(config, addSheetRowConfigKey))
	return err
}

var _ tools.BindingValidator = (*addSheetRowTool)(nil)
