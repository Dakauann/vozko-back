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

func (c *Client) callingSettingsEndpoint() string {
	if c.omitPhoneNumberInPath {
		return c.baseURL + "/calling/settings"
	}
	return fmt.Sprintf("%s/%s/settings", c.baseURL, c.phoneNumberID)
}

type callingSettingsResponse struct {
	Calling struct {
		Status string `json:"status"`
	} `json:"calling"`
}

type callingSettingsRequest struct {
	MessagingProduct string `json:"messaging_product"`
	Calling          struct {
		Status string `json:"status"`
	} `json:"calling"`
}

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
