package copilottools

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/readiness"
)

type fakeReadiness struct {
	snap   *readiness.Snapshot
	err    error
	person readiness.Person
}

func (f *fakeReadiness) Snapshot(_ context.Context, p readiness.Person) (*readiness.Snapshot, error) {
	f.person = p
	return f.snap, f.err
}

func atLimitSnapshot() *readiness.Snapshot {
	return &readiness.Snapshot{SubscriptionActive: true, BalanceMicros: 1_500_000, Capabilities: []readiness.Status{
		{Capability: readiness.OfficialWhatsApp, Count: 1, Usage: &readiness.Usage{Used: 1, Total: 1}, Blocker: readiness.BlockerAtLimit},
	}}
}

func TestOfferActionIsReadableByAnyChatUser(t *testing.T) {
	if m := NewOfferActionTool(&fakeReadiness{}).Meta(); m.Mutating || m.Resource != "ai_chat" || m.Action != "read" {
		t.Fatalf("meta = %+v", m)
	}
	def := NewOfferActionTool(&fakeReadiness{}).Definition()
	if len(def.Parameters["kind"].Enum) != len(copilot.ActionKinds()) {
		t.Fatalf("kind enum = %v", def.Parameters["kind"].Enum)
	}
}

func TestOfferActionBuildsTheCardFromTheLiveSnapshot(t *testing.T) {
	r := &fakeReadiness{snap: atLimitSnapshot()}
	res := NewOfferActionTool(r).Execute(context.Background(), member(), map[string]interface{}{"kind": "connect_whatsapp_business"})
	if res.Status != copilot.StatusOK || res.Card == nil || res.Card.Status.Blocker != readiness.BlockerAtLimit {
		t.Fatalf("result = %+v", res)
	}
	if r.person.WorkspaceID != "ws-1" || r.person.UserID != "u-1" {
		t.Fatalf("snapshot asked for %+v", r.person)
	}
	data := res.Data.(map[string]interface{})
	if data["can_act"] != false || data["blocker"] != readiness.BlockerAtLimit {
		t.Fatalf("the model must learn what the card says: %+v", data)
	}
}

func TestOfferActionRefusesUnknownKindsAndMissingState(t *testing.T) {
	if res := NewOfferActionTool(&fakeReadiness{snap: atLimitSnapshot()}).Execute(context.Background(), member(), map[string]interface{}{"kind": "open_url"}); res.Status != copilot.StatusError || res.Card != nil {
		t.Fatalf("unknown kind = %+v", res)
	}
	if res := NewOfferActionTool(&fakeReadiness{snap: atLimitSnapshot()}).Execute(context.Background(), member(), map[string]interface{}{"kind": "connect_telegram"}); res.Status != copilot.StatusError || res.Card != nil {
		t.Fatalf("missing capability = %+v", res)
	}
	if res := NewOfferActionTool(&fakeReadiness{err: errors.New("db")}).Execute(context.Background(), member(), map[string]interface{}{"kind": "top_up_balance"}); res.Status != copilot.StatusError || res.Card != nil {
		t.Fatalf("snapshot failure = %+v", res)
	}
}
