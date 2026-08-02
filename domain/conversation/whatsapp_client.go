package conversation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"vozko/domain/channel"
)

// WhatsApp interactive-message limits, per Meta's WhatsApp Cloud API docs.
// Reply buttons: developers.facebook.com/docs/whatsapp/cloud-api/messages/interactive-reply-buttons-messages
// List messages: developers.facebook.com/docs/whatsapp/cloud-api/messages/interactive-list-messages
// Kept here as the single source of truth so the typed Send*Input.Validate and the
// workflow node config validation enforce the SAME numbers.
const (
	WhatsAppButtonMaxCount    = 3
	WhatsAppButtonIDMaxLen    = 256
	WhatsAppButtonTitleMaxLen = 20
	WhatsAppButtonBodyMaxLen  = 1024

	WhatsAppHeaderTextMaxLen = 60
	WhatsAppFooterMaxLen     = 60

	WhatsAppListBodyMaxLen         = 4096
	WhatsAppListButtonMaxLen       = 20
	WhatsAppListSectionTitleMaxLen = 24
	WhatsAppListRowIDMaxLen        = 200
	WhatsAppListRowTitleMaxLen     = 24
	WhatsAppListRowDescMaxLen      = 72
	WhatsAppListRowsMax            = 10
	WhatsAppListSectionsMax        = 10
)

// WhatsAppInteractiveLimits states WhatsApp's single-choice bounds in the same
// shape every other channel reports through its Descriptor.
//
// WhatsApp has no Descriptor yet — it is the channel still being migrated onto
// the adapter abstraction — so this stands in for one. It is built from the
// constants directly above so the numbers the workflow editor shows and the
// numbers Validate enforces cannot drift apart.
func WhatsAppInteractiveLimits() channel.InteractiveLimits {
	return channel.InteractiveLimits{
		MaxOptionsButtons: WhatsAppButtonMaxCount,
		MaxOptionsList:    WhatsAppListRowsMax,
		// The button title cap is the tighter of the two styles, so quoting it
		// keeps an author inside both.
		MaxLabelRunes: WhatsAppButtonTitleMaxLen,
		// Likewise the list row id, the tighter of 256 and 200.
		MaxPayloadBytes: WhatsAppListRowIDMaxLen,
		// List rows carry a description line; button replies do not. WhatsApp is
		// the only channel with the slot at all.
		SupportsOptionDescriptions: true,
	}
}

// overLimit reports whether s exceeds max WhatsApp-counted characters. Meta
// counts characters (code points), not bytes, so accented Portuguese and emoji
// are counted correctly.
func overLimit(s string, max int) bool {
	return utf8.RuneCountInString(s) > max
}

var (
	ErrWhatsAppRecipientRequired    = errors.New("whatsapp recipient is required")
	ErrWhatsAppBodyRequired         = errors.New("whatsapp message body is required")
	ErrWhatsAppClientDisabled       = errors.New("whatsapp client not configured")
	ErrWhatsAppTemplateNameRequired = errors.New("template name is required")
	ErrWhatsAppTemplateIDRequired   = errors.New("template ID is required")
	ErrWhatsAppWABAIDRequired       = errors.New("whatsapp business account ID is required")
)

