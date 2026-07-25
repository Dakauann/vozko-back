package businessphone_usecase

import (
	"errors"
	"testing"

	businessphone "vozko/domain/whatsapp/business_phone"
)

func seedPhone(repo *mockRepository, id, displayPhone, wabaID string) *businessphone.WhatsAppBusinessPhoneNumber {
	phone := &businessphone.WhatsAppBusinessPhoneNumber{
		ID:                 id,
		MetaPhoneNumberID:  "meta_" + id,
		WABAId:             wabaID,
		DisplayPhoneNumber: displayPhone,
		Status:             businessphone.StatusConnected,
		QualityRating:      businessphone.QualityRatingGreen,
		NameStatus:         businessphone.NameStatusApproved,
		VerifiedName:       "OldName",
		MessagingLimitTier: "TIER_1K",
	}
	repo.phoneNumbers[id] = phone
	return phone
}

func TestQualityUpdate_Flagged(t *testing.T) {
	repo := newMockRepo()
	seedPhone(repo, "p1", "+15551234567", "waba1")
	uc := NewHandlePhoneWebhook(repo, newMockWABARepo())

	payload := &businessphone.PhoneWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []businessphone.PhoneWebhookEntry{{
			ID: "waba1",
			Changes: []businessphone.PhoneWebhookChange{{
				Field: businessphone.FieldPhoneNumberQualityUpdate,
				Value: businessphone.PhoneWebhookValue{
					DisplayPhoneNumber: "+15551234567",
					CurrentLimit:       "TIER_10K",
					Event:              "FLAGGED",
				},
			}},
		}},
	}

	if err := uc.Execute(payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	phone := repo.phoneNumbers["p1"]
	if phone.Status != businessphone.StatusFlagged {
		t.Errorf("status = %s, want FLAGGED", phone.Status)
	}
	if phone.MessagingLimitTier != "TIER_10K" {
		t.Errorf("messaging limit = %s, want TIER_10K", phone.MessagingLimitTier)
	}
}

func TestQualityUpdate_Unflagged(t *testing.T) {
	repo := newMockRepo()
	phone := seedPhone(repo, "p1", "+15551234567", "waba1")
	phone.Status = businessphone.StatusFlagged
	uc := NewHandlePhoneWebhook(repo, newMockWABARepo())

	payload := &businessphone.PhoneWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []businessphone.PhoneWebhookEntry{{
			ID: "waba1",
			Changes: []businessphone.PhoneWebhookChange{{
				Field: businessphone.FieldPhoneNumberQualityUpdate,
				Value: businessphone.PhoneWebhookValue{
					DisplayPhoneNumber: "+15551234567",
					Event:              "UNFLAGGED",
				},
			}},
		}},
	}

	if err := uc.Execute(payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if phone.Status != businessphone.StatusConnected {
		t.Errorf("status = %s, want CONNECTED", phone.Status)
	}
}

func TestQualityUpdate_Restricted(t *testing.T) {
	repo := newMockRepo()
	seedPhone(repo, "p1", "+15551234567", "waba1")
	uc := NewHandlePhoneWebhook(repo, newMockWABARepo())

	payload := &businessphone.PhoneWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []businessphone.PhoneWebhookEntry{{
			ID: "waba1",
			Changes: []businessphone.PhoneWebhookChange{{
				Field: businessphone.FieldPhoneNumberQualityUpdate,
				Value: businessphone.PhoneWebhookValue{
					DisplayPhoneNumber: "+15551234567",
					CurrentLimit:       "TIER_250",
					Event:              "RESTRICTED",
				},
			}},
		}},
	}

	if err := uc.Execute(payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	phone := repo.phoneNumbers["p1"]
	if phone.Status != businessphone.StatusRestricted {
		t.Errorf("status = %s, want RESTRICTED", phone.Status)
	}
	if phone.MessagingLimitTier != "TIER_250" {
		t.Errorf("messaging limit = %s, want TIER_250", phone.MessagingLimitTier)
	}
}

