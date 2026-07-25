package whatsapp_business_phone

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"vozko/domain/cache"
	businessphone "vozko/domain/whatsapp/business_phone"
)

// Dialog360PartnerClient implements businessphone.Dialog360PartnerService against
// the 360dialog Partner API (hub.360dialog.io/api/v2) using the partner level
// x-api-key header.
//
// Endpoint shapes are taken from the 360dialog Partner API docs. A few response
// field names (marked below) were not fully sampled in the docs and must be
// confirmed against a real partner account; they are decoded leniently so a
// mismatch surfaces as a clear error rather than a silent zero value.
type Dialog360PartnerClient struct {
	baseURL    string
	partnerID  string
	apiKey     string
	solutionID string
	httpClient *http.Client
	throttle   *dialog360Throttle
}

// WithRateLimit throttles every partner API call through a Redis-backed limiter so we
// stay within 360dialog's 5 requests / 30s partner-management limit and stop hitting
// 429s. shared may be nil (no throttling, e.g. in tests).
func (c *Dialog360PartnerClient) WithRateLimit(shared cache.SharedState) *Dialog360PartnerClient {
	if shared != nil {
		c.throttle = newDialog360Throttle(shared, 5, 30*time.Second, 40*time.Second)
	}
	return c
}

func NewDialog360PartnerClient(baseURL, partnerID, apiKey, solutionID string, httpClient *http.Client) *Dialog360PartnerClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = "https://hub.360dialog.io/api/v2"
	}
	return &Dialog360PartnerClient{
		baseURL:    baseURL,
		partnerID:  strings.TrimSpace(partnerID),
		apiKey:     strings.TrimSpace(apiKey),
		solutionID: strings.TrimSpace(solutionID),
		httpClient: httpClient,
	}
}

func (c *Dialog360PartnerClient) do(method, path string, body any, out any) error {
	if c.partnerID == "" {
		return fmt.Errorf("360dialog partner client: D360_PARTNER_ID is not configured")
	}
	if c.apiKey == "" {
		return fmt.Errorf("360dialog partner client: D360_PARTNER_API_KEY is not configured")
	}

	var reqBody io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("360dialog partner client: marshal request: %w", err)
		}
		reqBody = bytes.NewReader(raw)
	}

	// Respect 360dialog's 5/30s partner-management limit: wait for a slot instead of
	// firing and getting 429'd. No-op when no limiter is configured.
	c.throttle.Acquire()

	url := c.baseURL + path
	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		return fmt.Errorf("360dialog partner client: build request: %w", err)
	}
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("360dialog partner client: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("360dialog partner client: %s %s returned %d: %s", method, path, resp.StatusCode, string(respBody))
	}

	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("360dialog partner client: decode %s %s response: %w", method, path, err)
		}
	}
	return nil
}

func (c *Dialog360PartnerClient) partnerPath(suffix string) string {
	return fmt.Sprintf("/partners/%s%s", c.partnerID, suffix)
}

func (c *Dialog360PartnerClient) CreateClient(name, email string) (string, error) {
	reqBody := map[string]string{}
	if name != "" {
		reqBody["name"] = name
	}
	if email != "" {
		reqBody["email"] = email
	}
	var out struct {
		// The docs describe a created client object; the id field carries the
		// client_id. Confirm the exact key against a live response.
		ClientID string `json:"client_id"`
		ID       string `json:"id"`
	}
	if err := c.do(http.MethodPost, c.partnerPath("/account_sharing/clients"), reqBody, &out); err != nil {
		return "", err
	}
	if out.ClientID != "" {
		return out.ClientID, nil
	}
	if out.ID != "" {
		return out.ID, nil
	}
	return "", fmt.Errorf("360dialog partner client: account_sharing/clients returned no client id")
}

