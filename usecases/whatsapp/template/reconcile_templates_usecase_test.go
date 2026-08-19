package template_usecase

import (
	"testing"

	"vozko/domain/shared"
	businessphone "vozko/domain/whatsapp/business_phone"
	"vozko/domain/whatsapp/template"
)

type reconcilePhoneRepoMock struct {
	phones     []*businessphone.WhatsAppBusinessPhoneNumber
	listAllErr error
}

func (m *reconcilePhoneRepoMock) Create(*businessphone.WhatsAppBusinessPhoneNumber) error { return nil }
func (m *reconcilePhoneRepoMock) Update(string, *businessphone.WhatsAppBusinessPhoneNumber) error {
	return nil
}
func (m *reconcilePhoneRepoMock) Delete(string) error { return nil }
func (m *reconcilePhoneRepoMock) FindByID(string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	return nil, businessphone.ErrPhoneNumberNotFound
}
func (m *reconcilePhoneRepoMock) FindByMetaPhoneNumberID(string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	return nil, businessphone.ErrPhoneNumberNotFound
}
func (m *reconcilePhoneRepoMock) FindByMetaPhoneNumberIDUnscoped(string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	return nil, businessphone.ErrPhoneNumberNotFound
}
func (m *reconcilePhoneRepoMock) FindByDisplayPhoneNumber(string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	return nil, businessphone.ErrPhoneNumberNotFound
}
func (m *reconcilePhoneRepoMock) FindByWABAId(string) ([]*businessphone.WhatsAppBusinessPhoneNumber, error) {
	return nil, nil
}
func (m *reconcilePhoneRepoMock) List(businessphone.ListInput) (*shared.PaginatedResult[*businessphone.WhatsAppBusinessPhoneNumber], error) {
	return nil, nil
}
func (m *reconcilePhoneRepoMock) ListAll() ([]*businessphone.WhatsAppBusinessPhoneNumber, error) {
	if m.listAllErr != nil {
		return nil, m.listAllErr
	}
	return m.phones, nil
}
func (m *reconcilePhoneRepoMock) BatchUpdate([]*businessphone.WhatsAppBusinessPhoneNumber) error {
	return nil
}
func (m *reconcilePhoneRepoMock) UpdateStatus(string, businessphone.Status) error { return nil }
func (m *reconcilePhoneRepoMock) UpdateBusinessProfile(string, businessphone.BusinessProfile) error {
	return nil
}
func (m *reconcilePhoneRepoMock) SyncFromMeta(*businessphone.WhatsAppBusinessPhoneNumber) error {
	return nil
}
func (m *reconcilePhoneRepoMock) ClearAccessToken(string) error { return nil }
func (m *reconcilePhoneRepoMock) ClearOwner(string) error       { return nil }
func (m *reconcilePhoneRepoMock) Restore(string) error          { return nil }

type reconcileSyncUseCaseMock struct {
	inputs     []template.SyncTemplatesInput
	errByPhone map[string]error
}

func (m *reconcileSyncUseCaseMock) Execute(input template.SyncTemplatesInput) ([]*template.Template, error) {
	m.inputs = append(m.inputs, input)
	if m.errByPhone != nil {
		if err := m.errByPhone[input.BusinessPhoneID]; err != nil {
			return nil, err
		}
	}
	return nil, nil
}

func TestReconcileTemplates_SyncsOnlyEligiblePhones(t *testing.T) {
	phoneRepo := &reconcilePhoneRepoMock{phones: []*businessphone.WhatsAppBusinessPhoneNumber{
		nil,
		{ID: "phone-connected", WABAId: "waba-1", AccessToken: "token-1", Status: businessphone.StatusConnected},
		{ID: "phone-rate-limited", WABAId: "waba-2", AccessToken: "token-2", Status: businessphone.StatusRateLimited},
		{ID: "phone-disconnected", WABAId: "waba-3", AccessToken: "token-3", Status: businessphone.StatusDisconnected},
		{ID: "phone-no-token", WABAId: "waba-4", Status: businessphone.StatusConnected},
		{ID: "phone-no-waba", AccessToken: "token-5", Status: businessphone.StatusConnected},
		// 360dialog channels authenticate with the channel D360-API-KEY, never an
		// access token, they must still be reconciled so their template statuses
		// refresh (previously they were silently skipped and stayed "pending").
		{ID: "phone-d360", WABAId: "waba-6", Provider: businessphone.ProviderDialog360, Dialog360APIKey: "d360-key", Status: businessphone.StatusConnected},
		// A 360dialog phone whose onboarding never captured a key is not reconcilable.
		{ID: "phone-d360-nokey", WABAId: "waba-7", Provider: businessphone.ProviderDialog360, Status: businessphone.StatusConnected},
	}}
	syncUC := &reconcileSyncUseCaseMock{}
	uc := NewReconcileTemplatesUseCase(phoneRepo, syncUC)

	if err := uc.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := map[string]bool{}
	for _, in := range syncUC.inputs {
		got[in.BusinessPhoneID] = true
	}
	wantSynced := []string{"phone-connected", "phone-rate-limited", "phone-d360"}
	if len(syncUC.inputs) != len(wantSynced) {
		t.Fatalf("expected %d sync calls, got %d (%v)", len(wantSynced), len(syncUC.inputs), got)
	}
	for _, id := range wantSynced {
		if !got[id] {
			t.Errorf("expected phone %q to be synced", id)
		}
	}
	for _, id := range []string{"phone-disconnected", "phone-no-token", "phone-no-waba", "phone-d360-nokey"} {
		if got[id] {
			t.Errorf("phone %q must NOT be synced", id)
		}
	}
}

func TestReconcileTemplates_IgnoresIndividualSyncFailures(t *testing.T) {
	phoneRepo := &reconcilePhoneRepoMock{phones: []*businessphone.WhatsAppBusinessPhoneNumber{
		{ID: "phone-1", WABAId: "waba-1", AccessToken: "token-1", Status: businessphone.StatusConnected},
		{ID: "phone-2", WABAId: "waba-2", AccessToken: "token-2", Status: businessphone.StatusConnected},
	}}
	syncUC := &reconcileSyncUseCaseMock{errByPhone: map[string]error{"phone-1": businessphone.ErrInvalidAccessToken}}
	uc := NewReconcileTemplatesUseCase(phoneRepo, syncUC)

	if err := uc.Execute(); err != nil {
		t.Fatalf("expected sync errors to be logged and swallowed, got %v", err)
	}
	if len(syncUC.inputs) != 2 {
		t.Fatalf("expected both eligible phones to be attempted, got %d", len(syncUC.inputs))
	}
}

func (m *reconcilePhoneRepoMock) UpdateCallsEnabled(string, bool) error { return nil }
