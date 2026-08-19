package whatsapp_campaign_usecase

import (
	"context"
	"testing"

	"vozko/domain/shared"
	businessphone "vozko/domain/whatsapp/business_phone"
	wc "vozko/domain/whatsapp_campaign"
	"vozko/domain/workspace_phone_access"
)

type ownershipWorkspacePhoneAccessRepo struct {
	hasAccess bool
	err       error
	lastWS    string
	lastPhone string
}

func (m *ownershipWorkspacePhoneAccessRepo) Create(*workspace_phone_access.WorkspacePhoneAccess) error {
	return nil
}
func (m *ownershipWorkspacePhoneAccessRepo) Delete(string) error { return nil }
func (m *ownershipWorkspacePhoneAccessRepo) DeleteByWorkspaceAndPhone(string, string) error {
	return nil
}
func (m *ownershipWorkspacePhoneAccessRepo) FindByID(string) (*workspace_phone_access.WorkspacePhoneAccess, error) {
	return nil, workspace_phone_access.ErrAccessNotFound
}
func (m *ownershipWorkspacePhoneAccessRepo) FindByWorkspaceAndPhone(string, string) (*workspace_phone_access.WorkspacePhoneAccess, error) {
	return nil, workspace_phone_access.ErrAccessNotFound
}
func (m *ownershipWorkspacePhoneAccessRepo) ListByWorkspace(workspace_phone_access.ListWorkspaceAccessInput) (*shared.PaginatedResult[*workspace_phone_access.WorkspacePhoneAccess], error) {
	return &shared.PaginatedResult[*workspace_phone_access.WorkspacePhoneAccess]{Items: []*workspace_phone_access.WorkspacePhoneAccess{}}, nil
}
func (m *ownershipWorkspacePhoneAccessRepo) ListByPhone(workspace_phone_access.ListPhoneAccessInput) (*shared.PaginatedResult[*workspace_phone_access.WorkspacePhoneAccess], error) {
	return &shared.PaginatedResult[*workspace_phone_access.WorkspacePhoneAccess]{Items: []*workspace_phone_access.WorkspacePhoneAccess{}}, nil
}
func (m *ownershipWorkspacePhoneAccessRepo) GetPhoneIDsForWorkspace(string) ([]string, error) {
	return nil, nil
}
func (m *ownershipWorkspacePhoneAccessRepo) HasAccess(workspaceID, phoneID string) (bool, error) {
	m.lastWS = workspaceID
	m.lastPhone = phoneID
	return m.hasAccess, m.err
}

type ownershipBusinessPhoneRepo struct {
	phones map[string]*businessphone.WhatsAppBusinessPhoneNumber
}

func newOwnershipBusinessPhoneRepo() *ownershipBusinessPhoneRepo {
	return &ownershipBusinessPhoneRepo{phones: make(map[string]*businessphone.WhatsAppBusinessPhoneNumber)}
}

func (m *ownershipBusinessPhoneRepo) Create(phone *businessphone.WhatsAppBusinessPhoneNumber) error {
	m.phones[phone.ID] = phone
	return nil
}

func (m *ownershipBusinessPhoneRepo) Update(id string, phone *businessphone.WhatsAppBusinessPhoneNumber) error {
	m.phones[id] = phone
	return nil
}

func (m *ownershipBusinessPhoneRepo) Delete(id string) error {
	delete(m.phones, id)
	return nil
}

func (m *ownershipBusinessPhoneRepo) FindByID(id string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	phone, ok := m.phones[id]
	if !ok {
		return nil, businessphone.ErrPhoneNumberNotFound
	}
	return phone, nil
}

func (m *ownershipBusinessPhoneRepo) FindByMetaPhoneNumberID(string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	return nil, businessphone.ErrPhoneNumberNotFound
}

func (m *ownershipBusinessPhoneRepo) FindByDisplayPhoneNumber(string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	return nil, businessphone.ErrPhoneNumberNotFound
}

func (m *ownershipBusinessPhoneRepo) FindByWABAId(wabaID string) ([]*businessphone.WhatsAppBusinessPhoneNumber, error) {
	result := make([]*businessphone.WhatsAppBusinessPhoneNumber, 0)
	for _, phone := range m.phones {
		if phone.WABAId == wabaID {
			result = append(result, phone)
		}
	}
	return result, nil
}

func (m *ownershipBusinessPhoneRepo) List(businessphone.ListInput) (*shared.PaginatedResult[*businessphone.WhatsAppBusinessPhoneNumber], error) {
	return &shared.PaginatedResult[*businessphone.WhatsAppBusinessPhoneNumber]{Items: []*businessphone.WhatsAppBusinessPhoneNumber{}}, nil
}