func TestQualityUpdate_PhoneNotFound(t *testing.T) {
	repo := newMockRepo()
	uc := NewHandlePhoneWebhook(repo, newMockWABARepo())

	payload := &businessphone.PhoneWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []businessphone.PhoneWebhookEntry{{
			ID: "waba1",
			Changes: []businessphone.PhoneWebhookChange{{
				Field: businessphone.FieldPhoneNumberQualityUpdate,
				Value: businessphone.PhoneWebhookValue{
					DisplayPhoneNumber: "+19999999999",
					Event:              "FLAGGED",
				},
			}},
		}},
	}

	if err := uc.Execute(payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}
}

func TestQualityUpdate_NoChange(t *testing.T) {
	repo := newMockRepo()
	phone := seedPhone(repo, "p1", "+15551234567", "waba1")
	phone.Status = businessphone.StatusFlagged
	phone.MessagingLimitTier = "TIER_10K"
	uc := NewHandlePhoneWebhook(repo, newMockWABARepo())

	payload := &businessphone.PhoneWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []businessphone.PhoneWebhookEntry{{
			ID: "waba1",
			Changes: []businessphone.PhoneWebhookChange{{
				Field: businessphone.FieldPhoneNumberQualityUpdate,
				Value: businessphone.PhoneWebhookValue{
					DisplayPhoneNumber: "+15551234567",
					CurrentLimit:       "TIER_10K",
					Event:              "FLAGGED",
				},
			}},
		}},
	}

	if err := uc.Execute(payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}

}

func TestNameUpdate_Approved(t *testing.T) {
	repo := newMockRepo()
	phone := seedPhone(repo, "p1", "+15551234567", "waba1")
	phone.NameStatus = businessphone.NameStatusPendingReview
	phone.VerifiedName = "OldName"
	uc := NewHandlePhoneWebhook(repo, newMockWABARepo())

	payload := &businessphone.PhoneWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []businessphone.PhoneWebhookEntry{{
			ID: "waba1",
			Changes: []businessphone.PhoneWebhookChange{{
				Field: businessphone.FieldPhoneNumberNameUpdate,
				Value: businessphone.PhoneWebhookValue{
					DisplayPhoneNumber:    "+15551234567",
					Decision:              "APPROVED",
					RequestedVerifiedName: "NewBrandName",
				},
			}},
		}},
	}

	if err := uc.Execute(payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if phone.VerifiedName != "NewBrandName" {
		t.Errorf("verified name = %s, want NewBrandName", phone.VerifiedName)
	}
	if phone.NameStatus != businessphone.NameStatusApproved {
		t.Errorf("name status = %s, want APPROVED", phone.NameStatus)
	}
}

func TestNameUpdate_Rejected(t *testing.T) {
	repo := newMockRepo()
	phone := seedPhone(repo, "p1", "+15551234567", "waba1")
	phone.NameStatus = businessphone.NameStatusPendingReview
	uc := NewHandlePhoneWebhook(repo, newMockWABARepo())

	payload := &businessphone.PhoneWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []businessphone.PhoneWebhookEntry{{
			ID: "waba1",
			Changes: []businessphone.PhoneWebhookChange{{
				Field: businessphone.FieldPhoneNumberNameUpdate,
				Value: businessphone.PhoneWebhookValue{
					DisplayPhoneNumber:    "+15551234567",
					Decision:              "REJECTED",
					RequestedVerifiedName: "BadName",
				},
			}},
		}},
	}

	if err := uc.Execute(payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if phone.NameStatus != businessphone.NameStatusDeclined {
		t.Errorf("name status = %s, want DECLINED", phone.NameStatus)
	}

	if phone.VerifiedName != "OldName" {
		t.Errorf("verified name = %s, want OldName (unchanged)", phone.VerifiedName)
	}
}

func TestAccountAlerts_MessagingLimitUpdate(t *testing.T) {
	repo := newMockRepo()
	seedPhone(repo, "p1", "+15551234567", "waba1")
	uc := NewHandlePhoneWebhook(repo, newMockWABARepo())

	payload := &businessphone.PhoneWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []businessphone.PhoneWebhookEntry{{
			ID: "waba1",
			Changes: []businessphone.PhoneWebhookChange{{
				Field: businessphone.FieldAccountAlerts,
				Value: businessphone.PhoneWebhookValue{
					PhoneNumber:  "+15551234567",
					CurrentLimit: "TIER_100K",
					Event:        "MESSAGING_LIMIT_UPDATE",
				},
			}},
		}},
	}

	if err := uc.Execute(payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	phone := repo.phoneNumbers["p1"]
	if phone.MessagingLimitTier != "TIER_100K" {
		t.Errorf("messaging limit = %s, want TIER_100K", phone.MessagingLimitTier)
	}
}

