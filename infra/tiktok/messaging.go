package tiktok

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"

	ttdomain "vozko/domain/tiktok"
)

const MaxInboundMediaBytes = 64 << 20

const capabilityImageSend = "IMAGE_SEND"

var errDownloadURLInvalid = errors.New("tiktok: media download url is not https")

type sendResponse struct {
	Message struct {
		MessageID string `json:"message_id"`
	} `json:"message"`
}

func (c *Client) sendPayload(ctx context.Context, creds ttdomain.Credentials, payload map[string]any, retrySafe bool) (string, error) {
	payload["business_id"] = creds.OpenID
	payload["recipient_type"] = "CONVERSATION"
	var out sendResponse
	if err := c.postJSON(ctx, request{path: "/business/message/send/", family: FamilyMessageSend, creds: &creds, retrySafe: retrySafe}, payload, &out); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.Message.MessageID), nil
}

func (c *Client) sendMessage(ctx context.Context, creds ttdomain.Credentials, payload map[string]any) (string, error) {
	id, err := c.sendPayload(ctx, creds, payload, false)
	if err != nil {
		return "", err
	}
	if id == "" {
		return "", &Error{Code: CodeDecode, Message: "tiktok accepted the message without returning its id", Ambiguous: true}
	}
	return id, nil
}

func (c *Client) SendText(ctx context.Context, creds ttdomain.Credentials, conversationID, text, replyTo string) (string, error) {
	payload := map[string]any{
		"recipient":    conversationID,
		"message_type": "TEXT",
		"text":         map[string]string{"body": text},
	}
	if strings.TrimSpace(replyTo) != "" {
		payload["referenced_message_info"] = map[string]string{"referenced_message_id": replyTo}
	}
	return c.sendMessage(ctx, creds, payload)
}

func (c *Client) SendImage(ctx context.Context, creds ttdomain.Credentials, conversationID, mediaID string) (string, error) {
	return c.sendMessage(ctx, creds, map[string]any{
		"recipient":    conversationID,
		"message_type": "IMAGE",
		"image":        map[string]string{"media_id": mediaID},
	})
}

func (c *Client) SendCard(ctx context.Context, creds ttdomain.Credentials, conversationID string, card ttdomain.Card) (string, error) {
	buttons := make([]map[string]string, 0, len(card.Buttons))
	for _, b := range card.Buttons {
		buttons = append(buttons, map[string]string{"type": "REPLY", "title": b.Text, "id": b.ID})
	}
	return c.sendMessage(ctx, creds, map[string]any{
		"recipient":    conversationID,
		"message_type": "TEMPLATE",
		"template":     map[string]any{"type": "QA_BUTTON_CARD", "title": card.Title, "buttons": buttons},
	})
}

func (c *Client) SendAction(ctx context.Context, creds ttdomain.Credentials, conversationID string, action ttdomain.SenderAction) error {
	_, err := c.sendPayload(ctx, creds, map[string]any{
		"recipient":     conversationID,
		"message_type":  "SENDER_ACTION",
		"sender_action": string(action),
	}, true)
	return err
}

func (c *Client) UploadImage(ctx context.Context, creds ttdomain.Credentials, data []byte, fileName, mimeType string) (string, error) {
	if mimeType != ttdomain.MIMEJPEG && mimeType != ttdomain.MIMEPNG {
		return "", ttdomain.ErrMediaUnsupported
	}
	if len(data) == 0 || len(data) > ttdomain.MaxImageBytes {
		return "", ttdomain.ErrMediaTooLarge
	}
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	if err := w.WriteField("business_id", creds.OpenID); err != nil {
		return "", err
	}
	if err := w.WriteField("media_type", string(ttdomain.MediaKindImage)); err != nil {
		return "", err
	}
	part, err := w.CreateFormFile("file", safeFileName(fileName, mimeType))
	if err != nil {
		return "", err
	}
	if _, err := part.Write(data); err != nil {
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}
	raw, err := c.do(ctx, request{method: http.MethodPost, path: "/business/message/media/upload/", family: FamilyMediaUpload, creds: &creds, body: body.Bytes(), contentType: w.FormDataContentType(), retrySafe: true})
	if err != nil {
		return "", err
	}
	var out struct {
		MediaID string `json:"media_id"`
	}
	if err := decodeData(raw, &out); err != nil {
		return "", err
	}
	if strings.TrimSpace(out.MediaID) == "" {
		return "", &Error{Code: CodeDecode, Message: "upload returned no media id"}
	}
	return out.MediaID, nil
}

func safeFileName(name, mimeType string) string {
	name = strings.Map(func(r rune) rune {
		if r == '"' || r == '\\' || r == '/' || r < 0x20 {
			return '_'
		}
		return r
	}, strings.TrimSpace(name))
	if name != "" {
		return name
	}
	if mimeType == ttdomain.MIMEPNG {
		return "image.png"
	}
	return "image.jpg"
}

