package businessphone_usecase

import (
	"errors"
	"testing"

	businessphone "vozko/domain/whatsapp/business_phone"
)

func TestListUseCase_Execute(t *testing.T) {
	repo := newMockRepo()
	repo.phoneNumbers["1"] = &businessphone.WhatsAppBusinessPhoneNumber{
		ID:                 "1",
		WABAId:             "waba-123",
		DisplayPhoneNumber: "+5511999998888",
		VerifiedName:       "Test Business",
		Status:             businessphone.StatusConnected,
		QualityRating:      businessphone.QualityRatingGreen,
	}
	repo.phoneNumbers["2"] = &businessphone.WhatsAppBusinessPhoneNumber{
		ID:                 "2",
		WABAId:             "waba-123",
		DisplayPhoneNumber: "+5511888887777",
		VerifiedName:       "Another Business",
		Status:             businessphone.StatusPending,
		QualityRating:      businessphone.QualityRatingYellow,
	}

	uc := NewListUseCase(repo)

	result, err := uc.Execute(businessphone.ListInput{})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.TotalItems != 2 {
		t.Errorf("Expected 2 items, got %d", result.TotalItems)
	}
}

func TestListUseCase_Execute_FilterByStatus(t *testing.T) {
	repo := newMockRepo()
	repo.phoneNumbers["1"] = &businessphone.WhatsAppBusinessPhoneNumber{
		ID:               "1",
		OwnerWorkspaceID: "ws-1",
		Status:           businessphone.StatusConnected,
	}
	repo.phoneNumbers["2"] = &businessphone.WhatsAppBusinessPhoneNumber{
		ID:               "2",
		OwnerWorkspaceID: "ws-2",
		Status:           businessphone.StatusPending,
	}

	uc := NewListUseCase(repo)

	result, err := uc.Execute(businessphone.ListInput{
		Status: businessphone.StatusConnected,
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.TotalItems != 1 {
		t.Errorf("Expected 1 item, got %d", result.TotalItems)
	}
}

func TestListUseCase_Execute_FilterByOwnerWorkspace(t *testing.T) {
	repo := newMockRepo()
	repo.phoneNumbers["1"] = &businessphone.WhatsAppBusinessPhoneNumber{
		ID:               "1",
		OwnerWorkspaceID: "ws-1",
	}
	repo.phoneNumbers["2"] = &businessphone.WhatsAppBusinessPhoneNumber{
		ID:               "2",
		OwnerWorkspaceID: "ws-2",
	}

	uc := NewListUseCase(repo)

	result, err := uc.Execute(businessphone.ListInput{OwnerWorkspaceID: "ws-1"})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.TotalItems != 1 {
		t.Fatalf("expected 1 item, got %d", result.TotalItems)
	}
	if len(result.Items) != 1 || result.Items[0].ID != "1" {
		t.Fatalf("expected phone 1, got %+v", result.Items)
	}
}

func TestListUseCase_Execute_FilterByGrantedAccess(t *testing.T) {
	repo := newMockRepo()
	repo.phoneNumbers["1"] = &businessphone.WhatsAppBusinessPhoneNumber{
		ID:               "1",
		OwnerWorkspaceID: "ws-owner",
	}
	repo.phoneNumbers["2"] = &businessphone.WhatsAppBusinessPhoneNumber{
		ID:               "2",
		OwnerWorkspaceID: "ws-other",
	}

	uc := NewListUseCase(repo)

	result, err := uc.Execute(businessphone.ListInput{OwnerWorkspaceID: "ws-1", AccessPhoneIDs: []string{"1"}})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.TotalItems != 1 {
		t.Fatalf("expected 1 item, got %d", result.TotalItems)
	}
	if len(result.Items) != 1 || result.Items[0].ID != "1" {
		t.Fatalf("expected granted phone 1, got %+v", result.Items)
	}
}

func TestListUseCase_Execute_Error(t *testing.T) {
	repo := newMockRepo()
	repo.listErr = errors.New("database error")

	uc := NewListUseCase(repo)

	_, err := uc.Execute(businessphone.ListInput{})
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
}

func TestGetUseCase_Execute(t *testing.T) {
	repo := newMockRepo()
	expected := &businessphone.WhatsAppBusinessPhoneNumber{
		ID:                 "test-id",
		DisplayPhoneNumber: "+5511999998888",
		VerifiedName:       "Test Business",
	}
	repo.phoneNumbers["test-id"] = expected

	uc := NewGetUseCase(repo)

	result, err := uc.Execute("test-id")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.ID != expected.ID {
		t.Errorf("Expected ID %s, got %s", expected.ID, result.ID)
	}
}

func TestGetUseCase_Execute_NotFound(t *testing.T) {
	repo := newMockRepo()
	uc := NewGetUseCase(repo)

	_, err := uc.Execute("nonexistent")
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
	if !errors.Is(err, businessphone.ErrPhoneNumberNotFound) {
		t.Errorf("Expected ErrPhoneNumberNotFound, got %v", err)
	}
}

func TestSyncPhoneNumberUseCase_Execute(t *testing.T) {
	repo := newMockRepo()
	repo.phoneNumbers["phone-id"] = &businessphone.WhatsAppBusinessPhoneNumber{
		ID:                 "phone-id",
		MetaPhoneNumberID:  "meta-phone-1",
		WABAId:             "waba-123",
		DisplayPhoneNumber: "+5511999998888",
		VerifiedName:       "Old Name",
		Status:             businessphone.StatusPending,
	}

	metaAPI := newMockMetaAPI()
	metaAPI.phoneNumber = &businessphone.MetaPhoneNumberInfo{
		ID:                 "meta-phone-1",
		DisplayPhoneNumber: "+5511999998888",
		VerifiedName:       "Updated Name",
		Status:             "CONNECTED",
		QualityRating:      "GREEN",
	}

	uc := NewSyncPhoneNumberUseCase(repo, metaAPI)

	result, err := uc.Execute(businessphone.SyncPhoneNumberInput{
		PhoneID:     "phone-id",
		AccessToken: "access-token",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.VerifiedName != "Updated Name" {
		t.Errorf("Expected VerifiedName 'Updated Name', got '%s'", result.VerifiedName)
	}
	if result.Status != businessphone.StatusConnected {
		t.Errorf("Expected Status CONNECTED, got %s", result.Status)
	}
}

func TestSyncPhoneNumberUseCase_Execute_NotFound(t *testing.T) {
	repo := newMockRepo()
	metaAPI := newMockMetaAPI()

	uc := NewSyncPhoneNumberUseCase(repo, metaAPI)

	_, err := uc.Execute(businessphone.SyncPhoneNumberInput{
		PhoneID:     "nonexistent",
		AccessToken: "access-token",
	})
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
}

func TestRegisterPhoneUseCase_Execute(t *testing.T) {
	repo := newMockRepo()
	metaAPI := newMockMetaAPI()
	metaAPI.phoneNumber = &businessphone.MetaPhoneNumberInfo{
		ID:                 "meta-phone-1",
		DisplayPhoneNumber: "+5511999998888",
		VerifiedName:       "Test Business",
		Status:             "CONNECTED",
		QualityRating:      "GREEN",
		NameStatus:         "APPROVED",
	}

	repo.phoneNumbers["phone-id"] = &businessphone.WhatsAppBusinessPhoneNumber{
		ID:                "phone-id",
		MetaPhoneNumberID: "meta-phone-1",
		Status:            businessphone.StatusPending,
	}

	uc := NewRegisterPhoneUseCase(repo, metaAPI)

	result, err := uc.Execute(businessphone.RegisterPhoneInput{
		PhoneID:     "phone-id",
		Pin:         "123456",
		AccessToken: "access-token",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.Status != businessphone.StatusConnected {
		t.Errorf("Expected Status CONNECTED, got %s", result.Status)
	}
}

func TestRegisterPhoneUseCase_Execute_AlreadyRegistered(t *testing.T) {
	repo := newMockRepo()
	metaAPI := newMockMetaAPI()

	repo.phoneNumbers["phone-id"] = &businessphone.WhatsAppBusinessPhoneNumber{
		ID:                "phone-id",
		MetaPhoneNumberID: "meta-phone-1",
		Status:            businessphone.StatusConnected,
	}

	uc := NewRegisterPhoneUseCase(repo, metaAPI)

	_, err := uc.Execute(businessphone.RegisterPhoneInput{
		PhoneID:     "phone-id",
		Pin:         "123456",
		AccessToken: "access-token",
	})
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
	if !errors.Is(err, businessphone.ErrPhoneNumberAlreadyRegistered) {
		t.Errorf("Expected ErrPhoneNumberAlreadyRegistered, got %v", err)
	}
}

func TestRegisterPhoneUseCase_Execute_ValidationErrors(t *testing.T) {
	repo := newMockRepo()
	metaAPI := newMockMetaAPI()
	uc := NewRegisterPhoneUseCase(repo, metaAPI)

	tests := []struct {
		name  string
		input businessphone.RegisterPhoneInput
	}{
		{
			name: "Missing PhoneID",
			input: businessphone.RegisterPhoneInput{
				Pin:         "123456",
				AccessToken: "token",
			},
		},
		{
			name: "Missing Pin",
			input: businessphone.RegisterPhoneInput{
				PhoneID:     "phone-id",
				AccessToken: "token",
			},
		},
		{
			name: "Missing AccessToken",
			input: businessphone.RegisterPhoneInput{
				PhoneID: "phone-id",
				Pin:     "123456",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := uc.Execute(tt.input)
			if err == nil {
				t.Fatal("Expected error, got nil")
			}
		})
	}
}

func TestDeregisterPhoneUseCase_Execute(t *testing.T) {
	repo := newMockRepo()
	metaAPI := newMockMetaAPI()
	metaAPI.phoneNumber = &businessphone.MetaPhoneNumberInfo{
		ID:     "meta-phone-1",
		Status: "DISCONNECTED",
	}

	repo.phoneNumbers["phone-id"] = &businessphone.WhatsAppBusinessPhoneNumber{
		ID:                "phone-id",
		MetaPhoneNumberID: "meta-phone-1",
		Status:            businessphone.StatusConnected,
	}

	uc := NewDeregisterPhoneUseCase(repo, metaAPI)

	result, err := uc.Execute(businessphone.DeregisterPhoneInput{
		PhoneID:     "phone-id",
		AccessToken: "access-token",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.Status != businessphone.StatusDisconnected {
		t.Errorf("Expected Status DISCONNECTED, got %s", result.Status)
	}
}

func TestRequestVerificationCodeUseCase_Execute(t *testing.T) {
	repo := newMockRepo()
	repo.phoneNumbers["phone-id"] = &businessphone.WhatsAppBusinessPhoneNumber{
		ID:                "phone-id",
		MetaPhoneNumberID: "meta-phone-1",
		Status:            businessphone.StatusPending,
	}

	metaAPI := newMockMetaAPI()

	uc := NewRequestVerificationCodeUseCase(repo, metaAPI)

	err := uc.Execute(businessphone.RequestVerificationCodeInput{
		PhoneID:     "phone-id",
		Method:      businessphone.VerificationMethodSMS,
		Language:    "pt_BR",
		AccessToken: "access-token",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
}

func TestRequestVerificationCodeUseCase_Execute_PhoneNotFound(t *testing.T) {
	repo := newMockRepo()
	metaAPI := newMockMetaAPI()

	uc := NewRequestVerificationCodeUseCase(repo, metaAPI)

	err := uc.Execute(businessphone.RequestVerificationCodeInput{
		PhoneID:     "nonexistent",
		Method:      businessphone.VerificationMethodSMS,
		Language:    "pt_BR",
		AccessToken: "access-token",
	})
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
}

func TestVerifyCodeUseCase_Execute(t *testing.T) {
	repo := newMockRepo()
	repo.phoneNumbers["phone-id"] = &businessphone.WhatsAppBusinessPhoneNumber{
		ID:                "phone-id",
		MetaPhoneNumberID: "meta-phone-1",
		Status:            businessphone.StatusVerifying,
	}

	metaAPI := newMockMetaAPI()
	metaAPI.verifyResult = true

	uc := NewVerifyCodeUseCase(repo, metaAPI)

	result, err := uc.Execute(businessphone.VerifyCodeInput{
		PhoneID:     "phone-id",
		Code:        "123456",
		AccessToken: "access-token",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.Status != businessphone.StatusConnected {
		t.Errorf("Expected Status CONNECTED, got %s", result.Status)
	}
}

func TestVerifyCodeUseCase_Execute_InvalidCode(t *testing.T) {
	repo := newMockRepo()
	repo.phoneNumbers["phone-id"] = &businessphone.WhatsAppBusinessPhoneNumber{
		ID:                "phone-id",
		MetaPhoneNumberID: "meta-phone-1",
		Status:            businessphone.StatusVerifying,
	}

	metaAPI := newMockMetaAPI()
	metaAPI.verifyResult = false

	uc := NewVerifyCodeUseCase(repo, metaAPI)

	_, err := uc.Execute(businessphone.VerifyCodeInput{
		PhoneID:     "phone-id",
		Code:        "wrong-code",
		AccessToken: "access-token",
	})
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
	if !errors.Is(err, businessphone.ErrVerificationFailed) {
		t.Errorf("Expected ErrVerificationFailed, got %v", err)
	}
}

func TestUpdateBusinessProfileUseCase_Execute(t *testing.T) {
	repo := newMockRepo()
	repo.phoneNumbers["phone-id"] = &businessphone.WhatsAppBusinessPhoneNumber{
		ID:                "phone-id",
		MetaPhoneNumberID: "meta-phone-1",
		Status:            businessphone.StatusConnected,
	}

	metaAPI := newMockMetaAPI()

	uc := NewUpdateBusinessProfileUseCase(repo, metaAPI, nil)

	profile := businessphone.BusinessProfile{
		About:       "Test about",
		Description: "Test description",
		Email:       "test@example.com",
		Vertical:    businessphone.VerticalRetail,
	}

	result, err := uc.Execute(businessphone.UpdateBusinessProfileInput{
		PhoneID:     "phone-id",
		Profile:     profile,
		AccessToken: "access-token",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.BusinessProfile.About != "Test about" {
		t.Errorf("Expected About 'Test about', got '%s'", result.BusinessProfile.About)
	}
}

func TestUpdateBusinessProfileUseCase_Execute_PhoneNotFound(t *testing.T) {
	repo := newMockRepo()
	metaAPI := newMockMetaAPI()

	uc := NewUpdateBusinessProfileUseCase(repo, metaAPI, nil)

	_, err := uc.Execute(businessphone.UpdateBusinessProfileInput{
		PhoneID:     "nonexistent",
		Profile:     businessphone.BusinessProfile{},
		AccessToken: "access-token",
	})
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
}

func TestGetBusinessProfileUseCase_Execute(t *testing.T) {
	repo := newMockRepo()
	repo.phoneNumbers["phone-id"] = &businessphone.WhatsAppBusinessPhoneNumber{
		ID:                "phone-id",
		MetaPhoneNumberID: "meta-phone-1",
		Status:            businessphone.StatusConnected,
	}

	metaAPI := newMockMetaAPI()
	metaAPI.businessProfile = &businessphone.MetaBusinessProfile{
		About:       "Meta about",
		Description: "Meta description",
		Email:       "meta@example.com",
	}

	uc := NewGetBusinessProfileUseCase(repo, metaAPI, nil)

	result, err := uc.Execute(businessphone.GetBusinessProfileInput{
		PhoneID:     "phone-id",
		AccessToken: "access-token",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.About != "Meta about" {
		t.Errorf("Expected About 'Meta about', got '%s'", result.About)
	}
}

func TestGetBusinessProfileUseCase_Execute_PhoneNotFound(t *testing.T) {
	repo := newMockRepo()
	metaAPI := newMockMetaAPI()

	uc := NewGetBusinessProfileUseCase(repo, metaAPI, nil)

	_, err := uc.Execute(businessphone.GetBusinessProfileInput{
		PhoneID:     "nonexistent",
		AccessToken: "access-token",
	})
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
}

func TestDeletePhoneNumberUseCase_Execute(t *testing.T) {
	repo := newMockRepo()
	repo.phoneNumbers["phone-id"] = &businessphone.WhatsAppBusinessPhoneNumber{
		ID: "phone-id",
	}

	uc := NewDeletePhoneNumberUseCase(repo)

	err := uc.Execute("phone-id")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if len(repo.phoneNumbers) != 0 {
		t.Errorf("Expected 0 phones in repo, got %d", len(repo.phoneNumbers))
	}
}

func TestDeletePhoneNumberUseCase_Execute_NotFound(t *testing.T) {
	repo := newMockRepo()
	repo.deleteErr = businessphone.ErrPhoneNumberNotFound

	uc := NewDeletePhoneNumberUseCase(repo)

	err := uc.Execute("nonexistent")
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
}
