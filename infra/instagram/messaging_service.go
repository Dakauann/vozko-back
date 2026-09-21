package instagram

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"vozko/domain/cache"
	igdomain "vozko/domain/instagram"
	"vozko/infra/meta"
)

const (
	sendTextPerSecond      = 100
	sendMediaPerSecond     = 10
	conversationsPerSecond = 2
)

type messagingService struct {
	client *meta.Client

	textLimiter  cache.RateLimiter
	mediaLimiter cache.RateLimiter
	convLimiter  cache.RateLimiter
}

type MessagingConfig struct {
	GraphVersion       string
	AppSecret          string
	HTTPClient         *http.Client
	RateLimiterFactory cache.RateLimiterFactory
}

func NewMessagingService(cfg MessagingConfig) (igdomain.MessagingService, error) {
	client, err := meta.NewClient(meta.Config{
		Host:       GraphHost,
		APIVersion: graphVersionOr(cfg.GraphVersion),
		AppSecret:  cfg.AppSecret,
		HTTPClient: cfg.HTTPClient,
	})
	if err != nil {
		return nil, err
	}

	s := &messagingService{client: client}
	if cfg.RateLimiterFactory != nil {
		s.textLimiter = cfg.RateLimiterFactory("ig_send_text", sendTextPerSecond, time.Second)
		s.mediaLimiter = cfg.RateLimiterFactory("ig_send_media", sendMediaPerSecond, time.Second)
		s.convLimiter = cfg.RateLimiterFactory("ig_conversations", conversationsPerSecond, time.Second)
	}
	return s, nil
}

type sendResponse struct {
	RecipientID string `json:"recipient_id"`
	MessageID   string `json:"message_id"`
}

type textBody struct {
	Recipient recipient   `json:"recipient"`
	Message   textMessage `json:"message"`
	ReplyTo   *replyTo    `json:"reply_to,omitempty"`
}

type recipient struct {
	ID        string `json:"id,omitempty"`
	CommentID string `json:"comment_id,omitempty"`
}

type textMessage struct {
	Text         string       `json:"text"`
	QuickReplies []quickReply `json:"quick_replies,omitempty"`
}

type quickReply struct {
	ContentType string `json:"content_type"`
	Title       string `json:"title"`
	Payload     string `json:"payload"`
}

type replyTo struct {
	MID string `json:"mid"`
}

type mediaBody struct {
	Recipient recipient    `json:"recipient"`
	Message   mediaMessage `json:"message"`
	ReplyTo   *replyTo     `json:"reply_to,omitempty"`
}

type mediaMessage struct {
	Attachment attachment `json:"attachment"`
}

type attachment struct {
	Type    string            `json:"type"`
	Payload attachmentPayload `json:"payload"`
}

type attachmentPayload struct {
	URL string `json:"url"`
}

type reactionBody struct {
	Recipient    recipient        `json:"recipient"`
	SenderAction string           `json:"sender_action"`
	Payload      *reactionPayload `json:"payload,omitempty"`
}

type reactionPayload struct {
	MessageID string `json:"message_id"`
	Reaction  string `json:"reaction,omitempty"`
}

type senderActionBody struct {
	Recipient    recipient `json:"recipient"`
	SenderAction string    `json:"sender_action"`
}

func (s *messagingService) SendText(ctx context.Context, igUserID, token string, in igdomain.SendTextInput) (*igdomain.SendResult, error) {
	if len(in.Text) > igdomain.MaxTextBytes {
		return nil, igdomain.ErrTextTooLong
	}
	if err := s.allow(s.textLimiter, igUserID); err != nil {
		return nil, err
	}

	body := textBody{
		Recipient: recipient{ID: in.RecipientIGSID},
		Message:   textMessage{Text: in.Text, QuickReplies: quickRepliesFor(in.QuickReplies)},
	}
	if in.ReplyToMID != "" {
		body.ReplyTo = &replyTo{MID: in.ReplyToMID}
	}

	var out sendResponse
	if err := s.client.Do(ctx, meta.Request{
		Method: http.MethodPost,
		Path:   "/" + igUserID + "/messages",
		Token:  token,
		Body:   body,
	}, &out); err != nil {
		return nil, err
	}
	return &igdomain.SendResult{RecipientID: out.RecipientID, MessageID: out.MessageID}, nil
}

