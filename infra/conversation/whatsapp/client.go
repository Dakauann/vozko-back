package whatsapp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/lead"
)

type Config struct {
	BaseURL       string
	PhoneNumberID string
	WABAId        string
	AccessToken   string
	AppID         string
	HTTPClient    *http.Client
	// AuthHeaderName and AuthValuePrefix parameterize the credential header so
	// the same client serves both providers. Empty defaults to the Meta scheme
	// ("Authorization: Bearer <token>"). For 360dialog set AuthHeaderName to
	// "D360-API-KEY" and AuthValuePrefix to "" with AccessToken set to the
	// per channel API key.
	AuthHeaderName  string
	AuthValuePrefix string
	// OmitPhoneNumberInPath drops the phone-number-id segment from the send/media
	// paths. Meta uses "{base}/{phone_number_id}/messages"; 360dialog identifies
	// the channel by the API key, so its path is "{base}/messages". Set true for
	// 360dialog channels.
	OmitPhoneNumberInPath bool
	// TemplatesChannelScoped switches template management to 360dialog's
	// channel-scoped API. Meta uses the WABA-scoped Graph path
	// "{base}/{waba_id}/message_templates"; 360dialog scopes by the API key and
	// uses "{base}/v1/configs/templates" (no waba id, name-keyed delete, and a
	// "waba_templates" list envelope). Set true for 360dialog channels.
	TemplatesChannelScoped bool
}

type Client struct {
	baseURL                string
	phoneNumberID          string
	wabaID                 string
	accessToken            string
	appID                  string
	httpClient             *http.Client
	authHeaderName         string
	authValuePrefix        string
	omitPhoneNumberInPath  bool
	templatesChannelScoped bool
}

// messagesEndpointFor builds the send endpoint, omitting the phone-number-id for
// providers (360dialog) that scope the channel by the API key instead.
func (c *Client) messagesEndpointFor(phoneNumberID string) string {
	if c.omitPhoneNumberInPath {
		return c.baseURL + "/messages"
	}
	return fmt.Sprintf("%s/%s/messages", c.baseURL, phoneNumberID)
}

func (c *Client) messagesEndpoint() string { return c.messagesEndpointFor(c.phoneNumberID) }

func (c *Client) mediaEndpoint() string {
	if c.omitPhoneNumberInPath {
		return c.baseURL + "/media"
	}
	return fmt.Sprintf("%s/%s/media", c.baseURL, c.phoneNumberID)
}

// templatesCollectionEndpoint builds the create/list template endpoint. 360dialog
// scopes templates by the API key ("{base}/v1/configs/templates"); Meta scopes by
// the WABA in the Graph path ("{base}/{waba_id}/message_templates").
func (c *Client) templatesCollectionEndpoint() string {
	if c.templatesChannelScoped {
		return c.baseURL + "/v1/configs/templates"
	}
	return fmt.Sprintf("%s/%s/message_templates", c.baseURL, c.wabaID)
}

// setAuth applies the provider appropriate credential header to a request. It
// is used by every Cloud API call so Meta and 360dialog channels share the same
// request builders (360dialog wraps the Meta Cloud API, so only the credential
// header and base URL differ).
func (c *Client) setAuth(req *http.Request) {
	name := c.authHeaderName
	if name == "" {
		req.Header.Set("Authorization", "Bearer "+c.accessToken)
		return
	}
	req.Header.Set(name, c.authValuePrefix+c.accessToken)
}

func (c *Client) uploadMedia(ctx context.Context, data []byte, fileName string, mimeType string) (string, error) {
	if c == nil || c.phoneNumberID == "" || c.accessToken == "" {
		return "", conversation.ErrWhatsAppClientDisabled
	}

	if len(data) == 0 {
		return "", fmt.Errorf("media data is empty")
	}

	if strings.TrimSpace(mimeType) == "" {
		mimeType = http.DetectContentType(data)
	}

	log.Printf("[whatsapp-upload] Starting upload: file=%s, mimeType=%s, size=%d bytes", fileName, mimeType, len(data))

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	if err := writer.WriteField("messaging_product", "whatsapp"); err != nil {
		return "", fmt.Errorf("failed to write messaging_product field: %w", err)
	}

	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, fileName))
	h.Set("Content-Type", mimeType)

	filePart, err := writer.CreatePart(h)
	if err != nil {
		return "", fmt.Errorf("failed to create file part: %w", err)
	}

	if _, err := filePart.Write(data); err != nil {
		return "", fmt.Errorf("failed to write media data: %w", err)
	}

	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("failed to close multipart writer: %w", err)
	}

	endpoint := c.mediaEndpoint()
	log.Printf("[whatsapp-upload] Sending request to: %s", endpoint)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &body)
	if err != nil {
		return "", fmt.Errorf("failed to create upload request: %w", err)
	}
	c.setAuth(req)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	log.Printf("[whatsapp-upload] Executing HTTP request...")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		log.Printf("[whatsapp-upload] Request failed: %v", err)
		return "", fmt.Errorf("upload request failed: %w", err)
	}
	defer resp.Body.Close()
	log.Printf("[whatsapp-upload] Got response: status=%d", resp.StatusCode)

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read upload response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("[whatsapp-upload] Upload failed: status=%d body=%s", resp.StatusCode, string(respBody))
		return "", fmt.Errorf("whatsapp media upload failed: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	log.Printf("[whatsapp-upload] Upload successful, parsing response: %s", string(respBody))

	var uploadResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(respBody, &uploadResp); err != nil {
		return "", fmt.Errorf("failed to parse upload response: %w", err)
	}

	if uploadResp.ID == "" {
		return "", fmt.Errorf("upload response missing media ID: %s", string(respBody))
	}

	return uploadResp.ID, nil
}

func NewClient(cfg Config) conversation.WhatsAppClient {
	baseURL := strings.TrimSpace(cfg.BaseURL)
	if baseURL == "" {
		baseURL = "https://graph.facebook.com/v22.0"
	}
	baseURL = strings.TrimRight(baseURL, "/")

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}

	return &Client{
		baseURL:                baseURL,
		phoneNumberID:          strings.TrimSpace(cfg.PhoneNumberID),
		wabaID:                 strings.TrimSpace(cfg.WABAId),
		accessToken:            strings.TrimSpace(cfg.AccessToken),
		appID:                  strings.TrimSpace(cfg.AppID),
		httpClient:             httpClient,
		authHeaderName:         strings.TrimSpace(cfg.AuthHeaderName),
		authValuePrefix:        cfg.AuthValuePrefix,
		omitPhoneNumberInPath:  cfg.OmitPhoneNumberInPath,
		templatesChannelScoped: cfg.TemplatesChannelScoped,
	}
}

func (c *Client) UploadAudio(ctx context.Context, audioData []byte, fileName string) (string, error) {
	if len(audioData) == 0 {
		return "", fmt.Errorf("audio data is empty")
	}

	mimeType := "audio/ogg"
	lowerName := strings.ToLower(fileName)
	switch {
	case strings.HasSuffix(lowerName, ".mp3"):
		mimeType = "audio/mpeg"
	case strings.HasSuffix(lowerName, ".m4a"):
		mimeType = "audio/mp4"
	case strings.HasSuffix(lowerName, ".aac"):
		mimeType = "audio/aac"
	case strings.HasSuffix(lowerName, ".amr"):
		mimeType = "audio/amr"
	case strings.HasSuffix(lowerName, ".ogg"), strings.HasSuffix(lowerName, ".opus"):
		mimeType = "audio/ogg"
	case strings.HasSuffix(lowerName, ".wav"):
		mimeType = "audio/wav"
	}

	return c.uploadMedia(ctx, audioData, fileName, mimeType)
}

func (c *Client) UploadImage(ctx context.Context, imageData []byte, fileName string, mimeType string) (string, error) {
	if len(imageData) == 0 {
		return "", fmt.Errorf("image data is empty")
	}

	if strings.TrimSpace(mimeType) == "" {
		mimeType = http.DetectContentType(imageData)
	}

	return c.uploadMedia(ctx, imageData, fileName, mimeType)
}

