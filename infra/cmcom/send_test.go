package cmcom_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"vozko/domain/carriermessaging"
	"vozko/domain/rcs"
	"vozko/infra/cmcom"
	"vozko/infra/cmcom/cmfake"
)

const testReference = "9c5ac3abb8c94775b8d7ceb274ba335a"

func newClient(t *testing.T) (*cmcom.Client, *cmfake.Fake) {
	t.Helper()
	fake := cmfake.New()
	t.Cleanup(fake.Close)
	client, err := cmcom.NewClient(fake.Config())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client, fake
}

func image(name string) *rcs.Media {
	return &rcs.Media{Kind: rcs.MediaImage, URL: "https://cdn.example.com/" + name, MIMEType: "image/jpeg", Name: name}
}

func sendRequest(content rcs.Content) carriermessaging.SendRequest {
	return carriermessaging.SendRequest{
		Reference:       testReference,
		SenderProfileID: "3f0f4b6e-2b8c-4d4e-9a51-6a7b8c9d0e1f",
		Phone:           "5511987654321",
		Content:         content.WithTrackedPostbacks(),
		Validity:        30 * time.Minute,
	}
}

func decodeJSON(t *testing.T, raw []byte) any {
	t.Helper()
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("json: %v\n%s", err, raw)
	}
	return out
}

func assertJSON(t *testing.T, got json.RawMessage, want string) {
	t.Helper()
	g, w := decodeJSON(t, got), decodeJSON(t, []byte(want))
	if !reflect.DeepEqual(g, w) {
		pretty, _ := json.MarshalIndent(g, "", "  ")
		t.Fatalf("got\n%s\nwant\n%s", pretty, want)
	}
}

func allSuggestions() []rcs.Suggestion {
	return []rcs.Suggestion{
		{Kind: rcs.SuggestReply, Label: "Quero"},
		{Kind: rcs.SuggestOpenURL, Label: "Ver site", URL: "https://example.com/promo"},
		{Kind: rcs.SuggestOpenURL, Label: "Abrir aqui", URL: "https://example.com/app", Webview: true},
		{Kind: rcs.SuggestDial, Label: "Ligar", Phone: "+5511987654321"},
		{Kind: rcs.SuggestViewLocation, Label: "Mapa", Latitude: -23.55052, Longitude: -46.633308, LocationLabel: "Loja"},
		{Kind: rcs.SuggestCalendar, Label: "Agendar", Calendar: &rcs.CalendarEvent{
			Title: "Visita", Description: "Visita na loja", StartsAt: "2026-11-02T13:00:00-03:00", EndsAt: "2026-11-02T14:00:00-03:00",
		}},
	}
}

const allSuggestionsJSON = `[
	{"action":"Reply","label":"Quero","postbackdata":"vz:m0"},
	{"action":"OpenUrl","label":"Ver site","url":"https://example.com/promo","postbackdata":"vz:m1"},
	{"action":"OpenUrl","label":"Abrir aqui","url":"https://example.com/app","postbackdata":"vz:m2",
	 "openUrlProperties":{"application":"WEBVIEW","webviewMode":"full"}},
	{"action":"Dial","label":"Ligar","dial":{"phoneNumber":"+5511987654321"},"postbackdata":"vz:m3"},
	{"action":"ViewLocation","label":"Mapa","viewLocation":{"latitude":-23.55052,"longitude":-46.633308,"label":"Loja"},"postbackdata":"vz:m4"},
	{"action":"CreateCalendarEvent","label":"Agendar","postbackdata":"vz:m5",
	 "calendar":{"title":"Visita","description":"Visita na loja","startTime":"2026-11-02T16:00:00Z","endTime":"2026-11-02T17:00:00Z"}}
]`

