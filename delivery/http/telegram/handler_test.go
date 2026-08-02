package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"vozko/domain/shared"
	tgdomain "vozko/domain/telegram"
	"vozko/infra/http/middleware"
	tguc "vozko/usecases/telegram"
)

// listAccountsRepo is the minimum AccountRepository the list endpoint touches.
type listAccountsRepo struct {
	tgdomain.AccountRepository
	items []*tgdomain.Account
}

func (r *listAccountsRepo) ListByWorkspace(_ context.Context, in tgdomain.ListAccountsInput) (*shared.PaginatedResult[*tgdomain.Account], error) {
	return shared.NewPaginatedResult(r.items, shared.NormalizePagination(in.Options.Pagination), int64(len(r.items))), nil
}

func withWorkspace(req *http.Request, workspaceID string) *http.Request {
	return req.WithContext(context.WithValue(
		req.Context(), middleware.WorkspaceIDContextKey, workspaceID))
}

// The list endpoint must answer the SAME envelope every other paginated list in
// this API answers: {"data": [...], "meta": {...}}.
//
// It originally returned a bespoke {"items": [...], "page": ...}. That parses
// without error in the browser client, which reads response.data.data — so the
// account was created, the request was 200, and the table was simply blank with
// nothing anywhere saying why.
func TestListAccountsUsesTheStandardPaginatedEnvelope(t *testing.T) {
	account := &tgdomain.Account{
		ID: "acct-1", WorkspaceID: "ws-1", Mode: tgdomain.ModeBot,
		BotUserID: 8608280305, BotUsername: "vozkotest_bot", Status: tgdomain.StatusActive,
	}

	h := NewHandler(HandlerDeps{
		List: tguc.NewListAccountsUseCase(&listAccountsRepo{items: []*tgdomain.Account{account}}),
	})

	req := httptest.NewRequest(http.MethodGet, "/telegram/accounts", nil)
	req = withWorkspace(req, "ws-1")
	rec := httptest.NewRecorder()

	h.ListAccounts(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	var body struct {
		Data []struct {
			ID          string `json:"id"`
			BotUserID   string `json:"botUserId"`
			BotUsername string `json:"botUsername"`
		} `json:"data"`
		Meta struct {
			Page       int   `json:"page"`
			TotalItems int64 `json:"total_items"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response does not decode: %v — body=%s", err, rec.Body.String())
	}

	if len(body.Data) != 1 {
		t.Fatalf("data has %d rows, want 1 — the browser client reads exactly this key; body=%s",
			len(body.Data), rec.Body.String())
	}
	if body.Data[0].BotUsername != "vozkotest_bot" {
		t.Errorf("botUsername = %q", body.Data[0].BotUsername)
	}
	// Rendered as a string: a Telegram id exceeds 2^53 and would lose precision
	// as a JSON number.
	if body.Data[0].BotUserID != "8608280305" {
		t.Errorf("botUserId = %q, want a decimal string", body.Data[0].BotUserID)
	}
}

// No credential may ever appear in a response. An over-broad RBAC grant must not
// leak the ability to impersonate the bot.
func TestListAccountsNeverSerializesCredentials(t *testing.T) {
	account := &tgdomain.Account{
		ID: "acct-1", WorkspaceID: "ws-1", Mode: tgdomain.ModeBot, BotUserID: 1,
		BotToken: "8608280305:SUPERSECRET", WebhookSecret: "webhook-secret-value",
		Status: tgdomain.StatusActive,
	}
	h := NewHandler(HandlerDeps{
		List: tguc.NewListAccountsUseCase(&listAccountsRepo{items: []*tgdomain.Account{account}}),
	})

	req := httptest.NewRequest(http.MethodGet, "/telegram/accounts", nil)
	req = withWorkspace(req, "ws-1")
	rec := httptest.NewRecorder()
	h.ListAccounts(rec, req)

	body := rec.Body.String()
	for _, secret := range []string{"SUPERSECRET", "webhook-secret-value"} {
		if contains(body, secret) {
			t.Fatalf("credential %q leaked into the response: %s", secret, body)
		}
	}
}

func contains(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}

var _ = time.Now
