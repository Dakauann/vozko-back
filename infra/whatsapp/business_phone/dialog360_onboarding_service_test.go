package whatsapp_business_phone

import (
	"errors"
	"testing"
	"time"

	businessphone "vozko/domain/whatsapp/business_phone"
	"vozko/domain/whatsapp/waba"
	wc "vozko/domain/whatsapp_campaign"
)

type fakeOrganic struct{ calls int }

func (f *fakeOrganic) Execute(workspaceID, businessPhoneID, displayPhoneNumber string) (*wc.Campaign, bool, error) {
	f.calls++
	return &wc.Campaign{}, true, nil
}

// fakePartner implements businessphone.Dialog360PartnerService for tests.
type fakePartner struct {
	channels       []businessphone.Dialog360Channel
	genKeyCalls    int
	genKeyResponse *businessphone.APIKeyResult
	createCalls         int
	createErr           error  // when set, CreateClient returns this error
	findByEmailID       string // when set, FindClientByEmail returns this id (existing client)
	findOnlyAfterCreate bool   // models 360dialog's "created but returned error": empty until a create is attempted
	findCalls           int
	registerCalls       int
	registerErr         error
	lastRegister        businessphone.RegisterNumberInput
}

func (f *fakePartner) CreateClient(name, email string) (string, error) {
	f.createCalls++
	if f.createErr != nil {
		return "", f.createErr
	}
	return "client-1", nil
}
func (f *fakePartner) FindClientByEmail(email string) (string, error) {
	f.findCalls++
	if f.findOnlyAfterCreate && f.createCalls == 0 {
		return "", nil
	}
	return f.findByEmailID, nil
}
func (f *fakePartner) RegisterNumber(in businessphone.RegisterNumberInput) error {
	f.registerCalls++
	f.lastRegister = in
	return f.registerErr
}
func (f *fakePartner) ListChannels() ([]businessphone.Dialog360Channel, error) {
	return f.channels, nil
}
func (f *fakePartner) GetChannel(channelID string) (*businessphone.Dialog360Channel, error) {
	for i := range f.channels {
		if f.channels[i].ID == channelID {
			ch := f.channels[i]
			return &ch, nil
		}
	}
	return nil, nil
}
func (f *fakePartner) GenerateAPIKey(channelID string) (*businessphone.APIKeyResult, error) {
	f.genKeyCalls++
	if f.genKeyResponse != nil {
		return f.genKeyResponse, nil
	}
	return &businessphone.APIKeyResult{APIKey: "minted-key", Address: "https://waba-v2.360dialog.io"}, nil
}
func (f *fakePartner) GetPartnerBalance() (*businessphone.Dialog360Balance, error) { return nil, nil }
func (f *fakePartner) CancelChannel(clientID, channelID string) error              { return nil }
func (f *fakePartner) ReactivateChannel(clientID, channelID string) error          { return nil }
func (f *fakePartner) SetWebhookURL(url string) error                              { return nil }

// fakePhoneRepo embeds the interface so it satisfies it; only used methods are
// overridden (others panic if unexpectedly called).
type fakePhoneRepo struct {
	businessphone.Repository
	phones []*businessphone.WhatsAppBusinessPhoneNumber
}

func (r *fakePhoneRepo) Create(p *businessphone.WhatsAppBusinessPhoneNumber) error {
	if p.ID == "" {
		p.ID = "phone-" + p.MetaPhoneNumberID
	}
	r.phones = append(r.phones, p)
	return nil
}
func (r *fakePhoneRepo) Update(id string, p *businessphone.WhatsAppBusinessPhoneNumber) error {
	for i := range r.phones {
		if r.phones[i].ID == id {
			r.phones[i] = p
			return nil
		}
	}
	return businessphone.ErrPhoneNumberNotFound
}
func (r *fakePhoneRepo) FindByID(id string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	for _, p := range r.phones {
		if p.ID == id {
			return p, nil
		}
	}
	return nil, businessphone.ErrPhoneNumberNotFound
}
func (r *fakePhoneRepo) FindByMetaPhoneNumberID(metaID string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	for _, p := range r.phones {
		if p.MetaPhoneNumberID == metaID {
			return p, nil
		}
	}
	return nil, businessphone.ErrPhoneNumberNotFound
}
func (r *fakePhoneRepo) FindByWABAId(wabaID string) ([]*businessphone.WhatsAppBusinessPhoneNumber, error) {
	var out []*businessphone.WhatsAppBusinessPhoneNumber
	for _, p := range r.phones {
		if p.WABAId == wabaID {
			out = append(out, p)
		}
	}
	return out, nil
}
func (r *fakePhoneRepo) ListAll() ([]*businessphone.WhatsAppBusinessPhoneNumber, error) {
	return r.phones, nil
}

