package businessphone_usecase

import (
	"log"
	"time"

	"github.com/google/uuid"

	businessphone "vozko/domain/whatsapp/business_phone"
	"vozko/domain/whatsapp/waba"
)

type onboardEmbeddedSignupUseCase struct {
	repo       businessphone.Repository
	wabaRepo   waba.Repository
	metaClient businessphone.MetaAPIService
}

func NewOnboardEmbeddedSignupUseCase(
	repo businessphone.Repository,
	wabaRepo waba.Repository,
	metaClient businessphone.MetaAPIService,
) businessphone.OnboardEmbeddedSignupUseCase {
	return &onboardEmbeddedSignupUseCase{
		repo:       repo,
		wabaRepo:   wabaRepo,
		metaClient: metaClient,
	}
}

func (uc *onboardEmbeddedSignupUseCase) Execute(input businessphone.OnboardEmbeddedSignupInput) (*businessphone.OnboardEmbeddedSignupResult, error) {
	if input.PhoneNumberID == "" {
		return nil, businessphone.ErrMetaPhoneNumberIDRequired
	}
	if input.WABAId == "" {
		return nil, businessphone.ErrInvalidWABAId
	}
	if input.OwnerWorkspaceID == "" {
		return nil, businessphone.ErrOwnerWorkspaceIDRequired
	}
	if input.OwnerAssignedBy == "" {
		return nil, businessphone.ErrOwnerAssignedByRequired
	}
	if input.Provider.Normalized() == businessphone.ProviderDialog360 {
		// 360dialog channels authenticate with the per channel D360-API-KEY, not a
		// Graph access token. Require the key at the connected (finalize) stage.
		if input.Dialog360APIKey == "" {
			return nil, businessphone.ErrInvalidAccessToken
		}
	} else if input.AccessToken == "" {
		return nil, businessphone.ErrInvalidAccessToken
	}

	log.Printf("[onboard-embedded-signup] Onboarding phone %s for WABA %s (business_id=%s)", input.PhoneNumberID, input.WABAId, input.BusinessID)

	if err := uc.upsertWABA(input); err != nil {
		log.Printf("[onboard-embedded-signup] Failed to upsert WABA %s: %v (continuing with phone onboarding)", input.WABAId, err)
	}

	// Meta enrichment is skipped for 360dialog channels: the credential is a
	// D360-API-KEY, not a Graph token, and 360dialog owns the phone record. The
	// 360dialog onboarding path supplies display details directly.
	var metaPhone *businessphone.MetaPhoneNumberInfo
	if input.Provider.Normalized() != businessphone.ProviderDialog360 {
		var err error
		metaPhone, err = uc.metaClient.GetPhoneNumber(input.PhoneNumberID, input.AccessToken)
		if err != nil {
			log.Printf("[onboard-embedded-signup] Could not fetch phone details from Meta: %v (will save with minimal data)", err)
		}
	}

	existing, err := uc.repo.FindByMetaPhoneNumberID(input.PhoneNumberID)
	if err == nil && existing != nil {
		log.Printf("[onboard-embedded-signup] Phone %s already exists (id=%s), updating...", input.PhoneNumberID, existing.ID)
		return uc.updateExistingPhone(existing, input, metaPhone)
	}

	softDeleted, sdErr := uc.repo.FindByMetaPhoneNumberIDUnscoped(input.PhoneNumberID)
	if sdErr == nil && softDeleted != nil {
		log.Printf("[onboard-embedded-signup] Phone %s was soft-deleted (id=%s), restoring...", input.PhoneNumberID, softDeleted.ID)
		if err := uc.repo.Restore(softDeleted.ID); err != nil {
			log.Printf("[onboard-embedded-signup] Failed to restore phone: %v", err)
			return nil, err
		}
		return uc.updateExistingPhone(softDeleted, input, metaPhone)
	}

	phone := &businessphone.WhatsAppBusinessPhoneNumber{
		ID:                         uuid.New().String(),
		Provider:                   input.Provider.Normalized(),
		MetaPhoneNumberID:          input.PhoneNumberID,
		WABAId:                     input.WABAId,
		BusinessPortfolioID:        input.BusinessID,
		AccessToken:                input.AccessToken,
		Dialog360ChannelID:         input.Dialog360ChannelID,
		Dialog360APIKey:            input.Dialog360APIKey,
		Status:                     businessphone.StatusConnected,
		WABAName:                   input.WABAName,
		AccountReviewStatus:        input.AccountReviewStatus,
		BusinessVerificationStatus: input.BusinessVerificationStatus,
		OwnershipType:              input.OwnershipType,
		MessagingLimitTier:         input.MessagingLimitTier,
	}

	if metaPhone != nil {
		phone.DisplayPhoneNumber = metaPhone.DisplayPhoneNumber
		phone.VerifiedName = metaPhone.VerifiedName
		phone.Status = businessphone.Status(metaPhone.Status)
		phone.QualityRating = businessphone.QualityRating(metaPhone.QualityRating)
		phone.NameStatus = businessphone.NameStatus(metaPhone.NameStatus)
		phone.CodeVerificationStatus = metaPhone.CodeVerificationStatus
		phone.IsOfficialBusiness = metaPhone.IsOfficialBusiness
	}

	if phone.Status == "" {
		phone.Status = businessphone.StatusConnected
	}

	if err := phone.AssignOwner(input.OwnerWorkspaceID, input.OwnerAssignedBy, time.Now().UTC()); err != nil {
		return nil, err
	}

	if err := uc.repo.Create(phone); err != nil {
		log.Printf("[onboard-embedded-signup] Failed to create phone: %v", err)
		return nil, err
	}

	log.Printf("[onboard-embedded-signup] Created new phone %s (meta_id=%s, display=%s)", phone.ID, phone.MetaPhoneNumberID, phone.DisplayPhoneNumber)
	return &businessphone.OnboardEmbeddedSignupResult{
		Phone: phone,
		IsNew: true,
	}, nil
}

