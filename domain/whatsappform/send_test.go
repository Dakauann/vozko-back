package whatsappform

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"vozko/domain/conversation"
)

var fixedNow = time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

func TestMintTokenIsOpaqueAndParsesBack(t *testing.T) {
	token, err := MintToken(bytes.NewReader(bytes.Repeat([]byte{7}, 16)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(token, tokenPrefix) || len(token) != len(tokenPrefix)+26 || !IsVozkoToken(token) {
		t.Fatalf("token = %q", token)
	}
	other, _ := MintToken(bytes.NewReader(bytes.Repeat([]byte{8}, 16)))
	if other == token {
		t.Fatal("different randomness gives a different token")
	}
	for _, foreign := range []string{"", "unused", "vf1_", "vf1_!!!!", "vf2_" + token[len(tokenPrefix):], token + "x"} {
		if IsVozkoToken(foreign) {
			t.Errorf("%q must not pass as a Vozko token", foreign)
		}
	}
	if _, err := MintToken(bytes.NewReader([]byte{1})); err == nil {
		t.Fatal("a short random source must fail, never yield a weak token")
	}
}

func sentSend() Send {
	return Send{
		ID: "send-1", WorkspaceID: "ws-1", FormID: "form-1", VersionID: "v-1", Token: "vf1_abc",
		BusinessPhoneID: "phone-1", ContactNumber: "5511987654321", MessageID: "wamid.out", Status: SendSent,
	}
}

func TestMatchResponse(t *testing.T) {
	send := sentSend()
	cases := []struct {
		name    string
		send    *Send
		inbound Inbound
		want    MatchResult
	}{
		{"by token from the same contact", &send, Inbound{Token: "vf1_abc", BusinessPhoneID: "phone-1", ContactNumber: "5511987654321"}, MatchByToken},
		{"by reply context when the token is missing", &send, Inbound{ContextMessageID: "wamid.out", BusinessPhoneID: "phone-1", ContactNumber: "5511987654321"}, MatchByContext},
		{"no send found", nil, Inbound{Token: "vf1_zzz", BusinessPhoneID: "phone-1", ContactNumber: "5511987654321"}, UnmatchedUnknownToken},
		{"no token and no context", nil, Inbound{BusinessPhoneID: "phone-1", ContactNumber: "5511987654321"}, UnmatchedNoToken},
		{"another contact", &send, Inbound{Token: "vf1_abc", BusinessPhoneID: "phone-1", ContactNumber: "5511900000000"}, UnmatchedSender},
		{"another business number", &send, Inbound{Token: "vf1_abc", BusinessPhoneID: "phone-2", ContactNumber: "5511987654321"}, UnmatchedSender},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := MatchResponse(tc.send, tc.inbound); got != tc.want {
				t.Fatalf("MatchResponse = %q, want %q", got, tc.want)
			}
		})
	}
	if !MatchByToken.Matched() || !MatchByContext.Matched() || UnmatchedSender.Matched() || UnmatchedNoToken.Matched() {
		t.Fatal("only token and context matches count")
	}
}

func TestSendTransitions(t *testing.T) {
	s := Send{Status: SendPrepared}
	if err := s.MarkSent("wamid.1", fixedNow); err != nil || s.Status != SendSent || s.MessageID != "wamid.1" || s.SentAt == nil {
		t.Fatalf("prepared to sent: %+v %v", s, err)
	}
	if err := s.MarkResponded(fixedNow); err != nil || s.Status != SendResponded {
		t.Fatalf("sent to responded: %+v %v", s, err)
	}
	if err := s.MarkResponded(fixedNow); err != nil {
		t.Fatal("a second answer to the same send keeps it responded")
	}
	failed := Send{Status: SendPrepared}
	if err := failed.MarkFailed("outside_window"); err != nil || failed.Status != SendFailed || failed.FailureReason != "outside_window" {
		t.Fatalf("prepared to failed: %+v %v", failed, err)
	}
	if err := failed.MarkSent("wamid.2", fixedNow); !errors.Is(err, ErrSendTransition) {
		t.Fatal("a failed send never becomes sent")
	}
	if err := (&Send{Status: SendPrepared}).MarkSent(" ", fixedNow); !errors.Is(err, ErrSendTransition) {
		t.Fatal("a send without the provider's message id is not sent")
	}
}

