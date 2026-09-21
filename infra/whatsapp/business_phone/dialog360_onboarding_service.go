package whatsapp_business_phone

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"vozko/brand"
	"vozko/domain/notification"
	businessphone "vozko/domain/whatsapp/business_phone"
	"vozko/domain/whatsapp/waba"
	wc "vozko/domain/whatsapp_campaign"
)

type Dialog360OnboardingService struct {
	partner         businessphone.Dialog360PartnerService
	phoneRepo       businessphone.Repository
	wabaRepo        waba.Repository
	organicCampaign wc.EnsureOrganicCoexistenceCampaignUseCase

	messagingWebhookURL string
	httpClient          *http.Client
	provisioningGate    businessphone.ProvisioningGate

	notifier     notification.Notifier
	dashboardURL string
}

func (s *Dialog360OnboardingService) WithMessagingWebhook(webhookURL string, httpClient *http.Client) *Dialog360OnboardingService {
	s.messagingWebhookURL = strings.TrimSpace(webhookURL)
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	s.httpClient = httpClient
	return s
}

func (s *Dialog360OnboardingService) WithProvisioningGate(gate businessphone.ProvisioningGate) *Dialog360OnboardingService {
	s.provisioningGate = gate
	return s
}

func (s *Dialog360OnboardingService) WithNotifier(n notification.Notifier, dashboardURL string) *Dialog360OnboardingService {
	s.notifier = n
	s.dashboardURL = dashboardURL
	return s
}

func (s *Dialog360OnboardingService) notifyOnboardingFailed(phone *businessphone.WhatsAppBusinessPhoneNumber) {
	if s.notifier == nil || phone == nil || phone.OwnerWorkspaceID == "" {
		return
	}
	_ = s.notifier.Notify(notification.Notification{
		WorkspaceID: phone.OwnerWorkspaceID,
		Subject:     "Falha na configuração do número - " + brand.Active().Name,
		Template:    "whatsapp_onboarding_failed.html",
		Placeholders: map[string]interface{}{
			"PhoneNumber":  phone.DisplayPhoneNumber,
			"Reason":       phone.OnboardingError,
			"DashboardURL": s.dashboardURL,
		},
		DedupKey: "phone_onboarding_failed:" + phone.ID,
		DedupTTL: 24 * time.Hour,
	})
}

func (s *Dialog360OnboardingService) notifyNumberLive(phone *businessphone.WhatsAppBusinessPhoneNumber) {
	if s.notifier == nil || phone == nil || phone.OwnerWorkspaceID == "" {
		return
	}
	_ = s.notifier.Notify(notification.Notification{
		WorkspaceID: phone.OwnerWorkspaceID,
		Subject:     "Seu número de WhatsApp está ativo - " + brand.Active().Name,
		Template:    "whatsapp_number_live.html",
		Placeholders: map[string]interface{}{
			"PhoneNumber":  phone.DisplayPhoneNumber,
			"DashboardURL": s.dashboardURL,
		},
		DedupKey: "phone_live:" + phone.ID,
		DedupTTL: 30 * 24 * time.Hour,
	})
}

func (s *Dialog360OnboardingService) checkProvisioningAllowed(workspaceID string) error {
	if s.provisioningGate == nil {
		return nil
	}
	ok, err := s.provisioningGate.CanProvisionPhone(workspaceID)
	if err != nil {
		log.Printf("[dialog360-onboarding] provisioning gate error for workspace %s: %v (denying, fail-closed)", workspaceID, err)
		return err
	}
	if !ok {
		return businessphone.ErrPhoneLimitReached
	}
	return nil
}

func NewDialog360OnboardingService(
	partner businessphone.Dialog360PartnerService,
	phoneRepo businessphone.Repository,
	wabaRepo waba.Repository,
	organicCampaign wc.EnsureOrganicCoexistenceCampaignUseCase,
) *Dialog360OnboardingService {
	return &Dialog360OnboardingService{
		partner:         partner,
		phoneRepo:       phoneRepo,
		wabaRepo:        wabaRepo,
		organicCampaign: organicCampaign,
	}
}

