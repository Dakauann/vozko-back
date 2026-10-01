package copilottools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/shared"
	tgdomain "vozko/domain/telegram"
	wd "vozko/domain/workspace/workspace_department"
	tguc "vozko/usecases/telegram"
)

const botToken = "123456:AAH-secret-token"

type botConnector struct {
	got tguc.ConnectInput
	err error
}

func (b *botConnector) Execute(_ context.Context, in tguc.ConnectInput) (*tgdomain.Account, error) {
	b.got = in
	if b.err != nil {
		return nil, b.err
	}
	return &tgdomain.Account{ID: "acc-1", WorkspaceID: in.WorkspaceID, BotUsername: "loja_bot", BotName: "Loja", Status: tgdomain.StatusActive}, nil
}

type botList []*tgdomain.Account

func (b botList) Execute(_ context.Context, in tgdomain.ListAccountsInput) (*shared.PaginatedResult[*tgdomain.Account], error) {
	return &shared.PaginatedResult[*tgdomain.Account]{Items: b, TotalItems: int64(len(b))}, nil
}

var telegramAdminCtx = copilot.Context{WorkspaceID: "ws-1", UserID: "user-1", Departments: &wd.DepartmentFilter{IsOwnerOrAdmin: true}}

func telegramDeps(connector *botConnector, bots botList) TelegramDeps {
	return TelegramDeps{Connect: connector, List: bots, Departments: departmentBook{salesDeptUUID: {ID: salesDeptUUID, WorkspaceID: "ws-1", Name: "Vendas"}}}
}

func TestConnectingABotAsksForItsTokenOnTheCardOnly(t *testing.T) {
	tool := NewConnectTelegramBotTool(telegramDeps(&botConnector{}, nil))
	asker, ok := tool.(copilot.SecretAsker)
	if !ok || len(asker.Secrets(nil)) != 1 || asker.Secrets(nil)[0].Key != "bot_token" {
		t.Fatal("the bot token must be a protected field")
	}
	for name := range tool.Definition().Parameters {
		if strings.Contains(name, "token") {
			t.Fatalf("the model must not be offered %q", name)
		}
	}
}

func TestConnectingABotUsesTheTypedTokenAndNeverEchoesIt(t *testing.T) {
	connector := &botConnector{}
	res := NewConnectTelegramBotTool(telegramDeps(connector, nil)).Execute(context.Background(), telegramAdminCtx, map[string]interface{}{
		"department_id": salesDeptUUID, "bot_token": botToken,
	})
	if res.Status != copilot.StatusOK || connector.got.BotToken != botToken || connector.got.WorkspaceID != "ws-1" || *connector.got.DepartmentID != salesDeptUUID {
		t.Fatalf("res=%+v got=%+v", res, connector.got)
	}
	raw, _ := json.Marshal(res)
	if strings.Contains(string(raw), "AAH-secret-token") {
		t.Fatalf("the token reached the model: %s", raw)
	}
}

func TestATelegramFailureNeverLeaksTheTokenInsideTheError(t *testing.T) {
	leaky := fmt.Errorf(`telegram: validate token: Post "https://api.telegram.org/bot%s/getMe": dial tcp: timeout`, botToken)
	for _, err := range []error{leaky, fmt.Errorf("%w: BotFather rejected this token", tgdomain.ErrBotTokenInvalid), tgdomain.ErrAccountAlreadyLinked} {
		res := NewConnectTelegramBotTool(telegramDeps(&botConnector{err: err}, nil)).Execute(context.Background(), telegramAdminCtx, map[string]interface{}{"bot_token": botToken})
		raw, _ := json.Marshal(res)
		if res.Status == copilot.StatusOK || strings.Contains(string(raw), "AAH-secret-token") {
			t.Fatalf("err=%v res=%s", err, raw)
		}
	}
}

func TestABotCannotBeConnectedToADepartmentOutsideTheUsersReach(t *testing.T) {
	scoped := copilot.Context{WorkspaceID: "ws-1", Departments: &wd.DepartmentFilter{DepartmentIDs: []string{"other"}, WorkspaceHasDepartments: true}}
	err := NewConnectTelegramBotTool(telegramDeps(&botConnector{}, nil)).(copilot.Validator).Validate(context.Background(), scoped, map[string]interface{}{"department_id": salesDeptUUID})
	if err == nil || !errors.Is(err, errInvalidArgs) {
		t.Fatalf("err = %v", err)
	}
}

func TestListingBotsShowsTheirStateWithoutSecrets(t *testing.T) {
	bots := botList{{ID: "acc-1", BotUsername: "loja_bot", BotName: "Loja", Status: tgdomain.StatusWebhookFailing, BotToken: botToken, StatusReason: "https://api.telegram.org/bot" + botToken}}
	res := NewListTelegramBotsTool(telegramDeps(&botConnector{}, bots)).Execute(context.Background(), telegramAdminCtx, nil)
	raw, _ := json.Marshal(res.Data)
	if res.Status != copilot.StatusOK || !strings.Contains(string(raw), "loja_bot") || strings.Contains(string(raw), "AAH-secret-token") {
		t.Fatalf("res = %s", raw)
	}
}