// FindClientByEmail pages through the partner's clients and returns the id of the
// one whose contact email matches (case-insensitive), or "" if none. The endpoint
// exposes no reliable server-side email filter, so we page (limit 200) and match
// client-side; the caller only reaches here once per WABA before the id is persisted,
// so the paging cost is a one-time onboarding step, not a per-request hot path.
func (c *Dialog360PartnerClient) FindClientByEmail(email string) (string, error) {
	target := strings.ToLower(strings.TrimSpace(email))
	if target == "" {
		return "", nil
	}
	const pageSize = 200
	const maxPages = 50 // 10k clients; log and stop rather than page forever.
	for page := 0; page < maxPages; page++ {
		var out struct {
			Clients []struct {
				ID          string `json:"id"`
				ContactInfo struct {
					Email string `json:"email"`
				} `json:"contact_info"`
			} `json:"clients"`
			Total int `json:"total"`
		}
		path := c.partnerPath(fmt.Sprintf("/clients?limit=%d&offset=%d", pageSize, page*pageSize))
		if err := c.do(http.MethodGet, path, nil, &out); err != nil {
			return "", err
		}
		for _, cl := range out.Clients {
			if strings.ToLower(strings.TrimSpace(cl.ContactInfo.Email)) == target {
				return cl.ID, nil
			}
		}
		if len(out.Clients) < pageSize || (page+1)*pageSize >= out.Total {
			break
		}
	}
	return "", nil
}

func (c *Dialog360PartnerClient) RegisterNumber(input businessphone.RegisterNumberInput) error {
	if c.solutionID == "" {
		return fmt.Errorf("360dialog partner client: D360_SOLUTION_ID is not configured (required by account_sharing/numbers)")
	}
	reqBody := map[string]string{
		"client_id":        input.ClientID,
		"waba_external_id": input.WABAExternalID,
		"solution_id":      c.solutionID,
	}
	if input.ChannelExternalID != "" {
		reqBody["channel_external_id"] = input.ChannelExternalID
	}
	return c.do(http.MethodPost, c.partnerPath("/account_sharing/numbers"), reqBody, nil)
}

// dialog360ChannelDTO decodes one channel from the partner /channels listing.
type dialog360ChannelDTO struct {
	ID          string `json:"id"`
	WABAAccount struct {
		ExternalID         string `json:"external_id"`
		Status             string `json:"status"`
		OnBehalfOfBusiness struct {
			Name string `json:"name"`
		} `json:"on_behalf_of_business_info"`
	} `json:"waba_account"`
	SetupInfo struct {
		PhoneNumber string `json:"phone_number"`
		PhoneName   string `json:"phone_name"`
	} `json:"setup_info"`
	CurrentQualityRating string `json:"current_quality_rating"`
	CurrentLimit         string `json:"current_limit"`
	HubStatus            string `json:"hub_status"`
	AvailabilityStatus   string `json:"availability_status"`
	CancelledAt          string `json:"cancelled_at"`
	Status               string `json:"status"`
	IsOnBizApp           bool   `json:"is_on_biz_app"`
}

func (dto dialog360ChannelDTO) toDomain() businessphone.Dialog360Channel {
	return businessphone.Dialog360Channel{
		ID:                 dto.ID,
		WABAExternalID:     dto.WABAAccount.ExternalID,
		PhoneNumber:        dto.SetupInfo.PhoneNumber,
		PhoneName:          dto.SetupInfo.PhoneName,
		QualityRating:      dto.CurrentQualityRating,
		MessagingTier:      dto.CurrentLimit,
		ReviewStatus:       dto.WABAAccount.Status,
		WABAName:           dto.WABAAccount.OnBehalfOfBusiness.Name,
		HubStatus:          dto.HubStatus,
		AvailabilityStatus: dto.AvailabilityStatus,
		Cancelled:          strings.TrimSpace(dto.CancelledAt) != "",
		Status:             dto.Status,
		IsOnBizApp:         dto.IsOnBizApp,
	}
}

type dialog360ChannelPage struct {
	Channels []dialog360ChannelDTO `json:"partner_channels"`
	Total    int                   `json:"total"`
}

