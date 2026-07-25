package businessphone_usecase

import (
	"errors"
	"testing"

	businessphone "vozko/domain/whatsapp/business_phone"
	workspace_addon "vozko/domain/workspace/workspace_addon"
)

type fakeOwnerReader struct {
	active       int64
	connected    []businessphone.OwnerPhone
	suspended    []businessphone.OwnerPhone
	connectedCnt map[string]int
	suspendedWS  []string
	channelRefs  []businessphone.Dialog360ChannelRef
	listErr      error
}

func (f *fakeOwnerReader) CountConnectedDialog360GroupedByOwner() (map[string]int, error) {
	return f.connectedCnt, f.listErr
}
func (f *fakeOwnerReader) ListWorkspaceIDsWithSuspendedDialog360() ([]string, error) {
	return f.suspendedWS, f.listErr
}
func (f *fakeOwnerReader) CountActiveDialog360ByOwner(string) (int64, error) { return f.active, nil }
func (f *fakeOwnerReader) FindConnectedDialog360ByOwner(string) ([]businessphone.OwnerPhone, error) {
	return f.connected, nil
}
func (f *fakeOwnerReader) FindSuspendedDialog360ByOwner(string) ([]businessphone.OwnerPhone, error) {
	return f.suspended, nil
}
func (f *fakeOwnerReader) ListDialog360ChannelRefs() ([]businessphone.Dialog360ChannelRef, error) {
	return f.channelRefs, f.listErr
}

type fakeEntitlements struct{ total int }

func (f *fakeEntitlements) Execute(string) ([]workspace_addon.WorkspaceEntitlement, error) {
	return []workspace_addon.WorkspaceEntitlement{
		{Kind: workspace_addon.EntitlementWhatsAppBusinessPhones, Total: f.total},
	}, nil
}

type fakePartnerSvc struct {
	cancelled     []string
	reactivated   []string
	cancelErr     error
	channels      []businessphone.Dialog360Channel
	listErr       error
	getChannelRes *businessphone.Dialog360Channel // returned by GetChannel (default nil,nil)
	getChannelErr error
}

func (f *fakePartnerSvc) CreateClient(string, string) (string, error) { return "", nil }
func (f *fakePartnerSvc) FindClientByEmail(string) (string, error)    { return "", nil }
func (f *fakePartnerSvc) GetChannel(string) (*businessphone.Dialog360Channel, error) {
	return f.getChannelRes, f.getChannelErr
}
func (f *fakePartnerSvc) RegisterNumber(businessphone.RegisterNumberInput) error { return nil }
func (f *fakePartnerSvc) ListChannels() ([]businessphone.Dialog360Channel, error) {
	return f.channels, f.listErr
}
func (f *fakePartnerSvc) GenerateAPIKey(string) (*businessphone.APIKeyResult, error) { return nil, nil }
func (f *fakePartnerSvc) GetPartnerBalance() (*businessphone.Dialog360Balance, error) {
	return nil, nil
}
func (f *fakePartnerSvc) CancelChannel(clientID, ch string) error {
	f.cancelled = append(f.cancelled, clientID+"/"+ch)
	return f.cancelErr
}
func (f *fakePartnerSvc) ReactivateChannel(clientID, ch string) error {
	f.reactivated = append(f.reactivated, clientID+"/"+ch)
	return nil
}
func (f *fakePartnerSvc) SetWebhookURL(string) error { return nil }

func seedDialog360Phone(repo *mockRepository, id, channelID string, status businessphone.Status) {
	repo.phoneNumbers[id] = &businessphone.WhatsAppBusinessPhoneNumber{
		ID:                 id,
		Provider:           businessphone.ProviderDialog360,
		Dialog360ChannelID: channelID,
		Status:             status,
	}
}

func TestOnEntitlementReduced_SuspendsExcessChannels(t *testing.T) {
	repo := newMockRepo()
	seedDialog360Phone(repo, "pA", "chA", businessphone.StatusConnected)
	seedDialog360Phone(repo, "pB", "chB", businessphone.StatusConnected)
	partner := &fakePartnerSvc{}
	reader := &fakeOwnerReader{
		connected: []businessphone.OwnerPhone{
			{ID: "pA", Dialog360ChannelID: "chA", Dialog360ClientID: "clA"},
			{ID: "pB", Dialog360ChannelID: "chB", Dialog360ClientID: "clB"},
		},
	}
	uc := NewDeactivateExcessPhonesUseCase(&fakeEntitlements{total: 1}, reader, repo, partner)

	if err := uc.OnEntitlementReduced("ws", workspace_addon.EntitlementWhatsAppBusinessPhones); err != nil {
		t.Fatalf("OnEntitlementReduced: %v", err)
	}
	if len(partner.cancelled) != 1 || partner.cancelled[0] != "clA/chA" {
		t.Fatalf("expected one client-scoped cancellation of clA/chA, got %v", partner.cancelled)
	}
	if repo.phoneNumbers["pA"].Status != businessphone.StatusSuspended {
		t.Fatalf("expected pA suspended, got %s", repo.phoneNumbers["pA"].Status)
	}
	if repo.phoneNumbers["pB"].Status != businessphone.StatusConnected {
		t.Fatalf("expected pB kept connected, got %s", repo.phoneNumbers["pB"].Status)
	}
}