func (s *messagingService) SendMedia(ctx context.Context, igUserID, token string, in igdomain.SendMediaInput) (*igdomain.SendResult, error) {
	kind, err := attachmentTypeFor(in.Kind)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.URL) == "" {
		return nil, fmt.Errorf("instagram: media send requires a publicly reachable URL")
	}
	if err := s.allow(s.mediaLimiter, igUserID); err != nil {
		return nil, err
	}

	body := mediaBody{
		Recipient: recipient{ID: in.RecipientIGSID},
		Message: mediaMessage{
			Attachment: attachment{Type: kind, Payload: attachmentPayload{URL: in.URL}},
		},
	}
	if in.ReplyToMID != "" {
		body.ReplyTo = &replyTo{MID: in.ReplyToMID}
	}

	var out sendResponse
	if err := s.client.Do(ctx, meta.Request{
		Method: http.MethodPost,
		Path:   "/" + igUserID + "/messages",
		Token:  token,
		Body:   body,
	}, &out); err != nil {
		return nil, err
	}
	return &igdomain.SendResult{RecipientID: out.RecipientID, MessageID: out.MessageID}, nil
}

func (s *messagingService) SendReaction(ctx context.Context, igUserID, token, recipientIGSID, targetMID, reaction string) error {
	if err := s.allow(s.textLimiter, igUserID); err != nil {
		return err
	}
	body := reactionBody{
		Recipient:    recipient{ID: recipientIGSID},
		SenderAction: "react",
		Payload:      &reactionPayload{MessageID: targetMID, Reaction: reaction},
	}
	return s.client.Do(ctx, meta.Request{
		Method: http.MethodPost,
		Path:   "/" + igUserID + "/messages",
		Token:  token,
		Body:   body,
	}, nil)
}

func (s *messagingService) RemoveReaction(ctx context.Context, igUserID, token, recipientIGSID, targetMID string) error {
	if err := s.allow(s.textLimiter, igUserID); err != nil {
		return err
	}
	body := reactionBody{
		Recipient:    recipient{ID: recipientIGSID},
		SenderAction: "unreact",
		Payload:      &reactionPayload{MessageID: targetMID},
	}
	return s.client.Do(ctx, meta.Request{
		Method: http.MethodPost,
		Path:   "/" + igUserID + "/messages",
		Token:  token,
		Body:   body,
	}, nil)
}

func (s *messagingService) SendTyping(ctx context.Context, igUserID, token, recipientIGSID string, on bool) error {
	action := "typing_off"
	if on {
		action = "typing_on"
	}
	return s.senderAction(ctx, igUserID, token, recipientIGSID, action)
}

func (s *messagingService) MarkSeen(ctx context.Context, igUserID, token, recipientIGSID string) error {
	return s.senderAction(ctx, igUserID, token, recipientIGSID, "mark_seen")
}

func (s *messagingService) senderAction(ctx context.Context, igUserID, token, recipientIGSID, action string) error {
	if err := s.allow(s.textLimiter, igUserID); err != nil {
		return err
	}
	return s.client.Do(ctx, meta.Request{
		Method: http.MethodPost,
		Path:   "/" + igUserID + "/messages",
		Token:  token,
		Body: senderActionBody{
			Recipient:    recipient{ID: recipientIGSID},
			SenderAction: action,
		},
	}, nil)
}

