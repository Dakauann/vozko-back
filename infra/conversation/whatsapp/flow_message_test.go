package whatsapp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"vozko/domain/conversation"
)

type flowRequest struct {
	path string
	auth string
	key  string
	body map[string]any
}

func flowGraph(t *testing.T, status int, reply string, dialog360 bool) (*Client, *[]flowRequest) {
	t.Helper()
	var requests []flowRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("body %s: %v", raw, err)
		}
		requests = append(requests, flowRequest{path: r.URL.Path, auth: r.Header.Get("Authorization"), key: r.Header.Get("D360-API-KEY"), body: body})
		w.WriteHeader(status)
		_, _ = w.Write([]byte(reply))
	}))
	t.Cleanup(server.Close)
	cfg := Config{BaseURL: server.URL, PhoneNumberID: "PNID", WABAId: "WABA", AccessToken: "tok"}
	if dialog360 {
		cfg.AuthHeaderName = "D360-API-KEY"
		cfg.OmitPhoneNumberInPath = true
		cfg.TemplatesChannelScoped = true
	}
	return NewClient(cfg).(*Client), &requests
}

func decodedJSON(t *testing.T, raw string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestSendFlowMessageBuildsTheInteractiveFlowPayload(t *testing.T) {
	cases := []struct {
		name  string
		input conversation.SendFlowMessageInput
		want  string
	}{
		{
			name:  "every part and draft mode",
			input: conversation.SendFlowMessageInput{To: "+55 11 99999-0000", FlowID: "F1", FlowToken: "tok_abc", CTA: "Responder", Header: "Cadastro", Body: "Preencha o formulário", Footer: "Leva 1 minuto", Draft: true},
			want: `{"messaging_product":"whatsapp","recipient_type":"individual","to":"5511999990000","type":"interactive","interactive":{
				"type":"flow","header":{"type":"text","text":"Cadastro"},"body":{"text":"Preencha o formulário"},"footer":{"text":"Leva 1 minuto"},
				"action":{"name":"flow","parameters":{"flow_message_version":"3","flow_token":"tok_abc","flow_id":"F1","flow_cta":"Responder","mode":"draft","flow_action":"navigate"}}}}`,
		},
		{
			name:  "body only and published",
			input: conversation.SendFlowMessageInput{To: "5511999990000", FlowID: "F1", FlowToken: "tok_abc", CTA: "Responder", Body: "Preencha"},
			want: `{"messaging_product":"whatsapp","recipient_type":"individual","to":"5511999990000","type":"interactive","interactive":{
				"type":"flow","body":{"text":"Preencha"},
				"action":{"name":"flow","parameters":{"flow_message_version":"3","flow_token":"tok_abc","flow_id":"F1","flow_cta":"Responder","flow_action":"navigate"}}}}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client, requests := flowGraph(t, http.StatusOK, `{"contacts":[{"wa_id":"5511999990000"}],"messages":[{"id":"wamid.flow","message_status":"accepted"}]}`, false)

			out, err := client.SendFlowMessage(context.Background(), tc.input)

			if err != nil {
				t.Fatal(err)
			}
			if out.MessageID != "wamid.flow" || out.MessageStatus != "accepted" || out.ContactWaID != "5511999990000" {
				t.Fatalf("out = %+v", out)
			}
			if len(*requests) != 1 {
				t.Fatalf("requests = %d", len(*requests))
			}
			got := (*requests)[0]
			if got.path != "/PNID/messages" || got.auth != "Bearer tok" {
				t.Fatalf("request = %s auth %q", got.path, got.auth)
			}
			if want := decodedJSON(t, tc.want); !reflect.DeepEqual(got.body, want) {
				gotJSON, _ := json.Marshal(got.body)
				t.Fatalf("body = %s", gotJSON)
			}
		})
	}
}

func TestSendFlowMessageUsesTheDialog360Path(t *testing.T) {
	client, requests := flowGraph(t, http.StatusOK, `{"messages":[{"id":"wamid.flow"}]}`, true)

	_, err := client.SendFlowMessage(context.Background(), conversation.SendFlowMessageInput{To: "5511999990000", FlowID: "F1", FlowToken: "tok_abc", CTA: "Responder", Body: "Preencha"})

	if err != nil {
		t.Fatal(err)
	}
	got := (*requests)[0]
	if got.path != "/messages" || got.key != "tok" || got.auth != "" {
		t.Fatalf("request = %+v", got)
	}
}

func TestSendFlowMessageRefusals(t *testing.T) {
	valid := conversation.SendFlowMessageInput{To: "5511999990000", FlowID: "F1", FlowToken: "tok_abc", CTA: "Responder", Body: "Preencha"}
	withoutToken := valid
	withoutToken.FlowToken = ""
	cases := []struct {
		name    string
		input   conversation.SendFlowMessageInput
		status  int
		wantErr error
		wantReq int
	}{
		{name: "missing token", input: withoutToken, status: http.StatusOK, wantErr: conversation.ErrWhatsAppFlowTokenRequired},
		{name: "meta refuses", input: valid, status: http.StatusBadRequest, wantReq: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client, requests := flowGraph(t, tc.status, `{"error":{"message":"Flow is blocked","code":132068}}`, false)

			out, err := client.SendFlowMessage(context.Background(), tc.input)

			if err == nil || out != nil {
				t.Fatalf("out = %+v err = %v", out, err)
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v", err)
			}
			if len(*requests) != tc.wantReq {
				t.Fatalf("requests = %d", len(*requests))
			}
		})
	}
}

func TestADisabledClientSendsNoFlowMessage(t *testing.T) {
	client := &Client{}

	_, err := client.SendFlowMessage(context.Background(), conversation.SendFlowMessageInput{To: "5511999990000", FlowID: "F1", FlowToken: "tok_abc", CTA: "Responder", Body: "Preencha"})

	if !errors.Is(err, conversation.ErrWhatsAppClientDisabled) {
		t.Fatalf("err = %v", err)
	}
}