func (m *ownershipBusinessPhoneRepo) ListAll() ([]*businessphone.WhatsAppBusinessPhoneNumber, error) {
	items := make([]*businessphone.WhatsAppBusinessPhoneNumber, 0, len(m.phones))
	for _, phone := range m.phones {
		items = append(items, phone)
	}
	return items, nil
}

func (m *ownershipBusinessPhoneRepo) BatchUpdate(phones []*businessphone.WhatsAppBusinessPhoneNumber) error {
	for _, phone := range phones {
		m.phones[phone.ID] = phone
	}
	return nil
}

func (m *ownershipBusinessPhoneRepo) UpdateStatus(string, businessphone.Status) error { return nil }

func (m *ownershipBusinessPhoneRepo) UpdateBusinessProfile(string, businessphone.BusinessProfile) error {
	return nil
}

func (m *ownershipBusinessPhoneRepo) SyncFromMeta(*businessphone.WhatsAppBusinessPhoneNumber) error {
	return nil
}

func (m *ownershipBusinessPhoneRepo) ClearAccessToken(string) error { return nil }
func (m *ownershipBusinessPhoneRepo) ClearOwner(string) error       { return nil }
func (m *ownershipBusinessPhoneRepo) FindByMetaPhoneNumberIDUnscoped(string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	return nil, businessphone.ErrPhoneNumberNotFound
}
func (m *ownershipBusinessPhoneRepo) Restore(string) error { return nil }

func TestCreateCampaignUseCase_RejectsBusinessPhoneOutsideWorkspace(t *testing.T) {
	businessPhones := newOwnershipBusinessPhoneRepo()
	businessPhones.phones["phone-1"] = &businessphone.WhatsAppBusinessPhoneNumber{ID: "phone-1", OwnerWorkspaceID: "ws-2"}
	phoneAccess := &ownershipWorkspacePhoneAccessRepo{hasAccess: true}

	uc := NewCreateCampaignUseCase(nil, nil, nil, nil, businessPhones, phoneAccess, nil, nil)

	_, err := uc.Execute(context.Background(), &wc.Campaign{
		WorkspaceID:     "ws-1",
		Name:            "Campaign",
		TemplateID:      "tmpl-1",
		BusinessPhoneID: "phone-1",
		Status:          wc.CampaignStatusStopped,
		PhoneInputs:     []wc.PhoneInput{{Number: "5511999999999", Variables: []string{"Alice"}}},
	})
	if err != wc.ErrCampaignBusinessPhoneNoAccess {
		t.Fatalf("expected ErrCampaignBusinessPhoneNoAccess, got %v", err)
	}
	if phoneAccess.lastWS != "" || phoneAccess.lastPhone != "" {
		t.Fatalf("expected legacy access fallback to be skipped for owned phone, got ws=%s phone=%s", phoneAccess.lastWS, phoneAccess.lastPhone)
	}
}

func TestUpdateCampaignUseCase_RejectsBusinessPhoneOutsideWorkspace(t *testing.T) {
	campaignRepo := newMockCampaignRepo()
	campaignRepo.campaigns["camp-1"] = &wc.Campaign{
		ID:              "camp-1",
		WorkspaceID:     "ws-1",
		Name:            "Campaign",
		TemplateID:      "tmpl-1",
		BusinessPhoneID: "phone-0",
		Status:          wc.CampaignStatusStopped,
	}

	businessPhones := newOwnershipBusinessPhoneRepo()
	businessPhones.phones["phone-2"] = &businessphone.WhatsAppBusinessPhoneNumber{ID: "phone-2", OwnerWorkspaceID: "ws-2"}
	phoneAccess := &ownershipWorkspacePhoneAccessRepo{hasAccess: true}

	uc := NewUpdateCampaignUseCase(campaignRepo, nil, nil, businessPhones, phoneAccess)

	_, err := uc.Execute("camp-1", &wc.Campaign{
		Name:            "Updated Campaign",
		TemplateID:      "tmpl-1",
		BusinessPhoneID: "phone-2",
		Status:          wc.CampaignStatusStopped,
	})
	if err != wc.ErrCampaignBusinessPhoneNoAccess {
		t.Fatalf("expected ErrCampaignBusinessPhoneNoAccess, got %v", err)
	}
	if phoneAccess.lastWS != "" || phoneAccess.lastPhone != "" {
		t.Fatalf("expected legacy access fallback to be skipped for owned phone, got ws=%s phone=%s", phoneAccess.lastWS, phoneAccess.lastPhone)
	}
}