type WhatsAppClient interface {
	SendTextMessage(ctx context.Context, input SendTextMessageInput) (*SendTextMessageOutput, error)
	SendAudioMessage(ctx context.Context, input SendAudioMessageInput) (*SendTextMessageOutput, error)
	SendAudioBytes(ctx context.Context, to string, audioData []byte, fileName string, contextMessageID string) (*SendTextMessageOutput, error)
	UploadAudio(ctx context.Context, audioData []byte, fileName string) (string, error)
	UploadImage(ctx context.Context, imageData []byte, fileName string, mimeType string) (string, error)
	UploadMedia(ctx context.Context, data []byte, fileName string, mimeType string) (string, error)
	DownloadMedia(ctx context.Context, mediaID string) ([]byte, string, error)
	SendImageMessage(ctx context.Context, input SendImageMessageInput) (*SendTextMessageOutput, error)
	SendVideoMessage(ctx context.Context, input SendVideoMessageInput) (*SendTextMessageOutput, error)
	SendDocumentMessage(ctx context.Context, input SendDocumentMessageInput) (*SendTextMessageOutput, error)
	SendStickerMessage(ctx context.Context, input SendStickerMessageInput) (*SendTextMessageOutput, error)
	SendButtonMessage(ctx context.Context, input SendButtonMessageInput) (*SendTextMessageOutput, error)
	SendListMessage(ctx context.Context, input SendListMessageInput) (*SendTextMessageOutput, error)

	SendCallPermissionRequest(ctx context.Context, input SendCallPermissionRequestInput) (*SendTextMessageOutput, error)

	SendTypingIndicator(ctx context.Context, messageID string) error
	MarkMessageAsRead(ctx context.Context, messageID string) error

	SendTemplateMessage(ctx context.Context, input SendTemplateMessageInput) (*SendTextMessageOutput, error)
	ListTemplates(ctx context.Context, input ListTemplatesInput) (*ListTemplatesOutput, error)
	GetTemplate(ctx context.Context, templateID string) (*Template, error)
	CreateTemplate(ctx context.Context, input CreateTemplateInput) (*CreateTemplateOutput, error)
	UpdateTemplate(ctx context.Context, templateID string, input UpdateTemplateInput) error
	DeleteTemplate(ctx context.Context, input DeleteTemplateInput) error

	UploadMediaForTemplate(ctx context.Context, input UploadMediaForTemplateInput) (string, error)
}

// WhatsAppBusinessProfile is the provider-neutral business profile (about, address,
// etc.) exchanged with the Cloud API. Mirrors the Meta/360dialog whatsapp_business_profile
// fields.
type WhatsAppBusinessProfile struct {
	About                string
	Address              string
	Description          string
	Email                string
	ProfilePictureURL    string
	ProfilePictureHandle string
	Websites             []string
	Vertical             string
}

// WhatsAppBusinessProfileClient reads and writes a channel's business profile. Like
// WhatsAppCallingClient it is a narrow capability implemented by the concrete Cloud API
// client (Meta at "{base}/{phone}/whatsapp_business_profile", 360dialog at the
// channel-scoped "{base}/whatsapp_business_profile"); obtain it by type-asserting a
// WhatsAppClient. Kept separate so message-only fakes need not implement it.
type WhatsAppBusinessProfileClient interface {
	GetBusinessProfile(ctx context.Context) (*WhatsAppBusinessProfile, error)
	UpdateBusinessProfile(ctx context.Context, profile WhatsAppBusinessProfile) error
}

// WhatsAppTemplateMediaClient reports how a channel wants a media (IMAGE/VIDEO/
// DOCUMENT) header asset referenced inside a create-template payload's
// example.header_handle. Meta's Graph endpoint wants a Resumable-Upload handle
// (so the URL must be uploaded first), whereas 360dialog's channel-scoped
// "/v1/configs/templates" endpoint wants the public media URL as-is and fetches
// it itself — it rejects an uploaded handle with 400 "it should be valid url
// address". It is a narrow capability implemented by the concrete Cloud API
// client; callers obtain it by type-asserting a WhatsAppClient. Kept separate so
// message-only fakes need not implement it (a fake that doesn't implement it
// keeps the historical upload-to-handle behaviour).
type WhatsAppTemplateMediaClient interface {
	// TemplateHeaderMediaWantsURL reports whether header_handle should carry the
	// public media URL verbatim (true, 360dialog) instead of an uploaded handle
	// (false, Meta).
	TemplateHeaderMediaWantsURL() bool
}

// WhatsAppCallingClient reads and toggles WhatsApp Business Calling for a channel.
// It is a narrow capability implemented by the concrete Cloud API client (Meta at
// "{base}/{phone}/settings", 360dialog at "{base}/calling/settings"); callers obtain
// it by type-asserting a WhatsAppClient. Kept separate from WhatsAppClient so the many
// message-only fakes need not implement it.
type WhatsAppCallingClient interface {
	// GetCallingStatus reports whether calling is ENABLED for the channel.
	GetCallingStatus(ctx context.Context) (bool, error)
	// SetCallingStatus enables or disables calling for the channel.
	SetCallingStatus(ctx context.Context, enabled bool) error
}

type SendTextMessageInput struct {
	To                string
	Body              string
	PreviewURL        bool
	FromPhoneNumberID string
	ContextMessageID  string
}