func TestAccountAlerts_VerifiedAccount(t *testing.T) {
	repo := newMockRepo()
	phone := seedPhone(repo, "p1", "+15551234567", "waba1")
	phone.IsOfficialBusiness = false
	uc := NewHandlePhoneWebhook(repo, newMockWABARepo())

	payload := &businessphone.PhoneWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []businessphone.PhoneWebhookEntry{{
			ID: "waba1",
			Changes: []businessphone.PhoneWebhookChange{{
				Field: businessphone.FieldAccountAlerts,
				Value: businessphone.PhoneWebhookValue{
					PhoneNumber: "+15551234567",
					Event:       "VERIFIED_ACCOUNT",
				},
			}},
		}},
	}

	if err := uc.Execute(payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !phone.IsOfficialBusiness {
		t.Error("IsOfficialBusiness should be true after VERIFIED_ACCOUNT")
	}
}

func TestAccountAlerts_DisabledUpdate(t *testing.T) {
	repo := newMockRepo()
	seedPhone(repo, "p1", "+15551234567", "waba1")
	uc := NewHandlePhoneWebhook(repo, newMockWABARepo())

	payload := &businessphone.PhoneWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []businessphone.PhoneWebhookEntry{{
			ID: "waba1",
			Changes: []businessphone.PhoneWebhookChange{{
				Field: businessphone.FieldAccountAlerts,
				Value: businessphone.PhoneWebhookValue{
					PhoneNumber: "+15551234567",
					Event:       "DISABLED_UPDATE",
				},
			}},
		}},
	}

	if err := uc.Execute(payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	phone := repo.phoneNumbers["p1"]
	if phone.Status != businessphone.StatusBanned {
		t.Errorf("status = %s, want BANNED", phone.Status)
	}
}

func TestCapabilityUpdate_UpdatesAllPhonesInWABA(t *testing.T) {
	repo := newMockRepo()
	seedPhone(repo, "p1", "+15551111111", "waba1")
	seedPhone(repo, "p2", "+15552222222", "waba1")
	seedPhone(repo, "p3", "+15553333333", "waba2")
	uc := NewHandlePhoneWebhook(repo, newMockWABARepo())

	payload := &businessphone.PhoneWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []businessphone.PhoneWebhookEntry{{
			ID: "waba1",
			Changes: []businessphone.PhoneWebhookChange{{
				Field: businessphone.FieldBusinessCapabilityUpdate,
				Value: businessphone.PhoneWebhookValue{
					MaxDailyConversationPerPhone: 100000,
				},
			}},
		}},
	}

	if err := uc.Execute(payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if repo.phoneNumbers["p1"].MessagingLimitTier != "TIER_100K" {
		t.Errorf("p1 tier = %s, want TIER_100K", repo.phoneNumbers["p1"].MessagingLimitTier)
	}
	if repo.phoneNumbers["p2"].MessagingLimitTier != "TIER_100K" {
		t.Errorf("p2 tier = %s, want TIER_100K", repo.phoneNumbers["p2"].MessagingLimitTier)
	}

	if repo.phoneNumbers["p3"].MessagingLimitTier != "TIER_1K" {
		t.Errorf("p3 tier = %s, want TIER_1K (unchanged)", repo.phoneNumbers["p3"].MessagingLimitTier)
	}
}