func (c *Client) UploadMedia(ctx context.Context, data []byte, fileName string, mimeType string) (string, error) {
	if len(data) == 0 {
		return "", fmt.Errorf("media data is empty")
	}

	if strings.TrimSpace(mimeType) == "" {
		mimeType = http.DetectContentType(data)
	}

	return c.uploadMedia(ctx, data, fileName, mimeType)
}

func (c *Client) SendAudioMessage(ctx context.Context, input conversation.SendAudioMessageInput) (*conversation.SendTextMessageOutput, error) {
	if c == nil || c.phoneNumberID == "" || c.accessToken == "" {
		return nil, conversation.ErrWhatsAppClientDisabled
	}

	to := lead.NormalizeWhatsAppNumber(strings.TrimSpace(input.To))
	audioURL := strings.TrimSpace(input.AudioURL)

	if to == "" {
		return nil, conversation.ErrWhatsAppRecipientRequired
	}
	if audioURL == "" {
		return nil, fmt.Errorf("audio URL or media ID is required")
	}

	var audioPayload map[string]interface{}
	if strings.HasPrefix(audioURL, "http://") || strings.HasPrefix(audioURL, "https://") {

		audioPayload = map[string]interface{}{"link": audioURL}
	} else {

		audioPayload = map[string]interface{}{"id": audioURL}
	}

	payload := map[string]interface{}{
		"messaging_product": "whatsapp",
		"recipient_type":    "individual",
		"to":                to,
		"type":              "audio",
		"audio":             audioPayload,
	}

	if contextID := strings.TrimSpace(input.ContextMessageID); contextID != "" {
		payload["context"] = map[string]interface{}{
			"message_id": contextID,
		}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal audio message: %w", err)
	}

	endpoint := c.messagesEndpoint()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create audio message request: %w", err)
	}
	c.setAuth(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("audio message request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read audio message response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("whatsapp send audio failed: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	var decoded sendTextMessageResponse
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return nil, fmt.Errorf("failed to parse audio message response: %w", err)
	}

	output := conversation.SendTextMessageOutput{}
	if len(decoded.Messages) > 0 {
		output.MessageID = decoded.Messages[0].ID
		output.MessageStatus = decoded.Messages[0].MessageStatus
	}
	if len(decoded.Contacts) > 0 {
		output.ContactWaID = decoded.Contacts[0].WaID
	}

	return &output, nil
}

func (c *Client) SendVoiceMessage(ctx context.Context, to string, audioMediaID string, contextMessageID string) (*conversation.SendTextMessageOutput, error) {
	if c == nil || c.phoneNumberID == "" || c.accessToken == "" {
		return nil, conversation.ErrWhatsAppClientDisabled
	}

	to = lead.NormalizeWhatsAppNumber(strings.TrimSpace(to))
	audioMediaID = strings.TrimSpace(audioMediaID)

	if to == "" {
		return nil, conversation.ErrWhatsAppRecipientRequired
	}
	if audioMediaID == "" {
		return nil, fmt.Errorf("audio media ID is required")
	}

	payload := map[string]interface{}{
		"messaging_product": "whatsapp",
		"recipient_type":    "individual",
		"to":                to,
		"type":              "audio",
		"audio": map[string]interface{}{
			"id":    audioMediaID,
			"voice": true,
		},
	}

	if ctxID := strings.TrimSpace(contextMessageID); ctxID != "" {
		payload["context"] = map[string]interface{}{
			"message_id": ctxID,
		}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal voice message: %w", err)
	}

	endpoint := c.messagesEndpoint()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create voice message request: %w", err)
	}
	c.setAuth(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("voice message request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read voice message response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("whatsapp voice message failed: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	var decoded struct {
		Messages []struct {
			ID string `json:"id"`
		} `json:"messages"`
		Contacts []struct {
			WaID string `json:"wa_id"`
		} `json:"contacts"`
	}
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return nil, fmt.Errorf("failed to parse voice message response: %w", err)
	}

	output := conversation.SendTextMessageOutput{}
	if len(decoded.Messages) > 0 {
		output.MessageID = decoded.Messages[0].ID
	}
	if len(decoded.Contacts) > 0 {
		output.ContactWaID = decoded.Contacts[0].WaID
	}

	return &output, nil
}

func (c *Client) SendAudioBytes(ctx context.Context, to string, audioData []byte, fileName string, contextMessageID string) (*conversation.SendTextMessageOutput, error) {
	if c == nil || c.phoneNumberID == "" || c.accessToken == "" {
		return nil, conversation.ErrWhatsAppClientDisabled
	}

	mediaID, err := c.UploadAudio(ctx, audioData, fileName)
	if err != nil {
		return nil, fmt.Errorf("failed to upload audio: %w", err)
	}

	return c.SendVoiceMessage(ctx, to, mediaID, contextMessageID)
}

// isDialog360 reports whether this client talks to 360dialog (which wraps the
// Meta Cloud API) rather than Meta directly. 360dialog channels authenticate
// with the D360-API-KEY header; Meta uses "Authorization: Bearer".
func (c *Client) isDialog360() bool { return c.authHeaderName == "D360-API-KEY" }

// TemplateHeaderMediaWantsURL implements conversation.WhatsAppTemplateMediaClient.
// 360dialog's channel-scoped template endpoint ("/v1/configs/templates") expects
// the public media URL verbatim in example.header_handle and fetches it itself;
// Meta's Graph endpoint expects a Resumable-Upload handle instead. templatesChannelScoped
// is exactly the 360dialog case, so it is the correct predicate.
func (c *Client) TemplateHeaderMediaWantsURL() bool { return c.templatesChannelScoped }

// mediaDownloadURL adapts the media URL returned by GET /{media-id} for the
// actual byte download.
//
// GET /{media-id} returns a `url` on Meta's lookaside CDN (https://lookaside.fbsbx.com/...).
// For Meta that URL is downloaded as-is with the Meta bearer and works. For
// 360dialog it does NOT: the lookaside host only accepts a Meta bearer, which a
// 360dialog channel does not have, so downloading it with the D360-API-KEY 401s.
// Per 360dialog's docs the bytes must instead be pulled from the 360dialog host,
// keeping the path + query string intact, authenticated with the D360-API-KEY:
//
//	https://lookaside.fbsbx.com/whatsapp_business/attachments/?mid=...&ext=...&hash=...
//	-> https://waba-v2.360dialog.io/whatsapp_business/attachments/?mid=...&ext=...&hash=...
//
// So for 360dialog we swap the returned URL's scheme+host for our (360dialog)
// base host; for Meta the URL is returned unchanged. (The JSON `url` is already
// backslash-unescaped by encoding/json, so no extra stripping is needed.)
func (c *Client) mediaDownloadURL(mediaURL string) string {
	if !c.isDialog360() {
		return mediaURL
	}
	base, err := url.Parse(c.baseURL)
	if err != nil || base.Host == "" {
		return mediaURL
	}
	u, err := url.Parse(mediaURL)
	if err != nil || u.Host == "" {
		return mediaURL
	}
	u.Scheme = base.Scheme
	u.Host = base.Host
	return u.String()
}

func (c *Client) DownloadMedia(ctx context.Context, mediaID string) ([]byte, string, error) {
	if c == nil || c.phoneNumberID == "" || c.accessToken == "" {
		return nil, "", conversation.ErrWhatsAppClientDisabled
	}

	mediaID = strings.TrimSpace(mediaID)
	if mediaID == "" {
		return nil, "", fmt.Errorf("media ID is required")
	}

	mediaInfoURL := fmt.Sprintf("%s/%s", c.baseURL, mediaID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, mediaInfoURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create media info request: %w", err)
	}
	c.setAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("media info request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read media info response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("failed to get media info: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	var mediaInfo struct {
		MessagingProduct string `json:"messaging_product"`
		URL              string `json:"url"`
		MimeType         string `json:"mime_type"`
		SHA256           string `json:"sha256"`
		FileSize         int64  `json:"file_size"`
		ID               string `json:"id"`
	}
	if err := json.Unmarshal(respBody, &mediaInfo); err != nil {
		return nil, "", fmt.Errorf("failed to parse media info response: %w", err)
	}

	if mediaInfo.URL == "" {
		return nil, "", fmt.Errorf("media URL not found in response: %s", string(respBody))
	}

	downloadReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.mediaDownloadURL(mediaInfo.URL), nil)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create download request: %w", err)
	}
	c.setAuth(downloadReq)

	downloadResp, err := c.httpClient.Do(downloadReq)
	if err != nil {
		return nil, "", fmt.Errorf("media download request failed: %w", err)
	}
	defer downloadResp.Body.Close()

	if downloadResp.StatusCode < 200 || downloadResp.StatusCode >= 300 {
		downloadBody, _ := io.ReadAll(downloadResp.Body)
		return nil, "", fmt.Errorf("media download failed: status=%d body=%s", downloadResp.StatusCode, string(downloadBody))
	}

	mediaBytes, err := io.ReadAll(downloadResp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read media content: %w", err)
	}

	mimeType := downloadResp.Header.Get("Content-Type")
	if mimeType == "" {
		mimeType = mediaInfo.MimeType
	}

	return mediaBytes, mimeType, nil
}

