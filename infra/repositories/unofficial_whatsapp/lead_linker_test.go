package unofficial_whatsapp_repository

import (
	"context"
	"testing"

	lead_domain "vozko/domain/lead"
	"vozko/domain/shared"
)

// linkerLeadRepo records what the linker asks of the lead port. Only the two
// methods the bridge uses are real; the rest of the port is satisfied by the
// embedded nil interface, which panics loudly if anything else is called.
type linkerLeadRepo struct {
	lead_domain.Repository

	byNumber map[string]*lead_domain.Lead
	findErr  error

	// What FindOrCreate was asked to write, so a test can assert on the write
	// that did NOT happen.
	findOrCreateCalls []lead_domain.LeadUpdate
}

func (r *linkerLeadRepo) FindByNumber(_ string, number string) (*lead_domain.Lead, error) {
	if r.findErr != nil {
		return nil, r.findErr
	}
	if l, ok := r.byNumber[number]; ok {
		return l, nil
	}
	return nil, lead_domain.ErrLeadNotFound
}

func (r *linkerLeadRepo) FindOrCreate(workspaceID, number string, update lead_domain.LeadUpdate) (*lead_domain.Lead, bool, error) {
	r.findOrCreateCalls = append(r.findOrCreateCalls, update)
	if l, ok := r.byNumber[number]; ok {
		if update.Name != "" {
			l.Name = update.Name
		}
		return l, false, nil
	}
	created := &lead_domain.Lead{ID: "new-lead", WorkspaceID: workspaceID, Number: number, Name: update.Name}
	r.byNumber[number] = created
	return created, true, nil
}

var _ = shared.EntryTypeUnofficialWhatsApp // keep the channel this guards named

// THE regression this file exists for.
//
// The contact and the lead are the same person on this channel, and the linker
// runs on every inbound message carrying whatever name the handset advertises.
// Passing that name through to an already-named lead rewrote the column each
// time, so an operator's rename in the CRM survived exactly until the customer
// sent their next message — a data loss with no error, no log and no way for
// the operator to tell it had happened.
func TestEnsureLeadForPhone_PushnameNeverOverwritesAnOperatorsName(t *testing.T) {
	repo := &linkerLeadRepo{byNumber: map[string]*lead_domain.Lead{
		"5584994409624": {ID: "lead-1", Name: "Dakauann Teste"},
	}}
	linker := NewLeadLinker(repo)

	id, err := linker.EnsureLeadForPhone(context.Background(), "ws-1", "5584994409624", "Dakauann")
	if err != nil {
		t.Fatalf("EnsureLeadForPhone: %v", err)
	}
	if id != "lead-1" {
		t.Fatalf("id = %q, want lead-1", id)
	}
	if got := repo.byNumber["5584994409624"].Name; got != "Dakauann Teste" {
		t.Fatalf("the pushname overwrote the operator's rename: name = %q", got)
	}
	if len(repo.findOrCreateCalls) != 0 {
		t.Fatalf("a named lead must not be written at all, got %d write(s): %+v",
			len(repo.findOrCreateCalls), repo.findOrCreateCalls)
	}
}

// The other half: the pushname is still what gives a brand-new lead a name, so
// the leads page does not fill up with bare numbers.
func TestEnsureLeadForPhone_PushnameStillNamesANewLead(t *testing.T) {
	repo := &linkerLeadRepo{byNumber: map[string]*lead_domain.Lead{}}
	linker := NewLeadLinker(repo)

	id, err := linker.EnsureLeadForPhone(context.Background(), "ws-1", "5584994409624", "Dakauann")
	if err != nil {
		t.Fatalf("EnsureLeadForPhone: %v", err)
	}
	if id == "" {
		t.Fatal("no lead id returned for a new number")
	}
	if got := repo.byNumber["5584994409624"].Name; got != "Dakauann" {
		t.Fatalf("new lead name = %q, want the pushname", got)
	}
}

// A lead that exists but has never been named is the gap the pushname is FOR:
// it must still be filled, otherwise clearing a name once would leave the row
// permanently anonymous.
func TestEnsureLeadForPhone_PushnameFillsABlankName(t *testing.T) {
	repo := &linkerLeadRepo{byNumber: map[string]*lead_domain.Lead{
		"5584994409624": {ID: "lead-1", Name: "   "},
	}}
	linker := NewLeadLinker(repo)

	if _, err := linker.EnsureLeadForPhone(context.Background(), "ws-1", "5584994409624", "Dakauann"); err != nil {
		t.Fatalf("EnsureLeadForPhone: %v", err)
	}
	if got := repo.byNumber["5584994409624"].Name; got != "Dakauann" {
		t.Fatalf("blank name = %q, want the pushname to fill it", got)
	}
}

// A lookup failure must not cost the message its lead. FindOrCreate still runs,
// which is the pre-existing behaviour, so the worst case degrades to what the
// code did before rather than dropping the link.
func TestEnsureLeadForPhone_LookupFailureStillLinks(t *testing.T) {
	repo := &linkerLeadRepo{
		byNumber: map[string]*lead_domain.Lead{},
		findErr:  context.DeadlineExceeded,
	}
	linker := NewLeadLinker(repo)

	id, err := linker.EnsureLeadForPhone(context.Background(), "ws-1", "5584994409624", "Dakauann")
	if err != nil {
		t.Fatalf("EnsureLeadForPhone: %v", err)
	}
	if id == "" {
		t.Fatal("a failed name lookup must not cost the message its lead link")
	}
}

func TestEnsureLeadForPhone_IgnoresUnusableInput(t *testing.T) {
	repo := &linkerLeadRepo{byNumber: map[string]*lead_domain.Lead{}}
	linker := NewLeadLinker(repo)

	for _, tc := range []struct{ ws, phone string }{
		{"", "5584994409624"},
		{"ws-1", "not-a-number"},
	} {
		id, err := linker.EnsureLeadForPhone(context.Background(), tc.ws, tc.phone, "Dakauann")
		if err != nil || id != "" {
			t.Errorf("EnsureLeadForPhone(%q, %q) = %q, %v; want \"\", nil", tc.ws, tc.phone, id, err)
		}
	}
}