var _ businessphone.Dialog360Onboarder = (*Dialog360OnboardingService)(nil)

func (s *Dialog360OnboardingService) StartProvisioning(in businessphone.Dialog360ProvisionInput) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	if strings.TrimSpace(in.WABAExternalID) == "" || strings.TrimSpace(in.PhoneNumberID) == "" {
		return nil, fmt.Errorf("dialog360 onboarding: waba_external_id and phone_number_id are required")
	}
	if strings.TrimSpace(in.OwnerWorkspaceID) == "" || strings.TrimSpace(in.OwnerAssignedBy) == "" {
		return nil, businessphone.ErrOwnerWorkspaceIDRequired
	}

	existing, ferr := s.phoneRepo.FindByMetaPhoneNumberID(in.PhoneNumberID)
	isExisting := ferr == nil && existing != nil
	if isExisting && existing.Status == businessphone.StatusConnected {
		return existing, nil
	}

	if !isExisting {
		if err := s.checkProvisioningAllowed(in.OwnerWorkspaceID); err != nil {
			return nil, err
		}
	}

	phone, err := s.ensurePendingPhone(in)
	if err != nil {
		return nil, fmt.Errorf("dialog360 onboarding: persist pending phone: %w", err)
	}

	if err := s.handover(in); err != nil {
		s.markFailed(phone, err)
		return phone, err
	}

	s.markHandoverAccepted(phone)
	log.Printf("[dialog360-onboarding] handover accepted: waba=%s phone=%s", in.WABAExternalID, in.PhoneNumberID)
	return phone, nil
}

func (s *Dialog360OnboardingService) Retry(phoneID, ownerWorkspaceID string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	phone, err := s.phoneRepo.FindByID(phoneID)
	if err != nil {
		return nil, err
	}
	if !phone.BelongsToWorkspace(ownerWorkspaceID) {
		return nil, businessphone.ErrPhoneNumberNotFound
	}
	if !phone.Provider.IsDialog360() {
		return nil, businessphone.ErrUnsupportedForProvider
	}
	if phone.Status == businessphone.StatusConnected {
		return phone, nil
	}
	in := businessphone.Dialog360ProvisionInput{
		WABAExternalID:     phone.WABAId,
		PhoneNumberID:      phone.MetaPhoneNumberID,
		OwnerWorkspaceID:   phone.OwnerWorkspaceID,
		OwnerAssignedBy:    phone.OwnerAssignedBy,
		DisplayPhoneNumber: phone.DisplayPhoneNumber,
		ClientName:         "workspace-" + phone.OwnerWorkspaceID + "@" + brand.Active().EmailDomain,
		ClientEmail:        "workspace-" + phone.OwnerWorkspaceID + "@" + brand.Active().EmailDomain,
	}
	if err := s.checkProvisioningAllowed(phone.OwnerWorkspaceID); err != nil {
		s.markFailed(phone, err)
		return phone, err
	}
	if err := s.handover(in); err != nil {
		s.markFailed(phone, err)
		return phone, err
	}
	s.markHandoverAccepted(phone)
	return phone, nil
}

func (s *Dialog360OnboardingService) handover(in businessphone.Dialog360ProvisionInput) error {
	clientID, err := s.resolveClientID(in)
	if err != nil {
		return err
	}
	if err := s.partner.RegisterNumber(businessphone.RegisterNumberInput{
		ClientID:          clientID,
		WABAExternalID:    in.WABAExternalID,
		ChannelExternalID: in.PhoneNumberID,
	}); err != nil {
		return fmt.Errorf("share number (account_sharing/numbers): %w", err)
	}
	return nil
}