func TestCreateCampaignUseCase_RejectsOrganicCampaignWhenPhoneOwnedByAnotherWorkspace(t *testing.T) {
	businessPhones := newOwnershipBusinessPhoneRepo()
	businessPhones.phones["phone-1"] = &businessphone.WhatsAppBusinessPhoneNumber{ID: "phone-1", OwnerWorkspaceID: "ws-2"}
	phoneAccess := &ownershipWorkspacePhoneAccessRepo{hasAccess: true}

	uc := NewCreateCampaignUseCase(nil, nil, nil, nil, businessPhones, phoneAccess, nil, nil)

	_, err := uc.Execute(context.Background(), &wc.Campaign{
		WorkspaceID:     "ws-1",
		Name:            "Organic Campaign",
		Type:            wc.CampaignTypeOrganic,
		BusinessPhoneID: "phone-1",
		Status:          wc.CampaignStatusStopped,
	})
	if err != wc.ErrCampaignBusinessPhoneNoAccess {
		t.Fatalf("expected ErrCampaignBusinessPhoneNoAccess, got %v", err)
	}
	if phoneAccess.lastWS != "" || phoneAccess.lastPhone != "" {
		t.Fatalf("expected legacy access fallback to be skipped for owned phone, got ws=%s phone=%s", phoneAccess.lastWS, phoneAccess.lastPhone)
	}
}

func TestCreateCampaignUseCase_AllowsGrantedBusinessPhoneAccessForOwnerlessPhone(t *testing.T) {
	businessPhones := newOwnershipBusinessPhoneRepo()
	businessPhones.phones["phone-1"] = &businessphone.WhatsAppBusinessPhoneNumber{ID: "phone-1"}
	phoneAccess := &ownershipWorkspacePhoneAccessRepo{hasAccess: true}

	uc := NewCreateCampaignUseCase(nil, nil, nil, nil, businessPhones, phoneAccess, nil, nil)

	allowed, err := hasTemporaryCampaignBusinessPhoneAccess("ws-1", "phone-1", businessPhones.phones["phone-1"], phoneAccess)
	if err != nil {
		t.Fatalf("unexpected access error: %v", err)
	}
	if !allowed {
		t.Fatal("expected granted workspace access to allow ownerless phone usage")
	}

	_, err = uc.Execute(context.Background(), &wc.Campaign{
		WorkspaceID:     "ws-1",
		Name:            "Campaign",
		TemplateID:      "tmpl-1",
		BusinessPhoneID: "phone-1",
		Status:          wc.CampaignStatusStopped,
		PhoneInputs:     []wc.PhoneInput{{Number: "5511999999999", Variables: []string{"Alice"}}},
	})
	if err != wc.ErrCampaignTemplateNotFound {
		t.Fatalf("expected to pass access check and fail later on template lookup, got %v", err)
	}
}

func TestUpdateCampaignUseCase_AllowsGrantedBusinessPhoneAccessForOwnerlessPhone(t *testing.T) {
	campaignRepo := newMockCampaignRepo()
	campaignRepo.campaigns["camp-1"] = &wc.Campaign{
		ID:              "camp-1",
		WorkspaceID:     "ws-1",
		Name:            "Campaign",
		TemplateID:      "tmpl-1",
		BusinessPhoneID: "phone-0",
		Status:          wc.CampaignStatusStopped,
	}

	businessPhones := newOwnershipBusinessPhoneRepo()
	businessPhones.phones["phone-2"] = &businessphone.WhatsAppBusinessPhoneNumber{ID: "phone-2"}
	phoneAccess := &ownershipWorkspacePhoneAccessRepo{hasAccess: true}

	uc := NewUpdateCampaignUseCase(campaignRepo, nil, nil, businessPhones, phoneAccess)

	_, err := uc.Execute("camp-1", &wc.Campaign{
		Name:            "Updated Campaign",
		TemplateID:      "tmpl-1",
		BusinessPhoneID: "phone-2",
		Status:          wc.CampaignStatusStopped,
	})
	if err != wc.ErrCampaignTemplateNotFound {
		t.Fatalf("expected to pass access check and fail later on template lookup, got %v", err)
	}
	if phoneAccess.lastWS != "ws-1" || phoneAccess.lastPhone != "phone-2" {
		t.Fatalf("expected access fallback for ws-1/phone-2, got ws=%s phone=%s", phoneAccess.lastWS, phoneAccess.lastPhone)
	}
}

