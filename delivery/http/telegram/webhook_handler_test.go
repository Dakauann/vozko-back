package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	tgdomain "vozko/domain/telegram"
	"vozko/domain/webhook"
	tguc "vozko/usecases/telegram"
)

type stubAccounts struct {
	account *tgdomain.Account
	err     error
	calls   int
}

func (s *stubAccounts) FindByIDForWebhook(_ context.Context, _ string) (*tgdomain.Account, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return s.account, nil
}

type stubPublisher struct {
	published []publishedMessage
	err       error
}

type publishedMessage struct {
	Topic   string
	Payload []byte
}

func (s *stubPublisher) Publish(topic string, payload []byte) error {
	if s.err != nil {
		return s.err
	}
	s.published = append(s.published, publishedMessage{Topic: topic, Payload: payload})
	return nil
}

var _ webhook.PublishWebhookUseCase = (*stubPublisher)(nil)

const validUpdate = `{"update_id":1,"message":{"message_id":1,"from":{"id":5,"is_bot":false,"first_name":"M"},"chat":{"id":5,"type":"private"},"date":1785312000,"text":"oi"}}`

func serve(
	t *testing.T,
	accounts AccountLookup,
	publisher webhook.PublishWebhookUseCase,
	accountID, secret, body string,
) *httptest.ResponseRecorder {
	t.Helper()

	h := NewWebhookHandler(accounts, publisher)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/telegram/"+accountID, strings.NewReader(body))
	if secret != "" {
		req.Header.Set(tgdomain.SecretTokenHeader, secret)
	}
	req = mux.SetURLVars(req, map[string]string{"accountId": accountID})

	rec := httptest.NewRecorder()
	h.Handle(rec, req)
	return rec
}

func activeAccount() *tgdomain.Account {
	return &tgdomain.Account{
		ID:            "acct-1",
		WorkspaceID:   "ws-1",
		Status:        tgdomain.StatusActive,
		WebhookSecret: "s3cr3t-token",
	}
}

func TestWebhookAcceptsValidUpdate(t *testing.T) {
	accounts := &stubAccounts{account: activeAccount()}
	publisher := &stubPublisher{}

	rec := serve(t, accounts, publisher, "acct-1", "s3cr3t-token", validUpdate)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if len(publisher.published) != 1 {
		t.Fatalf("published %d messages, want 1", len(publisher.published))
	}
	if publisher.published[0].Topic != webhook.TopicTelegramMessage {
		t.Errorf("topic = %q, want the message topic", publisher.published[0].Topic)
	}

	// The account id must travel with the payload: the update itself carries no
	// bot identity, so it cannot be re-derived downstream.
	var queued tguc.QueuedUpdate
	if err := json.Unmarshal(publisher.published[0].Payload, &queued); err != nil {
		t.Fatalf("queued payload does not decode: %v", err)
	}
	if queued.AccountID != "acct-1" {
		t.Errorf("AccountID = %q, want the id from the URL", queued.AccountID)
	}
	if len(queued.Update) == 0 {
		t.Error("the raw update must be carried through verbatim")
	}
}

// The secret token is the ONLY authenticity control this endpoint has: Telegram
// does not sign the body. Every way of getting it wrong must be refused.
func TestWebhookRejectsBadSecret(t *testing.T) {
	cases := map[string]string{
		"wrong secret":   "not-the-secret",
		"missing header": "",
		"empty string":   " ",
	}

	for name, secret := range cases {
		t.Run(name, func(t *testing.T) {
			accounts := &stubAccounts{account: activeAccount()}
			publisher := &stubPublisher{}

			rec := serve(t, accounts, publisher, "acct-1", secret, validUpdate)

			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401", rec.Code)
			}
			if len(publisher.published) != 0 {
				t.Error("nothing may be published for an unverified request")
			}
		})
	}
}

// An account row with no secret can only mean it predates registration or was
// tampered with. Accepting unverified input is never the safer default.
func TestWebhookRejectsAccountWithoutSecret(t *testing.T) {
	account := activeAccount()
	account.WebhookSecret = ""

	rec := serve(t, &stubAccounts{account: account}, &stubPublisher{}, "acct-1", "", validUpdate)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

// 401 rather than 404 on purpose: a 404 tells a scanner which account ids exist.
func TestWebhookAnswers401ForUnknownAccount(t *testing.T) {
	accounts := &stubAccounts{err: tgdomain.ErrAccountNotFound}
	rec := serve(t, accounts, &stubPublisher{}, "acct-missing", "whatever", validUpdate)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 (not 404, which would enumerate accounts)", rec.Code)
	}
}