type SendAudioMessageInput struct {
	To               string
	AudioURL         string
	ContextMessageID string
}

func (in SendAudioMessageInput) Validate() error {
	if strings.TrimSpace(in.To) == "" {
		return ErrWhatsAppRecipientRequired
	}
	if strings.TrimSpace(in.AudioURL) == "" {
		return errors.New("audio URL is required")
	}
	return nil
}

func (in SendTextMessageInput) Validate() error {
	if strings.TrimSpace(in.To) == "" {
		return ErrWhatsAppRecipientRequired
	}
	if strings.TrimSpace(in.Body) == "" {
		return ErrWhatsAppBodyRequired
	}
	return nil
}

type SendTextMessageOutput struct {
	MessageID       string
	ContactWaID     string
	MessageStatus   string
	RequestPayload  []byte
	ResponsePayload []byte
	ResponseStatus  int
}

type SendCallPermissionRequestInput struct {
	To               string
	BodyText         string
	ContextMessageID string
}

func (in SendCallPermissionRequestInput) Validate() error {
	if strings.TrimSpace(in.To) == "" {
		return ErrWhatsAppRecipientRequired
	}
	if strings.TrimSpace(in.BodyText) == "" {
		return ErrWhatsAppBodyRequired
	}
	return nil
}

type SendImageMessageInput struct {
	To               string
	ImageID          string
	Link             string
	Caption          string
	ContextMessageID string
}

func (in SendImageMessageInput) Validate() error {
	if strings.TrimSpace(in.To) == "" {
		return ErrWhatsAppRecipientRequired
	}
	if strings.TrimSpace(in.ImageID) == "" && strings.TrimSpace(in.Link) == "" {
		return errors.New("image ID or link is required")
	}
	return nil
}

type SendVideoMessageInput struct {
	To               string
	VideoID          string
	Link             string
	Caption          string
	ContextMessageID string
}

func (in SendVideoMessageInput) Validate() error {
	if strings.TrimSpace(in.To) == "" {
		return ErrWhatsAppRecipientRequired
	}
	if strings.TrimSpace(in.VideoID) == "" && strings.TrimSpace(in.Link) == "" {
		return errors.New("video ID or link is required")
	}
	return nil
}

type SendDocumentMessageInput struct {
	To               string
	DocumentID       string
	Link             string
	Caption          string
	Filename         string
	ContextMessageID string
}

func (in SendDocumentMessageInput) Validate() error {
	if strings.TrimSpace(in.To) == "" {
		return ErrWhatsAppRecipientRequired
	}
	if strings.TrimSpace(in.DocumentID) == "" && strings.TrimSpace(in.Link) == "" {
		return errors.New("document ID or link is required")
	}
	return nil
}

type SendStickerMessageInput struct {
	To               string
	StickerID        string
	Link             string
	ContextMessageID string
}

func (in SendStickerMessageInput) Validate() error {
	if strings.TrimSpace(in.To) == "" {
		return ErrWhatsAppRecipientRequired
	}
	if strings.TrimSpace(in.StickerID) == "" && strings.TrimSpace(in.Link) == "" {
		return errors.New("sticker ID or link is required")
	}
	return nil
}

type ButtonType string

const (
	ButtonTypeReply    ButtonType = "reply"
	ButtonTypeCopyCode ButtonType = "copy_code"
)

type HeaderType string

const (
	HeaderTypeText  HeaderType = "text"
	HeaderTypeImage HeaderType = "image"
	HeaderTypeVideo HeaderType = "video"
)

type InteractiveButton struct {
	Type ButtonType

	ID    string
	Title string

	CopyCode string
}

type SendButtonMessageInput struct {
	To         string
	HeaderType HeaderType
	HeaderText string
	MediaID    string
	MediaLink  string
	BodyText   string
	FooterText string
	Buttons    []InteractiveButton
}