func TestCapabilityUpdate_Unlimited(t *testing.T) {
	repo := newMockRepo()
	seedPhone(repo, "p1", "+15551111111", "waba1")
	uc := NewHandlePhoneWebhook(repo, newMockWABARepo())

	payload := &businessphone.PhoneWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []businessphone.PhoneWebhookEntry{{
			ID: "waba1",
			Changes: []businessphone.PhoneWebhookChange{{
				Field: businessphone.FieldBusinessCapabilityUpdate,
				Value: businessphone.PhoneWebhookValue{
					MaxDailyConversationPerPhone: 999999,
				},
			}},
		}},
	}

	if err := uc.Execute(payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if repo.phoneNumbers["p1"].MessagingLimitTier != "TIER_UNLIMITED" {
		t.Errorf("tier = %s, want TIER_UNLIMITED", repo.phoneNumbers["p1"].MessagingLimitTier)
	}
}

func TestAccountUpdate_Ban(t *testing.T) {
	repo := newMockRepo()
	seedPhone(repo, "p1", "+15551234567", "waba1")
	seedPhone(repo, "p2", "+15559999999", "waba1")
	uc := NewHandlePhoneWebhook(repo, newMockWABARepo())

	payload := &businessphone.PhoneWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []businessphone.PhoneWebhookEntry{{
			ID: "waba1",
			Changes: []businessphone.PhoneWebhookChange{{
				Field: businessphone.FieldAccountUpdate,
				Value: businessphone.PhoneWebhookValue{
					Event:   "DISABLED_UPDATE",
					BanInfo: &businessphone.BanInfo{WABABanState: "DISABLE", WABABanDate: "January 31, 2025"},
				},
			}},
		}},
	}

	if err := uc.Execute(payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if repo.phoneNumbers["p1"].Status != businessphone.StatusBanned {
		t.Errorf("p1 status = %s, want BANNED", repo.phoneNumbers["p1"].Status)
	}
	if repo.phoneNumbers["p2"].Status != businessphone.StatusBanned {
		t.Errorf("p2 status = %s, want BANNED", repo.phoneNumbers["p2"].Status)
	}
}

func TestAccountUpdate_Reinstate(t *testing.T) {
	repo := newMockRepo()
	p1 := seedPhone(repo, "p1", "+15551234567", "waba1")
	p1.Status = businessphone.StatusBanned
	p2 := seedPhone(repo, "p2", "+15559999999", "waba1")
	p2.Status = businessphone.StatusBanned
	uc := NewHandlePhoneWebhook(repo, newMockWABARepo())

	payload := &businessphone.PhoneWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []businessphone.PhoneWebhookEntry{{
			ID: "waba1",
			Changes: []businessphone.PhoneWebhookChange{{
				Field: businessphone.FieldAccountUpdate,
				Value: businessphone.PhoneWebhookValue{
					BanInfo: &businessphone.BanInfo{WABABanState: "REINSTATE"},
				},
			}},
		}},
	}

	if err := uc.Execute(payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if p1.Status != businessphone.StatusConnected {
		t.Errorf("p1 status = %s, want CONNECTED", p1.Status)
	}
	if p2.Status != businessphone.StatusConnected {
		t.Errorf("p2 status = %s, want CONNECTED", p2.Status)
	}
}

func TestExecute_NilPayload(t *testing.T) {
	repo := newMockRepo()
	uc := NewHandlePhoneWebhook(repo, newMockWABARepo())

	if err := uc.Execute(nil); err != nil {
		t.Fatalf("Execute(nil): %v", err)
	}
}

func TestExecute_EmptyEntries(t *testing.T) {
	repo := newMockRepo()
	uc := NewHandlePhoneWebhook(repo, newMockWABARepo())

	payload := &businessphone.PhoneWebhookPayload{
		Object: "whatsapp_business_account",
		Entry:  []businessphone.PhoneWebhookEntry{},
	}

	if err := uc.Execute(payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}
}

func TestQualityUpdate_NormalizesPhoneNumber(t *testing.T) {
	repo := newMockRepo()
	seedPhone(repo, "p1", "+15551234567", "waba1")
	uc := NewHandlePhoneWebhook(repo, newMockWABARepo())

	payload := &businessphone.PhoneWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []businessphone.PhoneWebhookEntry{{
			ID: "waba1",
			Changes: []businessphone.PhoneWebhookChange{{
				Field: businessphone.FieldPhoneNumberQualityUpdate,
				Value: businessphone.PhoneWebhookValue{
					DisplayPhoneNumber: "+1 555 123 4567",
					Event:              "FLAGGED",
					CurrentLimit:       "TIER_10K",
				},
			}},
		}},
	}

	if err := uc.Execute(payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	phone := repo.phoneNumbers["p1"]
	if phone.Status != businessphone.StatusFlagged {
		t.Errorf("status = %s, want FLAGGED (phone number should have been normalized)", phone.Status)
	}
}

