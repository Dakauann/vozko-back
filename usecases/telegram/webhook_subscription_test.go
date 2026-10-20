package telegram

import (
	"context"
	"testing"
	"time"

	tgdomain "vozko/domain/telegram"
)

func healthCheckOf(account *tgdomain.Account, info *tgdomain.WebhookInfo) (*CheckWebhookHealthUseCase, *fakeAccounts, *fakeBotAPI) {
	accounts := &fakeAccounts{
		ListForHealthCheckFn: func(context.Context, time.Time, int) ([]*tgdomain.Account, error) {
			return []*tgdomain.Account{account}, nil
		},
	}
	api := &fakeBotAPI{
		GetWebhookInfoFn: func(context.Context, string) (*tgdomain.WebhookInfo, error) {
			return info, nil
		},
	}
	return NewCheckWebhookHealthUseCase(accounts, api, webhookBase), accounts, api
}

func connectedBot() *tgdomain.Account {
	return &tgdomain.Account{ID: "acct-1", BotUsername: "vozko_bot", BotToken: "t", WebhookSecret: "kept-secret", Status: tgdomain.StatusActive}
}

func TestHealthCheckResubscribesABotRegisteredBeforeReactionsWereAllowed(t *testing.T) {
	account := connectedBot()
	stale := []string{"message", "edited_message", "callback_query", "my_chat_member"}
	uc, accounts, api := healthCheckOf(account, &tgdomain.WebhookInfo{URL: tgdomain.WebhookURLFor(webhookBase, account.ID), AllowedUpdates: stale})

	if err := uc.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}

	if len(api.WebhookCfgs) != 1 {
		t.Fatalf("setWebhook calls = %d, want one resubscription", len(api.WebhookCfgs))
	}
	cfg := api.WebhookCfgs[0]
	if !tgdomain.SubscribesToAllowedUpdates(cfg.AllowedUpdates) {
		t.Fatalf("allowed updates = %v, want the current list", cfg.AllowedUpdates)
	}
	if cfg.URL != tgdomain.WebhookURLFor(webhookBase, account.ID) || cfg.SecretToken != "kept-secret" || cfg.DropPendingUpdates {
		t.Fatalf("config = %+v, want the same URL and secret with pending updates kept", cfg)
	}
	if len(accounts.HealthWrites) != 1 {
		t.Fatalf("health writes = %d, want the probe still recorded", len(accounts.HealthWrites))
	}
}

func TestHealthCheckLeavesABotAlone(t *testing.T) {
	account := connectedBot()
	ours := tgdomain.WebhookURLFor(webhookBase, account.ID)
	cases := []struct {
		name string
		info *tgdomain.WebhookInfo
	}{
		{"when it already receives the current update types", &tgdomain.WebhookInfo{URL: ours, AllowedUpdates: tgdomain.AllowedUpdates()}},
		{"when its webhook points somewhere we did not register", &tgdomain.WebhookInfo{URL: "https://elsewhere.example.com/hook", AllowedUpdates: []string{"message"}}},
		{"when it has no webhook at all", &tgdomain.WebhookInfo{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			uc, _, api := healthCheckOf(account, tc.info)

			if err := uc.Execute(context.Background()); err != nil {
				t.Fatal(err)
			}
			if len(api.WebhookCfgs) != 0 {
				t.Fatalf("setWebhook calls = %+v, want none", api.WebhookCfgs)
			}
		})
	}
}
