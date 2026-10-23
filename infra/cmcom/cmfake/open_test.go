package cmfake_test

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"vozko/domain/carriermessaging"
	"vozko/domain/rcs"
	"vozko/infra/cmcom"
	"vozko/infra/cmcom/cmfake"
)

const (
	openToken   = "9f8e7d6c-5b4a-4392-8170-6f5e4d3c2b1a"
	openAccount = "1a2b3c4d-5e6f-4071-8293-a4b5c6d7e8f9"
)

type sendLog struct {
	mu      sync.Mutex
	results []cmfake.SendResult
}

func (l *sendLog) add(r cmfake.SendResult) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.results = append(l.results, r)
}

func (l *sendLog) all() []cmfake.SendResult {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]cmfake.SendResult(nil), l.results...)
}

func openFake(t *testing.T, log *sendLog) *cmcom.Client {
	t.Helper()
	fake, err := cmfake.Open(cmfake.Options{
		ProductToken: openToken,
		AccountID:    openAccount,
		Outcome: func(phone string) cmfake.Outcome {
			if strings.HasSuffix(phone, "3") {
				return cmfake.Reject(403, "Message rejected")
			}
			return cmfake.Accept()
		},
		OnSend: log.add,
		ProfileProgression: []cmfake.ProfileStatus{
			{EntityStatus: "PENDING"},
			{EntityStatus: "CUSTOMER_ACTION_REQUIRED"},
			{EntityStatus: "ACTIVE"},
		},
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	client, err := cmcom.NewClient(cmcom.Config{ProductToken: openToken, AccountID: openAccount, GatewayURL: server.URL, APIURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestOpenRefusesBadCredentials(t *testing.T) {
	cases := []cmfake.Options{
		{ProductToken: "", AccountID: openAccount},
		{ProductToken: openToken, AccountID: "account"},
		{ProductToken: "token", AccountID: openAccount},
	}
	for i, opts := range cases {
		if _, err := cmfake.Open(opts); err == nil {
			t.Fatalf("case %d: Open must refuse %+v", i, opts)
		}
	}
}

func TestOpenScriptsOutcomesAndReportsEverySend(t *testing.T) {
	log := &sendLog{}
	client := openFake(t, log)
	accepted, err := client.Send(context.Background(), carriermessaging.SendRequest{
		Reference: fmt.Sprintf("%032d", 1), SenderProfileID: "agent_1", Phone: "5511987654320",
		Content: rcs.Content{Text: "oi"}, Validity: time.Hour,
	})
	if err != nil || !accepted.Accepted {
		t.Fatalf("Send = %+v, %v", accepted, err)
	}
	rejected, err := client.SendSMS(context.Background(), carriermessaging.SMSRequest{
		Reference: fmt.Sprintf("%032d", 2), From: "29292", Phone: "5511987654323", Text: "Loja: oi",
	})
	if err != nil || rejected != (carriermessaging.SendOutcome{ErrorCode: "403", Detail: "Message rejected"}) {
		t.Fatalf("SendSMS = %+v, %v", rejected, err)
	}
	results := log.all()
	if len(results) != 2 {
		t.Fatalf("results = %+v", results)
	}
	first, second := results[0], results[1]
	if !first.Accepted || first.Message.Phone != "5511987654320" || first.Message.Reference != fmt.Sprintf("%032d", 1) || first.Message.AllowedChannels[0] != "RCS" {
		t.Fatalf("first = %+v", first)
	}
	if second.Accepted || second.ErrorCode != 403 || second.Detail != "Message rejected" || second.Message.From != "29292" {
		t.Fatalf("second = %+v", second)
	}
}

func TestOpenProfilesFollowTheProgression(t *testing.T) {
	client := openFake(t, &sendLog{})
	s := rcs.Sender{
		BrandName: "Padaria Sol", Description: "Atualizações da Padaria Sol",
		LogoURL: "https://cdn.example.com/logo.png", BannerURL: "https://cdn.example.com/banner.png",
		Color: "#FF8800", UseCase: rcs.UseCaseTransactional, Sector: rcs.SectorRetail,
		ContactName: "Maria", ContactEmail: "maria@padariasol.com.br", ContactPhone: "+5511987654321",
		WebsiteURL: "https://padariasol.com.br", PrivacyURL: "https://padariasol.com.br/p", TermsURL: "https://padariasol.com.br/t",
		CNPJ: "12345678000195", BillingCategory: rcs.BillingNonConversational,
	}
	sub, err := client.SubmitSender(context.Background(), s)
	if err != nil {
		t.Fatalf("SubmitSender: %v", err)
	}
	s.ProviderRequestID, s.ProviderProfileID = sub.RequestID, sub.ProfileID
	want := []rcs.ProviderState{rcs.ProviderPending, rcs.ProviderAwaitingBrand, rcs.ProviderLaunched, rcs.ProviderLaunched}
	for i, w := range want {
		got, err := client.SenderStatus(context.Background(), s)
		if err != nil || got.State != w {
			t.Fatalf("poll %d = %+v, %v; want %s", i, got, err, w)
		}
	}
}
