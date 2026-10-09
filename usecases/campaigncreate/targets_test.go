package campaigncreate

import (
	"errors"
	"testing"

	"vozko/domain/campaign"
	"vozko/domain/lead"
)

type bookOfLeads struct {
	byID       map[string]*lead.Lead
	idCalls    [][]string
	numberRuns [][]lead.BulkLeadInput
	err        error
}

func (b *bookOfLeads) FindByIDs(workspaceID string, ids []string) ([]*lead.Lead, error) {
	b.idCalls = append(b.idCalls, ids)
	if b.err != nil {
		return nil, b.err
	}
	var out []*lead.Lead
	for _, id := range ids {
		if l, ok := b.byID[id]; ok {
			out = append(out, l)
		}
	}
	return out, nil
}

func (b *bookOfLeads) FindOrCreateMany(workspaceID string, inputs []lead.BulkLeadInput) (map[string]*lead.Lead, error) {
	b.numberRuns = append(b.numberRuns, inputs)
	out := map[string]*lead.Lead{}
	for _, in := range inputs {
		number := lead.NormalizeNumber(in.Number)
		out[number] = &lead.Lead{ID: "lead-" + number, WorkspaceID: workspaceID, Number: number}
	}
	return out, nil
}

func book() *bookOfLeads {
	return &bookOfLeads{byID: map[string]*lead.Lead{
		"lead-a": {ID: "lead-a", WorkspaceID: "ws-1", Number: "5584999990001"},
		"lead-x": {ID: "lead-x", WorkspaceID: "ws-2", Number: "5584999990009"},
	}}
}

func TestResolveFindsLeadTargetsInTheWorkspaceAndNumbersThroughOneCall(t *testing.T) {
	leads := book()
	got, err := Resolve(leads, leads, "ws-1", []Target{{LeadID: "lead-a"}, {LeadID: "lead-x"}, {LeadID: "lead-gone"}, {Number: "5584999990005", Name: "Ana"}})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(leads.idCalls) != 1 || len(leads.idCalls[0]) != 3 || len(leads.numberRuns) != 1 || len(leads.numberRuns[0]) != 1 {
		t.Fatalf("calls ids %v numbers %v, want one of each", leads.idCalls, leads.numberRuns)
	}
	if got.Of(Target{LeadID: "lead-a"}) == nil {
		t.Fatal("the lead target was not found")
	}
	if got.Of(Target{LeadID: "lead-x"}) != nil {
		t.Fatal("a lead of another workspace was resolved")
	}
	if l := got.Of(Target{Number: "5584999990005"}); l == nil || l.ID != "lead-5584999990005" {
		t.Fatalf("the number target resolved to %v", l)
	}
	if ids := got.LeadIDs(); len(ids) != 2 {
		t.Fatalf("LeadIDs = %v, want the two resolved leads", ids)
	}
}

func TestResolveRefusesLeadTargetsWithoutTheLookup(t *testing.T) {
	leads := book()
	if _, err := Resolve(nil, leads, "ws-1", []Target{{LeadID: "lead-a"}}); !errors.Is(err, campaign.ErrLeadTargetsUnavailable) {
		t.Fatalf("Resolve = %v, want ErrLeadTargetsUnavailable", err)
	}
	if _, err := Resolve(leads, nil, "ws-1", []Target{{Number: "5584999990005"}}); !errors.Is(err, campaign.ErrLeadTargetsUnavailable) {
		t.Fatalf("Resolve = %v, want ErrLeadTargetsUnavailable", err)
	}
	leads.err = errors.New("db down")
	if _, err := Resolve(leads, leads, "ws-1", []Target{{LeadID: "lead-a"}}); err == nil {
		t.Fatal("a failed lookup resolved")
	}
}

type record struct{ id string }

type keyBook struct {
	byKey map[string]*record
	err   error
}

var errNotFound = errors.New("not found")

func (k keyBook) FindByIdempotencyKey(workspaceID, key string) (*record, error) {
	if k.err != nil {
		return nil, k.err
	}
	if r, ok := k.byKey[workspaceID+"|"+key]; ok {
		return r, nil
	}
	return nil, errNotFound
}

func TestFindKeyed(t *testing.T) {
	keys := keyBook{byKey: map[string]*record{"ws-1|k:1/1": {id: "c-1"}}}
	if found, ok, err := FindKeyed[*record](keys, "ws-1", "k:1/1", errNotFound); err != nil || !ok || found.id != "c-1" {
		t.Fatalf("FindKeyed = %v %v %v", found, ok, err)
	}
	if _, ok, err := FindKeyed[*record](keys, "ws-1", "other", errNotFound); err != nil || ok {
		t.Fatalf("an unknown key = %v %v", ok, err)
	}
	if _, ok, err := FindKeyed[*record](keys, "ws-1", "", errNotFound); err != nil || ok {
		t.Fatalf("no key = %v %v", ok, err)
	}
	if _, _, err := FindKeyed[*record](nil, "ws-1", "k:1/1", errNotFound); !errors.Is(err, campaign.ErrIdempotencyUnavailable) {
		t.Fatalf("no key store = %v, want ErrIdempotencyUnavailable", err)
	}
	if _, _, err := FindKeyed[*record](keyBook{err: errors.New("db down")}, "ws-1", "k:1/1", errNotFound); err == nil {
		t.Fatal("a failed lookup found nothing silently")
	}
}