// ListChannels returns EVERY channel on the partner account, paging through the
// listing (the endpoint caps a page, so a large partner spans several). Used by the
// reconcile, which needs the full fleet; it must not silently see only page one.
func (c *Dialog360PartnerClient) ListChannels() ([]businessphone.Dialog360Channel, error) {
	const pageSize = 200
	const maxPages = 100 // 20k channels; log-and-stop rather than page forever.
	var channels []businessphone.Dialog360Channel
	for page := 0; page < maxPages; page++ {
		var out dialog360ChannelPage
		path := c.partnerPath(fmt.Sprintf("/channels?limit=%d&offset=%d", pageSize, page*pageSize))
		if err := c.do(http.MethodGet, path, nil, &out); err != nil {
			return nil, err
		}
		for _, dto := range out.Channels {
			channels = append(channels, dto.toDomain())
		}
		if len(out.Channels) < pageSize || (out.Total > 0 && len(channels) >= out.Total) {
			break
		}
	}
	return channels, nil
}

// GetChannel fetches a single channel by id. 360dialog supports filtering the
// listing by id, so this is an O(1) call — the finalize path uses it instead of
// paging the whole fleet on every onboarding. Returns nil if the channel is absent.
func (c *Dialog360PartnerClient) GetChannel(channelID string) (*businessphone.Dialog360Channel, error) {
	if strings.TrimSpace(channelID) == "" {
		return nil, nil
	}
	var out dialog360ChannelPage
	filter := url.QueryEscape(fmt.Sprintf(`{"id":"%s"}`, channelID))
	if err := c.do(http.MethodGet, c.partnerPath("/channels?filters="+filter), nil, &out); err != nil {
		return nil, err
	}
	for _, dto := range out.Channels {
		if dto.ID == channelID {
			ch := dto.toDomain()
			return &ch, nil
		}
	}
	return nil, nil
}

func (c *Dialog360PartnerClient) GenerateAPIKey(channelID string) (*businessphone.APIKeyResult, error) {
	var out struct {
		APIKey  string `json:"api_key"`
		Address string `json:"address"`
	}
	path := c.partnerPath(fmt.Sprintf("/channels/%s/api_keys", channelID))
	if err := c.do(http.MethodPost, path, map[string]string{}, &out); err != nil {
		return nil, err
	}
	if out.APIKey == "" {
		return nil, fmt.Errorf("360dialog partner client: api_keys returned no api_key for channel %s", channelID)
	}
	return &businessphone.APIKeyResult{APIKey: out.APIKey, Address: out.Address}, nil
}

func (c *Dialog360PartnerClient) GetPartnerBalance() (*businessphone.Dialog360Balance, error) {
	// The Partner API returns an array of per currency balances, for example
	// [{"currency":"usd","total":0.0}]. We surface the first entry.
	var out []struct {
		Currency string  `json:"currency"`
		Total    float64 `json:"total"`
	}
	if err := c.do(http.MethodGet, c.partnerPath("/balance"), nil, &out); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return &businessphone.Dialog360Balance{}, nil
	}
	return &businessphone.Dialog360Balance{Amount: out[0].Total, Currency: out[0].Currency}, nil
}

func (c *Dialog360PartnerClient) channelControlPath(clientID, channelID, action string) string {
	return fmt.Sprintf("/partners/%s/clients/%s/channels/%s/control/%s", c.partnerID, clientID, channelID, action)
}

func (c *Dialog360PartnerClient) CancelChannel(clientID, channelID string) error {
	if clientID == "" {
		return fmt.Errorf("360dialog partner client: CancelChannel requires a clientID for channel %s", channelID)
	}
	return c.do(http.MethodPost, c.channelControlPath(clientID, channelID, "cancellation_request"), map[string]string{}, nil)
}

func (c *Dialog360PartnerClient) ReactivateChannel(clientID, channelID string) error {
	if clientID == "" {
		return fmt.Errorf("360dialog partner client: ReactivateChannel requires a clientID for channel %s", channelID)
	}
	return c.do(http.MethodPost, c.channelControlPath(clientID, channelID, "reactivate"), map[string]string{}, nil)
}

func (c *Dialog360PartnerClient) SetWebhookURL(webhookURL string) error {
	reqBody := map[string]string{"webhook_url": webhookURL}
	return c.do(http.MethodPatch, fmt.Sprintf("/partners/%s", c.partnerID), reqBody, nil)
}

var _ businessphone.Dialog360PartnerService = (*Dialog360PartnerClient)(nil)
