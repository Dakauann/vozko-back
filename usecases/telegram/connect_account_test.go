package telegram

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tgdomain "vozko/domain/telegram"
)

const webhookBase = "https://api.example.com"

func TestConnectRegistersWebhookAndActivates(t *testing.T) {
	accounts := &fakeAccounts{}
	api := &fakeBotAPI{}
	uc := NewConnectAccountUseCase(accounts, api, webhookBase)

	account, err := uc.Execute(context.Background(), ConnectInput{
		WorkspaceID: "ws-1",
		BotToken:    "77777:secret",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if account.Status != tgdomain.StatusActive {
		t.Errorf("status = %s, want ACTIVE", account.Status)
	}
	if account.BotUserID != 77777 || account.BotUsername != "vozko_bot" {
		t.Errorf("identity not taken from getMe: %+v", account)
	}

	if account.WebhookSecret == "" {
		t.Fatal("a webhook secret must be generated")
	}
	if len(api.WebhookCfgs) != 1 {
		t.Fatalf("setWebhook called %d times, want 1", len(api.WebhookCfgs))
	}
	cfg := api.WebhookCfgs[0]
	if cfg.SecretToken != account.WebhookSecret {
		t.Error("the registered secret must match the persisted one, or every delivery 401s")
	}
	if cfg.URL != tgdomain.WebhookURLFor(webhookBase, account.ID) {
		t.Errorf("registered URL = %q, want the per-account path", cfg.URL)
	}
	if len(cfg.AllowedUpdates) == 0 {
		t.Error("allowed_updates must be narrowed explicitly")
	}
}

func TestConnectValidatesTokenBeforeCreating(t *testing.T) {
	accounts := &fakeAccounts{}
	api := &fakeBotAPI{
		GetMeFn: func(context.Context, string) (*tgdomain.BotProfile, error) {
			return nil, &tgdomain.APIError{Code: 401, Description: "Unauthorized"}
		},
	}
	uc := NewConnectAccountUseCase(accounts, api, webhookBase)

	_, err := uc.Execute(context.Background(), ConnectInput{WorkspaceID: "ws-1", BotToken: "bad"})
	if !errors.Is(err, tgdomain.ErrBotTokenInvalid) {
		t.Errorf("err = %v, want ErrBotTokenInvalid", err)
	}
	if len(accounts.Created) != 0 {
		t.Error("no account row may be created for an invalid token")
	}
}

func TestConnectRequiresTokenAndWorkspace(t *testing.T) {
	uc := NewConnectAccountUseCase(&fakeAccounts{}, &fakeBotAPI{}, webhookBase)

	if _, err := uc.Execute(context.Background(), ConnectInput{WorkspaceID: "ws-1"}); !errors.Is(err, tgdomain.ErrBotTokenRequired) {
		t.Errorf("err = %v, want ErrBotTokenRequired", err)
	}
	if _, err := uc.Execute(context.Background(), ConnectInput{BotToken: "77777:s"}); !errors.Is(err, tgdomain.ErrWorkspaceIDRequired) {
		t.Errorf("err = %v, want ErrWorkspaceIDRequired", err)
	}
}

func TestConnectRefusesBotOwnedByAnotherWorkspace(t *testing.T) {
	existing := &tgdomain.Account{ID: "acct-1", WorkspaceID: "ws-OTHER", BotUserID: 77777}
	accounts := &fakeAccounts{
		FindByBotUserIDUnscopedFn: func(context.Context, int64) (*tgdomain.Account, error) {
			return existing, nil
		},
	}
	uc := NewConnectAccountUseCase(accounts, &fakeBotAPI{}, webhookBase)

	_, err := uc.Execute(context.Background(), ConnectInput{WorkspaceID: "ws-1", BotToken: "77777:s"})
	if !errors.Is(err, tgdomain.ErrAccountAlreadyLinked) {
		t.Errorf("err = %v, want ErrAccountAlreadyLinked", err)
	}
}

func TestConnectRestoresSoftDeletedAccount(t *testing.T) {
	existing := &tgdomain.Account{
		ID: "acct-1", WorkspaceID: "ws-1", BotUserID: 77777, Status: tgdomain.StatusRevoked,
	}
	accounts := &fakeAccounts{
		FindByBotUserIDUnscopedFn: func(context.Context, int64) (*tgdomain.Account, error) {
			return existing, nil
		},
	}
	uc := NewConnectAccountUseCase(accounts, &fakeBotAPI{}, webhookBase)

	account, err := uc.Execute(context.Background(), ConnectInput{WorkspaceID: "ws-1", BotToken: "77777:s"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(accounts.RestoredIDs) != 1 || accounts.RestoredIDs[0] != "acct-1" {
		t.Errorf("restored = %v, want the existing row", accounts.RestoredIDs)
	}
	if len(accounts.Created) != 0 {
		t.Error("a restore must not also create a second row")
	}
	if account.ID != "acct-1" {
		t.Errorf("account id = %q, want the restored row's", account.ID)
	}
}

func TestConnectFailsWhenTelegramCannotReachUs(t *testing.T) {
	accounts := &fakeAccounts{}
	api := &fakeBotAPI{
		GetWebhookInfoFn: func(context.Context, string) (*tgdomain.WebhookInfo, error) {
			return &tgdomain.WebhookInfo{
				URL:              "https://api.example.com/webhooks/telegram/acct-created",
				LastErrorMessage: "Connection refused",
			}, nil
		},
	}
	uc := NewConnectAccountUseCase(accounts, api, webhookBase)

	account, err := uc.Execute(context.Background(), ConnectInput{WorkspaceID: "ws-1", BotToken: "77777:s"})
	if err == nil {
		t.Fatal("expected an error when Telegram reports a delivery failure")
	}
	if account == nil {
		t.Fatal("the account must be returned so the UI can offer a retry")
	}
	if account.Status != tgdomain.StatusWebhookFailing {
		t.Errorf("status = %s, want WEBHOOK_FAILING", account.Status)
	}
}

func TestConnectDetectsWebhookURLMismatch(t *testing.T) {
	api := &fakeBotAPI{
		GetWebhookInfoFn: func(context.Context, string) (*tgdomain.WebhookInfo, error) {
			return &tgdomain.WebhookInfo{URL: "https://stale.example.com/webhooks/telegram/x"}, nil
		},
	}
	uc := NewConnectAccountUseCase(&fakeAccounts{}, api, webhookBase)

	_, err := uc.Execute(context.Background(), ConnectInput{WorkspaceID: "ws-1", BotToken: "77777:s"})
	if err == nil || !strings.Contains(err.Error(), "reports webhook") {
		t.Errorf("err = %v, want a URL-mismatch error", err)
	}
}

func TestHealthCheckMarksFailingWebhook(t *testing.T) {
	account := &tgdomain.Account{
		ID: "acct-1", BotUsername: "vozko_bot", BotToken: "t", Status: tgdomain.StatusActive,
	}
	accounts := &fakeAccounts{
		ListForHealthCheckFn: func(context.Context, time.Time, int) ([]*tgdomain.Account, error) {
			return []*tgdomain.Account{account}, nil
		},
	}
	api := &fakeBotAPI{
		GetWebhookInfoFn: func(context.Context, string) (*tgdomain.WebhookInfo, error) {
			return &tgdomain.WebhookInfo{PendingCount: 500, LastErrorMessage: "Read timeout"}, nil
		},
	}

	if err := NewCheckWebhookHealthUseCase(accounts, api).Execute(context.Background()); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(accounts.HealthWrites) != 1 || accounts.HealthWrites[0].PendingCount != 500 {
		t.Errorf("health writes = %+v, want the probe result recorded", accounts.HealthWrites)
	}
	if len(accounts.StatusWrites) != 1 || accounts.StatusWrites[0].Status != tgdomain.StatusWebhookFailing {
		t.Errorf("status writes = %+v, want WEBHOOK_FAILING", accounts.StatusWrites)
	}
}

func TestHealthCheckRecoversWebhook(t *testing.T) {
	account := &tgdomain.Account{
		ID: "acct-1", BotToken: "t", Status: tgdomain.StatusWebhookFailing, WebhookPendingCount: 500,
	}
	accounts := &fakeAccounts{
		ListForHealthCheckFn: func(context.Context, time.Time, int) ([]*tgdomain.Account, error) {
			return []*tgdomain.Account{account}, nil
		},
	}
	api := &fakeBotAPI{
		GetWebhookInfoFn: func(context.Context, string) (*tgdomain.WebhookInfo, error) {
			return &tgdomain.WebhookInfo{PendingCount: 0}, nil
		},
	}

	if err := NewCheckWebhookHealthUseCase(accounts, api).Execute(context.Background()); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(accounts.StatusWrites) != 1 || accounts.StatusWrites[0].Status != tgdomain.StatusActive {
		t.Errorf("status writes = %+v, want a recovery to ACTIVE", accounts.StatusWrites)
	}
}

func TestHealthCheckMarksRevokedToken(t *testing.T) {
	account := &tgdomain.Account{ID: "acct-1", BotToken: "t", Status: tgdomain.StatusActive}
	accounts := &fakeAccounts{
		ListForHealthCheckFn: func(context.Context, time.Time, int) ([]*tgdomain.Account, error) {
			return []*tgdomain.Account{account}, nil
		},
	}
	api := &fakeBotAPI{
		GetWebhookInfoFn: func(context.Context, string) (*tgdomain.WebhookInfo, error) {
			return nil, &tgdomain.APIError{Code: 401, Description: "Unauthorized"}
		},
	}

	if err := NewCheckWebhookHealthUseCase(accounts, api).Execute(context.Background()); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(accounts.StatusWrites) != 1 || accounts.StatusWrites[0].Status != tgdomain.StatusTokenInvalid {
		t.Errorf("status writes = %+v, want TOKEN_INVALID", accounts.StatusWrites)
	}
}

func TestHealthCheckIsolatesTenantFailures(t *testing.T) {
	broken := &tgdomain.Account{ID: "acct-broken", BotToken: "bad", Status: tgdomain.StatusActive}
	healthy := &tgdomain.Account{ID: "acct-ok", BotToken: "good", Status: tgdomain.StatusActive}

	accounts := &fakeAccounts{
		ListForHealthCheckFn: func(context.Context, time.Time, int) ([]*tgdomain.Account, error) {
			return []*tgdomain.Account{broken, healthy}, nil
		},
	}
	api := &fakeBotAPI{
		GetWebhookInfoFn: func(_ context.Context, token string) (*tgdomain.WebhookInfo, error) {
			if token == "bad" {
				return nil, errors.New("network unreachable")
			}
			return &tgdomain.WebhookInfo{PendingCount: 0}, nil
		},
	}

	if err := NewCheckWebhookHealthUseCase(accounts, api).Execute(context.Background()); err != nil {
		t.Fatalf("Execute must not fail because one tenant did: %v", err)
	}
	if len(accounts.HealthWrites) != 2 {
		t.Errorf("health writes = %d, want both accounts probed", len(accounts.HealthWrites))
	}
}

func TestReregisterRotatesTheSecret(t *testing.T) {
	account := &tgdomain.Account{
		ID:            "acct-1",
		WorkspaceID:   "ws-1",
		BotToken:      "77777:s",
		WebhookSecret: "old-secret",
		Status:        tgdomain.StatusWebhookFailing,
	}
	accounts := &fakeAccounts{
		FindByIDFn: func(context.Context, string) (*tgdomain.Account, error) { return account, nil },
	}
	api := &fakeBotAPI{}

	updated, err := NewReregisterWebhookUseCase(accounts, api, webhookBase).
		Execute(context.Background(), "ws-1", "acct-1")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if updated.WebhookSecret == "old-secret" || updated.WebhookSecret == "" {
		t.Errorf("secret = %q, want a freshly rotated value", updated.WebhookSecret)
	}
	if len(api.WebhookCfgs) != 1 || api.WebhookCfgs[0].SecretToken != updated.WebhookSecret {
		t.Error("the rotated secret must be the one registered with Telegram")
	}
	if updated.Status != tgdomain.StatusActive {
		t.Errorf("status = %s, want a recovery to ACTIVE", updated.Status)
	}
}

func TestReregisterRefusesForeignWorkspace(t *testing.T) {
	account := &tgdomain.Account{ID: "acct-1", WorkspaceID: "ws-OTHER", BotToken: "t"}
	accounts := &fakeAccounts{
		FindByIDFn: func(context.Context, string) (*tgdomain.Account, error) { return account, nil },
	}

	_, err := NewReregisterWebhookUseCase(accounts, &fakeBotAPI{}, webhookBase).
		Execute(context.Background(), "ws-1", "acct-1")
	if !errors.Is(err, tgdomain.ErrAccountNotFound) {
		t.Errorf("err = %v, want ErrAccountNotFound", err)
	}
}