func TestMultipleChangesInSinglePayload(t *testing.T) {
	repo := newMockRepo()
	phone := seedPhone(repo, "p1", "+15551234567", "waba1")
	phone.NameStatus = businessphone.NameStatusPendingReview
	uc := NewHandlePhoneWebhook(repo, newMockWABARepo())

	payload := &businessphone.PhoneWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []businessphone.PhoneWebhookEntry{{
			ID: "waba1",
			Changes: []businessphone.PhoneWebhookChange{
				{
					Field: businessphone.FieldPhoneNumberQualityUpdate,
					Value: businessphone.PhoneWebhookValue{
						DisplayPhoneNumber: "+15551234567",
						Event:              "FLAGGED",
						CurrentLimit:       "TIER_10K",
					},
				},
				{
					Field: businessphone.FieldPhoneNumberNameUpdate,
					Value: businessphone.PhoneWebhookValue{
						DisplayPhoneNumber:    "+15551234567",
						Decision:              "APPROVED",
						RequestedVerifiedName: "NewName",
					},
				},
			},
		}},
	}

	if err := uc.Execute(payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if phone.Status != businessphone.StatusFlagged {
		t.Errorf("status = %s, want FLAGGED", phone.Status)
	}
	if phone.VerifiedName != "NewName" {
		t.Errorf("verified name = %s, want NewName", phone.VerifiedName)
	}
}

func TestQualityUpdate_RepoUpdateError(t *testing.T) {
	repo := newMockRepo()
	seedPhone(repo, "p1", "+15551234567", "waba1")
	repo.updateErr = errors.New("db unavailable")
	uc := NewHandlePhoneWebhook(repo, newMockWABARepo())

	payload := &businessphone.PhoneWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []businessphone.PhoneWebhookEntry{{
			ID: "waba1",
			Changes: []businessphone.PhoneWebhookChange{{
				Field: businessphone.FieldPhoneNumberQualityUpdate,
				Value: businessphone.PhoneWebhookValue{
					DisplayPhoneNumber: "+15551234567",
					Event:              "FLAGGED",
				},
			}},
		}},
	}

	if err := uc.Execute(payload); err != nil {
		t.Fatalf("Execute should not return error: %v", err)
	}
}

func TestConversationLimitToTier(t *testing.T) {
	tests := []struct {
		input int
		want  string
	}{
		{0, ""},
		{-1, ""},
		{250, "TIER_250"},
		{1000, "TIER_1K"},
		{10000, "TIER_10K"},
		{100000, "TIER_100K"},
		{100001, "TIER_UNLIMITED"},
	}

	for _, tt := range tests {
		got := conversationLimitToTier(tt.input)
		if got != tt.want {
			t.Errorf("conversationLimitToTier(%d) = %s, want %s", tt.input, got, tt.want)
		}
	}
}

