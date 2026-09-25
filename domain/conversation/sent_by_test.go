package conversation

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestSentByBuildsEachSender(t *testing.T) {
	cases := []struct {
		name      string
		sender    SentBy
		kind      SenderKind
		id        string
		direction MessageHistoryDirection
	}{
		{"contact with handle", SentByContact("5511999"), SenderContact, "5511999", MessageDirectionInbound},
		{"contact without handle", SentByContact(""), SenderContact, "", MessageDirectionInbound},
		{"person", SentByPerson("user-1"), SenderHuman, "user-1", MessageDirectionOutbound},
		{"agent", SentByAI("agent-1"), SenderAI, "ai:agent-1", MessageDirectionOutbound},
		{"agent already prefixed", SentByAI("ai:agent-1"), SenderAI, "ai:agent-1", MessageDirectionOutbound},
		{"ai without an agent record", SentByAI(""), SenderAI, "", MessageDirectionOutbound},
		{"workflow", SentByWorkflow("wf-1"), SenderWorkflow, "workflow:wf-1", MessageDirectionOutbound},
		{"campaign", SentByCampaign("camp-1"), SenderCampaign, "campaign:camp-1", MessageDirectionOutbound},
		{"outside vozko", SentExternally(), SenderExternal, "", MessageDirectionOutbound},
		{"vozko itself", SentBySystem(), SenderSystem, "", MessageDirectionOutbound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !tc.sender.Valid() {
				t.Fatalf("%+v is not valid", tc.sender)
			}
			if tc.sender.Kind() != tc.kind || tc.sender.ID() != tc.id {
				t.Fatalf("got %s %q, want %s %q", tc.sender.Kind(), tc.sender.ID(), tc.kind, tc.id)
			}
			if tc.sender.Direction() != tc.direction {
				t.Fatalf("direction = %s, want %s", tc.sender.Direction(), tc.direction)
			}
		})
	}
}

func TestSentByRefusesAnUnidentifiedSender(t *testing.T) {
	for name, sender := range map[string]SentBy{
		"zero value":            {},
		"person without id":     SentByPerson("  "),
		"workflow without id":   SentByWorkflow(""),
		"campaign without id":   SentByCampaign(""),
		"legacy unknown row":    RestoreSentBy("unknown", ""),
		"unknown kind":          RestoreSentBy("robot", "x"),
		"restored person no id": RestoreSentBy("human", ""),
	} {
		t.Run(name, func(t *testing.T) {
			if sender.Valid() {
				t.Fatalf("%+v must not be a valid sender", sender)
			}
			if sender.Direction() != MessageDirectionUnknown {
				t.Fatalf("an unidentified sender has no direction, got %s", sender.Direction())
			}
		})
	}
}

func TestRestoreSentByReadsWhatWasStored(t *testing.T) {
	for _, stored := range []SentBy{
		SentByContact("5511999"), SentByPerson("user-1"), SentByAI("agent-1"), SentByAI(""),
		SentByWorkflow("wf-1"), SentByCampaign("camp-1"), SentExternally(), SentBySystem(),
	} {
		restored := RestoreSentBy(string(stored.Kind()), stored.ID())
		if restored != stored {
			t.Fatalf("restored %+v, want %+v", restored, stored)
		}
	}
}