func (uc *onboardEmbeddedSignupUseCase) updateExistingPhone(
	existing *businessphone.WhatsAppBusinessPhoneNumber,
	input businessphone.OnboardEmbeddedSignupInput,
	metaPhone *businessphone.MetaPhoneNumberInfo,
) (*businessphone.OnboardEmbeddedSignupResult, error) {
	existing.WABAId = input.WABAId
	existing.BusinessPortfolioID = input.BusinessID
	existing.AccessToken = input.AccessToken
	if input.Provider.Normalized() != businessphone.ProviderMeta {
		existing.Provider = input.Provider.Normalized()
	}
	if input.Dialog360ChannelID != "" {
		existing.Dialog360ChannelID = input.Dialog360ChannelID
	}
	if input.Dialog360APIKey != "" {
		existing.Dialog360APIKey = input.Dialog360APIKey
	}
	existing.WABAName = input.WABAName
	existing.AccountReviewStatus = input.AccountReviewStatus
	existing.BusinessVerificationStatus = input.BusinessVerificationStatus
	existing.OwnershipType = input.OwnershipType
	existing.MessagingLimitTier = input.MessagingLimitTier

	if metaPhone != nil {
		existing.DisplayPhoneNumber = metaPhone.DisplayPhoneNumber
		existing.VerifiedName = metaPhone.VerifiedName
		existing.Status = businessphone.Status(metaPhone.Status)
		existing.QualityRating = businessphone.QualityRating(metaPhone.QualityRating)
		existing.NameStatus = businessphone.NameStatus(metaPhone.NameStatus)
		existing.CodeVerificationStatus = metaPhone.CodeVerificationStatus
		existing.IsOfficialBusiness = metaPhone.IsOfficialBusiness
	}

	if existing.Status == "" {
		existing.Status = businessphone.StatusConnected
	}

	if !existing.BelongsToWorkspace(input.OwnerWorkspaceID) || existing.OwnerAssignedAt == nil || existing.OwnerAssignedBy == "" {
		if err := existing.AssignOwner(input.OwnerWorkspaceID, input.OwnerAssignedBy, time.Now().UTC()); err != nil {
			return nil, err
		}
	}

	if err := uc.repo.Update(existing.ID, existing); err != nil {
		log.Printf("[onboard-embedded-signup] Failed to update phone: %v", err)
		return nil, err
	}

	log.Printf("[onboard-embedded-signup] Updated existing phone %s", existing.ID)
	return &businessphone.OnboardEmbeddedSignupResult{
		Phone: existing,
		IsNew: false,
	}, nil
}

func (uc *onboardEmbeddedSignupUseCase) upsertWABA(input businessphone.OnboardEmbeddedSignupInput) error {
	existing, err := uc.wabaRepo.FindByMetaWABAId(input.WABAId)
	if err == nil && existing != nil {
		existing.Name = input.WABAName
		existing.BusinessPortfolioID = input.BusinessID
		existing.AccessToken = input.AccessToken
		existing.AccountReviewStatus = input.AccountReviewStatus
		existing.BusinessVerificationStatus = input.BusinessVerificationStatus
		existing.OwnershipType = input.OwnershipType
		existing.MessagingLimitTier = input.MessagingLimitTier
		if input.Provider.Normalized() != businessphone.ProviderMeta {
			existing.Provider = string(input.Provider.Normalized())
		}
		if input.Dialog360ClientID != "" {
			existing.Dialog360ClientID = input.Dialog360ClientID
		}
		if err := uc.wabaRepo.Update(existing.ID, existing); err != nil {
			return err
		}
		log.Printf("[onboard-embedded-signup] Updated WABA %s (meta_id=%s)", existing.ID, existing.MetaWABAId)
		return nil
	}

	account, err := waba.NewWhatsAppBusinessAccount(input.WABAId, input.WABAName)
	if err != nil {
		return err
	}
	account.Provider = string(input.Provider.Normalized())
	account.BusinessPortfolioID = input.BusinessID
	account.AccessToken = input.AccessToken
	account.Dialog360ClientID = input.Dialog360ClientID
	account.AccountReviewStatus = input.AccountReviewStatus
	account.BusinessVerificationStatus = input.BusinessVerificationStatus
	account.OwnershipType = input.OwnershipType
	account.MessagingLimitTier = input.MessagingLimitTier

	if err := uc.wabaRepo.Create(account); err != nil {
		return err
	}
	log.Printf("[onboard-embedded-signup] Created WABA %s (meta_id=%s)", account.ID, account.MetaWABAId)
	return nil
}