func TestNormalizePhoneNumber(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"+15551234567", "+15551234567"},
		{"+1 555 123 4567", "+15551234567"},
		{"+1 (555) 123-4567", "+15551234567"},
		{"15551234567", "15551234567"},
	}

	for _, tt := range tests {
		got := normalizePhoneNumber(tt.input)
		if got != tt.want {
			t.Errorf("normalizePhoneNumber(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestCapabilityUpdate_AlsoUpdatesWABAEntity(t *testing.T) {
	repo := newMockRepo()
	seedPhone(repo, "p1", "+15551111111", "waba1")
	wabaRepo := newMockWABARepo()
	seedWABA(wabaRepo, "w1", "waba1")
	uc := NewHandlePhoneWebhook(repo, wabaRepo)

	payload := &businessphone.PhoneWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []businessphone.PhoneWebhookEntry{{
			ID: "waba1",
			Changes: []businessphone.PhoneWebhookChange{{
				Field: businessphone.FieldBusinessCapabilityUpdate,
				Value: businessphone.PhoneWebhookValue{
					MaxDailyConversationPerPhone: 100000,
				},
			}},
		}},
	}

	if err := uc.Execute(payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if repo.phoneNumbers["p1"].MessagingLimitTier != "TIER_100K" {
		t.Errorf("phone tier = %s, want TIER_100K", repo.phoneNumbers["p1"].MessagingLimitTier)
	}

	w := wabaRepo.accounts["w1"]
	if w.MessagingLimitTier != "TIER_100K" {
		t.Errorf("WABA tier = %s, want TIER_100K", w.MessagingLimitTier)
	}
}

func TestCapabilityUpdate_WABANotFound_StillUpdatesPhones(t *testing.T) {
	repo := newMockRepo()
	seedPhone(repo, "p1", "+15551111111", "waba1")
	wabaRepo := newMockWABARepo()
	uc := NewHandlePhoneWebhook(repo, wabaRepo)

	payload := &businessphone.PhoneWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []businessphone.PhoneWebhookEntry{{
			ID: "waba1",
			Changes: []businessphone.PhoneWebhookChange{{
				Field: businessphone.FieldBusinessCapabilityUpdate,
				Value: businessphone.PhoneWebhookValue{
					MaxDailyConversationPerPhone: 10000,
				},
			}},
		}},
	}

	if err := uc.Execute(payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if repo.phoneNumbers["p1"].MessagingLimitTier != "TIER_10K" {
		t.Errorf("phone tier = %s, want TIER_10K", repo.phoneNumbers["p1"].MessagingLimitTier)
	}
}

func TestCapabilityUpdate_WABATierNoChange(t *testing.T) {
	repo := newMockRepo()
	seedPhone(repo, "p1", "+15551111111", "waba1")
	wabaRepo := newMockWABARepo()
	w := seedWABA(wabaRepo, "w1", "waba1")
	w.MessagingLimitTier = "TIER_100K"
	uc := NewHandlePhoneWebhook(repo, wabaRepo)

	payload := &businessphone.PhoneWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []businessphone.PhoneWebhookEntry{{
			ID: "waba1",
			Changes: []businessphone.PhoneWebhookChange{{
				Field: businessphone.FieldBusinessCapabilityUpdate,
				Value: businessphone.PhoneWebhookValue{
					MaxDailyConversationPerPhone: 100000,
				},
			}},
		}},
	}

	if err := uc.Execute(payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if wabaRepo.accounts["w1"].MessagingLimitTier != "TIER_100K" {
		t.Errorf("WABA tier = %s, want TIER_100K", wabaRepo.accounts["w1"].MessagingLimitTier)
	}
}

func TestAccountUpdate_VerifiedAccount(t *testing.T) {
	repo := newMockRepo()
	phone := seedPhone(repo, "p1", "+15551234567", "waba1")
	phone.IsOfficialBusiness = false
	uc := NewHandlePhoneWebhook(repo, newMockWABARepo())

	payload := &businessphone.PhoneWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []businessphone.PhoneWebhookEntry{{
			ID: "waba1",
			Changes: []businessphone.PhoneWebhookChange{{
				Field: businessphone.FieldAccountUpdate,
				Value: businessphone.PhoneWebhookValue{
					Event:       "VERIFIED_ACCOUNT",
					PhoneNumber: "+15551234567",
				},
			}},
		}},
	}

	if err := uc.Execute(payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !phone.IsOfficialBusiness {
		t.Error("IsOfficialBusiness should be true after VERIFIED_ACCOUNT via account_update")
	}
}

func TestAccountUpdate_PartnerAdded(t *testing.T) {
	repo := newMockRepo()
	uc := NewHandlePhoneWebhook(repo, newMockWABARepo())

	payload := &businessphone.PhoneWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []businessphone.PhoneWebhookEntry{{
			ID: "biz_portfolio_123",
			Changes: []businessphone.PhoneWebhookChange{{
				Field: businessphone.FieldAccountUpdate,
				Value: businessphone.PhoneWebhookValue{
					Event: "PARTNER_ADDED",
					WABAInfo: &businessphone.WABAInfo{
						WABAId:          "waba_999",
						OwnerBusinessID: "biz_123",
					},
				},
			}},
		}},
	}

	if err := uc.Execute(payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}
}

func TestAccountUpdate_ScheduleForDisable(t *testing.T) {
	repo := newMockRepo()
	seedPhone(repo, "p1", "+15551234567", "waba1")
	seedPhone(repo, "p2", "+15559999999", "waba1")
	uc := NewHandlePhoneWebhook(repo, newMockWABARepo())

	payload := &businessphone.PhoneWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []businessphone.PhoneWebhookEntry{{
			ID: "waba1",
			Changes: []businessphone.PhoneWebhookChange{{
				Field: businessphone.FieldAccountUpdate,
				Value: businessphone.PhoneWebhookValue{
					Event:   "DISABLED_UPDATE",
					BanInfo: &businessphone.BanInfo{WABABanState: "SCHEDULE_FOR_DISABLE", WABABanDate: "February 1, 2025"},
				},
			}},
		}},
	}

	if err := uc.Execute(payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if repo.phoneNumbers["p1"].Status != businessphone.StatusFlagged {
		t.Errorf("p1 status = %s, want FLAGGED", repo.phoneNumbers["p1"].Status)
	}
	if repo.phoneNumbers["p2"].Status != businessphone.StatusFlagged {
		t.Errorf("p2 status = %s, want FLAGGED", repo.phoneNumbers["p2"].Status)
	}
}

func TestAccountReviewUpdate_Approved(t *testing.T) {
	repo := newMockRepo()
	wabaRepo := newMockWABARepo()
	w := seedWABA(wabaRepo, "w1", "waba1")
	w.AccountReviewStatus = "PENDING"
	uc := NewHandlePhoneWebhook(repo, wabaRepo)

	payload := &businessphone.PhoneWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []businessphone.PhoneWebhookEntry{{
			ID: "waba1",
			Changes: []businessphone.PhoneWebhookChange{{
				Field: businessphone.FieldAccountReviewUpdate,
				Value: businessphone.PhoneWebhookValue{
					Decision: "APPROVED",
				},
			}},
		}},
	}

	if err := uc.Execute(payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if w.AccountReviewStatus != "APPROVED" {
		t.Errorf("account review status = %s, want APPROVED", w.AccountReviewStatus)
	}
}

func TestAccountReviewUpdate_Rejected(t *testing.T) {
	repo := newMockRepo()
	wabaRepo := newMockWABARepo()
	w := seedWABA(wabaRepo, "w1", "waba1")
	w.AccountReviewStatus = "PENDING"
	uc := NewHandlePhoneWebhook(repo, wabaRepo)

	payload := &businessphone.PhoneWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []businessphone.PhoneWebhookEntry{{
			ID: "waba1",
			Changes: []businessphone.PhoneWebhookChange{{
				Field: businessphone.FieldAccountReviewUpdate,
				Value: businessphone.PhoneWebhookValue{
					Decision: "REJECTED",
				},
			}},
		}},
	}

	if err := uc.Execute(payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if w.AccountReviewStatus != "REJECTED" {
		t.Errorf("account review status = %s, want REJECTED", w.AccountReviewStatus)
	}
}

func TestAccountReviewUpdate_WABANotFound(t *testing.T) {
	repo := newMockRepo()
	wabaRepo := newMockWABARepo()
	uc := NewHandlePhoneWebhook(repo, wabaRepo)

	payload := &businessphone.PhoneWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []businessphone.PhoneWebhookEntry{{
			ID: "waba_unknown",
			Changes: []businessphone.PhoneWebhookChange{{
				Field: businessphone.FieldAccountReviewUpdate,
				Value: businessphone.PhoneWebhookValue{
					Decision: "APPROVED",
				},
			}},
		}},
	}

	if err := uc.Execute(payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}
}

