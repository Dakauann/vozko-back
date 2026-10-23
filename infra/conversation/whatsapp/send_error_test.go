package whatsapp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/whatsapp/template"
	wo "vozko/domain/whatsapp_outreach"
	"vozko/domain/workflow"
)

func refusingGraph(t *testing.T, status int, body string) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return NewClient(Config{BaseURL: server.URL, PhoneNumberID: "PNID", AccessToken: "tok"}).(*Client)
}

func cloudAPIError(code int) string {
	return fmt.Sprintf(`{"error":{"message":"(#%d) refused","type":"OAuthException","code":%d,"error_data":{"messaging_product":"whatsapp","details":"x"},"fbtrace_id":"AbC"}}`, code, code)
}

func everyCloudAPISend(c *Client) map[string]func() error {
	ctx := context.Background()
	return map[string]func() error{
		"text": func() error {
			_, err := c.SendTextMessage(ctx, conversation.SendTextMessageInput{To: "5511999990000", Body: "oi"})
			return err
		},
		"template": func() error {
			_, err := c.SendTemplateMessage(ctx, conversation.SendTemplateMessageInput{To: "5511999990000", TemplateName: "boas_vindas", Language: "pt_BR"})
			return err
		},
		"image": func() error {
			_, err := c.SendImageMessage(ctx, conversation.SendImageMessageInput{To: "5511999990000", Link: "https://cdn.example.com/a.png"})
			return err
		},
		"buttons": func() error {
			_, err := c.SendButtonMessage(ctx, conversation.SendButtonMessageInput{To: "5511999990000", BodyText: "Quer?", Buttons: []conversation.InteractiveButton{{ID: "sim", Title: "Sim"}}})
			return err
		},
	}
}

func TestMetaPermanentSendErrorsBecomeTheDomainRefusalsTheEngineNeverRetries(t *testing.T) {
	cases := []struct {
		name string
		code int
		want error
	}{
		{"131047 re-engagement: more than 24 h since the customer last wrote", 131047, conversation.ErrWindowClosed},
		{"131026 message undeliverable: not on WhatsApp or cannot receive", 131026, wo.ErrInvalidPhone},
		{"131050 the contact stopped marketing messages", 131050, wo.ErrLeadOptedOut},
		{"132001 template does not exist in this language or is not approved", 132001, template.ErrTemplateNotSendable},
		{"132015 template paused for low quality", 132015, template.ErrTemplateNotSendable},
		{"132016 template disabled for good", 132016, template.ErrTemplateNotSendable},
		{"131051 unsupported message type: the same message is refused again", 131051, conversation.ErrMessageTypeUnsupported},
	}
	for _, tc := range cases {
		for kind, send := range everyCloudAPISend(refusingGraph(t, http.StatusBadRequest, cloudAPIError(tc.code))) {
			t.Run(tc.name+"/"+kind, func(t *testing.T) {
				err := send()
				if !errors.Is(err, tc.want) || !workflow.IsPermanentRefusal(err) {
					t.Fatalf("err = %v, want %v and a permanent refusal", err, tc.want)
				}
				if !strings.Contains(err.Error(), fmt.Sprintf("status=%d", http.StatusBadRequest)) || !strings.Contains(err.Error(), fmt.Sprintf(`"code":%d`, tc.code)) {
					t.Fatalf("err = %v, want the provider status and body kept in the message", err)
				}
			})
		}
	}
}

func TestMetaTransientSendErrorsStayRetryable(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
	}{
		{"130429 throughput limit", http.StatusBadRequest, cloudAPIError(130429)},
		{"131056 pair rate limit", http.StatusBadRequest, cloudAPIError(131056)},
		{"80007 business account rate limit", http.StatusBadRequest, cloudAPIError(80007)},
		{"131000 something went wrong", http.StatusInternalServerError, cloudAPIError(131000)},
		{"131049 Meta held the message for ecosystem engagement, retry later", http.StatusBadRequest, cloudAPIError(131049)},
		{"130472 experiment holdout: Meta kept this contact out of a marketing experiment, not a refusal of the message", http.StatusBadRequest, cloudAPIError(130472)},
		{"a 5xx without a Meta body", http.StatusBadGateway, "upstream down"},
	}
	for _, tc := range cases {
		for kind, send := range everyCloudAPISend(refusingGraph(t, tc.status, tc.body)) {
			t.Run(tc.name+"/"+kind, func(t *testing.T) {
				err := send()
				if err == nil || workflow.IsPermanentRefusal(err) {
					t.Fatalf("err = %v, want a failure the engine retries", err)
				}
			})
		}
	}
}