func (c *Client) SendImageMessage(ctx context.Context, input conversation.SendImageMessageInput) (*conversation.SendTextMessageOutput, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}
	if c == nil || c.phoneNumberID == "" || c.accessToken == "" {
		return nil, conversation.ErrWhatsAppClientDisabled
	}

	imagePayload := make(map[string]interface{})

	if strings.TrimSpace(input.ImageID) != "" {
		imagePayload["id"] = strings.TrimSpace(input.ImageID)
	} else {
		imagePayload["link"] = strings.TrimSpace(input.Link)
	}

	if strings.TrimSpace(input.Caption) != "" {
		imagePayload["caption"] = strings.TrimSpace(input.Caption)
	}

	normalizedTo := lead.NormalizeWhatsAppNumber(input.To)

	payload := map[string]interface{}{
		"messaging_product": "whatsapp",
		"recipient_type":    "individual",
		"to":                normalizedTo,
		"type":              "image",
		"image":             imagePayload,
	}
	if strings.TrimSpace(input.ContextMessageID) != "" {
		payload["context"] = map[string]string{"message_id": strings.TrimSpace(input.ContextMessageID)}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal image message: %w", err)
	}

	endpoint := c.messagesEndpoint()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create image message request: %w", err)
	}
	c.setAuth(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("image message request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read image message response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("whatsapp send image failed: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	var decoded sendTextMessageResponse
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return nil, fmt.Errorf("failed to parse image message response: %w", err)
	}

	output := conversation.SendTextMessageOutput{}
	if len(decoded.Messages) > 0 {
		output.MessageID = decoded.Messages[0].ID
		output.MessageStatus = decoded.Messages[0].MessageStatus
	}
	if len(decoded.Contacts) > 0 {
		output.ContactWaID = decoded.Contacts[0].WaID
	}

	return &output, nil
}

func (c *Client) SendVideoMessage(ctx context.Context, input conversation.SendVideoMessageInput) (*conversation.SendTextMessageOutput, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}
	if c == nil || c.phoneNumberID == "" || c.accessToken == "" {
		return nil, conversation.ErrWhatsAppClientDisabled
	}

	videoPayload := make(map[string]interface{})

	if strings.TrimSpace(input.VideoID) != "" {
		videoPayload["id"] = strings.TrimSpace(input.VideoID)
	} else {
		videoPayload["link"] = strings.TrimSpace(input.Link)
	}

	if strings.TrimSpace(input.Caption) != "" {
		videoPayload["caption"] = strings.TrimSpace(input.Caption)
	}

	normalizedTo := lead.NormalizeWhatsAppNumber(input.To)

	payload := map[string]interface{}{
		"messaging_product": "whatsapp",
		"recipient_type":    "individual",
		"to":                normalizedTo,
		"type":              "video",
		"video":             videoPayload,
	}
	if strings.TrimSpace(input.ContextMessageID) != "" {
		payload["context"] = map[string]string{"message_id": strings.TrimSpace(input.ContextMessageID)}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal video message: %w", err)
	}

	endpoint := c.messagesEndpoint()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create video message request: %w", err)
	}
	c.setAuth(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("video message request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read video message response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("whatsapp send video failed: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	var decoded sendTextMessageResponse
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return nil, fmt.Errorf("failed to parse video message response: %w", err)
	}

	output := conversation.SendTextMessageOutput{}
	if len(decoded.Messages) > 0 {
		output.MessageID = decoded.Messages[0].ID
		output.MessageStatus = decoded.Messages[0].MessageStatus
	}
	if len(decoded.Contacts) > 0 {
		output.ContactWaID = decoded.Contacts[0].WaID
	}

	return &output, nil
}

func (c *Client) SendDocumentMessage(ctx context.Context, input conversation.SendDocumentMessageInput) (*conversation.SendTextMessageOutput, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}
	if c == nil || c.phoneNumberID == "" || c.accessToken == "" {
		return nil, conversation.ErrWhatsAppClientDisabled
	}

	documentPayload := make(map[string]interface{})

	if strings.TrimSpace(input.DocumentID) != "" {
		documentPayload["id"] = strings.TrimSpace(input.DocumentID)
	} else {
		documentPayload["link"] = strings.TrimSpace(input.Link)
	}

	if strings.TrimSpace(input.Caption) != "" {
		documentPayload["caption"] = strings.TrimSpace(input.Caption)
	}

	if strings.TrimSpace(input.Filename) != "" {
		documentPayload["filename"] = strings.TrimSpace(input.Filename)
	}

	normalizedTo := lead.NormalizeWhatsAppNumber(input.To)

	payload := map[string]interface{}{
		"messaging_product": "whatsapp",
		"recipient_type":    "individual",
		"to":                normalizedTo,
		"type":              "document",
		"document":          documentPayload,
	}
	if strings.TrimSpace(input.ContextMessageID) != "" {
		payload["context"] = map[string]string{"message_id": strings.TrimSpace(input.ContextMessageID)}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal document message: %w", err)
	}

	endpoint := c.messagesEndpoint()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create document message request: %w", err)
	}
	c.setAuth(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("document message request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read document message response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("whatsapp send document failed: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	var decoded sendTextMessageResponse
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return nil, fmt.Errorf("failed to parse document message response: %w", err)
	}

	output := conversation.SendTextMessageOutput{}
	if len(decoded.Messages) > 0 {
		output.MessageID = decoded.Messages[0].ID
		output.MessageStatus = decoded.Messages[0].MessageStatus
	}
	if len(decoded.Contacts) > 0 {
		output.ContactWaID = decoded.Contacts[0].WaID
	}

	return &output, nil
}

func (c *Client) SendCallPermissionRequest(ctx context.Context, input conversation.SendCallPermissionRequestInput) (*conversation.SendTextMessageOutput, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}
	if c == nil || c.phoneNumberID == "" || c.accessToken == "" {
		return nil, conversation.ErrWhatsAppClientDisabled
	}

	normalizedTo := lead.NormalizeWhatsAppNumber(input.To)
	payload := map[string]interface{}{
		"messaging_product": "whatsapp",
		"recipient_type":    "individual",
		"to":                normalizedTo,
		"type":              "interactive",
		"interactive": map[string]interface{}{
			"type":   "call_permission_request",
			"action": map[string]string{"name": "call_permission_request"},
			"body":   map[string]string{"text": strings.TrimSpace(input.BodyText)},
		},
	}
	if strings.TrimSpace(input.ContextMessageID) != "" {
		payload["context"] = map[string]string{"message_id": strings.TrimSpace(input.ContextMessageID)}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal call permission request: %w", err)
	}

	endpoint := c.messagesEndpoint()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create call permission request: %w", err)
	}
	c.setAuth(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call permission request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read call permission response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("whatsapp send call permission request failed: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	var decoded sendTextMessageResponse
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return nil, fmt.Errorf("failed to parse call permission response: %w", err)
	}
	output := conversation.SendTextMessageOutput{}
	if len(decoded.Messages) > 0 {
		output.MessageID = decoded.Messages[0].ID
		output.MessageStatus = decoded.Messages[0].MessageStatus
	}
	if len(decoded.Contacts) > 0 {
		output.ContactWaID = decoded.Contacts[0].WaID
	}
	return &output, nil
}

