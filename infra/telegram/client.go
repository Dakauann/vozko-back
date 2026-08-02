// Package telegram is the Bot API transport for the Telegram channel.
//
// It is deliberately the only place that knows about HTTPS, multipart bodies or
// Telegram's response envelope. Everything above it works with
// domain/telegram's contracts.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strconv"
	"strings"
	"time"

	tgdomain "vozko/domain/telegram"
)

// DefaultBaseURL is Telegram's hosted Bot API.
//
// It is configurable for one reason: a self-hosted Local Bot API Server lifts
// the 20MB inbound download ceiling and raises uploads to 2000MB. That is the
// only escape from the channel's hardest product limit, so the knob exists even
// though almost every deployment leaves it alone.
const DefaultBaseURL = "https://api.telegram.org"

// requestTimeout bounds a single Bot API call. Telegram's own webhook delivery
// is impatient, and a send that hangs holds a rate-limit slot the whole time.
const requestTimeout = 30 * time.Second

// maxDownloadBytes mirrors the documented bot download ceiling. Enforced here as
// well as at classification time so a lying file_size cannot make us stream an
// unbounded body into memory.
const maxDownloadBytes = tgdomain.MaxDownloadBytes

// Config configures the client.
type Config struct {
	// BaseURL defaults to DefaultBaseURL.
	BaseURL    string
	HTTPClient *http.Client
}

// Client implements domain/telegram.BotAPI.
//
// The bot token is a per-call argument rather than client state: a workspace can
// connect several bots, and binding the token to the call is what guarantees a
// reply leaves from the same bot the message arrived on.
type Client struct {
	baseURL string
	http    *http.Client
}

// NewClient builds the Bot API client.
func NewClient(cfg Config) *Client {
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		base = DefaultBaseURL
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: requestTimeout}
	}
	return &Client{baseURL: base, http: httpClient}
}

var _ tgdomain.BotAPI = (*Client)(nil)

// ---------------------------------------------------------------- envelope

// apiResponse is Telegram's uniform reply: "The response contains a JSON object,
// which always has a Boolean field 'ok'".
type apiResponse struct {
	OK          bool                `json:"ok"`
	Result      json.RawMessage     `json:"result"`
	Description string              `json:"description"`
	ErrorCode   int                 `json:"error_code"`
	Parameters  *responseParameters `json:"parameters"`
}

// responseParameters is the part of a failure that makes recovery possible
// rather than guessed: an explicit wait, and the new id of a migrated chat.
type responseParameters struct {
	MigrateToChatID int64 `json:"migrate_to_chat_id"`
	RetryAfter      int   `json:"retry_after"`
}

// AsAPIError extracts a structured Bot API error, if this is one.
func AsAPIError(err error) (*tgdomain.APIError, bool) {
	var apiErr *tgdomain.APIError
	if errors.As(err, &apiErr) {
		return apiErr, true
	}
	return nil, false
}

// call issues a JSON request and decodes result into out.
func (c *Client) call(ctx context.Context, token, method string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("telegram: encode %s: %w", method, err)
		}
		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.methodURL(token, method), reader)
	if err != nil {
		return fmt.Errorf("telegram: build %s: %w", method, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.do(req, method, out)
}

func (c *Client) do(req *http.Request, method string, out any) error {
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("telegram: %s: %w", method, err)
	}
	defer resp.Body.Close()

	// Bounded read: a Bot API response is small, and an unbounded one from a
	// misconfigured proxy should not be able to exhaust memory.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("telegram: %s: read body: %w", method, err)
	}

	var envelope apiResponse
	if err := json.Unmarshal(raw, &envelope); err != nil {
		// A non-JSON body means something in front of Telegram answered — a
		// proxy, a captive portal. Surface the status so it is diagnosable.
		return &tgdomain.APIError{
			HTTPStatus:  resp.StatusCode,
			Code:        resp.StatusCode,
			Description: fmt.Sprintf("%s: unreadable response: %s", method, truncate(string(raw), 200)),
		}
	}

	if !envelope.OK {
		apiErr := &tgdomain.APIError{
			HTTPStatus:  resp.StatusCode,
			Code:        envelope.ErrorCode,
			Description: envelope.Description,
		}
		if envelope.Parameters != nil {
			apiErr.RetryAfter = envelope.Parameters.RetryAfter
			apiErr.MigrateToChatID = envelope.Parameters.MigrateToChatID
		}
		if apiErr.Code == 0 {
			apiErr.Code = resp.StatusCode
		}
		return apiErr
	}

	if out == nil || len(envelope.Result) == 0 {
		return nil
	}
	if err := json.Unmarshal(envelope.Result, out); err != nil {
		return fmt.Errorf("telegram: %s: decode result: %w", method, err)
	}
	return nil
}

