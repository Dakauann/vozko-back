package whatsapp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"vozko/domain/conversation"
)

// callingSettingsEndpoint builds the WhatsApp Business Calling settings endpoint.
// 360dialog exposes a channel-scoped shortcut ("{base}/calling/settings", authed by the
// D360-API-KEY) — verified live against waba-v2.360dialog.io, which returns the Meta
// Cloud API shape {"calling":{"status":...}}. Meta scopes it by phone number id
// ("{base}/{phone_number_id}/settings").
func (c *Client) callingSettingsEndpoint() string {
	if c.omitPhoneNumberInPath {
		return c.baseURL + "/calling/settings"
	}
	return fmt.Sprintf("%s/%s/settings", c.baseURL, c.phoneNumberID)
}

type callingSettingsResponse struct {
	Calling struct {
		Status string `json:"status"` // ENABLED | DISABLED | NOT_SET
	} `json:"calling"`
}

type callingSettingsRequest struct {
	MessagingProduct string `json:"messaging_product"`
	Calling          struct {
		Status string `json:"status"` // ENABLED | DISABLED
	} `json:"calling"`
}

// GetCallingStatus reports whether calling is ENABLED for the channel. NOT_SET/DISABLED
// both read as false.
func (c *Client) GetCallingStatus(ctx context.Context) (bool, error) {
	if c == nil || c.accessToken == "" {
		return false, conversation.ErrWhatsAppClientDisabled
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.callingSettingsEndpoint(), nil)
	if err != nil {
		return false, err
	}
	c.setAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, fmt.Errorf("whatsapp get calling settings failed: status=%d body=%s", resp.StatusCode, string(body))
	}

	var decoded callingSettingsResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return false, err
	}
	return strings.EqualFold(decoded.Calling.Status, "ENABLED"), nil
}

// SetCallingStatus enables or disables calling for the channel. The body follows the
// Meta Cloud API shape, which 360dialog's waba-v2 endpoint mirrors.
func (c *Client) SetCallingStatus(ctx context.Context, enabled bool) error {
	if c == nil || c.accessToken == "" {
		return conversation.ErrWhatsAppClientDisabled
	}

	status := "DISABLED"
	if enabled {
		status = "ENABLED"
	}
	var payload callingSettingsRequest
	payload.MessagingProduct = "whatsapp"
	payload.Calling.Status = status

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.callingSettingsEndpoint(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	c.setAuth(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("whatsapp set calling status failed: status=%d body=%s", resp.StatusCode, string(respBody))
	}
	log.Printf("[whatsapp-calling] set calling status=%s ok (channel-scoped=%v)", status, c.omitPhoneNumberInPath)
	return nil
}

var _ conversation.WhatsAppCallingClient = (*Client)(nil)