func TestPublicationLifecycle(t *testing.T) {
	cases := []struct {
		from, to PublicationStatus
		allowed  bool
	}{
		{PublicationCreating, PublicationPublished, true},
		{PublicationCreating, PublicationFailed, true},
		{PublicationPublished, PublicationThrottled, true},
		{PublicationThrottled, PublicationBlocked, true},
		{PublicationBlocked, PublicationThrottled, true},
		{PublicationThrottled, PublicationPublished, true},
		{PublicationPublished, PublicationDeprecated, true},
		{PublicationDeprecated, PublicationPublished, false},
		{PublicationBlocked, PublicationPublished, false},
		{PublicationFailed, PublicationCreating, true},
		{PublicationPublished, PublicationCreating, false},
	}
	for _, tc := range cases {
		if got := tc.from.CanMoveTo(tc.to); got != tc.allowed {
			t.Errorf("%s to %s = %v, want %v", tc.from, tc.to, got, tc.allowed)
		}
	}
	for status, sendable := range map[PublicationStatus]bool{
		PublicationPublished: true, PublicationThrottled: true, PublicationBlocked: false,
		PublicationDeprecated: false, PublicationCreating: false, PublicationDraft: false, PublicationFailed: false,
	} {
		if status.Sendable() != sendable {
			t.Errorf("%s sendable = %v", status, status.Sendable())
		}
	}
	if got := PublicationStatusFromMeta("PUBLISHED"); got != PublicationPublished {
		t.Fatalf("PUBLISHED = %q", got)
	}
	if got := PublicationStatusFromMeta("SOMETHING_NEW"); got != "" {
		t.Fatalf("an unknown Meta status maps to nothing, got %q", got)
	}
}

func TestSendCopyRules(t *testing.T) {
	ok := SendCopy{Body: "Preencha seus dados", CTA: "Abrir formulário"}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	cases := []SendCopy{
		{Body: " ", CTA: "Abrir"},
		{Body: "x", CTA: ""},
		{Body: "x", CTA: strings.Repeat("a", conversation.WhatsAppFlowCTAMaxLen+1)},
		{Body: "x", CTA: "Abrir 🙂"},
		{Body: strings.Repeat("a", conversation.WhatsAppButtonBodyMaxLen+1), CTA: "Abrir"},
		{Body: "x", CTA: "Abrir", Header: strings.Repeat("a", conversation.WhatsAppHeaderTextMaxLen+1)},
		{Body: "x", CTA: "Abrir", Footer: strings.Repeat("a", conversation.WhatsAppFooterMaxLen+1)},
	}
	for _, c := range cases {
		if err := c.Validate(); !errors.Is(err, ErrCopyInvalid) {
			t.Errorf("%+v must be refused, got %v", c, err)
		}
	}
}

func TestMetaFlowNameIsUniquePerFormAndBounded(t *testing.T) {
	if got := MetaFlowName("Cadastro", "0f8c2d1e-aaaa-bbbb"); got != "Cadastro · Vozko 0f8c2d1e" {
		t.Fatalf("name = %q", got)
	}
	long := MetaFlowName(strings.Repeat("é", 100), "0f8c2d1e-aaaa")
	if !strings.HasPrefix(long, strings.Repeat("é", 60)+" · Vozko") {
		t.Fatalf("a long name is cut at 60 characters, got %q", long)
	}
}

func TestPreviewFlowNameNeverCollidesWithThePublishedOne(t *testing.T) {
	id := "0f8c2d1e-aaaa"
	if PreviewFlowName(id) == MetaFlowName("Prévia", id) || PreviewFlowName(id) != "Vozko · prévia 0f8c2d1e" {
		t.Fatalf("preview name = %q", PreviewFlowName(id))
	}
}

func TestARepublishedFlowGetsANameNoDeprecatedFlowHolds(t *testing.T) {
	first := MetaFlowName("Cadastro", "0f8c2d1e-aaaa")
	again := RepublishedFlowName("Cadastro", "0f8c2d1e-aaaa", 3)
	if again == first || again != first+" · v3" {
		t.Fatalf("names = %q, %q", first, again)
	}
}

func TestAnAnswerProvesDeliveryEvenWhenTheSendLookedFailed(t *testing.T) {
	for _, status := range []SendStatus{SendPrepared, SendFailed} {
		s := Send{Status: status}
		if err := s.MarkResponded(fixedNow); err != nil || s.Status != SendResponded {
			t.Fatalf("%s to responded: %+v %v", status, s, err)
		}
	}
}

func TestTheSameContactMatchesWithOrWithoutTheMobileNinthDigit(t *testing.T) {
	send := sentSend()
	for _, from := range []string{"5511987654321", "551187654321"} {
		in := Inbound{Token: "vf1_abc", BusinessPhoneID: "phone-1", ContactNumber: from}
		if got := MatchResponse(&send, in); got != MatchByToken {
			t.Errorf("%s: %q", from, got)
		}
	}
	other := Inbound{Token: "vf1_abc", BusinessPhoneID: "phone-1", ContactNumber: "5511987654320"}
	if MatchResponse(&send, other) != UnmatchedSender {
		t.Fatal("a different number never matches")
	}
}
