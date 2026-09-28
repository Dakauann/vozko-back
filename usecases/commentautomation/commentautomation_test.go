package commentautomation

import (
	"context"
	"errors"
	"testing"
	"time"

	ca "vozko/domain/commentautomation"
	"vozko/domain/privatereply"
	"vozko/domain/shared"
)

type memoryRules struct {
	byID map[string]*ca.Rule
	seq  int
}

func newMemoryRules(rules ...*ca.Rule) *memoryRules {
	m := &memoryRules{byID: map[string]*ca.Rule{}}
	for _, r := range rules {
		m.byID[r.ID] = r
	}
	return m
}

func (m *memoryRules) Create(_ context.Context, r *ca.Rule) error {
	m.seq++
	r.ID = "rule-" + string(rune('0'+m.seq))
	m.byID[r.ID] = r
	return nil
}
func (m *memoryRules) Update(_ context.Context, r *ca.Rule) error { m.byID[r.ID] = r; return nil }
func (m *memoryRules) Delete(_ context.Context, _, id string) error {
	delete(m.byID, id)
	return nil
}
func (m *memoryRules) FindByID(_ context.Context, ws, id string) (*ca.Rule, error) {
	if r, ok := m.byID[id]; ok && r.WorkspaceID == ws {
		return r, nil
	}
	return nil, ca.ErrRuleNotFound
}
func (m *memoryRules) ListByAccount(_ context.Context, ws string, source shared.EntryType, account string) ([]*ca.Rule, error) {
	var out []*ca.Rule
	for _, r := range m.byID {
		if r.WorkspaceID == ws && r.Source == source && r.AccountID == account {
			out = append(out, r)
		}
	}
	return out, nil
}
func (m *memoryRules) ListCandidates(_ context.Context, source shared.EntryType, account, container string) ([]*ca.Rule, error) {
	var out []*ca.Rule
	for _, r := range m.byID {
		if r.Source == source && r.AccountID == account && (r.ContainerID == "" || r.ContainerID == container) {
			out = append(out, r)
		}
	}
	return out, nil
}

type verifier struct{ owner string }

func (v verifier) VerifyAccount(_ context.Context, ws, _ string) error {
	if ws != v.owner {
		return errors.New("not yours")
	}
	return nil
}

type recordingActions struct {
	calls []string
	fail  map[ca.Action]error
}

func (r *recordingActions) do(a ca.Action, detail string) error {
	r.calls = append(r.calls, string(a)+":"+detail)
	return r.fail[a]
}
func (r *recordingActions) ReplyPublicly(_ context.Context, _ *ca.Rule, id, text string) error {
	return r.do(ca.ActionPublicReply, id+":"+text)
}
func (r *recordingActions) ReplyPrivately(_ context.Context, _ *ca.Rule, id, text string) error {
	return r.do(ca.ActionPrivateReply, id+":"+text)
}
func (r *recordingActions) Hide(_ context.Context, _ *ca.Rule, id string) error {
	return r.do(ca.ActionHide, id)
}
func (r *recordingActions) Delete(_ context.Context, _ *ca.Rule, id string) error {
	return r.do(ca.ActionDelete, id)
}
func (r *recordingActions) Like(_ context.Context, _ *ca.Rule, id string) error {
	return r.do(ca.ActionLike, id)
}

func fbRule(id string, priority int, keywords ...string) *ca.Rule {
	r := &ca.Rule{ID: id, WorkspaceID: "ws", Source: shared.EntryTypeFacebook, AccountID: "page-1", Name: id, Enabled: true,
		Match: ca.MatchContains, Keywords: keywords, Priority: priority,
		Actions: []ca.Action{ca.ActionPrivateReply, ca.ActionPublicReply, ca.ActionLike}, PublicReplyText: "Oi {{username}}", PrivateReplyText: "Te chamei"}
	r.Normalize()
	return r
}

