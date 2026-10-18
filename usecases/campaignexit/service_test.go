package campaignexit_usecase

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"vozko/domain/cache"
	"vozko/domain/campaign"
)

type fakeFacts struct {
	calls   int
	numbers []string
	groups  []campaign.LeadFactsGroup
	err     error
}

func (f *fakeFacts) AudienceExitFacts(_ context.Context, _ string, numbers []string) ([]campaign.LeadFactsGroup, error) {
	f.calls++
	f.numbers = numbers
	return f.groups, f.err
}

type mapMemo struct{ values map[string][]byte }

func (m *mapMemo) Remember(ctx context.Context, key string, _ time.Duration, compute func(context.Context) ([]byte, error)) ([]byte, error) {
	if v, ok := m.values[key]; ok {
		return v, nil
	}
	v, err := compute(ctx)
	if err == nil {
		m.values[key] = v
	}
	return v, err
}

type countingGate struct{ acquired int }

func (g *countingGate) Acquire(context.Context) (func(), error) {
	g.acquired++
	return func() {}, nil
}

type busyGate struct{}

func (busyGate) Acquire(context.Context) (func(), error) { return nil, cache.ErrGateBusy }

type fixedVersions struct{ version string }

func (v fixedVersions) Version(string) (string, error) { return v.version, nil }
func (fixedVersions) Bump(string) error                { return nil }

func newService(t *testing.T, facts *fakeFacts, gate cache.Gate) *Service {
	t.Helper()
	svc, err := NewService(Deps{Facts: facts, Memo: &mapMemo{values: map[string][]byte{}}, Gate: gate, Versions: fixedVersions{version: "7"}})
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func TestTheAudienceExitsCountWhoTheSendRuleRefuses(t *testing.T) {
	facts := &fakeFacts{groups: []campaign.LeadFactsGroup{
		{Facts: campaign.LeadFacts{Found: true, HasIdentity: true}, Leads: 100},
		{Facts: campaign.LeadFacts{Found: true, HasIdentity: true, OptedOut: true}, Leads: 30},
		{Facts: campaign.LeadFacts{Found: true, HasIdentity: true, Blocked: true}, Leads: 7},
	}}
	gate := &countingGate{}
	svc := newService(t, facts, gate)

	got, err := svc.AudienceExits(context.Background(), "ws-1", []string{" 5511987650002 ", "5511987650001", "5511987650001", "", "abc"})
	if err != nil {
		t.Fatal(err)
	}
	if got.OptedOut != 30 || got.Blocked != 7 || got.NotReceiving() != 37 {
		t.Fatalf("exits = %+v", got)
	}
	if !reflect.DeepEqual(facts.numbers, []string{"5511987650001", "5511987650002"}) {
		t.Fatalf("numbers read = %v, want each number once, as campaign creation reads it, sorted", facts.numbers)
	}
	if gate.acquired != 1 {
		t.Fatalf("the read did not wait for the analytics gate")
	}
}

func TestTheSameListIsReadOnceWhileCached(t *testing.T) {
	facts := &fakeFacts{}
	svc := newService(t, facts, &countingGate{})
	for _, numbers := range [][]string{{"5511987650001", "5511987650002"}, {"5511987650002", "5511987650001"}} {
		if _, err := svc.AudienceExits(context.Background(), "ws-1", numbers); err != nil {
			t.Fatal(err)
		}
	}
	if facts.calls != 1 {
		t.Fatalf("read %d times, want once for the same list in any order", facts.calls)
	}
	if _, err := svc.AudienceExits(context.Background(), "ws-2", []string{"5511987650001", "5511987650002"}); err != nil {
		t.Fatal(err)
	}
	if facts.calls != 2 {
		t.Fatalf("another workspace reused the cached count")
	}
}

func TestAnEmptyListReadsNothing(t *testing.T) {
	facts := &fakeFacts{}
	got, err := newService(t, facts, &countingGate{}).AudienceExits(context.Background(), "ws-1", []string{"", " "})
	if err != nil || got.NotReceiving() != 0 || facts.calls != 0 {
		t.Fatalf("exits = %+v, %v, reads %d", got, err, facts.calls)
	}
}

func TestRefusals(t *testing.T) {
	svc := newService(t, &fakeFacts{}, &countingGate{})
	if _, err := svc.AudienceExits(context.Background(), " ", []string{"5511987650001"}); !errors.Is(err, ErrWorkspaceRequired) {
		t.Fatalf("without workspace = %v", err)
	}
	tooMany := make([]string, MaxNumbers+1)
	for i := range tooMany {
		tooMany[i] = "55119876" + string(rune('0'+i%10))
	}
	if _, err := svc.AudienceExits(context.Background(), "ws-1", tooMany); !errors.Is(err, ErrTooManyNumbers) {
		t.Fatalf("too many = %v", err)
	}
	failing := newService(t, &fakeFacts{err: errors.New("db down")}, &countingGate{})
	if _, err := failing.AudienceExits(context.Background(), "ws-1", []string{"5511987650001"}); err == nil {
		t.Fatal("a failed read must refuse")
	}
	busy := newService(t, &fakeFacts{}, busyGate{})
	if _, err := busy.AudienceExits(context.Background(), "ws-1", []string{"5511987650001"}); !errors.Is(err, cache.ErrGateBusy) {
		t.Fatalf("busy gate = %v", err)
	}
	if _, err := NewService(Deps{}); err == nil {
		t.Fatal("a service without its ports was built")
	}
}

func TestTheCodesOfTheRefusals(t *testing.T) {
	for err, code := range map[error]string{ErrWorkspaceRequired: "campaign_exit_workspace_required", ErrTooManyNumbers: "campaign_exit_too_many_numbers", ErrUnavailable: "campaign_exit_unavailable"} {
		if got := ErrorCode(err); got != code {
			t.Errorf("%v = %q, want %q", err, got, code)
		}
	}
}