func (c *Client) methodURL(token, method string) string {
	return c.baseURL + "/bot" + token + "/" + method
}

// ---------------------------------------------------------------- identity

type getMeResult struct {
	ID                   int64  `json:"id"`
	IsBot                bool   `json:"is_bot"`
	FirstName            string `json:"first_name"`
	Username             string `json:"username"`
	CanJoinGroups        bool   `json:"can_join_groups"`
	CanReadAllGroup      bool   `json:"can_read_all_group_messages"`
	CanConnectToBusiness bool   `json:"can_connect_to_business"`
}

func (c *Client) GetMe(ctx context.Context, token string) (*tgdomain.BotProfile, error) {
	var res getMeResult
	if err := c.call(ctx, token, "getMe", nil, &res); err != nil {
		return nil, err
	}
	if res.ID == 0 {
		return nil, tgdomain.ErrBotTokenInvalid
	}
	return &tgdomain.BotProfile{
		BotUserID:            res.ID,
		Username:             res.Username,
		FirstName:            res.FirstName,
		CanJoinGroups:        res.CanJoinGroups,
		CanReadAllGroup:      res.CanReadAllGroup,
		CanConnectToBusiness: res.CanConnectToBusiness,
	}, nil
}

// ---------------------------------------------------------------- webhooks

type setWebhookBody struct {
	URL                string   `json:"url"`
	SecretToken        string   `json:"secret_token,omitempty"`
	MaxConnections     int      `json:"max_connections,omitempty"`
	AllowedUpdates     []string `json:"allowed_updates,omitempty"`
	DropPendingUpdates bool     `json:"drop_pending_updates,omitempty"`
}

func (c *Client) SetWebhook(ctx context.Context, token string, cfg tgdomain.WebhookConfig) error {
	if cfg.MaxConnections <= 0 {
		cfg.MaxConnections = tgdomain.DefaultMaxConnections
	}
	return c.call(ctx, token, "setWebhook", setWebhookBody{
		URL:                cfg.URL,
		SecretToken:        cfg.SecretToken,
		MaxConnections:     cfg.MaxConnections,
		AllowedUpdates:     cfg.AllowedUpdates,
		DropPendingUpdates: cfg.DropPendingUpdates,
	}, nil)
}

func (c *Client) DeleteWebhook(ctx context.Context, token string, dropPending bool) error {
	return c.call(ctx, token, "deleteWebhook", map[string]any{
		"drop_pending_updates": dropPending,
	}, nil)
}

type webhookInfoResult struct {
	URL              string   `json:"url"`
	PendingCount     int      `json:"pending_update_count"`
	LastErrorDate    int64    `json:"last_error_date"`
	LastErrorMessage string   `json:"last_error_message"`
	MaxConnections   int      `json:"max_connections"`
	AllowedUpdates   []string `json:"allowed_updates"`
}

func (c *Client) GetWebhookInfo(ctx context.Context, token string) (*tgdomain.WebhookInfo, error) {
	var res webhookInfoResult
	if err := c.call(ctx, token, "getWebhookInfo", nil, &res); err != nil {
		return nil, err
	}
	info := &tgdomain.WebhookInfo{
		URL:              res.URL,
		PendingCount:     res.PendingCount,
		LastErrorMessage: res.LastErrorMessage,
		MaxConnections:   res.MaxConnections,
		AllowedUpdates:   res.AllowedUpdates,
	}
	if res.LastErrorDate > 0 {
		t := time.Unix(res.LastErrorDate, 0).UTC()
		info.LastErrorDate = &t
	}
	return info, nil
}