func TestSendRequestShape(t *testing.T) {
	cases := []struct {
		name        string
		content     rcs.Content
		bodyContent string
		richContent string
	}{
		{
			name:        "text",
			content:     rcs.Content{Text: "Seu pedido saiu para entrega"},
			bodyContent: "Seu pedido saiu para entrega",
			richContent: `{"conversation":[{"text":"Seu pedido saiu para entrega"}]}`,
		},
		{
			name:        "text with every suggestion kind",
			content:     rcs.Content{Text: "Escolha", Suggestions: allSuggestions()},
			bodyContent: "Escolha",
			richContent: `{"conversation":[{"text":"Escolha"}],"suggestions":` + allSuggestionsJSON + `}`,
		},
		{
			name:        "media only",
			content:     rcs.Content{Media: image("promo.jpg")},
			bodyContent: "https://cdn.example.com/promo.jpg",
			richContent: `{"conversation":[{"media":{"mediaName":"promo.jpg","mediaUri":"https://cdn.example.com/promo.jpg","mimeType":"image/jpeg"}}]}`,
		},
		{
			name: "media with text and a chip",
			content: rcs.Content{
				Text:        "Olha isso",
				Media:       &rcs.Media{Kind: rcs.MediaPDF, URL: "https://cdn.example.com/files/boleto.pdf", MIMEType: "application/pdf"},
				Suggestions: []rcs.Suggestion{{Kind: rcs.SuggestReply, Label: "Recebi"}},
			},
			bodyContent: "Olha isso",
			richContent: `{"conversation":[{"text":"Olha isso"},{"media":{"mediaName":"boleto.pdf","mediaUri":"https://cdn.example.com/files/boleto.pdf","mimeType":"application/pdf"}}],
				"suggestions":[{"action":"Reply","label":"Recebi","postbackdata":"vz:m0"}]}`,
		},
		{
			name: "rich card",
			content: rcs.Content{Cards: []rcs.Card{{
				Media: image("bolo.jpg"), Title: "Bolo do dia", Description: "Cenoura com chocolate",
				Suggestions: []rcs.Suggestion{
					{Kind: rcs.SuggestReply, Label: "Reservar"},
					{Kind: rcs.SuggestOpenURL, Label: "Cardápio", URL: "https://example.com/menu"},
				},
			}}},
			bodyContent: "Bolo do dia\nCenoura com chocolate",
			richContent: `{"conversation":[{"header":"Bolo do dia","text":"Cenoura com chocolate",
				"media":{"mediaName":"bolo.jpg","mediaUri":"https://cdn.example.com/bolo.jpg","mimeType":"image/jpeg"},
				"suggestions":[{"action":"Reply","label":"Reservar","postbackdata":"vz:c0b0"},
				               {"action":"OpenUrl","label":"Cardápio","url":"https://example.com/menu","postbackdata":"vz:c0b1"}]}]}`,
		},
		{
			name:        "rich card with media only",
			content:     rcs.Content{Cards: []rcs.Card{{Media: image("so-foto.jpg")}}},
			bodyContent: "https://cdn.example.com/so-foto.jpg",
			richContent: `{"conversation":[{"media":{"mediaName":"so-foto.jpg","mediaUri":"https://cdn.example.com/so-foto.jpg","mimeType":"image/jpeg"}}]}`,
		},
		{
			name: "carousel with chips",
			content: rcs.Content{
				Cards: []rcs.Card{
					{Media: image("a.jpg"), Title: "A", Description: "Primeiro", Suggestions: []rcs.Suggestion{{Kind: rcs.SuggestReply, Label: "Quero A"}}},
					{Title: "B", Suggestions: []rcs.Suggestion{{Kind: rcs.SuggestDial, Label: "Ligar", Phone: "+551130000000"}}},
				},
				Suggestions: []rcs.Suggestion{{Kind: rcs.SuggestReply, Label: "Nenhum"}},
			},
			bodyContent: "A\nPrimeiro",
			richContent: `{"conversation":[{"carousel":{"cardWidth":"Medium","cards":[
				{"header":"A","text":"Primeiro","media":{"mediaName":"a.jpg","mediaUri":"https://cdn.example.com/a.jpg","mimeType":"image/jpeg"},
				 "suggestions":[{"action":"Reply","label":"Quero A","postbackdata":"vz:c0b0"}]},
				{"header":"B","suggestions":[{"action":"Dial","label":"Ligar","dial":{"phoneNumber":"+551130000000"},"postbackdata":"vz:c1b0"}]}
			]}}],"suggestions":[{"action":"Reply","label":"Nenhum","postbackdata":"vz:m0"}]}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client, fake := newClient(t)
			outcome, err := client.Send(context.Background(), sendRequest(tc.content))
			if err != nil {
				t.Fatalf("Send: %v", err)
			}
			if outcome != (carriermessaging.SendOutcome{Accepted: true}) {
				t.Fatalf("outcome = %+v", outcome)
			}
			sends := fake.Sends()
			if len(sends) != 1 {
				t.Fatalf("sends = %d", len(sends))
			}
			s := sends[0]
			if s.ProductToken != fake.ProductToken {
				t.Fatalf("product token header = %q", s.ProductToken)
			}
			want := `{
				"from":"3f0f4b6e-2b8c-4d4e-9a51-6a7b8c9d0e1f",
				"to":[{"number":"005511987654321"}],
				"body":{"type":"Auto","content":` + mustJSON(t, tc.bodyContent) + `},
				"reference":"` + testReference + `",
				"allowedChannels":["RCS"],
				"validity":"30m",
				"richContent":` + tc.richContent + `}`
			assertJSON(t, s.Raw, want)
		})
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestSendValidityIsWholeMinutes(t *testing.T) {
	cases := []struct {
		validity time.Duration
		want     string
		ok       bool
	}{
		{30 * time.Minute, "30m", true},
		{24 * time.Hour, "1440m", true},
		{time.Minute, "1m", true},
		{0, "", false},
		{-time.Minute, "", false},
		{90 * time.Second, "", false},
		{24*time.Hour + time.Minute, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.validity.String(), func(t *testing.T) {
			client, fake := newClient(t)
			req := sendRequest(rcs.Content{Text: "oi"})
			req.Validity = tc.validity
			_, err := client.Send(context.Background(), req)
			if !tc.ok {
				if err == nil || len(fake.Sends()) != 0 {
					t.Fatalf("Send must refuse validity %s without calling the gateway (err %v)", tc.validity, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Send: %v", err)
			}
			if got := fake.Sends()[0].Validity; got != tc.want {
				t.Fatalf("validity = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSendRefusesBadRequestsBeforeCallingTheGateway(t *testing.T) {
	untracked := sendRequest(rcs.Content{Text: "x"})
	untracked.Content = rcs.Content{Text: "x", Suggestions: []rcs.Suggestion{{Kind: rcs.SuggestReply, Label: "Sim"}}}
	badDial := sendRequest(rcs.Content{Text: "x", Suggestions: []rcs.Suggestion{{Kind: rcs.SuggestDial, Label: "Ligar", Phone: "(11) 3000-0000"}}})
	cases := map[string]carriermessaging.SendRequest{
		"invalid content":        sendRequest(rcs.Content{}),
		"suggestion no postback": untracked,
		"dial with punctuation":  badDial,
		"short reference": func() carriermessaging.SendRequest {
			r := sendRequest(rcs.Content{Text: "x"})
			r.Reference = "abc"
			return r
		}(),
		"reference with hyphens": func() carriermessaging.SendRequest {
			r := sendRequest(rcs.Content{Text: "x"})
			r.Reference = "9c5ac3ab-b8c9-4775-b8d7-ceb274ba335a"
			return r
		}(),
		"missing sender": func() carriermessaging.SendRequest {
			r := sendRequest(rcs.Content{Text: "x"})
			r.SenderProfileID = " "
			return r
		}(),
		"bad phone": func() carriermessaging.SendRequest {
			r := sendRequest(rcs.Content{Text: "x"})
			r.Phone = "11 98765-4321"
			return r
		}(),
		"phone in 00form": func() carriermessaging.SendRequest {
			r := sendRequest(rcs.Content{Text: "x"})
			r.Phone = "005511987654321"
			return r
		}(),
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			client, fake := newClient(t)
			if _, err := client.Send(context.Background(), req); err == nil {
				t.Fatalf("Send must fail")
			}
			if n := len(fake.Sends()); n != 0 {
				t.Fatalf("gateway called %d times", n)
			}
		})
	}
}

func TestSendScriptedOutcomes(t *testing.T) {
	cases := []struct {
		name    string
		outcome cmfake.Outcome
		want    carriermessaging.SendOutcome
	}{
		{"accepted", cmfake.Accept(), carriermessaging.SendOutcome{Accepted: true}},
		{"rejected", cmfake.Reject(303, "Gsm '005511987654321' is not a number."), carriermessaging.SendOutcome{ErrorCode: "303", Detail: "Gsm '005511987654321' is not a number."}},
		{"unreachable", cmfake.Unreachable(), carriermessaging.SendOutcome{ErrorCode: "42", Detail: cmfake.UnreachableDetail, Unreachable: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client, fake := newClient(t)
			fake.Script("+5511987654321", tc.outcome)
			got, err := client.Send(context.Background(), sendRequest(rcs.Content{Text: "oi"}))
			if err != nil {
				t.Fatalf("Send: %v", err)
			}
			if got != tc.want {
				t.Fatalf("outcome = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestSendResponseMapping(t *testing.T) {
	accepted := `{"details":"Created 1 message(s)","errorCode":0,"messages":[{"to":"005511987654321","status":"Accepted","reference":"` + testReference + `","parts":1,"messageDetails":null,"messageErrorCode":0}]}`
	lost := []error{cmcom.ErrProviderResponse, carriermessaging.ErrSendOutcomeUnknown}
	cases := []struct {
		name     string
		status   int
		body     string
		want     carriermessaging.SendOutcome
		wantErrs []error
	}{
		{name: "accepted", status: 200, body: accepted, want: carriermessaging.SendOutcome{Accepted: true}},
		{name: "accepted without echoed reference", status: 200, body: `{"details":"Created 1 message(s)","errorCode":0,"messages":[{"to":"005511987654321","status":"Accepted","reference":null,"parts":1,"messageDetails":null,"messageErrorCode":0}]}`, want: carriermessaging.SendOutcome{Accepted: true}},
		{name: "partial batch rejection", status: 200, body: `{"details":"Created 0 message(s)","errorCode":201,"messages":[{"to":"005511987654321","status":"Rejected","reference":"` + testReference + `","parts":0,"messageDetails":"A body without content was found","messageErrorCode":304}]}`, want: carriermessaging.SendOutcome{ErrorCode: "304", Detail: "A body without content was found"}},
		{name: "all rejected is a 400", status: 400, body: `{"details":"Created 0 message(s)","errorCode":201,"messages":[{"to":"0044791112345a","status":"Rejected","reference":"` + testReference + `","parts":0,"messageDetails":"Gsm '0044791112345a' is not a number.","messageErrorCode":303}]}`, want: carriermessaging.SendOutcome{ErrorCode: "303", Detail: "Gsm '0044791112345a' is not a number."}},
		{name: "rejected with code 42", status: 400, body: `{"details":"Created 0 message(s)","errorCode":201,"messages":[{"to":"005511987654321","status":"Rejected","reference":"` + testReference + `","parts":0,"messageDetails":"not capable","messageErrorCode":42}]}`, want: carriermessaging.SendOutcome{ErrorCode: "42", Detail: "not capable", Unreachable: true}},
		{name: "accepted inside a 400", status: 400, body: `{"details":"","errorCode":201,"messages":[{"to":"005511987654321","status":"Accepted","reference":"` + testReference + `","messageErrorCode":0}]}`, wantErrs: []error{carriermessaging.ErrProviderRefused}},
		{name: "auth failure", status: 400, body: `{"details":"No account found for the given authentication/credentials","errorCode":101,"messages":[]}`, wantErrs: []error{carriermessaging.ErrProviderRefused}},
		{name: "invalid product token", status: 400, body: `{"details":"Invalid product token.","errorCode":103,"messages":[]}`, wantErrs: []error{carriermessaging.ErrProviderRefused}},
		{name: "malformed request", status: 400, body: `{"details":"Invalid JSON","errorCode":202,"messages":[]}`, wantErrs: []error{carriermessaging.ErrProviderRefused}},
		{name: "unauthorized status", status: 401, wantErrs: []error{carriermessaging.ErrProviderRefused}},
		{name: "forbidden status", status: 403, wantErrs: []error{carriermessaging.ErrProviderRefused}},
		{name: "unexpected redirect", status: 302, wantErrs: []error{carriermessaging.ErrProviderRefused}},
		{name: "throttled", status: 429, wantErrs: []error{carriermessaging.ErrProviderUnavailable}},
		{name: "server error", status: 500, body: `Unknown Error`, wantErrs: []error{carriermessaging.ErrSendOutcomeUnknown}},
		{name: "bad gateway", status: 502, wantErrs: []error{carriermessaging.ErrSendOutcomeUnknown}},
		{name: "service unavailable", status: 503, wantErrs: []error{carriermessaging.ErrSendOutcomeUnknown}},
		{name: "unknown message status", status: 200, body: `{"details":"","errorCode":0,"messages":[{"to":"005511987654321","status":"Queued","reference":"` + testReference + `","messageErrorCode":0}]}`, wantErrs: lost},
		{name: "unknown top level code", status: 200, body: `{"details":"","errorCode":999,"messages":[{"to":"005511987654321","status":"Accepted","reference":"` + testReference + `","messageErrorCode":0}]}`, wantErrs: lost},
		{name: "no message result", status: 200, body: `{"details":"Created 1 message(s)","errorCode":0,"messages":[]}`, wantErrs: lost},
		{name: "two message results", status: 200, body: `{"details":"","errorCode":0,"messages":[{"status":"Accepted","messageErrorCode":0},{"status":"Accepted","messageErrorCode":0}]}`, wantErrs: lost},
		{name: "another reference", status: 200, body: `{"details":"","errorCode":0,"messages":[{"to":"005511987654321","status":"Accepted","reference":"other","messageErrorCode":0}]}`, wantErrs: lost},
		{name: "accepted with an error code", status: 200, body: `{"details":"","errorCode":0,"messages":[{"to":"005511987654321","status":"Accepted","reference":"` + testReference + `","messageErrorCode":304}]}`, wantErrs: lost},
		{name: "rejected without an error code", status: 200, body: `{"details":"","errorCode":201,"messages":[{"to":"005511987654321","status":"Rejected","reference":"` + testReference + `","messageErrorCode":0}]}`, wantErrs: lost},
		{name: "unparseable body", status: 200, body: `<html>`, wantErrs: lost},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, channel := range []string{"rcs", "sms"} {
				client, fake := newClient(t)
				fake.RespondRaw(tc.status, tc.body)
				var got carriermessaging.SendOutcome
				var err error
				if channel == "rcs" {
					got, err = client.Send(context.Background(), sendRequest(rcs.Content{Text: "oi"}))
				} else {
					got, err = client.SendSMS(context.Background(), smsRequest())
				}
				if len(tc.wantErrs) == 0 {
					if err != nil || got != tc.want {
						t.Fatalf("%s: got %+v, %v; want %+v", channel, got, err, tc.want)
					}
					continue
				}
				for _, want := range tc.wantErrs {
					if !errors.Is(err, want) {
						t.Fatalf("%s: got %+v, %v; want %v", channel, got, err, want)
					}
				}
				assertOneSendClass(t, err)
			}
		})
	}
}

func assertOneSendClass(t *testing.T, err error) {
	t.Helper()
	classes := 0
	for _, class := range []error{carriermessaging.ErrProviderRefused, carriermessaging.ErrProviderUnavailable, carriermessaging.ErrSendOutcomeUnknown} {
		if errors.Is(err, class) {
			classes++
		}
	}
	if classes != 1 {
		t.Fatalf("a send failure must carry exactly one class, got %d: %v", classes, err)
	}
}

func smsRequest() carriermessaging.SMSRequest {
	return carriermessaging.SMSRequest{Reference: testReference, From: "29292", Phone: "+5511987654321", Text: "Loja: seu pedido saiu"}
}

func sendBoth(t *testing.T, client *cmcom.Client) map[string]error {
	t.Helper()
	_, rcsErr := client.Send(context.Background(), sendRequest(rcs.Content{Text: "oi"}))
	_, smsErr := client.SendSMS(context.Background(), smsRequest())
	return map[string]error{"rcs": rcsErr, "sms": smsErr}
}

func clientWithHTTP(t *testing.T, mutate func(*cmcom.Config)) (*cmcom.Client, *cmfake.Fake) {
	t.Helper()
	fake := cmfake.New()
	t.Cleanup(fake.Close)
	cfg := fake.Config()
	mutate(&cfg)
	client, err := cmcom.NewClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return client, fake
}

func TestSendTransportFailures(t *testing.T) {
	cases := []struct {
		name    string
		setup   func(t *testing.T) *cmcom.Client
		wantErr error
	}{
		{
			name: "closed listener",
			setup: func(t *testing.T) *cmcom.Client {
				client, fake := newClient(t)
				fake.Close()
				return client
			},
			wantErr: carriermessaging.ErrProviderUnavailable,
		},
		{
			name: "tls handshake failure",
			setup: func(t *testing.T) *cmcom.Client {
				server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					t.Errorf("the request must not reach the server")
				}))
				t.Cleanup(server.Close)
				client, _ := clientWithHTTP(t, func(c *cmcom.Config) {
					c.GatewayURL = server.URL
					c.HTTPClient = &http.Client{Timeout: 5 * time.Second}
				})
				return client
			},
			wantErr: carriermessaging.ErrProviderUnavailable,
		},
		{
			name: "client timeout while waiting for the answer",
			setup: func(t *testing.T) *cmcom.Client {
				client, fake := clientWithHTTP(t, func(c *cmcom.Config) {
					c.HTTPClient = &http.Client{Timeout: 150 * time.Millisecond}
				})
				fake.HangSends()
				return client
			},
			wantErr: carriermessaging.ErrSendOutcomeUnknown,
		},
		{
			name: "connection dropped before the answer",
			setup: func(t *testing.T) *cmcom.Client {
				client, fake := newClient(t)
				fake.DropSends()
				return client
			},
			wantErr: carriermessaging.ErrSendOutcomeUnknown,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := tc.setup(t)
			for channel, err := range sendBoth(t, client) {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("%s: got %v, want %v", channel, err, tc.wantErr)
				}
				assertOneSendClass(t, err)
			}
		})
	}
}

func TestSendContextDeadlineWhileWaitingIsUnknown(t *testing.T) {
	client, fake := newClient(t)
	fake.HangSends()
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	_, err := client.Send(ctx, sendRequest(rcs.Content{Text: "oi"}))
	if !errors.Is(err, carriermessaging.ErrSendOutcomeUnknown) {
		t.Fatalf("Send = %v, want ErrSendOutcomeUnknown", err)
	}
	if n := len(fake.Sends()); n != 1 {
		t.Fatalf("the request must have reached the gateway, got %d", n)
	}
}

func TestSendWithACancelledContextNeverLeaves(t *testing.T) {
	client, fake := newClient(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := client.Send(ctx, sendRequest(rcs.Content{Text: "oi"}))
	if !errors.Is(err, carriermessaging.ErrProviderUnavailable) {
		t.Fatalf("Send = %v, want ErrProviderUnavailable", err)
	}
	if n := len(fake.Sends()); n != 0 {
		t.Fatalf("gateway called %d times", n)
	}
}

func TestSendWithWrongTokenIsRefused(t *testing.T) {
	client, _ := clientWithHTTP(t, func(c *cmcom.Config) { c.ProductToken = "11111111-2222-3333-4444-555555555555" })
	for channel, err := range sendBoth(t, client) {
		if !errors.Is(err, carriermessaging.ErrProviderRefused) {
			t.Fatalf("%s: got %v, want ErrProviderRefused", channel, err)
		}
	}
}

func TestSendSMS(t *testing.T) {
	cases := []struct {
		from string
		want string
	}{
		{"29292", "29292"},
		{"+5511912345678", "5511912345678"},
		{" 5511912345678 ", "5511912345678"},
	}
	for _, tc := range cases {
		t.Run(tc.from, func(t *testing.T) {
			client, fake := newClient(t)
			req := smsRequest()
			req.From = tc.from
			got, err := client.SendSMS(context.Background(), req)
			if err != nil || got != (carriermessaging.SendOutcome{Accepted: true}) {
				t.Fatalf("SendSMS = %+v, %v", got, err)
			}
			sends := fake.Sends()
			if len(sends) != 1 || sends[0].From != tc.want {
				t.Fatalf("sends = %+v", sends)
			}
			assertJSON(t, sends[0].Raw, `{
				"from":"`+tc.want+`",
				"to":[{"number":"005511987654321"}],
				"body":{"type":"Auto","content":"Loja: seu pedido saiu"},
				"reference":"`+testReference+`",
				"allowedChannels":["SMS"]}`)
		})
	}
}

func TestSendSMSRefusals(t *testing.T) {
	cases := map[string]func(*carriermessaging.SMSRequest){
		"no route number":         func(r *carriermessaging.SMSRequest) { r.From = " " },
		"route number with words": func(r *carriermessaging.SMSRequest) { r.From = "Loja" },
		"route number too short":  func(r *carriermessaging.SMSRequest) { r.From = "12" },
		"route number too long":   func(r *carriermessaging.SMSRequest) { r.From = "12345678901234567" },
		"route number in 00 form": func(r *carriermessaging.SMSRequest) { r.From = "005511912345678" },
		"empty text":              func(r *carriermessaging.SMSRequest) { r.Text = "  " },
		"bad phone":               func(r *carriermessaging.SMSRequest) { r.Phone = "x" },
		"bad reference":           func(r *carriermessaging.SMSRequest) { r.Reference = "r" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			client, fake := newClient(t)
			req := smsRequest()
			mutate(&req)
			if _, err := client.SendSMS(context.Background(), req); !errors.Is(err, cmcom.ErrRequestInvalid) {
				t.Fatalf("SendSMS = %v, want ErrRequestInvalid", err)
			}
			if n := len(fake.Sends()); n != 0 {
				t.Fatalf("gateway called %d times", n)
			}
		})
	}
}

func TestSendSMSMapsRejection(t *testing.T) {
	client, fake := newClient(t)
	fake.Script("5511987654321", cmfake.Reject(402, "blacklisted"))
	got, err := client.SendSMS(context.Background(), smsRequest())
	if err != nil || got != (carriermessaging.SendOutcome{ErrorCode: "402", Detail: "blacklisted"}) {
		t.Fatalf("SendSMS = %+v, %v", got, err)
	}
}

func TestClientImplementsThePorts(t *testing.T) {
	client, _ := newClient(t)
	var _ carriermessaging.Provider = client
	var _ carriermessaging.SMSProvider = client
	if client.Name() != "cmcom" {
		t.Fatal(client.Name())
	}
}