func (in SendButtonMessageInput) Validate() error {
	if strings.TrimSpace(in.To) == "" {
		return ErrWhatsAppRecipientRequired
	}
	if strings.TrimSpace(in.BodyText) == "" {
		return ErrWhatsAppBodyRequired
	}
	if overLimit(in.BodyText, WhatsAppButtonBodyMaxLen) {
		return fmt.Errorf("body text exceeds %d characters", WhatsAppButtonBodyMaxLen)
	}
	if overLimit(in.FooterText, WhatsAppFooterMaxLen) {
		return fmt.Errorf("footer text exceeds %d characters", WhatsAppFooterMaxLen)
	}
	if in.HeaderType == HeaderTypeText && overLimit(in.HeaderText, WhatsAppHeaderTextMaxLen) {
		return fmt.Errorf("header text exceeds %d characters", WhatsAppHeaderTextMaxLen)
	}
	if len(in.Buttons) == 0 {
		return errors.New("at least one button is required")
	}
	if len(in.Buttons) > WhatsAppButtonMaxCount {
		return fmt.Errorf("maximum %d buttons allowed", WhatsAppButtonMaxCount)
	}
	hasCopy := false
	hasReply := false
	seenTitles := make(map[string]struct{}, len(in.Buttons))
	for _, b := range in.Buttons {
		switch b.Type {
		case ButtonTypeCopyCode:
			hasCopy = true
			if strings.TrimSpace(b.CopyCode) == "" {
				return errors.New("copy_code button requires a non-empty code value")
			}
		default:
			hasReply = true
			title := strings.TrimSpace(b.Title)
			if title == "" {
				return errors.New("every reply button requires a non-empty title")
			}
			if overLimit(title, WhatsAppButtonTitleMaxLen) {
				return fmt.Errorf("button title %q exceeds %d characters", title, WhatsAppButtonTitleMaxLen)
			}
			if overLimit(b.ID, WhatsAppButtonIDMaxLen) {
				return fmt.Errorf("button id exceeds %d characters", WhatsAppButtonIDMaxLen)
			}
			// Meta requires reply button titles to be unique within a message.
			if _, dup := seenTitles[title]; dup {
				return fmt.Errorf("duplicate reply button title %q", title)
			}
			seenTitles[title] = struct{}{}
		}
	}
	if hasCopy && hasReply {
		return errors.New("cannot mix copy_code buttons with reply buttons in the same message")
	}
	if hasCopy && len(in.Buttons) > 1 {
		return errors.New("only one copy_code button is allowed per message")
	}
	return nil
}

// ListRow is a single selectable row of a WhatsApp interactive list message. The
// ID is the stable identifier echoed back in the reply (WhatsAppListReply.ID);
// routing keys off it, never the Title (which is user-facing and length-capped).
type ListRow struct {
	ID          string
	Title       string
	Description string
}

type ListSection struct {
	Title string
	Rows  []ListRow
}

// SendListMessageInput is a WhatsApp interactive "list" message: a body with a
// menu button that opens up to 10 selectable rows grouped into sections. It is
// the >3-option counterpart to SendButtonMessageInput (reply buttons, max 3).
type SendListMessageInput struct {
	To         string
	HeaderText string
	BodyText   string
	FooterText string
	ButtonText string
	Sections   []ListSection
}

func (in SendListMessageInput) Validate() error {
	if strings.TrimSpace(in.To) == "" {
		return ErrWhatsAppRecipientRequired
	}
	if strings.TrimSpace(in.BodyText) == "" {
		return ErrWhatsAppBodyRequired
	}
	if overLimit(in.BodyText, WhatsAppListBodyMaxLen) {
		return fmt.Errorf("body text exceeds %d characters", WhatsAppListBodyMaxLen)
	}
	if overLimit(in.HeaderText, WhatsAppHeaderTextMaxLen) {
		return fmt.Errorf("header text exceeds %d characters", WhatsAppHeaderTextMaxLen)
	}
	if overLimit(in.FooterText, WhatsAppFooterMaxLen) {
		return fmt.Errorf("footer text exceeds %d characters", WhatsAppFooterMaxLen)
	}
	if strings.TrimSpace(in.ButtonText) == "" {
		return errors.New("list button text is required")
	}
	if overLimit(in.ButtonText, WhatsAppListButtonMaxLen) {
		return fmt.Errorf("list button text exceeds %d characters", WhatsAppListButtonMaxLen)
	}
	if len(in.Sections) > WhatsAppListSectionsMax {
		return fmt.Errorf("maximum %d list sections allowed", WhatsAppListSectionsMax)
	}
	rowCount := 0
	seen := make(map[string]struct{})
	for _, section := range in.Sections {
		if overLimit(section.Title, WhatsAppListSectionTitleMaxLen) {
			return fmt.Errorf("list section title %q exceeds %d characters", section.Title, WhatsAppListSectionTitleMaxLen)
		}
		for _, row := range section.Rows {
			id := strings.TrimSpace(row.ID)
			if id == "" {
				return errors.New("every list row requires a non-empty id")
			}
			if overLimit(id, WhatsAppListRowIDMaxLen) {
				return fmt.Errorf("list row id %q exceeds %d characters", id, WhatsAppListRowIDMaxLen)
			}
			title := strings.TrimSpace(row.Title)
			if title == "" {
				return errors.New("every list row requires a non-empty title")
			}
			if overLimit(title, WhatsAppListRowTitleMaxLen) {
				return fmt.Errorf("list row title %q exceeds %d characters", title, WhatsAppListRowTitleMaxLen)
			}
			if overLimit(row.Description, WhatsAppListRowDescMaxLen) {
				return fmt.Errorf("list row description exceeds %d characters", WhatsAppListRowDescMaxLen)
			}
			if _, dup := seen[id]; dup {
				return fmt.Errorf("duplicate list row id %q", id)
			}
			seen[id] = struct{}{}
			rowCount++
		}
	}
	if rowCount == 0 {
		return errors.New("at least one list row is required")
	}
	if rowCount > WhatsAppListRowsMax {
		return fmt.Errorf("maximum %d list rows allowed", WhatsAppListRowsMax)
	}
	return nil
}