func (s *messagingService) SendPrivateReply(ctx context.Context, igUserID, token, igCommentID, text string) (*igdomain.SendResult, error) {
	if len(text) > igdomain.MaxTextBytes {
		return nil, igdomain.ErrTextTooLong
	}
	if err := s.allow(s.textLimiter, igUserID); err != nil {
		return nil, err
	}

	body := textBody{
		Recipient: recipient{CommentID: igCommentID},
		Message:   textMessage{Text: text},
	}

	var out sendResponse
	if err := s.client.Do(ctx, meta.Request{
		Method: http.MethodPost,
		Path:   "/" + igUserID + "/messages",
		Token:  token,
		Body:   body,
	}, &out); err != nil {
		return nil, err
	}
	return &igdomain.SendResult{RecipientID: out.RecipientID, MessageID: out.MessageID}, nil
}

type contactProfileResponse struct {
	Name                 string `json:"name"`
	Username             string `json:"username"`
	ProfilePic           string `json:"profile_pic"`
	IsVerifiedUser       bool   `json:"is_verified_user"`
	FollowerCount        int    `json:"follower_count"`
	IsUserFollowBusiness bool   `json:"is_user_follow_business"`
	IsBusinessFollowUser bool   `json:"is_business_follow_user"`
}

func (s *messagingService) GetContactProfile(ctx context.Context, token, igsid string) (*igdomain.ContactProfileResult, error) {
	q := url.Values{}
	q.Set("fields", "name,username,profile_pic,is_verified_user,follower_count,is_user_follow_business,is_business_follow_user")

	var out contactProfileResponse
	if err := s.client.Do(ctx, meta.Request{
		Method: http.MethodGet,
		Path:   "/" + igsid,
		Token:  token,
		Query:  q,
	}, &out); err != nil {
		return nil, err
	}
	return &igdomain.ContactProfileResult{
		Username:             out.Username,
		Name:                 out.Name,
		ProfilePictureURL:    out.ProfilePic,
		IsVerifiedUser:       out.IsVerifiedUser,
		FollowerCount:        out.FollowerCount,
		IsUserFollowBusiness: out.IsUserFollowBusiness,
		IsBusinessFollowUser: out.IsBusinessFollowUser,
	}, nil
}

func (s *messagingService) GetConversations(ctx context.Context, igUserID, token string, limit int) error {
	if err := s.allow(s.convLimiter, igUserID); err != nil {
		return err
	}
	if limit <= 0 {
		limit = 1
	}
	q := url.Values{}
	q.Set("platform", "instagram")
	q.Set("limit", fmt.Sprint(limit))

	return s.client.Do(ctx, meta.Request{
		Method: http.MethodGet,
		Path:   "/" + igUserID + "/conversations",
		Token:  token,
		Query:  q,
	}, nil)
}

func (s *messagingService) allow(limiter cache.RateLimiter, accountID string) error {
	if limiter == nil {
		return nil
	}
	allowed, retryAfter, err := limiter.Allow(accountID)
	if err != nil {
		return nil
	}
	if !allowed {
		return &meta.Error{
			Code:        meta.CodeMessagingRate,
			IsTransient: true,
			Message: fmt.Sprintf("local rate limit for instagram account %s; retry in %s",
				accountID, retryAfter),
		}
	}
	return nil
}

func attachmentTypeFor(kind string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "image":
		return "image", nil
	case "video":
		return "video", nil
	case "audio":
		return "audio", nil
	case "document", "file":
		return "file", nil
	}
	return "", fmt.Errorf("instagram: unsupported media kind %q", kind)
}

func quickRepliesFor(options []igdomain.QuickReplyOption) []quickReply {
	if len(options) == 0 {
		return nil
	}
	out := make([]quickReply, 0, len(options))
	for _, o := range options {
		if len(out) >= igdomain.MaxQuickReplies {
			break
		}
		out = append(out, quickReply{
			ContentType: "text",
			Title:       truncateRunes(o.Title, igdomain.MaxQuickReplyTitleRunes),
			Payload:     o.Payload,
		})
	}
	return out
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}
