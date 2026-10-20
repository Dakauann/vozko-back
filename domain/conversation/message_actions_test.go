package conversation

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type messageActionsFixture struct {
	Now          time.Time                      `json:"now"`
	OperatorID   string                         `json:"operator_id"`
	Capabilities map[string]MessageCapabilities `json:"capabilities"`
	Cases        []messageActionsCase           `json:"cases"`
}

type messageActionsCase struct {
	Name         string          `json:"name"`
	Capabilities string          `json:"capabilities"`
	OperatorID   *string         `json:"operator_id"`
	Message      json.RawMessage `json:"message"`
	Expected     MessageActions  `json:"expected"`
}

func readMessageActionsFixture(t *testing.T) messageActionsFixture {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "message_actions.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture messageActionsFixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) == 0 {
		t.Fatal("the shared fixture has no cases")
	}
	return fixture
}

func (c messageActionsCase) operator(fixture messageActionsFixture) string {
	if c.OperatorID != nil {
		return *c.OperatorID
	}
	return fixture.OperatorID
}

func TestAllowedActionsFollowsTheFixtureTheFrontendAlsoReads(t *testing.T) {
	fixture := readMessageActionsFixture(t)
	for _, tc := range fixture.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			caps, ok := fixture.Capabilities[tc.Capabilities]
			if !ok {
				t.Fatalf("unknown capability set %q", tc.Capabilities)
			}
			var message Message
			if err := json.Unmarshal(tc.Message, &message); err != nil {
				t.Fatal(err)
			}
			got := AllowedActions(&message, caps, tc.operator(fixture), fixture.Now)
			if got != tc.Expected {
				t.Fatalf("AllowedActions = %+v, want %+v", got, tc.Expected)
			}
		})
	}
}

func TestEveryFixtureCaseStatesEveryActionSoTheFrontendMirrorCannotDefaultOne(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "message_actions.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name     string                     `json:"name"`
			Expected map[string]json.RawMessage `json:"expected"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, tc := range fixture.Cases {
		for _, action := range []string{"react", "edit", "delete", "resend"} {
			if _, ok := tc.Expected[action]; !ok {
				t.Errorf("%s: expected does not state %q", tc.Name, action)
			}
		}
	}
}

func TestTheFixtureSenderUserIDIsWhatTheWireSends(t *testing.T) {
	fixture := readMessageActionsFixture(t)
	for _, tc := range fixture.Cases {
		var message Message
		if err := json.Unmarshal(tc.Message, &message); err != nil {
			t.Fatal(err)
		}
		var declared, projected struct {
			SenderUserID string `json:"sender_user_id"`
		}
		if err := json.Unmarshal(tc.Message, &declared); err != nil {
			t.Fatal(err)
		}
		wire, err := json.Marshal(message)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(wire, &projected); err != nil {
			t.Fatal(err)
		}
		if declared.SenderUserID != projected.SenderUserID {
			t.Errorf("%s: fixture sender_user_id %q, wire sends %q", tc.Name, declared.SenderUserID, projected.SenderUserID)
		}
	}
}

func TestAllowedActionsOnNoMessageIsNothing(t *testing.T) {
	full := MessageCapabilities{React: true, Edit: true, Delete: true, EditWindowSeconds: 900, DeleteWindowSeconds: 900}
	if got := AllowedActions(nil, full, "user-ana", time.Now()); got != (MessageActions{}) {
		t.Fatalf("AllowedActions(nil) = %+v, want nothing", got)
	}
}

func TestCheckMessageActionSaysWhyItRefuses(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	provider := "pm-1"
	own := func(createdAt time.Time) *Message {
		return &Message{
			MessageType: MessageTypeOperator, Text: "oi", ExternalMessageID: &provider,
			SentBy: SentByPerson("user-ana"), CreatedAt: createdAt,
		}
	}
	full := MessageCapabilities{React: true, Edit: true, Delete: true, EditWindowSeconds: 900, DeleteWindowSeconds: 3600}
	cases := map[string]struct {
		action  MessageAction
		message *Message
		caps    MessageCapabilities
		want    error
	}{
		"allowed edit":                     {MessageActionEdit, own(now.Add(-time.Minute)), full, nil},
		"channel cannot edit":              {MessageActionEdit, own(now), MessageCapabilities{React: true}, ErrMessageActionUnsupported},
		"edit after its window":            {MessageActionEdit, own(now.Add(-time.Hour)), full, ErrMessageActionWindowExpired},
		"delete after its window":          {MessageActionDelete, own(now.Add(-2 * time.Hour)), full, ErrMessageActionWindowExpired},
		"someone else's message":           {MessageActionDelete, &Message{MessageType: MessageTypeOperator, Text: "oi", ExternalMessageID: &provider, SentBy: SentByPerson("user-bruno"), CreatedAt: now}, full, ErrMessageActionNotAllowed},
		"reaction on a channel without it": {MessageActionReact, own(now), MessageCapabilities{}, ErrMessageActionUnsupported},
		"unknown action":                   {MessageAction("forward"), own(now), full, ErrMessageActionUnsupported},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := CheckMessageAction(tc.action, tc.message, tc.caps, "user-ana", now); got != tc.want {
				t.Fatalf("CheckMessageAction = %v, want %v", got, tc.want)
			}
		})
	}
}

type plainChannel struct{}

func (plainChannel) ResolveEntry(context.Context, string) (*EntryContext, error) { return nil, nil }

type reactingChannel struct{ plainChannel }

func (reactingChannel) SendReaction(context.Context, *EntryContext, string, string) error { return nil }
func (reactingChannel) RemoveReaction(context.Context, *EntryContext, string) error       { return nil }

type fullChannel struct{ reactingChannel }

func (fullChannel) EditText(context.Context, *EntryContext, string, string) error { return nil }
func (fullChannel) EditWindow() time.Duration                                     { return 15 * time.Minute }
func (fullChannel) Retract(context.Context, *EntryContext, string) error          { return nil }
func (fullChannel) RetractWindow() time.Duration                                  { return 48 * time.Hour }

type windowlessChannel struct{ plainChannel }

func (windowlessChannel) EditText(context.Context, *EntryContext, string, string) error { return nil }
func (windowlessChannel) EditWindow() time.Duration                                     { return 0 }
func (windowlessChannel) Retract(context.Context, *EntryContext, string) error          { return nil }
func (windowlessChannel) RetractWindow() time.Duration                                  { return 0 }

func TestMessageCapabilitiesComeFromTheInterfacesAChannelImplements(t *testing.T) {
	cases := map[string]struct {
		channel MessageActionChannel
		want    MessageCapabilities
	}{
		"no channel":                        {nil, MessageCapabilities{}},
		"a channel without any action":      {plainChannel{}, MessageCapabilities{}},
		"a channel that only reacts":        {reactingChannel{}, MessageCapabilities{React: true}},
		"a channel with every action":       {fullChannel{}, MessageCapabilities{React: true, Edit: true, Delete: true, EditWindowSeconds: 900, DeleteWindowSeconds: 172800}},
		"a channel that declares no window": {windowlessChannel{}, MessageCapabilities{}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := MessageCapabilitiesOf(tc.channel); got != tc.want {
				t.Fatalf("MessageCapabilitiesOf = %+v, want %+v", got, tc.want)
			}
		})
	}
}