func TestMessageWithoutASenderIsRefused(t *testing.T) {
	msg := &Message{ID: "m-1", EntryID: "e-1", EntryType: "whatsapp", From: "a", Text: "oi"}

	if err := msg.Validate(); !errors.Is(err, ErrMessageSenderRequired) {
		t.Fatalf("Validate() = %v, want %v", err, ErrMessageSenderRequired)
	}

	msg.SentBy = SentByPerson("user-1")
	if err := msg.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

func TestResolvedDirectionFollowsTheSender(t *testing.T) {
	outboundMedia := &Message{MessageType: MessageTypeMedia, SentBy: SentByWorkflow("wf-1")}
	if outboundMedia.ResolvedDirection() != MessageDirectionOutbound || outboundMedia.FromCustomer() {
		t.Fatal("workflow media is not a customer message")
	}

	phoneSend := &Message{MessageType: MessageTypeUserMessage, Direction: MessageDirectionInbound, SentBy: SentExternally()}
	if phoneSend.ResolvedDirection() != MessageDirectionOutbound {
		t.Fatal("a message sent from the phone is outbound whatever its stored direction")
	}

	callFromCustomer := &Message{MessageType: MessageTypeCallReceived, SentBy: SentByContact("5511999")}
	if !callFromCustomer.FromCustomer() {
		t.Fatal("a call the customer placed is inbound")
	}

	legacy := &Message{MessageType: MessageTypeUserMessage}
	if legacy.ResolvedDirection() != MessageDirectionInbound {
		t.Fatal("rows stored before senders existed keep their inferred direction")
	}
}

func TestAnAttributedSenderClaimsAnExternalEcho(t *testing.T) {
	cases := []struct {
		name     string
		ours     SentBy
		existing SentBy
		claims   bool
	}{
		{"workflow over its own echo", SentByWorkflow("wf-1"), SentExternally(), true},
		{"person over their own echo", SentByPerson("user-1"), SentExternally(), true},
		{"echo never replaces an attributed send", SentExternally(), SentByWorkflow("wf-1"), false},
		{"echo over echo", SentExternally(), SentExternally(), false},
		{"attributed over attributed", SentByPerson("user-1"), SentByAI("agent-1"), false},
		{"unidentified never claims", SentBy{}, SentExternally(), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.ours.Claims(tc.existing); got != tc.claims {
				t.Fatalf("Claims() = %v, want %v", got, tc.claims)
			}
		})
	}
}

func TestSentByParticipantIsTheBareIdentity(t *testing.T) {
	for _, tc := range []struct {
		sender SentBy
		want   string
	}{
		{SentByPerson("user-1"), "user-1"},
		{SentByAI("agent-1"), "agent-1"},
		{SentByAI(""), ""},
		{SentByWorkflow("wf-1"), "wf-1"},
		{SentByCampaign("camp-1"), "camp-1"},
		{SentByContact("5511999"), "5511999"},
		{SentExternally(), ""},
		{SentBySystem(), ""},
	} {
		if got := tc.sender.Participant(); got != tc.want {
			t.Errorf("%s Participant() = %q, want %q", tc.sender.Kind(), got, tc.want)
		}
	}
}

func TestWhichSendersAnswerTheCustomer(t *testing.T) {
	answers := map[SenderKind]bool{}
	for _, kind := range ReplySenderKinds() {
		answers[kind] = true
	}
	outbound := map[SenderKind]bool{}
	for _, kind := range OutboundSenderKinds() {
		outbound[kind] = true
	}

	for kind, want := range map[SenderKind][2]bool{
		SenderHuman:    {true, true},
		SenderAI:       {true, true},
		SenderWorkflow: {true, true},
		SenderExternal: {true, true},
		SenderCampaign: {false, true},
		SenderSystem:   {false, false},
		SenderContact:  {false, false},
	} {
		if answers[kind] != want[0] || outbound[kind] != want[1] {
			t.Errorf("%s: answers=%v outbound=%v, want answers=%v outbound=%v", kind, answers[kind], outbound[kind], want[0], want[1])
		}
	}
}

func TestWhichSendersSpeakForTheBusiness(t *testing.T) {
	for _, tc := range []struct {
		sender SentBy
		want   bool
	}{
		{SentByPerson("user-1"), true},
		{SentByAI(""), true},
		{SentByWorkflow("wf-1"), true},
		{SentByCampaign("camp-1"), true},
		{SentExternally(), true},
		{SentBySystem(), false},
		{SentByContact("5511"), false},
		{SentBy{}, false},
	} {
		if got := tc.sender.FromTheBusiness(); got != tc.want {
			t.Errorf("%s FromTheBusiness() = %v, want %v", tc.sender.Kind(), got, tc.want)
		}
	}
}

func TestTheSenderTravelsWithTheMessage(t *testing.T) {
	sent := Message{ID: "m-1", SentBy: SentByWorkflow("wf-1")}

	encoded, err := json.Marshal(sent)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"sentBy":{"kind":"workflow","id":"workflow:wf-1"}`) {
		t.Fatalf("the sender is not in the message JSON: %s", encoded)
	}

	var received Message
	if err := json.Unmarshal(encoded, &received); err != nil {
		t.Fatal(err)
	}
	if received.SentBy != sent.SentBy {
		t.Fatalf("relayed sender %+v, want %+v", received.SentBy, sent.SentBy)
	}
}