type fakeWABARepo struct {
	waba.Repository
	accounts []*waba.WhatsAppBusinessAccount
}

func (r *fakeWABARepo) FindByMetaWABAId(id string) (*waba.WhatsAppBusinessAccount, error) {
	for _, a := range r.accounts {
		if a.MetaWABAId == id {
			return a, nil
		}
	}
	return nil, waba.ErrWABANotFound
}
func (r *fakeWABARepo) Create(a *waba.WhatsAppBusinessAccount) error {
	if a.ID == "" {
		a.ID = "waba-" + a.MetaWABAId
	}
	r.accounts = append(r.accounts, a)
	return nil
}
func (r *fakeWABARepo) Update(id string, a *waba.WhatsAppBusinessAccount) error { return nil }

func newPendingPhone(metaID, wabaID string, createdAt time.Time) *businessphone.WhatsAppBusinessPhoneNumber {
	at := createdAt.UTC()
	return &businessphone.WhatsAppBusinessPhoneNumber{
		ID:                "phone-" + metaID,
		Provider:          businessphone.ProviderDialog360,
		MetaPhoneNumberID: metaID,
		WABAId:            wabaID,
		Status:            businessphone.StatusPending,
		OwnerWorkspaceID:  "ws-1",
		OwnerAssignedBy:   "user-1",
		OwnerAssignedAt:   &at,
		CreatedAt:         createdAt,
	}
}

func TestFinalize_ConnectsPendingAndMintsKey(t *testing.T) {
	now := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)
	phoneRepo := &fakePhoneRepo{phones: []*businessphone.WhatsAppBusinessPhoneNumber{
		newPendingPhone("pnid-1", "waba-ext-1", now),
	}}
	partner := &fakePartner{channels: []businessphone.Dialog360Channel{
		{ID: "chan-1", WABAExternalID: "waba-ext-1", PhoneNumber: "+5511999998888", Status: "live"},
	}}
	svc := NewDialog360OnboardingService(partner, phoneRepo, &fakeWABARepo{}, nil)

	if err := svc.Finalize("chan-1", now); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	got := phoneRepo.phones[0]
	if got.Status != businessphone.StatusConnected {
		t.Fatalf("status = %s, want CONNECTED", got.Status)
	}
	if got.Dialog360APIKey != "minted-key" || got.Dialog360ChannelID != "chan-1" {
		t.Fatalf("key/channel not persisted: key=%q channel=%q", got.Dialog360APIKey, got.Dialog360ChannelID)
	}
	if partner.genKeyCalls != 1 {
		t.Fatalf("GenerateAPIKey calls = %d, want 1", partner.genKeyCalls)
	}
}

func TestFinalize_IsIdempotentOnRedelivery(t *testing.T) {
	now := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)
	connected := newPendingPhone("pnid-1", "waba-ext-1", now)
	connected.Status = businessphone.StatusConnected
	connected.Dialog360ChannelID = "chan-1"
	connected.Dialog360APIKey = "existing-key"
	phoneRepo := &fakePhoneRepo{phones: []*businessphone.WhatsAppBusinessPhoneNumber{connected}}
	partner := &fakePartner{channels: []businessphone.Dialog360Channel{
		{ID: "chan-1", WABAExternalID: "waba-ext-1", PhoneNumber: "+5511999998888", Status: "live"},
	}}
	svc := NewDialog360OnboardingService(partner, phoneRepo, &fakeWABARepo{}, nil)

	if err := svc.Finalize("chan-1", now); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if partner.genKeyCalls != 0 {
		t.Fatalf("GenerateAPIKey called %d times on redelivery, want 0 (must not invalidate the working key)", partner.genKeyCalls)
	}
	if phoneRepo.phones[0].Dialog360APIKey != "existing-key" {
		t.Fatalf("working key was overwritten: %q", phoneRepo.phones[0].Dialog360APIKey)
	}
}