func (s *Dialog360OnboardingService) resolveClientID(in businessphone.Dialog360ProvisionInput) (string, error) {
	if existing, err := s.wabaRepo.FindByMetaWABAId(in.WABAExternalID); err == nil && existing != nil && strings.TrimSpace(existing.Dialog360ClientID) != "" {
		return existing.Dialog360ClientID, nil
	}
	if id, err := s.partner.FindClientByEmail(in.ClientEmail); err != nil {
		log.Printf("[dialog360-onboarding] lookup client by email failed for WABA %s: %v (will attempt create)", in.WABAExternalID, err)
	} else if id != "" {
		s.persistClientID(in.WABAExternalID, id)
		return id, nil
	}
	clientID, err := s.partner.CreateClient(in.ClientName, in.ClientEmail)
	if err != nil {
		if id, ferr := s.partner.FindClientByEmail(in.ClientEmail); ferr == nil && id != "" {
			s.persistClientID(in.WABAExternalID, id)
			return id, nil
		}
		return "", fmt.Errorf("create client (account_sharing/clients): %w", err)
	}
	s.persistClientID(in.WABAExternalID, clientID)
	return clientID, nil
}

func (s *Dialog360OnboardingService) persistClientID(wabaExternalID, clientID string) {
	if err := s.setWABAClient(wabaExternalID, clientID); err != nil {
		log.Printf("[dialog360-onboarding] could not persist client id on WABA %s: %v (continuing)", wabaExternalID, err)
	}
}

func (s *Dialog360OnboardingService) markFailed(phone *businessphone.WhatsAppBusinessPhoneNumber, cause error) {
	phone.Status = businessphone.StatusOnboardingFailed
	phone.OnboardingError = cause.Error()
	if err := s.phoneRepo.Update(phone.ID, phone); err != nil {
		log.Printf("[dialog360-onboarding] CRITICAL: could not persist failure state for phone %s: %v (original: %v)", phone.ID, err, cause)
		return
	}
	if !errors.Is(cause, businessphone.ErrPhoneLimitReached) {
		s.notifyOnboardingFailed(phone)
	}
}

func (s *Dialog360OnboardingService) markHandoverAccepted(phone *businessphone.WhatsAppBusinessPhoneNumber) {
	if phone.Status == businessphone.StatusPending && phone.OnboardingError == "" {
		return
	}
	phone.Status = businessphone.StatusPending
	phone.OnboardingError = ""
	if err := s.phoneRepo.Update(phone.ID, phone); err != nil {
		log.Printf("[dialog360-onboarding] could not clear failure state for phone %s: %v", phone.ID, err)
	}
}

func (s *Dialog360OnboardingService) ensurePendingPhone(in businessphone.Dialog360ProvisionInput) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	if existing, err := s.phoneRepo.FindByMetaPhoneNumberID(in.PhoneNumberID); err == nil && existing != nil {
		return existing, nil
	}
	if err := s.ensurePendingWABA(in.WABAExternalID); err != nil {
		log.Printf("[dialog360-onboarding] upsert WABA failed: %v (continuing)", err)
	}
	phone := &businessphone.WhatsAppBusinessPhoneNumber{
		Provider:           businessphone.ProviderDialog360,
		MetaPhoneNumberID:  in.PhoneNumberID,
		WABAId:             in.WABAExternalID,
		DisplayPhoneNumber: in.DisplayPhoneNumber,
		Status:             businessphone.StatusPending,
	}
	if err := phone.AssignOwner(in.OwnerWorkspaceID, in.OwnerAssignedBy, time.Now().UTC()); err != nil {
		return nil, err
	}
	if err := s.phoneRepo.Create(phone); err != nil {
		return nil, err
	}
	return phone, nil
}

func (s *Dialog360OnboardingService) ensurePendingWABA(wabaExternalID string) error {
	if existing, err := s.wabaRepo.FindByMetaWABAId(wabaExternalID); err == nil && existing != nil {
		return nil
	}
	account, err := waba.NewWhatsAppBusinessAccount(wabaExternalID, "")
	if err != nil {
		return err
	}
	account.Provider = string(businessphone.ProviderDialog360)
	return s.wabaRepo.Create(account)
}

func (s *Dialog360OnboardingService) setWABAClient(wabaExternalID, clientID string) error {
	existing, err := s.wabaRepo.FindByMetaWABAId(wabaExternalID)
	if err != nil || existing == nil {
		account, nerr := waba.NewWhatsAppBusinessAccount(wabaExternalID, "")
		if nerr != nil {
			return nerr
		}
		account.Provider = string(businessphone.ProviderDialog360)
		account.Dialog360ClientID = clientID
		return s.wabaRepo.Create(account)
	}
	existing.Provider = string(businessphone.ProviderDialog360)
	existing.Dialog360ClientID = clientID
	return s.wabaRepo.Update(existing.ID, existing)
}

