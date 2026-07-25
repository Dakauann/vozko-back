package whatsapp_business_phone

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"vozko/domain/coexistence"

	businessphone "vozko/domain/whatsapp/business_phone"
)

type MetaCoexistenceClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewMetaCoexistenceClient(httpClient *http.Client) coexistence.MetaCoexistenceService {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &MetaCoexistenceClient{
		baseURL:    "https://graph.facebook.com/v22.0",
		httpClient: httpClient,
	}
}

func (c *MetaCoexistenceClient) CheckCoexistenceStatus(phoneNumberID, accessToken string) (*coexistence.PhoneCoexistenceStatus, error) {
	endpoint := fmt.Sprintf("%s/%s?fields=is_on_biz_app,platform_type",
		c.baseURL, url.PathEscape(phoneNumberID))

	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create coexistence check request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("coexistence check request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read coexistence check response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, parseMetaAPIError(resp.StatusCode, body)
	}

	var status coexistence.PhoneCoexistenceStatus
	if err := json.Unmarshal(body, &status); err != nil {
		return nil, fmt.Errorf("failed to parse coexistence status: %w", err)
	}

	return &status, nil
}

func (c *MetaCoexistenceClient) InitiateContactsSync(phoneNumberID, accessToken string) (*coexistence.SyncResponse, error) {
	return c.initiateSync(phoneNumberID, accessToken, coexistence.SyncTypeContacts)
}

func (c *MetaCoexistenceClient) InitiateHistorySync(phoneNumberID, accessToken string) (*coexistence.SyncResponse, error) {
	return c.initiateSync(phoneNumberID, accessToken, coexistence.SyncTypeHistory)
}

func (c *MetaCoexistenceClient) initiateSync(phoneNumberID, accessToken string, syncType coexistence.SyncType) (*coexistence.SyncResponse, error) {
	endpoint := fmt.Sprintf("%s/%s/smb_app_data",
		c.baseURL, url.PathEscape(phoneNumberID))

	payload := coexistence.SyncRequest{
		MessagingProduct: "whatsapp",
		SyncType:         syncType,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal sync request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create sync request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sync request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read sync response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {

		var errResp struct {
			Error *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(body, &errResp) == nil && errResp.Error != nil {
			if errResp.Error.Code == coexistence.HistorySharingDeclinedCode {
				return nil, fmt.Errorf("%w: %s", businessphone.ErrMetaAPIError,
					"business declined to share messaging history (code 2593109)")
			}
		}
		return nil, parseMetaAPIError(resp.StatusCode, body)
	}

	var syncResp coexistence.SyncResponse
	if err := json.Unmarshal(body, &syncResp); err != nil {
		return nil, fmt.Errorf("failed to parse sync response: %w", err)
	}

	return &syncResp, nil
}
