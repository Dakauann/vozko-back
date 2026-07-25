package whatsapp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"vozko/domain/conversation"
)

// businessProfileEndpoint builds the whatsapp_business_profile endpoint. 360dialog scopes
// it by the channel API key ("{base}/whatsapp_business_profile" — verified live against
// waba-v2.360dialog.io, returning {"data":[{...}]}); Meta scopes it by phone number id.
func (c *Client) businessProfileEndpoint() string {
	if c.omitPhoneNumberInPath {
		return c.baseURL + "/whatsapp_business_profile"
	}
	return fmt.Sprintf("%s/%s/whatsapp_business_profile", c.baseURL, c.phoneNumberID)
}

type businessProfileGetResponse struct {
	Data []struct {
		About             string   `json:"about"`
		Address           string   `json:"address"`
		Description       string   `json:"description"`
		Email             string   `json:"email"`
		ProfilePictureURL string   `json:"profile_picture_url"`
		Websites          []string `json:"websites"`
		Vertical          string   `json:"vertical"`
	} `json:"data"`
}

type businessProfileUpdateRequest struct {
	MessagingProduct     string   `json:"messaging_product"`
	About                string   `json:"about,omitempty"`
	Address              string   `json:"address,omitempty"`
	Description          string   `json:"description,omitempty"`
	Email                string   `json:"email,omitempty"`
	Websites             []string `json:"websites,omitempty"`
	Vertical             string   `json:"vertical,omitempty"`
	ProfilePictureHandle string   `json:"profile_picture_handle,omitempty"`
}

func (c *Client) GetBusinessProfile(ctx context.Context) (*conversation.WhatsAppBusinessProfile, error) {
	if c == nil || c.accessToken == "" {
		return nil, conversation.ErrWhatsAppClientDisabled
	}

	endpoint := c.businessProfileEndpoint() + "?fields=about,address,description,email,profile_picture_url,websites,vertical"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	c.setAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("whatsapp get business profile failed: status=%d body=%s", resp.StatusCode, string(body))
	}

	var decoded businessProfileGetResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, err
	}
	if len(decoded.Data) == 0 {
		return &conversation.WhatsAppBusinessProfile{}, nil
	}
	d := decoded.Data[0]
	return &conversation.WhatsAppBusinessProfile{
		About:             d.About,
		Address:           d.Address,
		Description:       d.Description,
		Email:             d.Email,
		ProfilePictureURL: d.ProfilePictureURL,
		Websites:          d.Websites,
		Vertical:          d.Vertical,
	}, nil
}

func (c *Client) UpdateBusinessProfile(ctx context.Context, profile conversation.WhatsAppBusinessProfile) error {
	if c == nil || c.accessToken == "" {
		return conversation.ErrWhatsAppClientDisabled
	}

	payload := businessProfileUpdateRequest{
		MessagingProduct:     "whatsapp",
		About:                profile.About,
		Address:              profile.Address,
		Description:          profile.Description,
		Email:                profile.Email,
		Websites:             profile.Websites,
		Vertical:             profile.Vertical,
		ProfilePictureHandle: profile.ProfilePictureHandle,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.businessProfileEndpoint(), bytes.NewReader(body))
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
		return fmt.Errorf("whatsapp update business profile failed: status=%d body=%s", resp.StatusCode, string(respBody))
	}
	return nil
}

var _ conversation.WhatsAppBusinessProfileClient = (*Client)(nil)
