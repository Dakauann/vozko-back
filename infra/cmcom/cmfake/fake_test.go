package cmfake_test

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"vozko/domain/carriermessaging"
	"vozko/domain/rcs"
	"vozko/infra/cmcom"
	"vozko/infra/cmcom/cmfake"
)

func TestStatusReportJSONDecodes(t *testing.T) {
	at := time.Date(2026, 7, 1, 12, 30, 15, 0, time.UTC)
	cases := []struct {
		name   string
		report cmfake.StatusReport
		want   carriermessaging.StatusReport
	}{
		{"delivered", cmfake.StatusReport{Reference: "ref1", Phone: "5511987654321", Status: carriermessaging.StatusDelivered, At: at},
			carriermessaging.StatusReport{Reference: "ref1", Status: carriermessaging.StatusDelivered, ReceivedAt: at}},
		{"read with operator", cmfake.StatusReport{Reference: "ref2", Phone: "5511987654321", Status: carriermessaging.StatusRead, Operator: "72405", At: at},
			carriermessaging.StatusReport{Reference: "ref2", Status: carriermessaging.StatusRead, Operator: "72405", ReceivedAt: at}},
		{"unreachable", cmfake.StatusReport{Reference: "ref3", Phone: "5511987654321", Status: carriermessaging.StatusFailed, ErrorCode: cmcom.UnreachableErrorCode, ErrorDescription: "not capable", At: at},
			carriermessaging.StatusReport{Reference: "ref3", Status: carriermessaging.StatusFailed, ErrorCode: "42", ErrorDetail: "not capable", Unreachable: true, ReceivedAt: at}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, err := cmfake.StatusReportJSON(tc.report)
			if err != nil {
				t.Fatalf("StatusReportJSON: %v", err)
			}
			got, err := cmcom.DecodeStatusReports("application/json", body)
			if err != nil {
				t.Fatalf("decode: %v\n%s", err, body)
			}
			if len(got) != 1 || !got[0].ReceivedAt.Equal(tc.want.ReceivedAt) {
				t.Fatalf("got %+v", got)
			}
			got[0].ReceivedAt = tc.want.ReceivedAt
			if !reflect.DeepEqual(got[0], tc.want) {
				t.Fatalf("got %+v\nwant %+v", got[0], tc.want)
			}
		})
	}
	if _, err := cmfake.StatusReportJSON(cmfake.StatusReport{Reference: "r", Phone: "5511987654321", Status: carriermessaging.StatusExpired, At: at}); err == nil {
		t.Fatalf("a status CM never reports must be refused")
	}
	if _, err := cmfake.StatusReportJSON(cmfake.StatusReport{Reference: "r", Phone: "x", Status: carriermessaging.StatusRead, At: at}); err == nil {
		t.Fatalf("a bad phone must be refused")
	}
}

func TestInboundJSONDecodes(t *testing.T) {
	at := time.Date(2026, 7, 1, 12, 30, 15, 0, time.UTC)
	cases := []struct {
		name string
		in   cmfake.InboundMessage
		want carriermessaging.Inbound
	}{
		{"text reply", cmfake.InboundMessage{Reference: "in-1", Phone: "5511987654321", Name: "Ana", Channel: carriermessaging.ChannelRCS, To: "agent_1", Text: "SAIR", InReplyTo: "ref1", At: at},
			carriermessaging.Inbound{ProviderMessageID: "in-1", Phone: "5511987654321", ContactName: "Ana", Channel: carriermessaging.ChannelRCS, To: "agent_1", Text: "SAIR", InReplyTo: "ref1", ReceivedAt: at}},
		{"media", cmfake.InboundMessage{Reference: "in-2", Phone: "5511987654321", Channel: carriermessaging.ChannelRCS, To: "agent_1", MediaURL: "https://media.example.com/x.png", MediaType: "image/png", MediaTitle: "x", At: at},
			carriermessaging.Inbound{ProviderMessageID: "in-2", Phone: "5511987654321", Channel: carriermessaging.ChannelRCS, To: "agent_1", Media: &carriermessaging.InboundMedia{URL: "https://media.example.com/x.png", MIMEType: "image/png", Name: "x"}, ReceivedAt: at}},
		{"reply chip", cmfake.InboundMessage{Reference: "in-3", Phone: "5511987654321", Channel: carriermessaging.ChannelRCS, To: "agent_1", Text: "Quero", Postback: "vz:m0", At: at},
			carriermessaging.Inbound{ProviderMessageID: "in-3", Phone: "5511987654321", Channel: carriermessaging.ChannelRCS, To: "agent_1", Text: "Quero", Postback: "vz:m0", ReceivedAt: at}},
		{"sms on the shared short code", cmfake.InboundMessage{Reference: "in-4", Phone: "5511987654321", Channel: carriermessaging.ChannelSMS, To: "29292", Text: "SAIR", At: at},
			carriermessaging.Inbound{ProviderMessageID: "in-4", Phone: "5511987654321", Channel: carriermessaging.ChannelSMS, To: "29292", Text: "SAIR", ReceivedAt: at}},
		{"sms on a dedicated number", cmfake.InboundMessage{Reference: "in-5", Phone: "5511987654321", Channel: carriermessaging.ChannelSMS, To: "5511912345678", Text: "Oi", At: at},
			carriermessaging.Inbound{ProviderMessageID: "in-5", Phone: "5511987654321", Channel: carriermessaging.ChannelSMS, To: "5511912345678", Text: "Oi", ReceivedAt: at}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, err := cmfake.InboundJSON(tc.in)
			if err != nil {
				t.Fatalf("InboundJSON: %v", err)
			}
			got, err := cmcom.DecodeInbound(body)
			if err != nil {
				t.Fatalf("decode: %v\n%s", err, body)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %+v\nwant %+v", got, tc.want)
			}
		})
	}
}

