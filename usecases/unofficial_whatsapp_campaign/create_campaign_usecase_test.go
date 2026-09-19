package unofficial_whatsapp_campaign

import (
	"context"
	"errors"
	"sync"
	"testing"

	"vozko/domain/campaign"
	"vozko/domain/lead"
	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
)

// fakeLeadRepo resolves numbers to leads, mirroring FindOrCreateMany's contract:
// keyed by the NORMALIZED number.
type fakeLeadRepo struct {
	mu    sync.Mutex
	next  int
	byNum map[string]*lead.Lead
}

func newFakeLeadRepo() *fakeLeadRepo { return &fakeLeadRepo{byNum: map[string]*lead.Lead{}} }

func (f *fakeLeadRepo) FindOrCreateMany(_ string, inputs []lead.BulkLeadInput) (map[string]*lead.Lead, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]*lead.Lead{}
	for _, in := range inputs {
		l, ok := f.byNum[in.Number]
		if !ok {
			f.next++
			l = &lead.Lead{ID: "lead-" + itoa(f.next), Number: in.Number, Name: in.Name}
			f.byNum[in.Number] = l
		}
		out[in.Number] = l
	}
	return out, nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for ; n > 0; n /= 10 {
		s = string(rune('0'+n%10)) + s
	}
	return s
}

type fakeDepartments struct{ id string }

func (f fakeDepartments) Resolve(context.Context, string) (string, error) { return f.id, nil }

func newCreateHarness(t *testing.T) (uwc.CreateCampaignUseCase, *fakeCampaignRepo, *fakeEntryRepo, *fakeGateway, *fakeSpam) {
	t.Helper()
	campaigns := newFakeCampaignRepo()
	entries := newFakeEntryRepo()
	gateway := &fakeGateway{instance: &uw.Instance{
		ID: "inst-1", WorkspaceID: "ws-1", Status: uw.StatusConnected,
		SendDelayMinMS: 4000, SendDelayMaxMS: 9000,
	}}
	spam := &fakeSpam{skip: map[string]bool{}}

	uc := NewCreateCampaignUseCase(campaigns, entries, newFakeLeadRepo(), gateway, spam,
		fakeDepartments{id: "dept-1"})
	return uc, campaigns, entries, gateway, spam
}

func draft() *uwc.Campaign {
	return &uwc.Campaign{
		WorkspaceID: "ws-1",
		InstanceID:  "inst-1",
		Name:        "cobranca agosto",
		Message:     uwc.MessageSpec{Kind: uwc.KindText, Bodies: []string{"bom dia"}},
		Targets: []uwc.TargetInput{
			{Number: "5584999990001"},
			{Number: "5584999990002"},
		},
	}
}

func TestCreateMaterializesLeadBackedEntries(t *testing.T) {
	uc, _, entries, _, _ := newCreateHarness(t)

	created, err := uc.Execute(context.Background(), draft(), uw.Unrestricted())
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if created.DepartmentID != "dept-1" {
		t.Errorf("department = %q, want the resolver's", created.DepartmentID)
	}
	pending, _ := entries.ListByStatus(created.ID, campaign.SendStatusPending, 100)
	if len(pending) != 2 {
		t.Fatalf("entries = %d, want 2", len(pending))
	}
	for _, e := range pending {
		// The lead bridge is what makes these contacts reachable by exports and
		// every other lead-keyed tool rather than living in a silo.
		if e.LeadID == "" {
			t.Errorf("entry %s has no lead", e.Number)
		}
	}
}

// Pacing is COPIED from the number, so widening the number's range later cannot
// speed up a blast already in flight.
func TestCreateCopiesPacingFromTheNumber(t *testing.T) {
	uc, _, _, _, _ := newCreateHarness(t)

	created, err := uc.Execute(context.Background(), draft(), uw.Unrestricted())
	if err != nil {
		t.Fatal(err)
	}
	if created.SendDelayMinMS != 4000 || created.SendDelayMaxMS != 9000 {
		t.Fatalf("pacing = %d..%d, want the number's 4000..9000",
			created.SendDelayMinMS, created.SendDelayMaxMS)
	}
}

// A banned number can only ever fail. Refusing at creation beats letting the
// operator discover it at Start, after importing forty thousand numbers.
func TestCreateRefusesABannedNumber(t *testing.T) {
	uc, _, _, gateway, _ := newCreateHarness(t)
	gateway.instance.Status = uw.StatusBanned

	_, err := uc.Execute(context.Background(), draft(), uw.Unrestricted())
	var unusable *uwc.InstanceUnusableError
	if !errors.As(err, &unusable) {
		t.Fatalf("err = %v, want InstanceUnusableError", err)
	}
}

