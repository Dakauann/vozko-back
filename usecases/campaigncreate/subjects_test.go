package campaigncreate

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/address"
	"vozko/domain/campaign"
	"vozko/domain/lead"
)

type subjectLeads map[string]*lead.Lead

func (s subjectLeads) BindingLeads(_ context.Context, _ string, ids []string) ([]*lead.Lead, error) {
	out := []*lead.Lead{}
	for _, id := range ids {
		if l, ok := s[id]; ok {
			out = append(out, l)
		}
	}
	return out, nil
}

type addressBook struct{ attached int }

func (a *addressBook) AttachContactDetails(_ context.Context, _ string, leads []*lead.Lead) error {
	a.attached += len(leads)
	for _, l := range leads {
		l.Addresses = []lead.Address{{Primary: true, Postal: address.Postal{District: "Centro", City: "Barueri"}}}
	}
	return nil
}

type ownerNames map[string]string

func (o ownerNames) Names(ids ...string) map[string]string {
	out := map[string]string{}
	for _, id := range ids {
		out[id] = o[id]
	}
	return out
}

func plan(t *testing.T, sources ...campaign.BindingSource) campaign.BindingPlan {
	t.Helper()
	bindings := make([]campaign.VariableBinding, 0, len(sources))
	for _, s := range sources {
		bindings = append(bindings, campaign.VariableBinding{Source: s})
	}
	p, err := campaign.PlanBindings(bindings, len(bindings), nil)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSubjectsLoadOnlyWhatTheBindingsRead(t *testing.T) {
	book := &addressBook{}
	source := SubjectSource{
		Leads:    subjectLeads{"a": {ID: "a", WorkspaceID: "ws-1", Name: "Maria", Owner: "u-1"}, "x": {ID: "x", WorkspaceID: "ws-2"}},
		Contacts: book,
		Names:    ownerNames{"u-1": "Ana"},
	}
	subjects, err := source.Subjects(context.Background(), "ws-1", []string{"a", "x"}, plan(t, campaign.BindDistrict, campaign.BindOwnerName))
	if err != nil {
		t.Fatal(err)
	}
	if len(subjects) != 1 || subjects["a"].OwnerName != "Ana" || subjects["a"].Lead.PrimaryAddress().Postal.District != "Centro" || book.attached != 1 {
		t.Fatalf("subjects = %+v attached = %d", subjects, book.attached)
	}
	fixed, err := campaign.PlanBindings([]campaign.VariableBinding{{Source: campaign.BindLiteral, Value: "oi"}}, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	none, err := source.Subjects(context.Background(), "ws-1", []string{"a"}, fixed)
	if err != nil || len(none) != 0 || book.attached != 1 {
		t.Fatalf("a fixed text reads no lead: %+v %v", none, err)
	}
}

func TestSubjectsOfLeadsAlreadyInHandSkipTheLeadsWithoutID(t *testing.T) {
	source := SubjectSource{Leads: subjectLeads{}, Contacts: &addressBook{}, Names: ownerNames{}}
	subjects, err := source.SubjectsOf(context.Background(), "ws-1", []*lead.Lead{{ID: "a", Name: "Maria"}, {Name: "Sem id"}, nil}, plan(t, campaign.BindFirstName))
	if err != nil || len(subjects) != 1 {
		t.Fatalf("subjects = %+v, %v", subjects, err)
	}
}

func TestSubjectsFailClosedWhenTheSourceIsNotWired(t *testing.T) {
	if _, err := (SubjectSource{}).Subjects(context.Background(), "ws-1", []string{"a"}, plan(t, campaign.BindFirstName)); !errors.Is(err, campaign.ErrLeadTargetsUnavailable) {
		t.Fatalf("err = %v", err)
	}
}