func (c *Client) SendStickerMessage(ctx context.Context, input conversation.SendStickerMessageInput) (*conversation.SendTextMessageOutput, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}
	if c == nil || c.phoneNumberID == "" || c.accessToken == "" {
		return nil, conversation.ErrWhatsAppClientDisabled
	}

	stickerPayload := make(map[string]interface{})

	if strings.TrimSpace(input.StickerID) != "" {
		stickerPayload["id"] = strings.TrimSpace(input.StickerID)
	} else {
		stickerPayload["link"] = strings.TrimSpace(input.Link)
	}

	normalizedTo := lead.NormalizeWhatsAppNumber(input.To)

	payload := map[string]interface{}{
		"messaging_product": "whatsapp",
		"recipient_type":    "individual",
		"to":                normalizedTo,
		"type":              "sticker",
		"sticker":           stickerPayload,
	}
	if strings.TrimSpace(input.ContextMessageID) != "" {
		payload["context"] = map[string]string{"message_id": strings.TrimSpace(input.ContextMessageID)}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal sticker message: %w", err)
	}

	endpoint := c.messagesEndpoint()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create sticker message request: %w", err)
	}
	c.setAuth(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sticker message request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read sticker message response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("whatsapp send sticker failed: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	var decoded sendTextMessageResponse
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return nil, fmt.Errorf("failed to parse sticker message response: %w", err)
	}

	output := conversation.SendTextMessageOutput{}
	if len(decoded.Messages) > 0 {
		output.MessageID = decoded.Messages[0].ID
		output.MessageStatus = decoded.Messages[0].MessageStatus
	}
	if len(decoded.Contacts) > 0 {
		output.ContactWaID = decoded.Contacts[0].WaID
	}

	return &output, nil
}

func (c *Client) SendButtonMessage(ctx context.Context, input conversation.SendButtonMessageInput) (*conversation.SendTextMessageOutput, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}
	if c == nil || c.phoneNumberID == "" || c.accessToken == "" {
		return nil, conversation.ErrWhatsAppClientDisabled
	}

	hasCopyCode := false
	for _, btn := range input.Buttons {
		if btn.Type == conversation.ButtonTypeCopyCode {
			hasCopyCode = true
			break
		}
	}
	if hasCopyCode {
		var sb strings.Builder
		sb.WriteString(input.BodyText)
		for _, btn := range input.Buttons {
			if btn.Type == conversation.ButtonTypeCopyCode && btn.CopyCode != "" {
				sb.WriteString("\n\n")
				sb.WriteString("```\n")
				sb.WriteString(btn.CopyCode)
				sb.WriteString("\n```")
			}
		}
		if ft := strings.TrimSpace(input.FooterText); ft != "" {
			sb.WriteString("\n\n_")
			sb.WriteString(ft)
			sb.WriteString("_")
		}
		return c.SendTextMessage(ctx, conversation.SendTextMessageInput{
			To:   input.To,
			Body: sb.String(),
		})
	}

	buttons := make([]map[string]interface{}, len(input.Buttons))
	for i, btn := range input.Buttons {
		buttons[i] = map[string]interface{}{
			"type": "reply",
			"reply": map[string]interface{}{
				"id":    btn.ID,
				"title": btn.Title,
			},
		}
	}

	interactive := map[string]interface{}{
		"type": "button",
		"body": map[string]interface{}{
			"text": input.BodyText,
		},
		"action": map[string]interface{}{
			"buttons": buttons,
		},
	}

	if input.HeaderType != "" {
		header := make(map[string]interface{})
		header["type"] = string(input.HeaderType)

		switch input.HeaderType {
		case conversation.HeaderTypeText:
			header["text"] = input.HeaderText
		case conversation.HeaderTypeImage:
			if strings.TrimSpace(input.MediaID) != "" {
				header["image"] = map[string]interface{}{"id": strings.TrimSpace(input.MediaID)}
			} else if strings.TrimSpace(input.MediaLink) != "" {
				header["image"] = map[string]interface{}{"link": strings.TrimSpace(input.MediaLink)}
			}
		case conversation.HeaderTypeVideo:
			if strings.TrimSpace(input.MediaID) != "" {
				header["video"] = map[string]interface{}{"id": strings.TrimSpace(input.MediaID)}
			} else if strings.TrimSpace(input.MediaLink) != "" {
				header["video"] = map[string]interface{}{"link": strings.TrimSpace(input.MediaLink)}
			}
		}
		interactive["header"] = header
	}

	if strings.TrimSpace(input.FooterText) != "" {
		interactive["footer"] = map[string]interface{}{
			"text": strings.TrimSpace(input.FooterText),
		}
	}

	normalizedTo := lead.NormalizeWhatsAppNumber(input.To)

	payload := map[string]interface{}{
		"messaging_product": "whatsapp",
		"recipient_type":    "individual",
		"to":                normalizedTo,
		"type":              "interactive",
		"interactive":       interactive,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal button message: %w", err)
	}

	endpoint := c.messagesEndpoint()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create button message request: %w", err)
	}
	c.setAuth(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("button message request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read button message response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("whatsapp send button message failed: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	var decoded sendTextMessageResponse
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return nil, fmt.Errorf("failed to parse button message response: %w", err)
	}

	output := conversation.SendTextMessageOutput{}
	if len(decoded.Messages) > 0 {
		output.MessageID = decoded.Messages[0].ID
		output.MessageStatus = decoded.Messages[0].MessageStatus
	}
	if len(decoded.Contacts) > 0 {
		output.ContactWaID = decoded.Contacts[0].WaID
	}

	return &output, nil
}

func (c *Client) SendListMessage(ctx context.Context, input conversation.SendListMessageInput) (*conversation.SendTextMessageOutput, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}
	if c == nil || c.phoneNumberID == "" || c.accessToken == "" {
		return nil, conversation.ErrWhatsAppClientDisabled
	}

	sections := make([]map[string]interface{}, 0, len(input.Sections))
	for _, section := range input.Sections {
		rows := make([]map[string]interface{}, 0, len(section.Rows))
		for _, row := range section.Rows {
			r := map[string]interface{}{
				"id":    row.ID,
				"title": row.Title,
			}
			if strings.TrimSpace(row.Description) != "" {
				r["description"] = row.Description
			}
			rows = append(rows, r)
		}
		sec := map[string]interface{}{"rows": rows}
		if strings.TrimSpace(section.Title) != "" {
			sec["title"] = section.Title
		}
		sections = append(sections, sec)
	}

	interactive := map[string]interface{}{
		"type": "list",
		"body": map[string]interface{}{
			"text": input.BodyText,
		},
		"action": map[string]interface{}{
			"button":   input.ButtonText,
			"sections": sections,
		},
	}

	if strings.TrimSpace(input.HeaderText) != "" {
		interactive["header"] = map[string]interface{}{
			"type": "text",
			"text": input.HeaderText,
		}
	}
	if strings.TrimSpace(input.FooterText) != "" {
		interactive["footer"] = map[string]interface{}{
			"text": strings.TrimSpace(input.FooterText),
		}
	}

	normalizedTo := lead.NormalizeWhatsAppNumber(input.To)

	payload := map[string]interface{}{
		"messaging_product": "whatsapp",
		"recipient_type":    "individual",
		"to":                normalizedTo,
		"type":              "interactive",
		"interactive":       interactive,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal list message: %w", err)
	}

	endpoint := c.messagesEndpoint()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create list message request: %w", err)
	}
	c.setAuth(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list message request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read list message response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("whatsapp send list message failed: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	var decoded sendTextMessageResponse
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return nil, fmt.Errorf("failed to parse list message response: %w", err)
	}

	output := conversation.SendTextMessageOutput{}
	if len(decoded.Messages) > 0 {
		output.MessageID = decoded.Messages[0].ID
		output.MessageStatus = decoded.Messages[0].MessageStatus
	}
	if len(decoded.Contacts) > 0 {
		output.ContactWaID = decoded.Contacts[0].WaID
	}

	return &output, nil
}

