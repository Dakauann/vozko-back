package template_infra

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"vozko/domain/conversation"
	businessphone "vozko/domain/whatsapp/business_phone"
	whatsapp_client "vozko/infra/conversation/whatsapp"
)

const defaultDialog360MessagingBaseURL = "https://waba-v2.360dialog.io"

type clientFactory struct {
	phoneRepo        businessphone.Repository
	httpClient       *http.Client
	dialog360BaseURL string
	metaAppID        string
}

// NewWhatsAppClientFactory builds per-phone clients. metaAppID is the shared
// Meta app id (WHATSAPP_APP_ID) that scopes the Resumable Upload API for
// template header media on Meta-hosted phones; 360dialog phones don't need it.
func NewWhatsAppClientFactory(phoneRepo businessphone.Repository, httpClient *http.Client, dialog360BaseURL string, metaAppID string) conversation.WhatsAppClientFactory {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	if dialog360BaseURL == "" {
		dialog360BaseURL = defaultDialog360MessagingBaseURL
	}
	return &clientFactory{
		phoneRepo:        phoneRepo,
		httpClient:       httpClient,
		dialog360BaseURL: dialog360BaseURL,
		metaAppID:        metaAppID,
	}
}

// buildClient resolves the provider for a phone and constructs the matching
// client. 360dialog wraps the Meta Cloud API, so both providers use the same
// client body and differ only in base URL and credential header.
func (f *clientFactory) buildClient(phone *businessphone.WhatsAppBusinessPhoneNumber) (conversation.WhatsAppClient, error) {
	if phone.Provider.IsDialog360() {
		if phone.Dialog360APIKey == "" {
			return nil, errors.New("360dialog channel has no API key; finish onboarding before sending")
		}
		return whatsapp_client.NewClient(whatsapp_client.Config{
			BaseURL:         f.dialog360BaseURL,
			PhoneNumberID:   phone.MetaPhoneNumberID,
			WABAId:          phone.WABAId,
			AccessToken:     phone.Dialog360APIKey,
			AuthHeaderName:  "D360-API-KEY",
			AuthValuePrefix: "",
			// 360dialog scopes the channel by the API key; the send path is
			// "{base}/messages" with no phone-number-id segment (confirmed against
			// the 360dialog sandbox, which 404s on the Meta-style path).
			OmitPhoneNumberInPath: true,
			// Template management is channel-scoped too ("{base}/v1/configs/templates"),
			// not Meta's WABA-scoped Graph path.
			TemplatesChannelScoped: true,
			HTTPClient:             f.httpClient,
		}), nil
	}

	if phone.AccessToken == "" {
		return nil, errors.New("business phone has no credential; re-onboard the number before sending")
	}
	if phone.WABAId == "" {
		return nil, errors.New("business phone has no WABA ID")
	}
	return whatsapp_client.NewClient(whatsapp_client.Config{
		PhoneNumberID: phone.MetaPhoneNumberID,
		WABAId:        phone.WABAId,
		AccessToken:   phone.AccessToken,
		AppID:         f.metaAppID,
		HTTPClient:    f.httpClient,
	}), nil
}

func (f *clientFactory) ClientForPhone(businessPhoneID string) (conversation.WhatsAppClient, error) {
	phone, err := f.phoneRepo.FindByID(businessPhoneID)
	if err != nil {
		return nil, err
	}
	return f.buildClient(phone)
}

func (f *clientFactory) ClientForWABA(wabaID string) (conversation.WhatsAppClient, error) {
	phones, err := f.phoneRepo.FindByWABAId(wabaID)
	if err != nil {
		return nil, fmt.Errorf("failed to find phones for WABA %s: %w", wabaID, err)
	}

	for _, phone := range phones {
		if phone.Provider.IsDialog360() && phone.Dialog360APIKey != "" {
			return f.buildClient(phone)
		}
		if phone.AccessToken != "" {
			return f.buildClient(phone)
		}
	}

	return nil, fmt.Errorf("no phone with a valid credential found for WABA %s", wabaID)
}

func (f *clientFactory) WABAIdForPhone(businessPhoneID string) (string, error) {
	phone, err := f.phoneRepo.FindByID(businessPhoneID)
	if err != nil {
		return "", err
	}
	if phone.WABAId == "" {
		return "", errors.New("business phone has no WABA ID")
	}
	return phone.WABAId, nil
}