func TestWebhookAnswers401WithoutAccountID(t *testing.T) {
	h := NewWebhookHandler(&stubAccounts{account: activeAccount()}, &stubPublisher{})
	req := httptest.NewRequest(http.MethodPost, "/webhooks/telegram/", strings.NewReader(validUpdate))
	rec := httptest.NewRecorder()

	h.Handle(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

// A webhook-failing account is exactly the one whose next delivery matters most.
// Refusing it would turn a recoverable health blip into permanent message loss
// once Telegram's 24h retention expires.
func TestWebhookAcceptsWebhookFailingAccount(t *testing.T) {
	account := activeAccount()
	account.Status = tgdomain.StatusWebhookFailing

	publisher := &stubPublisher{}
	rec := serve(t, &stubAccounts{account: account}, publisher, "acct-1", "s3cr3t-token", validUpdate)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if len(publisher.published) != 1 {
		t.Error("a failing-webhook account must still have its updates accepted")
	}
}

// A publish failure must NOT be acked. Telegram discards undelivered updates
// after 24 hours and has no history API, so acknowledging something we failed to
// enqueue loses it permanently; a 500 makes Telegram redeliver, which the
// update_id dedup makes safe.
func TestWebhookAnswers500WhenPublishFails(t *testing.T) {
	publisher := &stubPublisher{err: errors.New("broker down")}
	rec := serve(t, &stubAccounts{account: activeAccount()}, publisher, "acct-1", "s3cr3t-token", validUpdate)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 so Telegram retries", rec.Code)
	}
}

// A malformed body will never parse, so it is acked to stop Telegram retrying
// forever, but it must never be published.
func TestWebhookAcksUndecodableBody(t *testing.T) {
	publisher := &stubPublisher{}
	rec := serve(t, &stubAccounts{account: activeAccount()}, publisher, "acct-1", "s3cr3t-token", "not json")

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 so Telegram stops retrying", rec.Code)
	}
	if len(publisher.published) != 0 {
		t.Error("an undecodable body must not be published")
	}
}

func TestWebhookRejectsNonPost(t *testing.T) {
	h := NewWebhookHandler(&stubAccounts{account: activeAccount()}, &stubPublisher{})
	req := httptest.NewRequest(http.MethodGet, "/webhooks/telegram/acct-1", nil)
	req = mux.SetURLVars(req, map[string]string{"accountId": "acct-1"})

	rec := httptest.NewRecorder()
	h.Handle(rec, req)

	// Unlike Meta, Telegram has no GET handshake, there is nothing to verify.
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}

// The Bot API adds update kinds several times a year. An unrecognised one must
// still be published, to the account topic, so it is logged rather than lost.
func TestTopicRouting(t *testing.T) {
	cases := map[string]struct {
		body string
		want string
	}{
		"message":            {validUpdate, webhook.TopicTelegramMessage},
		"edited_message":     {`{"update_id":2,"edited_message":{"message_id":1,"chat":{"id":5,"type":"private"},"date":1}}`, webhook.TopicTelegramMessage},
		"callback_query":     {`{"update_id":3,"callback_query":{"id":"q","from":{"id":5,"is_bot":false,"first_name":"M"}}}`, webhook.TopicTelegramMessage},
		"business_message":   {`{"update_id":4,"business_message":{"message_id":1,"chat":{"id":5,"type":"private"},"date":1}}`, webhook.TopicTelegramMessage},
		"deleted_business":   {`{"update_id":5,"deleted_business_messages":{"business_connection_id":"c","chat":{"id":5,"type":"private"},"message_ids":[1]}}`, webhook.TopicTelegramMessage},
		"my_chat_member":     {`{"update_id":6,"my_chat_member":{"chat":{"id":5,"type":"private"},"from":{"id":5,"is_bot":false,"first_name":"M"},"date":1}}`, webhook.TopicTelegramAccount},
		"business_connected": {`{"update_id":7,"business_connection":{"id":"c","user":{"id":5,"is_bot":false,"first_name":"L"},"user_chat_id":5,"date":1,"is_enabled":true}}`, webhook.TopicTelegramAccount},
		"unknown kind":       {`{"update_id":8,"poll_answer":{"poll_id":"p"}}`, webhook.TopicTelegramAccount},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			publisher := &stubPublisher{}
			rec := serve(t, &stubAccounts{account: activeAccount()}, publisher, "acct-1", "s3cr3t-token", tc.body)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			if len(publisher.published) != 1 {
				t.Fatalf("published %d, want 1, nothing may be silently dropped", len(publisher.published))
			}
			if publisher.published[0].Topic != tc.want {
				t.Errorf("topic = %q, want %q", publisher.published[0].Topic, tc.want)
			}
		})
	}
}

// The body is read only AFTER the tenant and secret are resolved, so an
// unauthenticated caller cannot make us buffer a megabyte.
func TestWebhookResolvesAccountBeforeReadingBody(t *testing.T) {
	accounts := &stubAccounts{err: tgdomain.ErrAccountNotFound}
	rec := serve(t, accounts, &stubPublisher{}, "acct-1", "s3cr3t-token", validUpdate)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if accounts.calls != 1 {
		t.Errorf("account lookups = %d, want exactly 1", accounts.calls)
	}
}