func TestUpdateCampaignUseCase_AllowsOrganicCampaignWithoutTemplate(t *testing.T) {
	campaignRepo := newMockCampaignRepo()
	campaignRepo.campaigns["camp-1"] = &wc.Campaign{
		ID:              "camp-1",
		WorkspaceID:     "ws-1",
		Name:            "Organic Campaign",
		Type:            wc.CampaignTypeOrganic,
		TemplateID:      "",
		BusinessPhoneID: "phone-1",
		Status:          wc.CampaignStatusPaused,
		Archived:        true,
	}

	businessPhones := newOwnershipBusinessPhoneRepo()
	businessPhones.phones["phone-1"] = &businessphone.WhatsAppBusinessPhoneNumber{
		ID:               "phone-1",
		OwnerWorkspaceID: "ws-1",
	}

	uc := NewUpdateCampaignUseCase(campaignRepo, newMockEntryRepo(), nil, businessPhones, nil)

	updated, err := uc.Execute("camp-1", &wc.Campaign{
		Name:                 "Updated Organic Campaign",
		Type:                 wc.CampaignTypeOrganic,
		BusinessPhoneID:      "phone-1",
		AgentID:              "agent-1",
		EnableAgentResponses: true,
		PreferAudio:          true,
		Archived:             true,
	})
	if err != nil {
		t.Fatalf("expected organic update to succeed without template, got %v", err)
	}
	if updated.Type != wc.CampaignTypeOrganic {
		t.Fatalf("expected organic type to be preserved, got %s", updated.Type)
	}
	if updated.TemplateID != "" {
		t.Fatalf("expected template id to remain empty for organic campaign, got %s", updated.TemplateID)
	}
	if updated.Status != wc.CampaignStatusPaused {
		t.Fatalf("expected status to be preserved, got %s", updated.Status)
	}
	if !updated.Archived {
		t.Fatal("expected archived flag from input to be applied")
	}
	if updated.AgentID != "agent-1" {
		t.Fatalf("expected agent id to be updated, got %s", updated.AgentID)
	}
	if !updated.PreferAudio {
		t.Fatal("expected preferAudio to be updated")
	}
}

// TestUpdateCampaignUseCase_PersistsArchivedFromInput guards the archive bug:
// the Archive/Unarchive handlers load the campaign, flip Archived, and call
// Execute. If the usecase drops input.Archived, archiving silently no-ops, the
// card disappears optimistically but reappears on reload. This asserts both
// transitions actually persist.
func TestUpdateCampaignUseCase_PersistsArchivedFromInput(t *testing.T) {
	campaignRepo := newMockCampaignRepo()
	campaignRepo.campaigns["camp-1"] = &wc.Campaign{
		ID:              "camp-1",
		WorkspaceID:     "ws-1",
		Name:            "Organic Campaign",
		Type:            wc.CampaignTypeOrganic,
		BusinessPhoneID: "phone-1",
		Status:          wc.CampaignStatusStopped,
		Archived:        false,
	}

	businessPhones := newOwnershipBusinessPhoneRepo()
	businessPhones.phones["phone-1"] = &businessphone.WhatsAppBusinessPhoneNumber{
		ID:               "phone-1",
		OwnerWorkspaceID: "ws-1",
	}

	uc := NewUpdateCampaignUseCase(campaignRepo, newMockEntryRepo(), nil, businessPhones, nil)

	archiveInput := func(archived bool) *wc.Campaign {
		return &wc.Campaign{
			Name:            "Organic Campaign",
			Type:            wc.CampaignTypeOrganic,
			BusinessPhoneID: "phone-1",
			Archived:        archived,
		}
	}

	// Archive: false -> true must stick.
	updated, err := uc.Execute("camp-1", archiveInput(true))
	if err != nil {
		t.Fatalf("archive update failed: %v", err)
	}
	if !updated.Archived {
		t.Fatal("expected campaign to be archived after Execute(Archived=true)")
	}
	if stored, _ := campaignRepo.FindByID("camp-1"); !stored.Archived {
		t.Fatal("expected archived=true to be persisted in the repository")
	}

	// Unarchive: true -> false must stick.
	updated, err = uc.Execute("camp-1", archiveInput(false))
	if err != nil {
		t.Fatalf("unarchive update failed: %v", err)
	}
	if updated.Archived {
		t.Fatal("expected campaign to be unarchived after Execute(Archived=false)")
	}
	if stored, _ := campaignRepo.FindByID("camp-1"); stored.Archived {
		t.Fatal("expected archived=false to be persisted in the repository")
	}
}

func (r *ownershipBusinessPhoneRepo) UpdateCallsEnabled(string, bool) error { return nil }
