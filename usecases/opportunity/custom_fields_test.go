package opportunity_usecase

import (
	"errors"
	"testing"
	"time"

	"vozko/domain/customfield"
)

func TestCreateRefusesWhenFieldDefinitionsAreNotWired(t *testing.T) {
	svc := NewService(Deps{
		Repo:      newFakeOppRepo(),
		Links:     &fakeLinks{},
		Stages:    salesStages(),
		Pipelines: fakePipelines{},
		Owners:    fakeOwners{},
		Leads:     leadsIn{"ws1": true},
		Entries:   entriesIn("ws1"),
		Assign:    assignAccessStub{granted: map[string]bool{"u1": true}},
		Clock:     func() time.Time { return fixedNow },
	})
	if _, err := svc.Create("ws1", baseCreate()); !errors.Is(err, customfield.ErrDefinitionsUnavailable) {
		t.Fatalf("Create() error = %v, want ErrDefinitionsUnavailable", err)
	}
}

func TestCreateRefusesWhenFieldDefinitionsCannotBeRead(t *testing.T) {
	boom := errors.New("definitions down")
	svc := serviceWithFields(newFakeOppRepo(), &fakeFieldRepo{err: boom})
	if _, err := svc.Create("ws1", baseCreate()); !errors.Is(err, boom) {
		t.Fatalf("Create() error = %v, want the read error", err)
	}
}

func TestUnknownCustomFieldKeepsItsOpportunityName(t *testing.T) {
	if !errors.Is(ErrUnknownCustomField, customfield.ErrUnknownKey) {
		t.Fatal("ErrUnknownCustomField must stay an alias of the domain error")
	}
}