// The spam window is applied at IMPORT, so an operator sees the real reachable
// count before starting rather than discovering it mid-run.
func TestCreateMarksCooldownSkipsUpFront(t *testing.T) {
	uc, _, entries, _, spam := newCreateHarness(t)
	spam.skip["lead-1"] = true

	created, err := uc.Execute(context.Background(), draft(), uw.Unrestricted())
	if err != nil {
		t.Fatal(err)
	}
	counts, _ := entries.CountByStatus(created.ID)
	if counts.Total != 2 {
		t.Fatalf("total = %d, want 2", counts.Total)
	}
	skipped, _ := entries.ListByStatus(created.ID, campaign.SendStatusNotEligiblePossibleSpam, 10)
	if len(skipped) != 1 {
		t.Fatalf("cooldown skips = %d, want 1", len(skipped))
	}
}

// The same person written two ways is one person; blasting them twice is the
// commonest ban complaint there is.
func TestCreateDedupsTargets(t *testing.T) {
	uc, _, entries, _, _ := newCreateHarness(t)
	in := draft()
	in.Targets = []uwc.TargetInput{
		{Number: "+55 84 99999-0001"},
		{Number: "5584999990001"},
	}

	created, err := uc.Execute(context.Background(), in, uw.Unrestricted())
	if err != nil {
		t.Fatal(err)
	}
	counts, _ := entries.CountByStatus(created.ID)
	if counts.Total != 1 {
		t.Fatalf("entries = %d, want 1 after dedup", counts.Total)
	}
}

// Variables are validated against the message BEFORE anything is written, so a
// campaign that would send a raw {{2}} never reaches the database.
func TestCreateRefusesTargetsMissingVariables(t *testing.T) {
	uc, campaigns, _, _, _ := newCreateHarness(t)
	in := draft()
	in.Message.Bodies = []string{"oi {{1}}"}

	_, err := uc.Execute(context.Background(), in, uw.Unrestricted())
	if !errors.Is(err, uwc.ErrCampaignVariablesMismatch) {
		t.Fatalf("err = %v, want ErrCampaignVariablesMismatch", err)
	}
	if len(campaigns.campaigns) != 0 {
		t.Fatal("a refused campaign was still persisted")
	}
}

// A caller outside the number's department must not be able to campaign from it.
func TestCreateHonoursDepartmentScope(t *testing.T) {
	uc, _, _, gateway, _ := newCreateHarness(t)
	other := "dept-other"
	gateway.instance.DepartmentID = &other

	_, err := uc.Execute(context.Background(), draft(), uw.DepartmentScope{
		DepartmentIDs: []string{"dept-mine"}, Restrict: true,
	})
	if err == nil {
		t.Fatal("a campaign was created from another department's number")
	}
}

// A demonstration campaign is born carrying results, so an administrator can
// show the product without blasting a real list to produce numbers.
func TestCreateSeedsOutcomesWhenAsked(t *testing.T) {
	uc, _, entries, _, _ := newCreateHarness(t)

	in := draft()
	in.Targets = nil
	for i := 0; i < 10; i++ {
		in.Targets = append(in.Targets, uwc.TargetInput{
			Number: "55849999900" + itoa(10+i),
		})
	}
	in.SeedOutcome = &campaign.SeededOutcome{SentPercent: 50, FailedPercent: 20}

	created, err := uc.Execute(context.Background(), in, uw.Unrestricted())
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	sent, _ := entries.ListByStatus(created.ID, campaign.SendStatusSent, 100)
	failed, _ := entries.ListByStatus(created.ID, campaign.SendStatusFailed, 100)
	pending, _ := entries.ListByStatus(created.ID, campaign.SendStatusPending, 100)
	if len(sent) != 5 || len(failed) != 2 || len(pending) != 3 {
		t.Fatalf("seeded split = %d sent / %d failed / %d pending, want 5 / 2 / 3",
			len(sent), len(failed), len(pending))
	}
	// The entries table and the export both read sentAt to answer "when", so a
	// settled row without one renders as a blank column.
	for _, e := range append(sent, failed...) {
		if e.SentAt == nil {
			t.Fatalf("settled entry %s has no sentAt", e.ID)
		}
	}
	for _, e := range pending {
		if e.SentAt != nil {
			t.Fatalf("pending entry %s was stamped as sent", e.ID)
		}
	}
}

// The ordinary campaign is untouched: every entry starts PENDING, as it did
// before this control existed.
func TestCreateWithoutSeedOutcomeStaysPending(t *testing.T) {
	uc, _, entries, _, _ := newCreateHarness(t)

	created, err := uc.Execute(context.Background(), draft(), uw.Unrestricted())
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	pending, _ := entries.ListByStatus(created.ID, campaign.SendStatusPending, 100)
	if len(pending) != 2 {
		t.Fatalf("pending = %d, want 2", len(pending))
	}
}

func TestCreateRefusesAnOversubscribedSeedOutcome(t *testing.T) {
	uc, _, _, _, _ := newCreateHarness(t)

	in := draft()
	in.SeedOutcome = &campaign.SeededOutcome{SentPercent: 80, FailedPercent: 40}

	if _, err := uc.Execute(context.Background(), in, uw.Unrestricted()); !errors.Is(err, campaign.ErrSeededOutcomeOverflow) {
		t.Fatalf("create = %v, want ErrSeededOutcomeOverflow", err)
	}
}