// ---------------------------------------------------------------- sending

// messageResult is the Message a successful send returns.
//
// Telegram answering synchronously with the full message is what removes the
// echo-reconciliation step every Meta channel needs: the provider id is known
// before the row is written.
type messageResult struct {
	MessageID int64 `json:"message_id"`
	Date      int64 `json:"date"`
	Chat      struct {
		ID int64 `json:"id"`
	} `json:"chat"`

	Photo []struct {
		FileID string `json:"file_id"`
	} `json:"photo"`
	Video     *fileIDOnly `json:"video"`
	Audio     *fileIDOnly `json:"audio"`
	Voice     *fileIDOnly `json:"voice"`
	Document  *fileIDOnly `json:"document"`
	Animation *fileIDOnly `json:"animation"`
}

type fileIDOnly struct {
	FileID string `json:"file_id"`
}

// fileID extracts the id Telegram assigned to the asset it just accepted, so it
// can be cached and reused for free on the next send.
func (m messageResult) fileID() string {
	switch {
	case len(m.Photo) > 0:
		return m.Photo[len(m.Photo)-1].FileID
	case m.Video != nil:
		return m.Video.FileID
	case m.Audio != nil:
		return m.Audio.FileID
	case m.Voice != nil:
		return m.Voice.FileID
	case m.Document != nil:
		return m.Document.FileID
	case m.Animation != nil:
		return m.Animation.FileID
	}
	return ""
}

func (m messageResult) toResult() *tgdomain.SendResult {
	out := &tgdomain.SendResult{
		MessageID: m.MessageID,
		ChatID:    m.Chat.ID,
		FileID:    m.fileID(),
	}
	if m.Date > 0 {
		out.Date = time.Unix(m.Date, 0).UTC()
	}
	return out
}

type sendMessageBody struct {
	BusinessConnectionID string          `json:"business_connection_id,omitempty"`
	ChatID               int64           `json:"chat_id"`
	Text                 string          `json:"text"`
	ParseMode            string          `json:"parse_mode,omitempty"`
	ReplyParameters      *replyParams    `json:"reply_parameters,omitempty"`
	ReplyMarkup          json.RawMessage `json:"reply_markup,omitempty"`
}

type replyParams struct {
	MessageID int64 `json:"message_id"`
	// AllowSendingWithoutReply keeps the send working when the quoted message has
	// since been deleted — otherwise a reply to an unsent message fails outright.
	AllowSendingWithoutReply bool `json:"allow_sending_without_reply"`
}

func (c *Client) SendText(ctx context.Context, token string, in tgdomain.SendTextInput) (*tgdomain.SendResult, error) {
	body := sendMessageBody{
		BusinessConnectionID: in.BusinessConnectionID,
		ChatID:               in.ChatID,
		Text:                 in.Text,
		ParseMode:            in.ParseMode,
	}
	if in.ReplyToMessageID != 0 {
		body.ReplyParameters = &replyParams{MessageID: in.ReplyToMessageID, AllowSendingWithoutReply: true}
	}
	if in.ReplyMarkup != "" {
		body.ReplyMarkup = json.RawMessage(in.ReplyMarkup)
	}

	var res messageResult
	if err := c.call(ctx, token, "sendMessage", body, &res); err != nil {
		return nil, err
	}
	return res.toResult(), nil
}

// sendMethod maps a media kind onto its Bot API method and form field.
func sendMethod(kind tgdomain.MediaKind) (method, field string) {
	switch kind {
	case tgdomain.MediaPhoto:
		return "sendPhoto", "photo"
	case tgdomain.MediaVideo:
		return "sendVideo", "video"
	case tgdomain.MediaAudio:
		return "sendAudio", "audio"
	case tgdomain.MediaVoice:
		return "sendVoice", "voice"
	default:
		return "sendDocument", "document"
	}
}