func (c *Client) SendTextMessage(ctx context.Context, input conversation.SendTextMessageInput) (*conversation.SendTextMessageOutput, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}
	if c == nil || c.phoneNumberID == "" || c.accessToken == "" {
		return nil, conversation.ErrWhatsAppClientDisabled
	}

	normalizedTo := lead.NormalizeWhatsAppNumber(input.To)

	payload := sendTextMessageRequest{
		MessagingProduct: "whatsapp",
		RecipientType:    "INDIVIDUAL",
		To:               normalizedTo,
		Type:             "text",
		Text: textContent{
			Body: strings.TrimSpace(input.Body),
		},
	}
	if input.PreviewURL {
		payload.Text.PreviewURL = boolPtr(true)
	}
	if strings.TrimSpace(input.ContextMessageID) != "" {
		payload.Context = &contextReference{MessageID: strings.TrimSpace(input.ContextMessageID)}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	phoneNumberID := c.phoneNumberID
	if input.FromPhoneNumberID != "" {
		phoneNumberID = input.FromPhoneNumberID
	}

	endpoint := c.messagesEndpointFor(phoneNumberID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	c.setAuth(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("whatsapp send failed: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	var decoded sendTextMessageResponse
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return nil, err
	}

	output := conversation.SendTextMessageOutput{}
	if len(decoded.Messages) > 0 {
		output.MessageID = decoded.Messages[0].ID
		output.MessageStatus = decoded.Messages[0].MessageStatus
	}
	if len(decoded.Contacts) > 0 {
		output.ContactWaID = decoded.Contacts[0].WaID
	}

	return &output, nil
}

type sendTextMessageRequest struct {
	MessagingProduct string            `json:"messaging_product"`
	RecipientType    string            `json:"recipient_type,omitempty"`
	To               string            `json:"to"`
	Type             string            `json:"type"`
	Text             textContent       `json:"text"`
	Context          *contextReference `json:"context,omitempty"`
}

type contextReference struct {
	MessageID string `json:"message_id"`
}

type textContent struct {
	Body       string `json:"body"`
	PreviewURL *bool  `json:"preview_url,omitempty"`
}

type sendTextMessageResponse struct {
	MessagingProduct string                `json:"messaging_product"`
	Contacts         []sendContactResponse `json:"contacts"`
	Messages         []sendMessageResponse `json:"messages"`
	Success          bool                  `json:"success"`
}

type sendContactResponse struct {
	Input string `json:"input"`
	WaID  string `json:"wa_id"`
	User  string `json:"user_id"`
}

type sendMessageResponse struct {
	ID            string `json:"id"`
	MessageStatus string `json:"message_status"`
}

func boolPtr(v bool) *bool {
	return &v
}

func isNumericString(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func (c *Client) SendTemplateMessage(ctx context.Context, input conversation.SendTemplateMessageInput) (*conversation.SendTextMessageOutput, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}
	if c == nil || c.phoneNumberID == "" || c.accessToken == "" {
		return nil, conversation.ErrWhatsAppClientDisabled
	}

	lang := input.Language
	if lang == "" {
		lang = "pt_BR"
	}

	normalizedTo := lead.NormalizeWhatsAppNumber(input.To)

	payload := sendTemplateMessageRequest{
		MessagingProduct: "whatsapp",
		To:               normalizedTo,
		Type:             "template",
		Template: templatePayload{
			Name: input.TemplateName,
			Language: templateLanguage{
				Code: lang,
			},
		},
		BizOpaqueCallbackData: input.BizOpaqueCallbackData,
	}

	var components []templateComponentPayload

	headerType := strings.ToLower(strings.TrimSpace(input.HeaderType))
	if headerType != "" && (input.HeaderMediaURL != "" || input.HeaderMediaID != "") {
		headerParam := templateParameter{Type: headerType}

		switch headerType {
		case "image":
			headerParam.Image = &templateMediaParam{}
			if input.HeaderMediaID != "" {
				headerParam.Image.ID = input.HeaderMediaID
			} else {
				headerParam.Image.Link = input.HeaderMediaURL
			}
		case "video":
			headerParam.Video = &templateMediaParam{}
			if input.HeaderMediaID != "" {
				headerParam.Video.ID = input.HeaderMediaID
			} else {
				headerParam.Video.Link = input.HeaderMediaURL
			}
		case "document":
			headerParam.Document = &templateDocumentParam{}
			if input.HeaderMediaID != "" {
				headerParam.Document.ID = input.HeaderMediaID
			} else {
				headerParam.Document.Link = input.HeaderMediaURL
			}
			if input.HeaderFilename != "" {
				headerParam.Document.Filename = input.HeaderFilename
			}
		}

		components = append(components, templateComponentPayload{
			Type:       "header",
			Parameters: []templateParameter{headerParam},
		})
	} else if len(input.HeaderTextParams) > 0 {
		headerParams := make([]templateParameter, len(input.HeaderTextParams))
		for i, p := range input.HeaderTextParams {
			headerParams[i] = templateParameter{Type: "text", Text: p}
		}
		components = append(components, templateComponentPayload{
			Type:       "header",
			Parameters: headerParams,
		})
	}

	if len(input.Parameters) > 0 {
		params := make([]templateParameter, len(input.Parameters))
		for i, p := range input.Parameters {
			param := templateParameter{Type: "text", Text: p}
			if input.IsNamedParameterFormat && len(input.ParameterNames) > i {
				paramName := input.ParameterNames[i]
				if paramName != "" && !isNumericString(paramName) {
					param.ParameterName = paramName
				}
			}
			params[i] = param
		}
		components = append(components, templateComponentPayload{
			Type:       "body",
			Parameters: params,
		})
	}

	if len(components) > 0 {
		payload.Template.Components = components
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	log.Printf("[WhatsApp SendTemplateMessage] Sending to %s, template: %s, payload: %s", input.To, input.TemplateName, string(body))

	phoneNumberID := c.phoneNumberID
	if input.FromPhoneNumberID != "" {
		phoneNumberID = strings.TrimSpace(input.FromPhoneNumberID)
	}

	endpoint := c.messagesEndpointFor(phoneNumberID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	c.setAuth(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &conversation.SendTextMessageOutput{
			RequestPayload:  body,
			ResponsePayload: respBody,
			ResponseStatus:  resp.StatusCode,
		}, fmt.Errorf("whatsapp send template failed: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	var decoded sendTextMessageResponse
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		// Meta ACCEPTED this send — the status was 2xx — and we could not read the
		// answer. Returning a bare error here made this indistinguishable from a
		// request that never arrived, so every caller refunded a message that had
		// already been delivered: we pay Meta and collect nothing.
		//
		// The output is returned alongside the error so the caller can see the 2xx
		// for itself, and the error is typed so it can be told apart from a
		// transport failure. Callers must not refund and must not resend; the
		// delivery-status webhook reconciles the attempt.
		return &conversation.SendTextMessageOutput{
			RequestPayload:  body,
			ResponsePayload: respBody,
			ResponseStatus:  resp.StatusCode,
		}, fmt.Errorf("%w: %v", conversation.ErrSendOutcomeUnknown, err)
	}

	output := conversation.SendTextMessageOutput{
		RequestPayload:  body,
		ResponsePayload: respBody,
		ResponseStatus:  resp.StatusCode,
	}
	if len(decoded.Messages) > 0 {
		output.MessageID = decoded.Messages[0].ID
		output.MessageStatus = decoded.Messages[0].MessageStatus
	}
	if len(decoded.Contacts) > 0 {
		output.ContactWaID = decoded.Contacts[0].WaID
	}

	return &output, nil
}

func (c *Client) SendTypingIndicator(ctx context.Context, messageID string) error {
	if c == nil || c.phoneNumberID == "" || c.accessToken == "" {
		return conversation.ErrWhatsAppClientDisabled
	}

	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return nil
	}

	payload := map[string]interface{}{
		"messaging_product": "whatsapp",
		"status":            "read",
		"message_id":        messageID,
		"typing_indicator": map[string]string{
			"type": "text",
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	endpoint := c.messagesEndpoint()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
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

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("whatsapp typing indicator failed: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	return nil
}

func (c *Client) MarkMessageAsRead(ctx context.Context, messageID string) error {
	if c == nil || c.phoneNumberID == "" || c.accessToken == "" {
		return conversation.ErrWhatsAppClientDisabled
	}

	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return nil
	}

	payload := map[string]interface{}{
		"messaging_product": "whatsapp",
		"status":            "read",
		"message_id":        messageID,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	endpoint := c.messagesEndpoint()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
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

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("whatsapp mark as read failed: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	return nil
}

func (c *Client) ListTemplates(ctx context.Context, input conversation.ListTemplatesInput) (*conversation.ListTemplatesOutput, error) {
	if c == nil || c.accessToken == "" || (!c.templatesChannelScoped && c.wabaID == "") {
		return nil, conversation.ErrWhatsAppWABAIDRequired
	}

	if c.templatesChannelScoped {
		return c.listTemplatesDialog360(ctx, input)
	}

	endpoint := fmt.Sprintf("%s/%s/message_templates?fields=id,name,status,category,language,parameter_format,components", c.baseURL, c.wabaID)
	if input.Status != "" {
		endpoint += "&status=" + input.Status
	}
	if input.Limit > 0 {
		endpoint += fmt.Sprintf("&limit=%d", input.Limit)
	}
	if strings.TrimSpace(input.After) != "" {
		endpoint += "&after=" + url.QueryEscape(strings.TrimSpace(input.After))
	}

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

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("whatsapp list templates failed: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	var decoded listTemplatesResponse
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return nil, err
	}

	templates := make([]conversation.Template, len(decoded.Data))
	for i, t := range decoded.Data {
		templates[i] = mapTemplateResponse(t)
	}

	return &conversation.ListTemplatesOutput{
		Templates: templates,
		HasMore:   decoded.Paging.Next != "",
		NextAfter: decoded.Paging.Cursors.After,
	}, nil
}

// listTemplatesDialog360 lists templates via 360dialog's channel-scoped endpoint
// (GET {base}/v1/configs/templates), which returns a "waba_templates" array with
// offset/limit paging instead of Meta's cursor-based "data" envelope.
func (c *Client) listTemplatesDialog360(ctx context.Context, input conversation.ListTemplatesInput) (*conversation.ListTemplatesOutput, error) {
	limit := input.Limit
	if limit <= 0 {
		limit = 1000 // 360dialog default page size
	}
	offset := 0
	if a := strings.TrimSpace(input.After); a != "" {
		if n, err := strconv.Atoi(a); err == nil && n >= 0 {
			offset = n
		}
	}
	endpoint := fmt.Sprintf("%s/v1/configs/templates?limit=%d&offset=%d", c.baseURL, limit, offset)

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

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("360dialog list templates failed: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	var decoded dialog360ListTemplatesResponse
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return nil, err
	}

	templates := make([]conversation.Template, len(decoded.WabaTemplates))
	for i, t := range decoded.WabaTemplates {
		templates[i] = mapTemplateResponse(t)
	}

	hasMore := len(decoded.WabaTemplates) == limit
	nextAfter := ""
	if hasMore {
		nextAfter = strconv.Itoa(offset + limit)
	}
	return &conversation.ListTemplatesOutput{
		Templates: templates,
		HasMore:   hasMore,
		NextAfter: nextAfter,
	}, nil
}

func (c *Client) GetTemplate(ctx context.Context, templateID string) (*conversation.Template, error) {
	if c == nil || c.accessToken == "" {
		return nil, conversation.ErrWhatsAppClientDisabled
	}
	if templateID == "" {
		return nil, conversation.ErrWhatsAppTemplateIDRequired
	}

	if c.templatesChannelScoped {
		// 360dialog exposes no Graph-style by-id GET; templates are name-keyed, so
		// resolve by listing and matching the id/name.
		listed, err := c.listTemplatesDialog360(ctx, conversation.ListTemplatesInput{})
		if err != nil {
			return nil, err
		}
		for i := range listed.Templates {
			if listed.Templates[i].ID == templateID || listed.Templates[i].Name == templateID {
				return &listed.Templates[i], nil
			}
		}
		return nil, conversation.ErrWhatsAppTemplateIDRequired
	}

	endpoint := fmt.Sprintf("%s/%s?fields=id,name,status,category,language,parameter_format,components", c.baseURL, templateID)

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

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("whatsapp get template failed: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	var decoded templateResponse
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return nil, err
	}

	result := mapTemplateResponse(decoded)
	return &result, nil
}

func (c *Client) CreateTemplate(ctx context.Context, input conversation.CreateTemplateInput) (*conversation.CreateTemplateOutput, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}
	// 360dialog scopes templates by the API key, so it needs no WABA id; Meta does.
	if c == nil || c.accessToken == "" || (!c.templatesChannelScoped && c.wabaID == "") {
		return nil, conversation.ErrWhatsAppWABAIDRequired
	}

	lang := input.Language
	if lang == "" {
		lang = "pt_BR"
	}
	category := input.Category
	if category == "" {
		category = "MARKETING"
	}

	payload := createTemplateRequest{
		Name:     input.Name,
		Language: lang,
		Category: category,
	}

	// 360dialog's channel-scoped template endpoint rejects Meta's top-level
	// parameter_format with 400 "Unknown field.", it wraps an older Meta template
	// API version that has no such field (it infers the format from the {{...}}
	// placeholders). Only Meta Cloud API accepts it.
	if input.ParameterFormat != "" && !c.templatesChannelScoped {
		payload.ParameterFormat = input.ParameterFormat
	}

	for _, comp := range input.Components {
		compType := strings.ToUpper(comp.Type)
		apiComp := createTemplateComponent{
			Type: compType,
			Text: comp.Text,
		}

		if compType == "HEADER" && comp.Format != "" {
			apiComp.Format = strings.ToUpper(comp.Format)
		}

		if compType == "BUTTONS" && len(comp.Buttons) > 0 {
			apiComp.Text = ""
			for _, btn := range comp.Buttons {
				btnType := strings.ToUpper(btn.Type)
				apiBtn := createTemplateButton{
					Type:        btnType,
					Text:        btn.Text,
					URL:         btn.URL,
					PhoneNumber: btn.PhoneNumber,
				}

				if btn.Example != "" {
					apiBtn.Example = []string{btn.Example}
				}

				if btnType == "COPY_CODE" && btn.Example != "" {

					apiBtn.Text = ""
				}
				apiComp.Buttons = append(apiComp.Buttons, apiBtn)
			}
		}

		if comp.Example != nil {
			apiComp.Example = &createTemplateExample{
				HeaderText:   comp.Example.HeaderText,
				HeaderHandle: comp.Example.HeaderHandle,
				BodyText:     comp.Example.BodyText,
			}

			for _, np := range comp.Example.BodyTextNamed {
				apiComp.Example.BodyTextNamed = append(apiComp.Example.BodyTextNamed, createNamedParamExample{
					ParamName: np.ParamName,
					Example:   np.Example,
				})
			}

			for _, np := range comp.Example.HeaderTextNamed {
				apiComp.Example.HeaderTextNamed = append(apiComp.Example.HeaderTextNamed, createNamedParamExample{
					ParamName: np.ParamName,
					Example:   np.Example,
				})
			}
		}

		payload.Components = append(payload.Components, apiComp)
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	log.Printf("[whatsapp-template] Creating template %q, payload: %s", input.Name, string(body))

	endpoint := c.templatesCollectionEndpoint()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	c.setAuth(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	log.Printf("[whatsapp-template] Meta response for %q: status=%d body=%s", input.Name, resp.StatusCode, string(respBody))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("whatsapp create template failed: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	var decoded createTemplateResponse
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return nil, err
	}

	// 360dialog's create response has no numeric id; key the template by name.
	id := decoded.ID
	if id == "" {
		id = decoded.Name
	}
	if id == "" {
		id = input.Name
	}

	rejectedReason := strings.TrimSpace(decoded.RejectedReason)
	if rejectedReason == "NONE" {
		rejectedReason = ""
	}
	if decoded.Status == "REJECTED" {
		// Meta's create response usually omits rejected_reason; fetch it via a
		// follow-up GET so callers (and the user) see the real cause
		// (e.g. INVALID_FORMAT) instead of a guess. 360dialog already returns the
		// reason inline (and exposes no Graph-style by-id GET), so skip the fetch.
		if rejectedReason == "" && !c.templatesChannelScoped {
			rejectedReason = c.fetchTemplateRejectedReason(ctx, decoded.ID)
		}
		log.Printf("[whatsapp-template] Template %q REJECTED. rejected_reason=%q. Verify parameter_format matches the placeholder style and that every variable has an example.", input.Name, rejectedReason)
	}

	return &conversation.CreateTemplateOutput{
		ID:             id,
		Status:         decoded.Status,
		Category:       decoded.Category,
		RejectedReason: rejectedReason,
	}, nil
}

// fetchTemplateRejectedReason does a best-effort GET of a just-created template
// to read Meta's rejected_reason, which the POST create response omits. Returns
// "" on any error or when Meta reports no reason.
func (c *Client) fetchTemplateRejectedReason(ctx context.Context, templateID string) string {
	if templateID == "" {
		return ""
	}
	endpoint := fmt.Sprintf("%s/%s?fields=status,rejected_reason", c.baseURL, templateID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return ""
	}
	c.setAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ""
	}

	var decoded struct {
		RejectedReason string `json:"rejected_reason"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		return ""
	}

	reason := strings.TrimSpace(decoded.RejectedReason)
	if reason == "NONE" {
		return ""
	}
	return reason
}

func (c *Client) UpdateTemplate(ctx context.Context, templateID string, input conversation.UpdateTemplateInput) error {
	if c == nil || c.accessToken == "" {
		return conversation.ErrWhatsAppClientDisabled
	}
	if templateID == "" {
		return conversation.ErrWhatsAppTemplateIDRequired
	}

	payload := make(map[string]interface{})
	if input.Category != "" {
		payload["category"] = input.Category
	}
	if len(input.Components) > 0 {
		comps := make([]createTemplateComponent, len(input.Components))
		for i, comp := range input.Components {
			comps[i] = createTemplateComponent{
				Type: strings.ToUpper(comp.Type),
				Text: comp.Text,
			}
			if comp.Example != nil {
				comps[i].Example = &createTemplateExample{
					BodyText: comp.Example.BodyText,
				}
			}
		}
		payload["components"] = comps
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	endpoint := fmt.Sprintf("%s/%s", c.baseURL, templateID)
	if c.templatesChannelScoped {
		// 360dialog edits the name-keyed resource. The exact method is not in the
		// public docs; POST mirrors the Meta edit and fails visibly if 360 expects
		// PATCH/PUT (template edits are rare). Confirm against a live channel.
		endpoint = fmt.Sprintf("%s/v1/configs/templates/%s", c.baseURL, url.PathEscape(templateID))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
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

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("whatsapp update template failed: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	return nil
}

func (c *Client) DeleteTemplate(ctx context.Context, input conversation.DeleteTemplateInput) error {
	if err := input.Validate(); err != nil {
		return err
	}
	if c == nil || c.accessToken == "" || (!c.templatesChannelScoped && c.wabaID == "") {
		return conversation.ErrWhatsAppWABAIDRequired
	}

	var endpoint string
	if c.templatesChannelScoped {
		// 360dialog deletes by template name in the path.
		endpoint = fmt.Sprintf("%s/v1/configs/templates/%s", c.baseURL, url.PathEscape(input.Name))
	} else {
		endpoint = fmt.Sprintf("%s/%s/message_templates?name=%s", c.baseURL, c.wabaID, input.Name)
		if input.TemplateID != "" {
			endpoint += "&hsm_id=" + input.TemplateID
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return err
	}
	c.setAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("whatsapp delete template failed: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	return nil
}

func (c *Client) UploadMediaForTemplate(ctx context.Context, input conversation.UploadMediaForTemplateInput) (string, error) {
	if c == nil || c.accessToken == "" {
		return "", fmt.Errorf("whatsapp client not properly configured for template media upload (missing credential)")
	}
	// Only Meta's Resumable Upload API is app-scoped ("{base}/{app_id}/uploads").
	// 360dialog proxies the same API scoped by the channel API key, so it needs
	// no App ID.
	if !c.isDialog360() && c.appID == "" {
		return "", fmt.Errorf("whatsapp client not properly configured for template media upload (requires App ID)")
	}

	var data []byte
	var err error
	if input.URL != "" {
		data, err = c.downloadFromURL(ctx, input.URL)
		if err != nil {
			return "", fmt.Errorf("failed to download media from URL: %w", err)
		}
	} else if len(input.Data) > 0 {
		data = input.Data
	} else {
		return "", fmt.Errorf("either URL or Data must be provided")
	}

	mimeType := input.MimeType
	if mimeType == "" {
		mimeType = c.inferMimeTypeFromFileName(input.FileName)
	}
	if mimeType == "" {
		mimeType = http.DetectContentType(data)
	}

	validMimeTypes := map[string]bool{
		"application/pdf": true,
		"image/jpeg":      true,
		"image/jpg":       true,
		"image/png":       true,
		"video/mp4":       true,
	}
	if !validMimeTypes[mimeType] {
		return "", fmt.Errorf("invalid MIME type for template media: %s (valid: pdf, jpeg, jpg, png, mp4)", mimeType)
	}

	fileName := input.FileName
	if fileName == "" {
		fileName = "media_file"
		switch mimeType {
		case "application/pdf":
			fileName += ".pdf"
		case "image/jpeg", "image/jpg":
			fileName += ".jpg"
		case "image/png":
			fileName += ".png"
		case "video/mp4":
			fileName += ".mp4"
		}
	}

	sessionID, err := c.createUploadSession(ctx, fileName, int64(len(data)), mimeType)
	if err != nil {
		return "", fmt.Errorf("failed to create upload session: %w", err)
	}

	log.Printf("[whatsapp-template-upload] Created upload session: %s for file %s (%d bytes, %s)", sessionID, fileName, len(data), mimeType)

	handle, err := c.uploadFileToSession(ctx, sessionID, data)
	if err != nil {
		return "", fmt.Errorf("failed to upload file to session: %w", err)
	}

	log.Printf("[whatsapp-template-upload] Successfully uploaded file, handle: %s", handle)

	return handle, nil
}

func (c *Client) downloadFromURL(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("failed to download from URL: status=%d", resp.StatusCode)
	}

	const maxSize = 50 * 1024 * 1024
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxSize))
	if err != nil {
		return nil, err
	}

	return data, nil
}

func (c *Client) inferMimeTypeFromFileName(fileName string) string {
	lowerName := strings.ToLower(fileName)
	switch {
	case strings.HasSuffix(lowerName, ".pdf"):
		return "application/pdf"
	case strings.HasSuffix(lowerName, ".jpg"), strings.HasSuffix(lowerName, ".jpeg"):
		return "image/jpeg"
	case strings.HasSuffix(lowerName, ".png"):
		return "image/png"
	case strings.HasSuffix(lowerName, ".mp4"):
		return "video/mp4"
	default:
		return ""
	}
}

// createUploadSession opens a Resumable Upload API session and returns its id.
// Meta scopes the endpoint by app id ("{base}/{app_id}/uploads"); 360dialog
// proxies the same API scoped by the channel API key ("{base}/uploads", no
// app-id segment and no file_name param, per docs.360dialog.com "Resumable
// Upload API", the documented source of template header_handle assets).
func (c *Client) createUploadSession(ctx context.Context, fileName string, fileLength int64, fileType string) (string, error) {
	params := url.Values{}
	params.Set("file_length", strconv.FormatInt(fileLength, 10))
	params.Set("file_type", fileType)

	var endpoint string
	if c.isDialog360() {
		endpoint = c.baseURL + "/uploads?" + params.Encode()
	} else {
		params.Set("file_name", fileName)
		endpoint = fmt.Sprintf("%s/%s/uploads?%s", c.baseURL, c.appID, params.Encode())
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return "", err
	}
	c.setAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("create upload session failed: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	var result struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("failed to parse upload session response: %w", err)
	}

	if result.ID == "" {
		return "", fmt.Errorf("upload session response missing ID: %s", string(respBody))
	}

	return result.ID, nil
}

func (c *Client) uploadFileToSession(ctx context.Context, sessionID string, data []byte) (string, error) {
	endpoint := fmt.Sprintf("%s/%s", c.baseURL, sessionID)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	// Meta's session upload wants "Authorization: OAuth <token>" (not Bearer);
	// 360dialog authenticates it with the channel D360-API-KEY header like every
	// other call.
	if c.isDialog360() {
		c.setAuth(req)
	} else {
		req.Header.Set("Authorization", fmt.Sprintf("OAuth %s", c.accessToken))
	}
	req.Header.Set("file_offset", "0")
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("upload file failed: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	var result struct {
		H string `json:"h"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("failed to parse upload response: %w", err)
	}

	if result.H == "" {
		return "", fmt.Errorf("upload response missing handle: %s", string(respBody))
	}

	return result.H, nil
}

func mapTemplateResponse(t templateResponse) conversation.Template {
	components := make([]conversation.TemplateComponent, len(t.Components))
	for i, c := range t.Components {
		components[i] = conversation.TemplateComponent{
			Type:   c.Type,
			Format: c.Format,
			Text:   c.Text,
		}

		for _, b := range c.Buttons {
			components[i].Buttons = append(components[i].Buttons, conversation.TemplateButton{
				Type:        b.Type,
				Text:        b.Text,
				URL:         b.URL,
				PhoneNumber: b.PhoneNumber,
			})
		}

		if c.Example != nil {
			components[i].Example = &conversation.TemplateExample{
				HeaderText:   c.Example.HeaderText,
				HeaderHandle: c.Example.HeaderHandle,
				BodyText:     c.Example.BodyText,
			}
			for _, np := range c.Example.BodyTextNamed {
				components[i].Example.BodyTextNamed = append(components[i].Example.BodyTextNamed, conversation.NamedParamExample{
					ParamName: np.ParamName,
					Example:   np.Example,
				})
			}
			for _, np := range c.Example.HeaderTextNamed {
				components[i].Example.HeaderTextNamed = append(components[i].Example.HeaderTextNamed, conversation.NamedParamExample{
					ParamName: np.ParamName,
					Example:   np.Example,
				})
			}
		}
	}
	id := t.ID
	if id == "" {
		// 360dialog templates have no numeric id; key them by name.
		id = t.Name
	}
	return conversation.Template{
		ID:              id,
		Name:            t.Name,
		Status:          t.Status,
		Category:        t.Category,
		Language:        t.Language,
		ParameterFormat: t.ParameterFormat,
		Components:      components,
	}
}

type sendTemplateMessageRequest struct {
	MessagingProduct string          `json:"messaging_product"`
	To               string          `json:"to"`
	Type             string          `json:"type"`
	Template         templatePayload `json:"template"`
	// BizOpaqueCallbackData rides along untouched and comes back on every
	// delivery-status webhook for this message. It is how a status event finds
	// the send attempt that paid for it. Same mechanism already used for call
	// signalling in calls_signaling.go.
	BizOpaqueCallbackData string `json:"biz_opaque_callback_data,omitempty"`
}

type templatePayload struct {
	Name       string                     `json:"name"`
	Language   templateLanguage           `json:"language"`
	Components []templateComponentPayload `json:"components,omitempty"`
}

type templateLanguage struct {
	Code string `json:"code"`
}

type templateComponentPayload struct {
	Type       string              `json:"type"`
	Parameters []templateParameter `json:"parameters,omitempty"`
}

type templateParameter struct {
	Type          string                 `json:"type"`
	Text          string                 `json:"text,omitempty"`
	ParameterName string                 `json:"parameter_name,omitempty"`
	Image         *templateMediaParam    `json:"image,omitempty"`
	Video         *templateMediaParam    `json:"video,omitempty"`
	Document      *templateDocumentParam `json:"document,omitempty"`
}

type templateMediaParam struct {
	Link string `json:"link,omitempty"`
	ID   string `json:"id,omitempty"`
}

type templateDocumentParam struct {
	Link     string `json:"link,omitempty"`
	ID       string `json:"id,omitempty"`
	Filename string `json:"filename,omitempty"`
}

type listTemplatesResponse struct {
	Data   []templateResponse `json:"data"`
	Paging struct {
		Cursors struct {
			Before string `json:"before"`
			After  string `json:"after"`
		} `json:"cursors"`
		Next string `json:"next"`
	} `json:"paging"`
}

// dialog360ListTemplatesResponse is 360dialog's channel-scoped list envelope: the
// templates live under "waba_templates" with offset/limit/total fields (no Meta
// "data"/cursor paging).
type dialog360ListTemplatesResponse struct {
	WabaTemplates []templateResponse `json:"waba_templates"`
	Total         int                `json:"total"`
	Limit         int                `json:"limit"`
	Offset        int                `json:"offset"`
}

type templateResponse struct {
	ID              string                      `json:"id"`
	Name            string                      `json:"name"`
	Status          string                      `json:"status"`
	Category        string                      `json:"category"`
	Language        string                      `json:"language"`
	ParameterFormat string                      `json:"parameter_format,omitempty"`
	Components      []templateComponentResponse `json:"components"`
}

type templateComponentResponse struct {
	Type    string                   `json:"type"`
	Format  string                   `json:"format,omitempty"`
	Text    string                   `json:"text,omitempty"`
	Buttons []templateButtonResponse `json:"buttons,omitempty"`
	Example *templateExampleResponse `json:"example,omitempty"`
}

type templateButtonResponse struct {
	Type        string   `json:"type"`
	Text        string   `json:"text,omitempty"`
	URL         string   `json:"url,omitempty"`
	PhoneNumber string   `json:"phone_number,omitempty"`
	Example     []string `json:"example,omitempty"`
}

type templateExampleResponse struct {
	HeaderText      []string                     `json:"header_text,omitempty"`
	HeaderHandle    []string                     `json:"header_handle,omitempty"`
	BodyText        [][]string                   `json:"body_text,omitempty"`
	BodyTextNamed   []templateNamedParamResponse `json:"body_text_named_params,omitempty"`
	HeaderTextNamed []templateNamedParamResponse `json:"header_text_named_params,omitempty"`
}

type templateNamedParamResponse struct {
	ParamName string `json:"param_name"`
	Example   string `json:"example"`
}

type createTemplateRequest struct {
	Name            string                    `json:"name"`
	Language        string                    `json:"language"`
	Category        string                    `json:"category"`
	ParameterFormat string                    `json:"parameter_format,omitempty"`
	Components      []createTemplateComponent `json:"components"`
}

type createTemplateComponent struct {
	Type    string                 `json:"type"`
	Text    string                 `json:"text,omitempty"`
	Format  string                 `json:"format,omitempty"`
	Buttons []createTemplateButton `json:"buttons,omitempty"`
	Example *createTemplateExample `json:"example,omitempty"`
}

type createTemplateButton struct {
	Type        string   `json:"type"`
	Text        string   `json:"text,omitempty"`
	URL         string   `json:"url,omitempty"`
	PhoneNumber string   `json:"phone_number,omitempty"`
	Example     []string `json:"example,omitempty"`
}

type createTemplateExample struct {
	HeaderText      []string                  `json:"header_text,omitempty"`
	HeaderHandle    []string                  `json:"header_handle,omitempty"`
	BodyText        [][]string                `json:"body_text,omitempty"`
	BodyTextNamed   []createNamedParamExample `json:"body_text_named_params,omitempty"`
	HeaderTextNamed []createNamedParamExample `json:"header_text_named_params,omitempty"`
}

type createNamedParamExample struct {
	ParamName string `json:"param_name"`
	Example   string `json:"example"`
}

type createTemplateResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	// Name and Namespace are 360dialog's identifiers; its create response carries
	// no numeric id, so callers fall back to the name to key the template.
	Name           string `json:"name"`
	Namespace      string `json:"namespace"`
	Category       string `json:"category"`
	RejectedReason string `json:"rejected_reason"`
}