func TestFirstMatchingRuleRunsEveryAction(t *testing.T) {
	rules := newMemoryRules(fbRule("r1", 1, "preço"))
	actions := &recordingActions{}

	NewEvaluator(rules).Evaluate(context.Background(), shared.EntryTypeFacebook, "page-1", "c-1",
		ca.Subject{ContainerID: "p-1", AuthorName: "Ana", Text: "qual o preço?"}, actions)

	want := []string{"private_reply:c-1:Te chamei", "public_reply:c-1:Oi Ana", "like:c-1"}
	if len(actions.calls) != len(want) {
		t.Fatalf("calls = %v", actions.calls)
	}
	for i := range want {
		if actions.calls[i] != want[i] {
			t.Errorf("call %d = %q, want %q", i, actions.calls[i], want[i])
		}
	}
}

func TestAConsumedPrivateReplyDoesNotStopTheOtherActions(t *testing.T) {
	rules := newMemoryRules(fbRule("r1", 1, "preço"))
	actions := &recordingActions{fail: map[ca.Action]error{ca.ActionPrivateReply: privatereply.ErrUsed}}

	NewEvaluator(rules).Evaluate(context.Background(), shared.EntryTypeFacebook, "page-1", "c-1",
		ca.Subject{Text: "preço"}, actions)

	if len(actions.calls) != 3 {
		t.Fatalf("calls = %v", actions.calls)
	}
}

func TestOurOwnCommentsNeverTriggerRules(t *testing.T) {
	rules := newMemoryRules(fbRule("r1", 1, "preço"))
	actions := &recordingActions{}
	NewEvaluator(rules).Evaluate(context.Background(), shared.EntryTypeFacebook, "page-1", "c-1",
		ca.Subject{Text: "preço", IsOurs: true}, actions)
	if len(actions.calls) != 0 {
		t.Fatalf("calls = %v", actions.calls)
	}
}

func TestManagerScopesRulesToTheAccountInThePath(t *testing.T) {
	existing := fbRule("r1", 1, "preço")
	rules := newMemoryRules(existing)
	m := NewManager(rules, map[shared.EntryType]AccountVerifier{shared.EntryTypeFacebook: verifier{owner: "ws"}})
	ctx := context.Background()

	if _, err := m.List(ctx, "ws-other", shared.EntryTypeFacebook, "page-1"); err == nil {
		t.Fatal("listing another workspace's account must fail")
	}
	moved := *existing
	moved.AccountID = "page-2"
	if _, err := m.Update(ctx, &moved); !errors.Is(err, ca.ErrRuleNotFound) {
		t.Fatalf("update through another account: %v", err)
	}
	if err := m.Delete(ctx, "ws", shared.EntryTypeFacebook, "page-2", "r1"); !errors.Is(err, ca.ErrRuleNotFound) {
		t.Fatalf("delete through another account: %v", err)
	}
	if _, err := m.List(ctx, "ws", shared.EntryTypeTelegram, "x"); !errors.Is(err, ca.ErrUnknownSource) {
		t.Fatalf("unknown source: %v", err)
	}
}

func TestAChannelRegisteredLaterCanManageRules(t *testing.T) {
	m := NewManager(newMemoryRules(), nil)
	if _, err := m.List(context.Background(), "ws", shared.EntryTypeFacebook, "page-1"); !errors.Is(err, ca.ErrUnknownSource) {
		t.Fatalf("before registration: %v", err)
	}
	m.Register(shared.EntryTypeFacebook, verifier{owner: "ws"})
	if _, err := m.List(context.Background(), "ws", shared.EntryTypeFacebook, "page-1"); err != nil {
		t.Fatalf("after registration: %v", err)
	}
}

func TestManagerCreatesAValidatedRule(t *testing.T) {
	rules := newMemoryRules()
	m := NewManager(rules, map[shared.EntryType]AccountVerifier{shared.EntryTypeInstagram: verifier{owner: "ws"}})

	created, err := m.Create(context.Background(), &ca.Rule{WorkspaceID: "ws", Source: shared.EntryTypeInstagram, AccountID: "acc",
		Name: " Todas ", Match: ca.MatchAny, Actions: []ca.Action{ca.ActionHide}, Enabled: true})
	if err != nil || created.ID == "" || created.Name != "Todas" {
		t.Fatalf("created %+v, %v", created, err)
	}
	if _, err := m.Create(context.Background(), &ca.Rule{WorkspaceID: "ws", Source: shared.EntryTypeInstagram, AccountID: "acc",
		Name: "x", Match: ca.MatchAny, Actions: []ca.Action{ca.ActionLike}}); !errors.Is(err, ca.ErrActionUnsupported) {
		t.Fatalf("instagram like: %v", err)
	}
}

