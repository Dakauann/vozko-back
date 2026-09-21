package uazapi

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	uw "vozko/domain/unofficial_whatsapp"
)

type chatDetailsResponse struct {
	WAChatID  string `json:"wa_chatid"`
	WAChatLID string `json:"wa_chatlid"`
	Phone     string `json:"phone"`

	WAContactName string `json:"wa_contactName"`
	WAName        string `json:"wa_name"`
	Name          string `json:"name"`

	Image        string `json:"image"`
	ImagePreview string `json:"imagePreview"`

	WAIsGroup   bool `json:"wa_isGroup"`
	WAIsBlocked bool `json:"wa_isBlocked"`
}

func (c *Client) ChatDetails(ctx context.Context, ref uw.InstanceRef, chatID string) (*uw.ChatProfile, error) {
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		return nil, uw.ErrContactNotFound
	}

	var resp chatDetailsResponse
	err := c.instanceCall(ctx, ref, http.MethodPost, "/chat/details", map[string]any{
		"number":  chatID,
		"preview": true,
	}, &resp)
	if err != nil {
		return nil, err
	}

	jid := firstNonEmpty(resp.WAChatID, chatID)
	profile := &uw.ChatProfile{
		JID:         jid,
		LID:         resp.WAChatLID,
		Name:        firstNonEmpty(resp.WAName, resp.Name),
		ContactName: resp.WAContactName,
		PictureURL:  firstNonEmpty(resp.ImagePreview, resp.Image),
		IsGroup:     resp.WAIsGroup || uw.IsGroupJID(jid),
		IsBlocked:   resp.WAIsBlocked,
	}
	if !profile.IsGroup {
		profile.PhoneNumber = uw.NormalizePhone(resp.Phone)
		if profile.PhoneNumber == "" {
			profile.PhoneNumber = uw.PhoneFromJID(jid)
		}
	}
	return profile, nil
}

func (c *Client) FetchAsset(ctx context.Context, url string) ([]byte, string, error) {
	url = strings.TrimSpace(url)
	if url == "" {
		return nil, "", fmt.Errorf("uazapi: no asset url")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", fmt.Errorf("uazapi: bad asset url: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("uazapi: fetch asset: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("uazapi: asset fetch returned %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, uw.MaxAvatarBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("uazapi: read asset: %w", err)
	}
	if len(data) > uw.MaxAvatarBytes {
		return nil, "", fmt.Errorf("uazapi: asset exceeds the %d byte limit", uw.MaxAvatarBytes)
	}
	return data, resp.Header.Get("Content-Type"), nil
}