func TestBusinessStatusUpdate_VerificationApproved(t *testing.T) {
	repo := newMockRepo()
	wabaRepo := newMockWABARepo()
	w := seedWABA(wabaRepo, "w1", "waba1")
	w.BusinessVerificationStatus = "PENDING"
	uc := NewHandlePhoneWebhook(repo, wabaRepo)

	payload := &businessphone.PhoneWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []businessphone.PhoneWebhookEntry{{
			ID: "waba1",
			Changes: []businessphone.PhoneWebhookChange{{
				Field: businessphone.FieldBusinessStatusUpdate,
				Value: businessphone.PhoneWebhookValue{
					Event: "BUSINESS_VERIFICATION_APPROVED",
				},
			}},
		}},
	}

	if err := uc.Execute(payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if w.BusinessVerificationStatus != "VERIFIED" {
		t.Errorf("business verification status = %s, want VERIFIED", w.BusinessVerificationStatus)
	}
}

func TestBusinessStatusUpdate_VerificationRejected(t *testing.T) {
	repo := newMockRepo()
	wabaRepo := newMockWABARepo()
	w := seedWABA(wabaRepo, "w1", "waba1")
	w.BusinessVerificationStatus = "PENDING"
	uc := NewHandlePhoneWebhook(repo, wabaRepo)

	payload := &businessphone.PhoneWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []businessphone.PhoneWebhookEntry{{
			ID: "waba1",
			Changes: []businessphone.PhoneWebhookChange{{
				Field: businessphone.FieldBusinessStatusUpdate,
				Value: businessphone.PhoneWebhookValue{
					Event: "BUSINESS_VERIFICATION_REJECTED",
				},
			}},
		}},
	}

	if err := uc.Execute(payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if w.BusinessVerificationStatus != "REJECTED" {
		t.Errorf("business verification status = %s, want REJECTED", w.BusinessVerificationStatus)
	}
}