func (s *Dialog360OnboardingService) backfillWABAName(wabaExternalID, name string) {
	name = strings.TrimSpace(name)
	if wabaExternalID == "" || name == "" {
		return
	}
	existing, err := s.wabaRepo.FindByMetaWABAId(wabaExternalID)
	if err != nil || existing == nil || strings.TrimSpace(existing.Name) != "" {
		return
	}
	existing.Name = name
	if err := s.wabaRepo.Update(existing.ID, existing); err != nil {
		log.Printf("[dialog360-onboarding] backfill WABA name %s: %v", wabaExternalID, err)
	}
}

func (s *Dialog360OnboardingService) Finalize(channelID string, now time.Time) error {
	matched, err := s.partner.GetChannel(channelID)
	if err != nil {
		return fmt.Errorf("dialog360 finalize: get channel: %w", err)
	}
	if matched == nil {
		return fmt.Errorf("dialog360 finalize: channel %s not found on partner account", channelID)
	}

	phone, err := s.findPendingPhoneForChannel(matched)
	if err != nil {
		return err
	}

	if strings.TrimSpace(phone.Dialog360APIKey) != "" {
		log.Printf("[dialog360-onboarding] channel %s already finalized, skipping key generation", channelID)
		return nil
	}

	key, err := s.partner.GenerateAPIKey(channelID)
	if err != nil {
		return fmt.Errorf("dialog360 finalize: generate api key: %w", err)
	}

	phone.Provider = businessphone.ProviderDialog360
	phone.Dialog360ChannelID = channelID
	phone.Dialog360APIKey = key.APIKey
	phone.Status = businessphone.StatusConnected
	if phone.DisplayPhoneNumber == "" {
		phone.DisplayPhoneNumber = matched.PhoneNumber
	}
	if matched.PhoneName != "" {
		phone.VerifiedName = matched.PhoneName
	}
	if matched.QualityRating != "" {
		phone.QualityRating = businessphone.QualityRating(matched.QualityRating)
	}
	if matched.MessagingTier != "" {
		phone.MessagingLimitTier = matched.MessagingTier
	}
	if matched.ReviewStatus != "" {
		phone.AccountReviewStatus = matched.ReviewStatus
	}
	if err := s.phoneRepo.Update(phone.ID, phone); err != nil {
		return fmt.Errorf("dialog360 finalize: persist connected phone: %w", err)
	}

	if matched.WABAName != "" {
		s.backfillWABAName(phone.WABAId, matched.WABAName)
	}

	s.notifyNumberLive(phone)

	s.configureChannelWebhook(key.Address, key.APIKey)

	if matched.IsOnBizApp && s.organicCampaign != nil {
		if _, _, cerr := s.organicCampaign.Execute(phone.OwnerWorkspaceID, phone.ID, phone.DisplayPhoneNumber); cerr != nil {
			log.Printf("[dialog360-onboarding] coexistence organic campaign failed for phone %s: %v (continuing)", phone.ID, cerr)
		} else {
			log.Printf("[dialog360-onboarding] coexistence channel %s: organic campaign ensured (phone=%s)", channelID, phone.ID)
		}
	}

	log.Printf("[dialog360-onboarding] channel %s finalized and connected (phone=%s)", channelID, phone.ID)
	return nil
}