func (c *Client) SendMedia(ctx context.Context, token string, in tgdomain.SendMediaInput) (*tgdomain.SendResult, error) {
	method, field := sendMethod(in.Kind)

	// A cached file_id or a URL is a plain JSON send; only raw bytes need
	// multipart. Preferring the id is what makes a repeat send of the same asset
	// free and unbounded.
	if in.FileID != "" || in.URL != "" {
		value := in.FileID
		if value == "" {
			value = in.URL
		}
		body := map[string]any{"chat_id": in.ChatID, field: value}
		addMediaCommon(body, in)

		var res messageResult
		if err := c.call(ctx, token, method, body, &res); err != nil {
			return nil, err
		}
		return res.toResult(), nil
	}

	if len(in.Bytes) == 0 {
		return nil, fmt.Errorf("telegram: %s requires a file_id, url or bytes", method)
	}
	return c.sendMultipart(ctx, token, method, field, in)
}

func addMediaCommon(body map[string]any, in tgdomain.SendMediaInput) {
	if in.BusinessConnectionID != "" {
		body["business_connection_id"] = in.BusinessConnectionID
	}
	if in.Caption != "" {
		body["caption"] = in.Caption
		body["parse_mode"] = "HTML"
	}
	if in.ReplyToMessageID != 0 {
		body["reply_parameters"] = replyParams{MessageID: in.ReplyToMessageID, AllowSendingWithoutReply: true}
	}
}

func (c *Client) sendMultipart(ctx context.Context, token, method, field string, in tgdomain.SendMediaInput) (*tgdomain.SendResult, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	writeField := func(name, value string) error {
		if value == "" {
			return nil
		}
		return writer.WriteField(name, value)
	}

	if err := writeField("chat_id", strconv.FormatInt(in.ChatID, 10)); err != nil {
		return nil, err
	}
	if err := writeField("business_connection_id", in.BusinessConnectionID); err != nil {
		return nil, err
	}
	if in.Caption != "" {
		if err := writeField("caption", in.Caption); err != nil {
			return nil, err
		}
		if err := writeField("parse_mode", "HTML"); err != nil {
			return nil, err
		}
	}
	if in.ReplyToMessageID != 0 {
		encoded, err := json.Marshal(replyParams{MessageID: in.ReplyToMessageID, AllowSendingWithoutReply: true})
		if err != nil {
			return nil, err
		}
		if err := writeField("reply_parameters", string(encoded)); err != nil {
			return nil, err
		}
	}

	filename := in.FileName
	if filename == "" {
		filename = "file"
	}
	// The part is written by hand rather than with CreateFormFile so the
	// Content-Type survives: Telegram uses it to decide how to render a document,
	// and CreateFormFile always writes application/octet-stream.
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition",
		fmt.Sprintf(`form-data; name="%s"; filename="%s"`, field, escapeQuotes(filename)))
	if in.MIMEType != "" {
		header.Set("Content-Type", in.MIMEType)
	}
	part, err := writer.CreatePart(header)
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(in.Bytes); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.methodURL(token, method), &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	var res messageResult
	if err := c.do(req, method, &res); err != nil {
		return nil, err
	}
	return res.toResult(), nil
}

// ---------------------------------------------------------------- mutations

func (c *Client) EditText(ctx context.Context, token string, chatID, messageID int64, text, parseMode, businessConnectionID string) error {
	body := map[string]any{
		"chat_id":    chatID,
		"message_id": messageID,
		"text":       text,
	}
	if parseMode != "" {
		body["parse_mode"] = parseMode
	}
	if businessConnectionID != "" {
		body["business_connection_id"] = businessConnectionID
	}
	return c.call(ctx, token, "editMessageText", body, nil)
}

func (c *Client) DeleteMessage(ctx context.Context, token string, chatID, messageID int64) error {
	return c.call(ctx, token, "deleteMessage", map[string]any{
		"chat_id":    chatID,
		"message_id": messageID,
	}, nil)
}