type SendTemplateMessageInput struct {
	To                     string
	TemplateName           string
	Language               string
	Parameters             []string
	ParameterNames         []string
	IsNamedParameterFormat bool
	HeaderType             string
	HeaderMediaURL         string
	HeaderMediaID          string
	HeaderFilename         string
	HeaderTextParams       []string
	FromPhoneNumberID      string
}

func (in SendTemplateMessageInput) Validate() error {
	if strings.TrimSpace(in.To) == "" {
		return ErrWhatsAppRecipientRequired
	}
	if strings.TrimSpace(in.TemplateName) == "" {
		return ErrWhatsAppTemplateNameRequired
	}
	return nil
}

type Template struct {
	ID              string
	Name            string
	Status          string
	Category        string
	Language        string
	ParameterFormat string
	Components      []TemplateComponent
}

type TemplateComponent struct {
	Type    string
	Format  string
	Text    string
	Buttons []TemplateButton
	Example *TemplateExample
}

type TemplateButton struct {
	Type        string `json:"type"`
	Text        string `json:"text"`
	URL         string `json:"url,omitempty"`
	PhoneNumber string `json:"phone_number,omitempty"`
	Example     string `json:"example,omitempty"`
}

type TemplateExample struct {
	HeaderText      []string            `json:"header_text,omitempty"`
	HeaderHandle    []string            `json:"header_handle,omitempty"`
	BodyText        [][]string          `json:"body_text,omitempty"`
	BodyTextNamed   []NamedParamExample `json:"body_text_named_params,omitempty"`
	HeaderTextNamed []NamedParamExample `json:"header_text_named_params,omitempty"`
}

type NamedParamExample struct {
	ParamName string `json:"param_name"`
	Example   string `json:"example"`
}

type ListTemplatesInput struct {
	Status string
	Limit  int
	After  string
}

type ListTemplatesOutput struct {
	Templates []Template
	HasMore   bool
	NextAfter string
}

type CreateTemplateInput struct {
	Name            string
	Language        string
	Category        string
	ParameterFormat string
	Components      []TemplateComponent
}

func (in CreateTemplateInput) Validate() error {
	if strings.TrimSpace(in.Name) == "" {
		return ErrWhatsAppTemplateNameRequired
	}
	return nil
}

type CreateTemplateOutput struct {
	ID             string
	Status         string
	Category       string
	RejectedReason string
}

type UploadMediaForTemplateInput struct {
	URL      string
	Data     []byte
	FileName string
	MimeType string
}

type UpdateTemplateInput struct {
	Category   string
	Components []TemplateComponent
}

type DeleteTemplateInput struct {
	Name       string
	TemplateID string
}

func (in DeleteTemplateInput) Validate() error {
	if strings.TrimSpace(in.Name) == "" {
		return ErrWhatsAppTemplateNameRequired
	}
	return nil
}