func TestReconcile_FlagsOrphanedChannelAndStalePending(t *testing.T) {
	now := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)
	// One healthy connected row, one stale pending row (created 2h ago).
	healthy := newPendingPhone("pnid-ok", "waba-ok", now)
	healthy.Status = businessphone.StatusConnected
	healthy.Dialog360ChannelID = "chan-ok"
	stale := newPendingPhone("pnid-stale", "waba-stale", now.Add(-2*time.Hour))
	phoneRepo := &fakePhoneRepo{phones: []*businessphone.WhatsAppBusinessPhoneNumber{healthy, stale}}

	partner := &fakePartner{channels: []businessphone.Dialog360Channel{
		{ID: "chan-ok", WABAExternalID: "waba-ok"},    // matches healthy
		{ID: "chan-orphan", WABAExternalID: "waba-x"}, // no local row -> orphan
	}}
	svc := NewDialog360OnboardingService(partner, phoneRepo, &fakeWABARepo{}, nil)

	report, err := svc.Reconcile(now, time.Hour)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(report.OrphanedChannels) != 1 || report.OrphanedChannels[0].ID != "chan-orphan" {
		t.Fatalf("orphaned channels = %+v, want [chan-orphan]", report.OrphanedChannels)
	}
	if len(report.StalePending) != 1 || report.StalePending[0] != "phone-pnid-stale" {
		t.Fatalf("stale pending = %+v, want [phone-pnid-stale]", report.StalePending)
	}
}

func TestStartProvisioning_MarksFailedButKeepsRowWhenShareFails(t *testing.T) {
	phoneRepo := &fakePhoneRepo{}
	wabaRepo := &fakeWABARepo{}
	partner := &fakePartner{registerErr: errors.New("502 from 360dialog")}
	svc := NewDialog360OnboardingService(partner, phoneRepo, wabaRepo, nil)

	phone, err := svc.StartProvisioning(businessphone.Dialog360ProvisionInput{
		WABAExternalID:   "waba-ext-1",
		PhoneNumberID:    "pnid-1",
		OwnerWorkspaceID: "ws-1",
		OwnerAssignedBy:  "user-1",
	})
	// The handover error is returned...
	if err == nil {
		t.Fatalf("expected error when account_sharing/numbers fails")
	}
	// ...but the phone row MUST exist and be visibly failed (the user's requirement).
	if phone == nil {
		t.Fatalf("phone row must be returned even on handover failure (must be visible in UI)")
	}
	if len(phoneRepo.phones) != 1 {
		t.Fatalf("phone row must be persisted, got %d rows", len(phoneRepo.phones))
	}
	got := phoneRepo.phones[0]
	if got.Status != businessphone.StatusOnboardingFailed {
		t.Fatalf("status = %s, want ONBOARDING_FAILED", got.Status)
	}
	if got.OnboardingError == "" {
		t.Fatalf("OnboardingError must carry the reason so the UI shows what is missing")
	}
}

func TestRetry_ReusesClientAndRecovers(t *testing.T) {
	// A row left FAILED by a prior attempt where the client was already created.
	failed := newPendingPhone("pnid-1", "waba-ext-1", time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC))
	failed.Status = businessphone.StatusOnboardingFailed
	failed.OnboardingError = "share number (account_sharing/numbers): 502"
	phoneRepo := &fakePhoneRepo{phones: []*businessphone.WhatsAppBusinessPhoneNumber{failed}}
	wabaRepo := &fakeWABARepo{accounts: []*waba.WhatsAppBusinessAccount{{
		ID: "waba-1", MetaWABAId: "waba-ext-1", Provider: "dialog360", Dialog360ClientID: "client-1",
	}}}
	partner := &fakePartner{} // RegisterNumber now succeeds
	svc := NewDialog360OnboardingService(partner, phoneRepo, wabaRepo, nil)

	phone, err := svc.Retry("phone-pnid-1", "ws-1")
	if err != nil {
		t.Fatalf("Retry: %v", err)
	}
	if phone.Status != businessphone.StatusPending || phone.OnboardingError != "" {
		t.Fatalf("after successful retry want PENDING with cleared error, got status=%s err=%q", phone.Status, phone.OnboardingError)
	}
	if partner.createCalls != 0 {
		t.Fatalf("CreateClient called %d times on retry, want 0 (must reuse the stored client id)", partner.createCalls)
	}
	if partner.registerCalls != 1 {
		t.Fatalf("RegisterNumber calls = %d, want 1", partner.registerCalls)
	}
}

