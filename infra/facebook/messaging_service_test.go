package facebook

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"vozko/domain/cache"
	fbdomain "vozko/domain/facebook"
	"vozko/infra/meta/metatest"
)

type allowAll struct{}

func (allowAll) Allow(string) (bool, time.Duration, error) { return true, 0, nil }

func allowAllFactory() cache.RateLimiterFactory {
	return func(string, int, time.Duration) cache.RateLimiter { return allowAll{} }
}

type capture struct {
	path string
	body map[string]any
}

func newMessaging(t *testing.T, reply string, got *capture) fbdomain.MessagingService {
	t.Helper()
	svc, err := NewMessagingService(MessagingConfig{
		Graph: GraphConfig{HTTPClient: metatest.Server(t, func(w http.ResponseWriter, r *http.Request) {
			got.path = r.URL.Path
			raw, _ := io.ReadAll(r.Body)
			got.body = map[string]any{}
			_ = json.Unmarshal(raw, &got.body)
			_, _ = w.Write([]byte(reply))
		})},
		RateLimiterFactory: allowAllFactory(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func TestSendStandardTextIsAResponse(t *testing.T) {
	got := &capture{}
	svc := newMessaging(t, `{"recipient_id":"psid","message_id":"m_1"}`, got)
	res, err := svc.Send(context.Background(), "page-1", "pt", fbdomain.OutboundMessage{
		Recipient: fbdomain.Recipient{PSID: "psid"}, Tier: fbdomain.SendStandard, Metadata: "vozko:operator",
		Text: "oi", ReplyToMID: "m_0",
	})
	if err != nil || res.MessageID != "m_1" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if got.path != "/v25.0/page-1/messages" || got.body["messaging_type"] != "RESPONSE" || got.body["tag"] != nil {
		t.Fatalf("path=%s body=%v", got.path, got.body)
	}
	msg := got.body["message"].(map[string]any)
	if msg["text"] != "oi" || msg["metadata"] != "vozko:operator" || got.body["reply_to"].(map[string]any)["mid"] != "m_0" {
		t.Fatalf("body = %v", got.body)
	}
}

func TestSendHumanAgentTier(t *testing.T) {
	got := &capture{}
	svc := newMessaging(t, `{"recipient_id":"psid","message_id":"m_1"}`, got)
	if _, err := svc.Send(context.Background(), "page-1", "pt", fbdomain.OutboundMessage{
		Recipient: fbdomain.Recipient{PSID: "psid"}, Tier: fbdomain.SendHumanAgent, Text: "oi",
	}); err != nil {
		t.Fatal(err)
	}
	if got.body["messaging_type"] != "MESSAGE_TAG" || got.body["tag"] != "HUMAN_AGENT" {
		t.Fatalf("body = %v", got.body)
	}
}

func TestPrivateReplyCarriesNoMessagingType(t *testing.T) {
	got := &capture{}
	svc := newMessaging(t, `{"recipient_id":"psid","message_id":"m_1"}`, got)
	res, err := svc.Send(context.Background(), "page-1", "pt", fbdomain.OutboundMessage{
		Recipient: fbdomain.Recipient{CommentID: "c_1"}, Tier: fbdomain.SendStandard, Text: "see inbox",
	})
	if err != nil || res.RecipientID != "psid" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if got.body["messaging_type"] != nil || got.body["recipient"].(map[string]any)["comment_id"] != "c_1" {
		t.Fatalf("body = %v", got.body)
	}
}

func TestSendButtonsBecomesAButtonTemplate(t *testing.T) {
	got := &capture{}
	svc := newMessaging(t, `{"recipient_id":"psid","message_id":"m_1"}`, got)
	if _, err := svc.Send(context.Background(), "page-1", "pt", fbdomain.OutboundMessage{
		Recipient: fbdomain.Recipient{PSID: "psid"}, Tier: fbdomain.SendStandard, Text: "Escolha",
		Buttons: []fbdomain.Option{{Title: "Vendas", Payload: "opt:1"}},
	}); err != nil {
		t.Fatal(err)
	}
	payload := got.body["message"].(map[string]any)["attachment"].(map[string]any)["payload"].(map[string]any)
	if payload["template_type"] != "button" || payload["text"] != "Escolha" {
		t.Fatalf("payload = %v", payload)
	}
}

func TestSendMediaByURLAndByID(t *testing.T) {
	got := &capture{}
	svc := newMessaging(t, `{"recipient_id":"psid","message_id":"m_1"}`, got)
	if _, err := svc.Send(context.Background(), "page-1", "pt", fbdomain.OutboundMessage{
		Recipient: fbdomain.Recipient{PSID: "psid"}, Tier: fbdomain.SendStandard, AttachmentKind: "document", AttachmentURL: "https://cdn/x.pdf",
	}); err != nil {
		t.Fatal(err)
	}
	att := got.body["message"].(map[string]any)["attachment"].(map[string]any)
	if att["type"] != "file" || att["payload"].(map[string]any)["url"] != "https://cdn/x.pdf" {
		t.Fatalf("attachment = %v", att)
	}
	if _, err := svc.Send(context.Background(), "page-1", "pt", fbdomain.OutboundMessage{
		Recipient: fbdomain.Recipient{PSID: "psid"}, Tier: fbdomain.SendStandard, AttachmentKind: "image", AttachmentID: "77",
	}); err != nil {
		t.Fatal(err)
	}
	if got.body["message"].(map[string]any)["attachment"].(map[string]any)["payload"].(map[string]any)["attachment_id"] != "77" {
		t.Fatalf("body = %v", got.body)
	}
}

func TestSendRejectsAMessageWithoutContent(t *testing.T) {
	svc := newMessaging(t, `{}`, &capture{})
	if _, err := svc.Send(context.Background(), "page-1", "pt", fbdomain.OutboundMessage{Recipient: fbdomain.Recipient{PSID: "psid"}}); err == nil {
		t.Fatal("empty message accepted")
	}
}

func TestReactionAndSenderActions(t *testing.T) {
	got := &capture{}
	svc := newMessaging(t, `{"recipient_id":"psid"}`, got)
	if err := svc.React(context.Background(), "page-1", "pt", "psid", "m_1", "❤️"); err != nil {
		t.Fatal(err)
	}
	if got.body["sender_action"] != "react" || got.body["payload"].(map[string]any)["message_id"] != "m_1" {
		t.Fatalf("body = %v", got.body)
	}
	if err := svc.SendAction(context.Background(), "page-1", "pt", "psid", fbdomain.ActionMarkSeen); err != nil {
		t.Fatal(err)
	}
	if got.body["sender_action"] != "mark_seen" || got.body["message"] != nil {
		t.Fatalf("body = %v", got.body)
	}
}

func TestGetProfile(t *testing.T) {
	got := &capture{}
	svc := newMessaging(t, `{"first_name":"Maria","last_name":"Silva","profile_pic":"https://pic"}`, got)
	p, err := svc.GetProfile(context.Background(), "pt", "psid")
	if err != nil || p.FirstName != "Maria" || p.PictureURL != "https://pic" || got.path != "/v25.0/psid" {
		t.Fatalf("profile=%+v err=%v path=%s", p, err, got.path)
	}
}

func TestThrottleRefusalBlocksTheSend(t *testing.T) {
	called := false
	svc, err := NewMessagingService(MessagingConfig{
		Graph: GraphConfig{HTTPClient: metatest.Server(t, func(http.ResponseWriter, *http.Request) { called = true })},
		RateLimiterFactory: func(string, int, time.Duration) cache.RateLimiter {
			return denyAll{}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Send(context.Background(), "page-1", "pt", fbdomain.OutboundMessage{Recipient: fbdomain.Recipient{PSID: "p"}, Text: "x"}); err == nil || called {
		t.Fatalf("err=%v called=%t", err, called)
	}
}

type denyAll struct{}

func (denyAll) Allow(string) (bool, time.Duration, error) { return false, time.Second, nil }