func TestBusinessStatusUpdate_DecisionFallback(t *testing.T) {
	repo := newMockRepo()
	wabaRepo := newMockWABARepo()
	w := seedWABA(wabaRepo, "w1", "waba1")
	uc := NewHandlePhoneWebhook(repo, wabaRepo)

	payload := &businessphone.PhoneWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []businessphone.PhoneWebhookEntry{{
			ID: "waba1",
			Changes: []businessphone.PhoneWebhookChange{{
				Field: businessphone.FieldBusinessStatusUpdate,
				Value: businessphone.PhoneWebhookValue{
					Event:    "UNKNOWN_EVENT",
					Decision: "SOME_STATUS",
				},
			}},
		}},
	}

	if err := uc.Execute(payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if w.BusinessVerificationStatus != "SOME_STATUS" {
		t.Errorf("business verification status = %s, want SOME_STATUS", w.BusinessVerificationStatus)
	}
}

func TestBusinessStatusUpdate_WABANotFound(t *testing.T) {
	repo := newMockRepo()
	wabaRepo := newMockWABARepo()
	uc := NewHandlePhoneWebhook(repo, wabaRepo)

	payload := &businessphone.PhoneWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []businessphone.PhoneWebhookEntry{{
			ID: "waba_unknown",
			Changes: []businessphone.PhoneWebhookChange{{
				Field: businessphone.FieldBusinessStatusUpdate,
				Value: businessphone.PhoneWebhookValue{
					Event: "BUSINESS_VERIFICATION_APPROVED",
				},
			}},
		}},
	}

	if err := uc.Execute(payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}
}

func TestBusinessStatusUpdate_NoChange(t *testing.T) {
	repo := newMockRepo()
	wabaRepo := newMockWABARepo()
	w := seedWABA(wabaRepo, "w1", "waba1")
	w.BusinessVerificationStatus = "VERIFIED"
	uc := NewHandlePhoneWebhook(repo, wabaRepo)

	payload := &businessphone.PhoneWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []businessphone.PhoneWebhookEntry{{
			ID: "waba1",
			Changes: []businessphone.PhoneWebhookChange{{
				Field: businessphone.FieldBusinessStatusUpdate,
				Value: businessphone.PhoneWebhookValue{
					Event: "VERIFIED",
				},
			}},
		}},
	}

	if err := uc.Execute(payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if w.BusinessVerificationStatus != "VERIFIED" {
		t.Errorf("business verification status = %s, want VERIFIED", w.BusinessVerificationStatus)
	}
}