// newFailedPhoneForRetry builds a FAILED dialog360 row to drive Retry through the
// client-resolution path (no persisted WABA client id present).
func newFailedPhoneForRetry() *fakePhoneRepo {
	failed := newPendingPhone("pnid-1", "waba-ext-1", time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC))
	failed.Status = businessphone.StatusOnboardingFailed
	failed.OnboardingError = "create client (account_sharing/clients): 360dialog returned 500"
	return &fakePhoneRepo{phones: []*businessphone.WhatsAppBusinessPhoneNumber{failed}}
}

// The number that motivated this fix: 360dialog created the client on prior attempts
// but returned 429/500, so no id was persisted. A retry must REUSE the existing
// client (found by its deterministic email) and never call CreateClient again — this
// is the exact duplication the production incident produced (3 leaked clients).
func TestRetry_ReusesExistingClientByEmail_NoDuplicate(t *testing.T) {
	phoneRepo := newFailedPhoneForRetry()
	wabaRepo := &fakeWABARepo{} // no persisted client id
	partner := &fakePartner{findByEmailID: "existing-client-9"}
	svc := NewDialog360OnboardingService(partner, phoneRepo, wabaRepo, nil)

	phone, err := svc.Retry("phone-pnid-1", "ws-1")
	if err != nil {
		t.Fatalf("Retry: %v", err)
	}
	if partner.createCalls != 0 {
		t.Fatalf("CreateClient called %d times, want 0 — must reuse the existing client, not leak a duplicate", partner.createCalls)
	}
	if partner.registerCalls != 1 || partner.lastRegister.ClientID != "existing-client-9" {
		t.Fatalf("RegisterNumber must run once with the reused client id, got calls=%d clientID=%q", partner.registerCalls, partner.lastRegister.ClientID)
	}
	if phone.Status != businessphone.StatusPending || phone.OnboardingError != "" {
		t.Fatalf("after successful retry want PENDING with cleared error, got status=%s err=%q", phone.Status, phone.OnboardingError)
	}
}

// If CreateClient itself errors but the client was actually created (360dialog's
// ack-then-500 bug), the retry must recover the id via the email lookup rather than
// failing — otherwise it re-creates on the next attempt and leaks duplicates.
func TestRetry_RecoversWhenCreateErroredButClientExists(t *testing.T) {
	phoneRepo := newFailedPhoneForRetry()
	partner := &fakePartner{
		createErr:           errors.New("360dialog returned 500: Internal Server Error"),
		findOnlyAfterCreate: true, // empty before the create, present after (it was created)
		findByEmailID:       "leaked-client-3",
	}
	svc := NewDialog360OnboardingService(partner, phoneRepo, &fakeWABARepo{}, nil)

	phone, err := svc.Retry("phone-pnid-1", "ws-1")
	if err != nil {
		t.Fatalf("Retry must recover from a create-but-500, got error: %v", err)
	}
	if partner.createCalls != 1 {
		t.Fatalf("CreateClient calls = %d, want exactly 1 (one attempt, then recover by lookup)", partner.createCalls)
	}
	if partner.lastRegister.ClientID != "leaked-client-3" {
		t.Fatalf("must register with the recovered client id, got %q", partner.lastRegister.ClientID)
	}
	if phone.Status != businessphone.StatusPending {
		t.Fatalf("want PENDING after recovery, got %s", phone.Status)
	}
}

// When no client exists yet, a single CreateClient runs and its id is used.
func TestRetry_CreatesClientWhenNoneExists(t *testing.T) {
	phoneRepo := newFailedPhoneForRetry()
	partner := &fakePartner{} // findByEmailID empty → nothing to reuse
	svc := NewDialog360OnboardingService(partner, phoneRepo, &fakeWABARepo{}, nil)

	if _, err := svc.Retry("phone-pnid-1", "ws-1"); err != nil {
		t.Fatalf("Retry: %v", err)
	}
	if partner.createCalls != 1 {
		t.Fatalf("CreateClient calls = %d, want 1 (create once when none exists)", partner.createCalls)
	}
	if partner.lastRegister.ClientID != "client-1" {
		t.Fatalf("must register with the newly created client id, got %q", partner.lastRegister.ClientID)
	}
}