func TestInboundJSONRefusesAnUnknownChannel(t *testing.T) {
	for _, channel := range []carriermessaging.Channel{"", "fax"} {
		in := cmfake.InboundMessage{Reference: "in", Phone: "5511987654321", Channel: channel, To: "29292", Text: "x", At: time.Now()}
		if _, err := cmfake.InboundJSON(in); err == nil {
			t.Fatalf("channel %q must be refused", channel)
		}
	}
}

func TestFakeRecordsSMSSends(t *testing.T) {
	fake := cmfake.New()
	defer fake.Close()
	client, err := cmcom.NewClient(fake.Config())
	if err != nil {
		t.Fatal(err)
	}
	req := carriermessaging.SMSRequest{Reference: fmt.Sprintf("%032d", 7), From: "29292", Phone: "+5511987654321", Text: "Loja: oi"}
	if _, err := client.SendSMS(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	sends := fake.Sends()
	if len(sends) != 1 {
		t.Fatalf("sends = %d", len(sends))
	}
	s := sends[0]
	if s.From != "29292" || s.Phone != "5511987654321" || s.BodyContent != "Loja: oi" || len(s.AllowedChannels) != 1 || s.AllowedChannels[0] != "SMS" {
		t.Fatalf("send = %+v", s)
	}
}

func TestSuggestionClickJSONDecodes(t *testing.T) {
	at := time.Date(2026, 7, 1, 12, 30, 15, 0, time.UTC)
	click := cmfake.SuggestionClick{EventID: "ev-1", Reference: "ref1", Phone: "5511987654321", To: "agent_1", Postback: "vz:c0b1", Label: "Ver site", At: at}
	body, err := cmfake.SuggestionClickJSON(click)
	if err != nil {
		t.Fatal(err)
	}
	got, ok, err := cmcom.DecodeEvent(body)
	if err != nil || !ok {
		t.Fatalf("decode: %v %v\n%s", ok, err, body)
	}
	want := carriermessaging.SuggestionClick{EventID: "ev-1", Reference: "ref1", Phone: "5511987654321", To: "agent_1", Postback: "vz:c0b1", Label: "Ver site", OccurredAt: at}
	if got != want {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestFakeRecordsConcurrentSends(t *testing.T) {
	fake := cmfake.New()
	defer fake.Close()
	client, err := cmcom.NewClient(fake.Config())
	if err != nil {
		t.Fatal(err)
	}
	fake.Script("5511900000003", cmfake.Reject(402, "blacklisted"))
	const n = 20
	var wg sync.WaitGroup
	outcomes := make([]carriermessaging.SendOutcome, n)
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			outcomes[i], errs[i] = client.Send(context.Background(), carriermessaging.SendRequest{
				Reference:       fmt.Sprintf("%032d", i),
				SenderProfileID: "agent_1",
				Phone:           fmt.Sprintf("55119000000%02d", i),
				Content:         rcs.Content{Text: "oi"},
				Validity:        time.Hour,
			})
		}(i)
	}
	wg.Wait()
	for i := range n {
		if errs[i] != nil {
			t.Fatalf("send %d: %v", i, errs[i])
		}
		if i == 3 && outcomes[i].Accepted {
			t.Fatalf("scripted rejection ignored")
		}
		if i != 3 && !outcomes[i].Accepted {
			t.Fatalf("send %d rejected: %+v", i, outcomes[i])
		}
	}
	sends := fake.Sends()
	if len(sends) != n {
		t.Fatalf("recorded %d sends, want %d", len(sends), n)
	}
	seen := map[string]bool{}
	for _, s := range sends {
		seen[s.Reference] = true
		if s.Phone == "" || s.To == "" || len(s.AllowedChannels) != 1 || s.AllowedChannels[0] != "RCS" {
			t.Fatalf("send = %+v", s)
		}
	}
	if len(seen) != n {
		t.Fatalf("references recorded = %d", len(seen))
	}
	fake.Reset()
	if len(fake.Requests()) != 0 || len(fake.Sends()) != 0 {
		t.Fatalf("Reset must clear the records")
	}
}

func TestRequestsAreCopies(t *testing.T) {
	fake := cmfake.New()
	defer fake.Close()
	client, err := cmcom.NewClient(fake.Config())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.SendSMS(context.Background(), carriermessaging.SMSRequest{Reference: fmt.Sprintf("%032d", 1), From: "29292", Phone: "5511987654321", Text: "oi"}); err != nil {
		t.Fatal(err)
	}
	first := fake.Requests()
	first[0].Body[0] = 'X'
	if fake.Requests()[0].Body[0] == 'X' {
		t.Fatalf("Requests must return copies")
	}
}