func TestOnEntitlementIncreased_ReactivatesUpToRoom(t *testing.T) {
	repo := newMockRepo()
	seedDialog360Phone(repo, "p1", "ch1", businessphone.StatusSuspended)
	seedDialog360Phone(repo, "p2", "ch2", businessphone.StatusSuspended)
	partner := &fakePartnerSvc{}
	reader := &fakeOwnerReader{
		active: 0,
		suspended: []businessphone.OwnerPhone{
			{ID: "p1", Dialog360ChannelID: "ch1", Dialog360ClientID: "cl1"},
			{ID: "p2", Dialog360ChannelID: "ch2", Dialog360ClientID: "cl2"},
		},
	}
	// total 1, active 0 -> room for 1 reactivation.
	uc := NewDeactivateExcessPhonesUseCase(&fakeEntitlements{total: 1}, reader, repo, partner)

	if err := uc.OnEntitlementIncreased("ws", workspace_addon.EntitlementWhatsAppBusinessPhones); err != nil {
		t.Fatalf("OnEntitlementIncreased: %v", err)
	}
	if len(partner.reactivated) != 1 || partner.reactivated[0] != "cl1/ch1" {
		t.Fatalf("expected one client-scoped reactivation of cl1/ch1, got %v", partner.reactivated)
	}
	if repo.phoneNumbers["p1"].Status != businessphone.StatusConnected {
		t.Fatalf("expected p1 reactivated, got %s", repo.phoneNumbers["p1"].Status)
	}
	if repo.phoneNumbers["p2"].Status != businessphone.StatusSuspended {
		t.Fatalf("expected p2 still suspended (no room), got %s", repo.phoneNumbers["p2"].Status)
	}
}

func TestOnEntitlementReduced_CancelFailure_LeavesConnected(t *testing.T) {
	repo := newMockRepo()
	seedDialog360Phone(repo, "pA", "chA", businessphone.StatusConnected)
	partner := &fakePartnerSvc{cancelErr: errors.New("360dialog 503")}
	reader := &fakeOwnerReader{
		connected: []businessphone.OwnerPhone{{ID: "pA", Dialog360ChannelID: "chA", Dialog360ClientID: "clA"}},
	}
	uc := NewDeactivateExcessPhonesUseCase(&fakeEntitlements{total: 0}, reader, repo, partner)

	if err := uc.OnEntitlementReduced("ws", workspace_addon.EntitlementWhatsAppBusinessPhones); err != nil {
		t.Fatalf("OnEntitlementReduced: %v", err)
	}
	if len(partner.cancelled) != 1 {
		t.Fatalf("expected a cancel attempt, got %v", partner.cancelled)
	}
	// The cancel failed, so the channel may still be billing: the number MUST stay
	// connected (visible + retried), never hidden behind a SUSPENDED row.
	if repo.phoneNumbers["pA"].Status != businessphone.StatusConnected {
		t.Fatalf("expected pA to stay connected after a failed cancel, got %s", repo.phoneNumbers["pA"].Status)
	}
}

func TestOnEntitlementReduced_MissingClientID_LeavesConnectedAndDoesNotCall(t *testing.T) {
	repo := newMockRepo()
	seedDialog360Phone(repo, "pA", "chA", businessphone.StatusConnected)
	partner := &fakePartnerSvc{}
	reader := &fakeOwnerReader{
		connected: []businessphone.OwnerPhone{{ID: "pA", Dialog360ChannelID: "chA", Dialog360ClientID: ""}},
	}
	uc := NewDeactivateExcessPhonesUseCase(&fakeEntitlements{total: 0}, reader, repo, partner)

	if err := uc.OnEntitlementReduced("ws", workspace_addon.EntitlementWhatsAppBusinessPhones); err != nil {
		t.Fatalf("OnEntitlementReduced: %v", err)
	}
	if len(partner.cancelled) != 0 {
		t.Fatalf("must not attempt a client-scoped cancel without a client id, got %v", partner.cancelled)
	}
	if repo.phoneNumbers["pA"].Status != businessphone.StatusConnected {
		t.Fatalf("expected pA to stay connected when client id is missing, got %s", repo.phoneNumbers["pA"].Status)
	}
}