func (c *Client) DeleteBusinessMessages(ctx context.Context, token, businessConnectionID string, messageIDs []int64) error {
	if len(messageIDs) == 0 {
		return nil
	}
	// "A JSON-serialized list of 1-100 identifiers of messages to delete."
	const maxBatch = 100
	for start := 0; start < len(messageIDs); start += maxBatch {
		end := start + maxBatch
		if end > len(messageIDs) {
			end = len(messageIDs)
		}
		if err := c.call(ctx, token, "deleteBusinessMessages", map[string]any{
			"business_connection_id": businessConnectionID,
			"message_ids":            messageIDs[start:end],
		}, nil); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) SendChatAction(ctx context.Context, token string, chatID int64, action tgdomain.ChatAction, businessConnectionID string) error {
	body := map[string]any{"chat_id": chatID, "action": string(action)}
	if businessConnectionID != "" {
		body["business_connection_id"] = businessConnectionID
	}
	return c.call(ctx, token, "sendChatAction", body, nil)
}

func (c *Client) SetMessageReaction(ctx context.Context, token string, chatID, messageID int64, emoji string) error {
	body := map[string]any{"chat_id": chatID, "message_id": messageID}
	if emoji == "" {
		// An empty list clears the reaction. Bots may set at most one.
		body["reaction"] = []any{}
	} else {
		body["reaction"] = []map[string]string{{"type": "emoji", "emoji": emoji}}
	}
	return c.call(ctx, token, "setMessageReaction", body, nil)
}

func (c *Client) ReadBusinessMessage(ctx context.Context, token, businessConnectionID string, chatID, messageID int64) error {
	return c.call(ctx, token, "readBusinessMessage", map[string]any{
		"business_connection_id": businessConnectionID,
		"chat_id":                chatID,
		"message_id":             messageID,
	}, nil)
}

func (c *Client) AnswerCallbackQuery(ctx context.Context, token, callbackQueryID, text string) error {
	body := map[string]any{"callback_query_id": callbackQueryID}
	if text != "" {
		body["text"] = text
	}
	return c.call(ctx, token, "answerCallbackQuery", body, nil)
}

// ---------------------------------------------------------------- files

type getFileResult struct {
	FileID   string `json:"file_id"`
	FilePath string `json:"file_path"`
	FileSize int64  `json:"file_size"`
}

func (c *Client) GetFile(ctx context.Context, token, fileID string) (*tgdomain.RemoteFile, error) {
	var res getFileResult
	if err := c.call(ctx, token, "getFile", map[string]any{"file_id": fileID}, &res); err != nil {
		return nil, err
	}
	return &tgdomain.RemoteFile{
		FileID:   res.FileID,
		Path:     res.FilePath,
		Size:     res.FileSize,
		TooLarge: res.FileSize > maxDownloadBytes,
	}, nil
}

func (c *Client) DownloadFile(ctx context.Context, token, filePath string) ([]byte, string, error) {
	if filePath == "" {
		return nil, "", errors.New("telegram: empty file path")
	}
	url := c.baseURL + "/file/bot" + token + "/" + filePath

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("telegram: download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", &tgdomain.APIError{
			HTTPStatus:  resp.StatusCode,
			Code:        resp.StatusCode,
			Description: "file download failed",
		}
	}

	// Read one byte past the ceiling so an oversized body is detected rather than
	// silently truncated into a corrupt file.
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxDownloadBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("telegram: download: %w", err)
	}
	if int64(len(data)) > maxDownloadBytes {
		return nil, "", tgdomain.ErrFileTooLarge
	}
	return data, resp.Header.Get("Content-Type"), nil
}

type userProfilePhotos struct {
	TotalCount int `json:"total_count"`
	// Photos is an array of size-arrays: one entry per photo, each holding that
	// photo's sizes smallest-first.
	Photos [][]struct {
		FileID string `json:"file_id"`
	} `json:"photos"`
}

func (c *Client) GetUserProfilePhotoFileID(ctx context.Context, token string, userID int64) (string, error) {
	var res userProfilePhotos
	if err := c.call(ctx, token, "getUserProfilePhotos", map[string]any{
		"user_id": userID,
		"limit":   1,
	}, &res); err != nil {
		return "", err
	}
	if len(res.Photos) == 0 || len(res.Photos[0]) == 0 {
		return "", nil
	}
	sizes := res.Photos[0]
	return sizes[len(sizes)-1].FileID, nil
}

// ---------------------------------------------------------------- helpers

func escapeQuotes(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