type memoryReplies struct {
	claimed map[string]bool
	sent    map[string]string
	failed  map[string]int
}

func newMemoryReplies() *memoryReplies {
	return &memoryReplies{claimed: map[string]bool{}, sent: map[string]string{}, failed: map[string]int{}}
}
func (m *memoryReplies) Claim(_ context.Context, _ shared.EntryType, id, _ string) (bool, error) {
	if m.claimed[id] {
		return false, nil
	}
	m.claimed[id] = true
	return true, nil
}
func (m *memoryReplies) MarkSent(_ context.Context, _ shared.EntryType, id, recipient, _ string) error {
	m.sent[id] = recipient
	return nil
}
func (m *memoryReplies) MarkFailed(_ context.Context, _ shared.EntryType, id string, code int, _ string) error {
	m.failed[id] = code
	return nil
}
func (m *memoryReplies) Find(context.Context, shared.EntryType, string) (*privatereply.Record, error) {
	return nil, nil
}
func (m *memoryReplies) FindMany(context.Context, shared.EntryType, []string) (map[string]*privatereply.Record, error) {
	return nil, nil
}

type codedFailure struct{ code int }

func (c codedFailure) Error() string         { return "graph refused" }
func (c codedFailure) ErrorCode() (int, int) { return c.code, 0 }
func deliverTo(recipient string) DeliverFunc {
	return func(context.Context) (*Delivery, error) {
		return &Delivery{RecipientRef: recipient, MessageID: "m1"}, nil
	}
}
func failWith(err error) DeliverFunc {
	return func(context.Context) (*Delivery, error) { return nil, err }
}

func TestPrivateReplyIsClaimedBeforeItIsSentAndOnlyOnce(t *testing.T) {
	replies := newMemoryReplies()
	sender := NewPrivateReplySender(replies)
	at := time.Now().Add(-time.Hour)

	out, err := sender.Send(context.Background(), shared.EntryTypeFacebook, "page-1", "c-1", &at, deliverTo("psid-1"))
	if err != nil || out.RecipientRef != "psid-1" || replies.sent["c-1"] != "psid-1" {
		t.Fatalf("out %+v, %v", out, err)
	}
	if _, err := sender.Send(context.Background(), shared.EntryTypeFacebook, "page-1", "c-1", &at, deliverTo("psid-1")); !errors.Is(err, privatereply.ErrUsed) {
		t.Fatalf("second send: %v", err)
	}
}

func TestAFailedPrivateReplyIsStillConsumed(t *testing.T) {
	replies := newMemoryReplies()
	at := time.Now().Add(-time.Hour)

	_, err := NewPrivateReplySender(replies).Send(context.Background(), shared.EntryTypeFacebook, "page-1", "c-1", &at, failWith(codedFailure{code: 10900}))
	if err == nil || replies.failed["c-1"] != 10900 || !replies.claimed["c-1"] {
		t.Fatalf("err %v, failed %v", err, replies.failed)
	}
}

func TestPrivateReplyRefusesAnUnknownOrPastDeadline(t *testing.T) {
	replies := newMemoryReplies()
	sender := NewPrivateReplySender(replies)
	old := time.Now().Add(-8 * 24 * time.Hour)

	if _, err := sender.Send(context.Background(), shared.EntryTypeFacebook, "p", "c-1", nil, deliverTo("x")); !errors.Is(err, privatereply.ErrDeadlineUnknown) {
		t.Fatalf("unknown: %v", err)
	}
	if _, err := sender.Send(context.Background(), shared.EntryTypeFacebook, "p", "c-2", &old, deliverTo("x")); !errors.Is(err, privatereply.ErrExpired) {
		t.Fatalf("expired: %v", err)
	}
	if len(replies.claimed) != 0 {
		t.Fatal("a refused reply must not be claimed")
	}
}
