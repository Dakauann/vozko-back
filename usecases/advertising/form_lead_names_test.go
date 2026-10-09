package advertising

import (
	"testing"

	ads "vozko/domain/advertising"
	"vozko/domain/lead"
)

type namedCRM struct{ leads []*lead.Lead }

func (c namedCRM) FindOrCreate(string, string, lead.LeadUpdate) (*lead.Lead, bool, error) {
	return nil, false, nil
}

func (c namedCRM) FindByIDs(string, []string) ([]*lead.Lead, error) { return c.leads, nil }

func TestFormLeadNamesNeverShowANumberAsAName(t *testing.T) {
	uc := &FormsUseCase{crm: namedCRM{leads: []*lead.Lead{
		{ID: "l-1", Number: "5511987654321", Name: "5511987654321"},
		{ID: "l-2", Number: "5511912345678", Name: "Maria"},
	}}}
	names, err := uc.crmNames("ws-1", []*ads.FormLead{{LeadID: "l-1"}, {LeadID: "l-2"}})
	if err != nil {
		t.Fatal(err)
	}
	if names["l-1"] != "" || names["l-2"] != "Maria" {
		t.Fatalf("names = %v", names)
	}
}