func (s *Dialog360OnboardingService) configureChannelWebhook(address, apiKey string) {
	if s.messagingWebhookURL == "" || s.httpClient == nil {
		return
	}
	base := strings.TrimRight(strings.TrimSpace(address), "/")
	if base == "" {
		base = "https://waba-v2.360dialog.io"
	}
	payload, err := json.Marshal(map[string]string{"url": s.messagingWebhookURL})
	if err != nil {
		log.Printf("[dialog360-onboarding] marshal webhook config failed: %v (continuing; Hub default applies)", err)
		return
	}
	req, err := http.NewRequest(http.MethodPost, base+"/v1/configs/webhook", bytes.NewReader(payload))
	if err != nil {
		log.Printf("[dialog360-onboarding] build webhook config request failed: %v (continuing; Hub default applies)", err)
		return
	}
	req.Header.Set("D360-API-KEY", apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		log.Printf("[dialog360-onboarding] set channel messaging webhook failed: %v (continuing; Hub default applies)", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<14))
		log.Printf("[dialog360-onboarding] set channel messaging webhook returned %d: %s (continuing; Hub default applies)", resp.StatusCode, string(b))
		return
	}
	log.Printf("[dialog360-onboarding] channel messaging webhook registered (%s)", s.messagingWebhookURL)
}

func (s *Dialog360OnboardingService) findPendingPhoneForChannel(ch *businessphone.Dialog360Channel) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	phones, err := s.phoneRepo.FindByWABAId(ch.WABAExternalID)
	if err != nil {
		return nil, fmt.Errorf("dialog360 finalize: find phones for waba %s: %w", ch.WABAExternalID, err)
	}
	for _, p := range phones {
		if p.Dialog360ChannelID == ch.ID {
			return p, nil
		}
	}
	for _, p := range phones {
		if p.Provider.IsDialog360() && p.Status == businessphone.StatusPending {
			return p, nil
		}
	}
	for _, p := range phones {
		if p.Provider.IsDialog360() {
			return p, nil
		}
	}
	return nil, fmt.Errorf("dialog360 finalize: no local row for channel %s (waba %s)", ch.ID, ch.WABAExternalID)
}

type ReconcileReport struct {
	OrphanedChannels []businessphone.Dialog360Channel
	StalePending     []string
}

func (s *Dialog360OnboardingService) Reconcile(now time.Time, staleAfter time.Duration) (*ReconcileReport, error) {
	channels, err := s.partner.ListChannels()
	if err != nil {
		return nil, fmt.Errorf("dialog360 reconcile: list channels: %w", err)
	}
	local, err := s.phoneRepo.ListAll()
	if err != nil {
		return nil, fmt.Errorf("dialog360 reconcile: list local phones: %w", err)
	}

	byChannelID := map[string]bool{}
	byWABA := map[string]bool{}
	report := &ReconcileReport{}
	for _, p := range local {
		if !p.Provider.IsDialog360() {
			continue
		}
		if p.Dialog360ChannelID != "" {
			byChannelID[p.Dialog360ChannelID] = true
		}
		if p.WABAId != "" {
			byWABA[p.WABAId] = true
		}
		if p.Status == businessphone.StatusPending && now.Sub(p.CreatedAt) > staleAfter {
			report.StalePending = append(report.StalePending, p.ID)
		}
	}

	for _, ch := range channels {
		if byChannelID[ch.ID] || byWABA[ch.WABAExternalID] {
			continue
		}
		report.OrphanedChannels = append(report.OrphanedChannels, ch)
	}

	if len(report.OrphanedChannels) > 0 || len(report.StalePending) > 0 {
		log.Printf("[dialog360-reconcile] drift detected: %d orphaned channels, %d stale pending rows",
			len(report.OrphanedChannels), len(report.StalePending))
	}
	return report, nil
}

func (s *Dialog360OnboardingService) RunPeriodicReconcile(interval, staleAfter time.Duration) {
	if interval <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			report, err := s.Reconcile(time.Now().UTC(), staleAfter)
			if err != nil {
				log.Printf("[dialog360-reconcile] error: %v", err)
				continue
			}
			for _, ch := range report.OrphanedChannels {
				log.Printf("[dialog360-reconcile] ORPHANED billable channel with no platform row: id=%s waba=%s phone=%s (manual action needed)", ch.ID, ch.WABAExternalID, ch.PhoneNumber)
			}
			for _, phoneID := range report.StalePending {
				log.Printf("[dialog360-reconcile] STALE pending onboarding: phone=%s (manual action needed)", phoneID)
			}
		}
	}()
}
