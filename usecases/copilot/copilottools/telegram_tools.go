package copilottools

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"vozko/domain/copilot"
	"vozko/domain/shared"
	tgdomain "vozko/domain/telegram"
	"vozko/domain/tools"
	"vozko/domain/workspace"
	tguc "vozko/usecases/telegram"
)

const (
	botTokenKey     = "bot_token"
	telegramBotPage = 50
)

type telegramConnector interface {
	Execute(ctx context.Context, in tguc.ConnectInput) (*tgdomain.Account, error)
}

type telegramLister interface {
	Execute(ctx context.Context, in tgdomain.ListAccountsInput) (*shared.PaginatedResult[*tgdomain.Account], error)
}

type TelegramDeps struct {
	Connect     telegramConnector
	List        telegramLister
	Departments departmentLookup
}

func telegramMeta(action workspace.Action, mutating bool) copilot.Meta {
	return copilot.Meta{Mutating: mutating, Resource: workspace.ResourceTelegramAccounts, Action: action}
}

var telegramStatuses = map[tgdomain.Status]string{
	tgdomain.StatusActive:         "conectado e recebendo mensagens",
	tgdomain.StatusPending:        "conectando",
	tgdomain.StatusWebhookFailing: "o Telegram não está entregando as mensagens",
}

func telegramStatus(status tgdomain.Status) string {
	if label, ok := telegramStatuses[status]; ok {
		return label
	}
	return "precisa de atenção na tela do Telegram"
}

type listTelegramBotsTool struct{ deps TelegramDeps }

func NewListTelegramBotsTool(deps TelegramDeps) copilot.Tool {
	return &listTelegramBotsTool{deps: deps}
}

func (t *listTelegramBotsTool) Meta() copilot.Meta { return telegramMeta(workspace.ActionRead, false) }

func (t *listTelegramBotsTool) Definition() tools.Definition {
	return definition("list_telegram_bots", "Lista os bots do Telegram conectados ao workspace e se estão recebendo mensagens.", struct{}{})
}

func (t *listTelegramBotsTool) Execute(ctx context.Context, cc copilot.Context, _ map[string]interface{}) copilot.Result {
	page, err := t.deps.List.Execute(ctx, tgdomain.ListAccountsInput{
		WorkspaceID: cc.WorkspaceID,
		Options:     shared.QueryOptions{Pagination: shared.Pagination{Page: 1, PageSize: telegramBotPage}},
	})
	if err != nil {
		log.Printf("[copilot] list_telegram_bots failed: %v", err)
		return copilot.Result{Status: copilot.StatusError, Message: "não foi possível ler os bots do Telegram agora"}
	}
	bots := make([]map[string]interface{}, 0, len(page.Items))
	for _, account := range page.Items {
		bots = append(bots, map[string]interface{}{
			"bot":    "@" + account.BotUsername,
			"name":   account.BotName,
			"status": telegramStatus(account.Status),
		})
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"bots": bots, "total": page.TotalItems}}
}

type connectTelegramBotArgs struct {
	DepartmentID string `json:"department_id" id:"true" desc:"id de list_departments para o bot atender um departamento; omita para o workspace todo"`
}

type connectTelegramBotTool struct{ deps TelegramDeps }

func NewConnectTelegramBotTool(deps TelegramDeps) copilot.Tool {
	return &connectTelegramBotTool{deps: deps}
}

func (t *connectTelegramBotTool) Meta() copilot.Meta {
	return telegramMeta(workspace.ActionCreate, true)
}

func (t *connectTelegramBotTool) Definition() tools.Definition {
	return definition("connect_telegram_bot",
		"Conecta um bot do Telegram criado no BotFather para receber e responder mensagens no CRM. O token do bot NUNCA passa "+
			"por você: o usuário digita no cartão de aprovação, num campo protegido. Nunca peça o token no chat; se ele escrever o "+
			"token na conversa, avise que deve ir só no cartão e sugira gerar outro no BotFather. Só depois da aprovação do usuário.",
		connectTelegramBotArgs{})
}

func (t *connectTelegramBotTool) Secrets(map[string]interface{}) []copilot.SecretField {
	return []copilot.SecretField{{Key: botTokenKey, Label: "Token do bot"}}
}

func (t *connectTelegramBotTool) Validate(_ context.Context, cc copilot.Context, args map[string]interface{}) error {
	a, err := validateArgs[connectTelegramBotArgs](nil, cc, args)
	if err != nil {
		return err
	}
	department := strings.TrimSpace(a.DepartmentID)
	if department == "" {
		return nil
	}
	if !cc.Departments.Allows(department) {
		return fmt.Errorf("%w: departamento fora do seu alcance; use um id de list_departments", errInvalidArgs)
	}
	if _, err := t.deps.Departments.Get(cc.WorkspaceID, department); err != nil {
		return fmt.Errorf("%w: departamento desconhecido; use o id exato de list_departments", errInvalidArgs)
	}
	return nil
}

func (t *connectTelegramBotTool) Describe(_ context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	var a connectTelegramBotArgs
	bindArgs(args, &a)
	scope := "todo o workspace"
	if department := strings.TrimSpace(a.DepartmentID); department != "" {
		if d, err := t.deps.Departments.Get(cc.WorkspaceID, department); err == nil && d != nil {
			scope = d.Name
		}
	}
	return []copilot.Field{{Key: "department", Value: scope}}
}

func (t *connectTelegramBotTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a connectTelegramBotArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	token, _ := args[botTokenKey].(string)
	if strings.TrimSpace(token) == "" {
		return copilot.Result{Status: copilot.StatusError, Message: "o token não foi preenchido; nada mudou"}
	}
	var department *string
	if id := strings.TrimSpace(a.DepartmentID); id != "" {
		department = &id
	}
	account, err := t.deps.Connect.Execute(ctx, tguc.ConnectInput{WorkspaceID: cc.WorkspaceID, DepartmentID: department, BotToken: token})
	if err != nil {
		return telegramConnectFailure(err, token)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{
		"bot":    "@" + account.BotUsername,
		"name":   account.BotName,
		"status": telegramStatus(account.Status),
	}}
}

func telegramConnectFailure(err error, token string) copilot.Result {
	switch {
	case errors.Is(err, tgdomain.ErrBotTokenInvalid), errors.Is(err, tgdomain.ErrBotTokenRequired):
		return copilot.Result{Status: copilot.StatusError, Message: "o Telegram recusou o token; peça ao usuário para conferir no BotFather e propor de novo"}
	case errors.Is(err, tgdomain.ErrAccountAlreadyLinked):
		return copilot.Result{Status: copilot.StatusError, Message: "esse bot já está conectado a outro workspace"}
	}
	log.Printf("[copilot] connect_telegram_bot failed: %s", strings.ReplaceAll(err.Error(), token, "[token]"))
	return copilot.Result{Status: copilot.StatusError, Message: "não foi possível conectar o bot agora; ele pode ter sido salvo sem receber mensagens, confira com list_telegram_bots"}
}