func (c *Client) DownloadMedia(ctx context.Context, creds ttdomain.Credentials, ref ttdomain.MediaRef) (ttdomain.DownloadedMedia, error) {
	var out struct {
		DownloadURL string `json:"download_url"`
	}
	err := c.postJSON(ctx, request{path: "/business/message/media/download/", family: FamilyMediaDownload, creds: &creds, retrySafe: true}, map[string]string{
		"business_id":     creds.OpenID,
		"conversation_id": ref.ConversationID,
		"message_id":      ref.MessageID,
		"media_id":        ref.MediaID,
		"media_type":      string(ref.Kind),
	}, &out)
	if err != nil {
		return ttdomain.DownloadedMedia{}, err
	}
	target, err := url.Parse(strings.TrimSpace(out.DownloadURL))
	if err != nil || target.Scheme != "https" || target.Host == "" {
		return ttdomain.DownloadedMedia{}, errDownloadURLInvalid
	}
	return c.fetchMedia(ctx, creds, target.String())
}

func (c *Client) fetchMedia(ctx context.Context, creds ttdomain.Credentials, target string) (ttdomain.DownloadedMedia, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return ttdomain.DownloadedMedia{}, err
	}
	req.Header.Set("x-user", creds.AccessToken)
	resp, err := c.media.Do(req)
	if err != nil {
		return ttdomain.DownloadedMedia{}, &Error{Code: CodeTransport, Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ttdomain.DownloadedMedia{}, &Error{HTTPStatus: resp.StatusCode, Message: "media download refused"}
	}
	data, err := readCapped(resp.Body, MaxInboundMediaBytes)
	if err != nil {
		return ttdomain.DownloadedMedia{}, &Error{Code: CodeResponseTooLarge, Err: err}
	}
	mimeType := strings.TrimSpace(strings.Split(resp.Header.Get("Content-Type"), ";")[0])
	if mimeType == "" || mimeType == "application/octet-stream" {
		mimeType = http.DetectContentType(data)
	}
	return ttdomain.DownloadedMedia{Data: data, MimeType: mimeType}, nil
}

func (c *Client) ImageAllowed(ctx context.Context, creds ttdomain.Credentials, conversationID string, conversationType ttdomain.ConversationType) (bool, error) {
	if conversationType == ttdomain.ConversationUnknown {
		conversationType = ttdomain.ConversationSingle
	}
	q := url.Values{}
	q.Set("business_id", creds.OpenID)
	q.Set("conversation_id", conversationID)
	q.Set("conversation_type", string(conversationType))
	q.Set("capability_types", jsonList([]string{capabilityImageSend}))
	var out struct {
		CapabilityInfos []struct {
			CapabilityType   string `json:"capability_type"`
			CapabilityResult bool   `json:"capability_result"`
		} `json:"capability_infos"`
	}
	if err := c.getJSON(ctx, request{path: "/business/message/capabilities/get/", query: q, family: FamilyCapabilities, creds: &creds}, &out); err != nil {
		return false, err
	}
	for _, info := range out.CapabilityInfos {
		if info.CapabilityType == capabilityImageSend {
			return info.CapabilityResult, nil
		}
	}
	return false, &Error{Code: CodeDecode, Message: fmt.Sprintf("capability %s missing from the answer", capabilityImageSend)}
}

func (c *Client) ProbeInbox(ctx context.Context, creds ttdomain.Credentials) error {
	q := url.Values{}
	q.Set("business_id", creds.OpenID)
	q.Set("conversation_type", string(ttdomain.ConversationSingle))
	q.Set("limit", "1")
	var out struct {
		HasMore bool `json:"has_more"`
	}
	return c.getJSON(ctx, request{path: "/business/message/conversation/list/", query: q, family: FamilyMessageRead, creds: &creds}, &out)
}

func (c *Client) Participants(ctx context.Context, creds ttdomain.Credentials, conversationID string) ([]ttdomain.Participant, error) {
	q := url.Values{}
	q.Set("business_id", creds.OpenID)
	q.Set("conversation_id", conversationID)
	var out struct {
		Participants []struct {
			Role         string `json:"role"`
			ID           string `json:"id"`
			Username     string `json:"username"`
			DisplayName  string `json:"display_name"`
			ProfileImage string `json:"profile_image"`
		} `json:"participants"`
	}
	if err := c.getJSON(ctx, request{path: "/business/message/content/list/", query: q, family: FamilyMessageRead, creds: &creds}, &out); err != nil {
		return nil, err
	}
	people := make([]ttdomain.Participant, 0, len(out.Participants))
	for _, p := range out.Participants {
		if !strings.EqualFold(p.Role, "PERSONAL_ACCOUNT") || !ttdomain.ValidOpaqueID(p.ID) {
			continue
		}
		people = append(people, ttdomain.Participant{
			PersonID:    p.ID,
			Username:    strings.TrimSpace(p.Username),
			DisplayName: strings.TrimSpace(p.DisplayName),
			AvatarURL:   strings.TrimSpace(p.ProfileImage),
		})
	}
	return people, nil
}