// TestFinalize_PopulatesChannelMetadata proves a dialog360-hosted number gets its
// verified name, quality, messaging tier and review status from the partner channel
// at finalize time — the only source, since these numbers have no Meta access token
// for the Graph sync. Without this the number connects but shows empty metadata.
func TestFinalize_PopulatesChannelMetadata(t *testing.T) {
	now := time.Date(2026, 7, 14, 1, 0, 0, 0, time.UTC)
	phoneRepo := &fakePhoneRepo{phones: []*businessphone.WhatsAppBusinessPhoneNumber{
		newPendingPhone("pnid-meta", "waba-meta", now),
	}}
	partner := &fakePartner{channels: []businessphone.Dialog360Channel{{
		ID: "chan-meta", WABAExternalID: "waba-meta", PhoneNumber: "15553785635",
		PhoneName: "Vozko Homolog TMP", QualityRating: "GREEN",
		MessagingTier: "TIER_1K", ReviewStatus: "approved", Status: "live",
	}}}
	svc := NewDialog360OnboardingService(partner, phoneRepo, &fakeWABARepo{}, nil)

	if err := svc.Finalize("chan-meta", now); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	got := phoneRepo.phones[0]
	if got.Status != businessphone.StatusConnected {
		t.Fatalf("want CONNECTED, got %s", got.Status)
	}
	if got.DisplayPhoneNumber != "15553785635" {
		t.Fatalf("display number = %q, want 15553785635", got.DisplayPhoneNumber)
	}
	if got.VerifiedName != "Vozko Homolog TMP" {
		t.Fatalf("verified name = %q, want 'Vozko Homolog TMP'", got.VerifiedName)
	}
	if got.QualityRating != businessphone.QualityRatingGreen {
		t.Fatalf("quality = %q, want GREEN", got.QualityRating)
	}
	if got.MessagingLimitTier != "TIER_1K" {
		t.Fatalf("tier = %q, want TIER_1K", got.MessagingLimitTier)
	}
	if got.AccountReviewStatus != "approved" {
		t.Fatalf("review status = %q, want approved", got.AccountReviewStatus)
	}
}

func TestStartProvisioning_DoesNotReRunHandoverOnConnectedNumber(t *testing.T) {
	connected := newPendingPhone("pnid-1", "waba-ext-1", time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC))
	connected.Status = businessphone.StatusConnected
	connected.Dialog360APIKey = "live-key"
	phoneRepo := &fakePhoneRepo{phones: []*businessphone.WhatsAppBusinessPhoneNumber{connected}}
	partner := &fakePartner{}
	svc := NewDialog360OnboardingService(partner, phoneRepo, &fakeWABARepo{}, nil)

	phone, err := svc.StartProvisioning(businessphone.Dialog360ProvisionInput{
		WABAExternalID:   "waba-ext-1",
		PhoneNumberID:    "pnid-1",
		OwnerWorkspaceID: "ws-1",
		OwnerAssignedBy:  "user-1",
	})
	if err != nil {
		t.Fatalf("StartProvisioning on connected: %v", err)
	}
	if phone.Status != businessphone.StatusConnected || phone.Dialog360APIKey != "live-key" {
		t.Fatalf("a connected number must be left untouched, got status=%s key=%q", phone.Status, phone.Dialog360APIKey)
	}
	if partner.registerCalls != 0 || partner.createCalls != 0 {
		t.Fatalf("must not call the partner API for an already-connected number (create=%d register=%d)", partner.createCalls, partner.registerCalls)
	}
}

func TestFinalize_CoexistenceCreatesOrganicCampaign(t *testing.T) {
	now := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)
	phoneRepo := &fakePhoneRepo{phones: []*businessphone.WhatsAppBusinessPhoneNumber{
		newPendingPhone("pnid-cx", "waba-cx", now),
	}}
	partner := &fakePartner{channels: []businessphone.Dialog360Channel{
		{ID: "chan-cx", WABAExternalID: "waba-cx", PhoneNumber: "+5511999990000", Status: "live", IsOnBizApp: true},
	}}
	organic := &fakeOrganic{}
	svc := NewDialog360OnboardingService(partner, phoneRepo, &fakeWABARepo{}, organic)

	if err := svc.Finalize("chan-cx", now); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if organic.calls != 1 {
		t.Fatalf("coexistence (is_on_biz_app) must ensure the organic campaign once, got %d", organic.calls)
	}
}

// guard against accidental interface drift
var _ businessphone.Dialog360PartnerService = (*fakePartner)(nil)
var _ = errors.New
