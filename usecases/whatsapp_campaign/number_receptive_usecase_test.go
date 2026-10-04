package whatsapp_campaign_usecase

import (
	"errors"
	"testing"

	businessphone "vozko/domain/whatsapp/business_phone"
	wc "vozko/domain/whatsapp_campaign"
)

type receptivePhonesStub struct {
	phone *businessphone.WhatsAppBusinessPhoneNumber
	err   error
}

func (s receptivePhonesStub) FindByID(string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	return s.phone, s.err
}

type receptiveRepoStub struct {
	wc.Repository
	latest   *wc.Campaign
	findErr  error
	created  *wc.Campaign
	written  *wc.ReceptiveSettings
	writeWS  string
	writeErr error
}

func (s *receptiveRepoStub) FindLatestOrganicByBusinessPhone(string, string) (*wc.Campaign, error) {
	return s.latest, s.findErr
}

func (s *receptiveRepoStub) Create(c *wc.Campaign) error {
	s.created = c
	return nil
}

func (s *receptiveRepoStub) UpdateReceptive(workspaceID, _ string, settings wc.ReceptiveSettings) ([]string, error) {
	s.written, s.writeWS = &settings, workspaceID
	return []string{"c1"}, s.writeErr
}

func ownedPhone() *businessphone.WhatsAppBusinessPhoneNumber {
	return &businessphone.WhatsAppBusinessPhoneNumber{ID: "phone-1", OwnerWorkspaceID: "owner", DisplayPhoneNumber: "+55 11 4000 1234"}
}

func newNumberReceptive(phones receptivePhonesStub, repo *receptiveRepoStub) *NumberReceptive {
	return NewNumberReceptive(phones, repo, NewEnsureReceptiveContainerUseCase(repo))
}

func TestTheOwnerReadsTheNumbersReceptiveSettings(t *testing.T) {
	repo := &receptiveRepoStub{latest: &wc.Campaign{Type: wc.CampaignTypeOrganic, AgentID: "agent-1", EnableAgentResponses: true}}
	got, err := newNumberReceptive(receptivePhonesStub{phone: ownedPhone()}, repo).Get("owner", "phone-1")
	if err != nil || got.AgentID != "agent-1" || !got.EnableAgentResponses {
		t.Fatalf("got %+v err %v", got, err)
	}
}

func TestANumberWithoutContainerReadsAsAutomationOff(t *testing.T) {
	repo := &receptiveRepoStub{findErr: wc.ErrCampaignNotFound}
	got, err := newNumberReceptive(receptivePhonesStub{phone: ownedPhone()}, repo).Get("owner", "phone-1")
	if err != nil || got != (wc.ReceptiveSettings{}) || repo.created != nil {
		t.Fatalf("got %+v err %v created %v", got, err, repo.created)
	}
}

func TestReadingFailsWhenTheContainerLookupFails(t *testing.T) {
	sentinel := errors.New("database down")
	repo := &receptiveRepoStub{findErr: sentinel}
	if _, err := newNumberReceptive(receptivePhonesStub{phone: ownedPhone()}, repo).Get("owner", "phone-1"); !errors.Is(err, sentinel) {
		t.Fatalf("err %v", err)
	}
}

func TestTheOwnerSavesTheSettingsOnEveryContainerOfTheNumber(t *testing.T) {
	repo := &receptiveRepoStub{findErr: wc.ErrCampaignNotFound}
	in := wc.ReceptiveSettings{AgentID: " agent-1 ", EnableAgentResponses: true, EnableAnalysis: true}
	got, err := newNumberReceptive(receptivePhonesStub{phone: ownedPhone()}, repo).Update("owner", "phone-1", in)
	if err != nil {
		t.Fatal(err)
	}
	if repo.created == nil || repo.created.WorkspaceID != "owner" {
		t.Fatal("the first save must create the number's container")
	}
	if repo.written == nil || repo.writeWS != "owner" || repo.written.AgentID != "agent-1" || got.AgentID != "agent-1" {
		t.Fatalf("written %+v ws %q got %+v", repo.written, repo.writeWS, got)
	}
}

func TestOnlyTheOwnerOfTheNumberConfiguresIt(t *testing.T) {
	cases := []struct {
		name  string
		phone *businessphone.WhatsAppBusinessPhoneNumber
		err   error
		want  error
	}{
		{"granted workspace", ownedPhone(), nil, wc.ErrReceptiveNotOwner},
		{"number without owner", &businessphone.WhatsAppBusinessPhoneNumber{ID: "phone-1"}, nil, wc.ErrReceptiveNotOwner},
		{"unknown number", nil, businessphone.ErrPhoneNumberNotFound, wc.ErrCampaignBusinessPhoneNotFound},
		{"number lookup returns nothing", nil, nil, wc.ErrCampaignBusinessPhoneNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &receptiveRepoStub{latest: &wc.Campaign{Type: wc.CampaignTypeOrganic, AgentID: "agent-1"}}
			uc := newNumberReceptive(receptivePhonesStub{phone: tc.phone, err: tc.err}, repo)
			if _, err := uc.Get("granted", "phone-1"); !errors.Is(err, tc.want) {
				t.Fatalf("get err %v", err)
			}
			if _, err := uc.Update("granted", "phone-1", wc.ReceptiveSettings{EnableAgentResponses: true}); !errors.Is(err, tc.want) {
				t.Fatalf("update err %v", err)
			}
			if repo.written != nil || repo.created != nil {
				t.Fatal("nothing may be written for a workspace that does not own the number")
			}
		})
	}
}

func TestAFailedNumberLookupIsReported(t *testing.T) {
	sentinel := errors.New("database down")
	uc := newNumberReceptive(receptivePhonesStub{err: sentinel}, &receptiveRepoStub{})
	if _, err := uc.Get("owner", "phone-1"); !errors.Is(err, sentinel) {
		t.Fatalf("err %v", err)
	}
}
