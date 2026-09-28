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
	"vozko/infra/meta/sendapi"
)

const (
	bucketText          = "ig_send_text"
	bucketMedia         = "ig_send_media"
	bucketConversations = "ig_conversations"
)

type messagingService struct {
	client   *meta.Client
	throttle *meta.Throttle
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
		APIVersion: meta.VersionOr(cfg.GraphVersion),
		AppSecret:  cfg.AppSecret,
		HTTPClient: cfg.HTTPClient,
	})
	if err != nil {
		return nil, err
	}
	throttle, err := meta.NewThrottle(cfg.RateLimiterFactory,
		meta.Bucket{Name: bucketText, Max: 100, Window: time.Second},
		meta.Bucket{Name: bucketMedia, Max: 10, Window: time.Second},
		meta.Bucket{Name: bucketConversations, Max: 2, Window: time.Second},
	)
	if err != nil {
		return nil, err
	}
	return &messagingService{client: client, throttle: throttle}, nil
}

func (s *messagingService) send(ctx context.Context, bucket, igUserID, token string, env sendapi.Envelope) (*sendapi.Response, error) {
	if err := s.throttle.Allow(bucket, igUserID); err != nil {
		return nil, err
	}
	return sendapi.Send(ctx, s.client, "/"+igUserID+"/messages", token, env)
}

func (s *messagingService) SendText(ctx context.Context, igUserID, token string, in igdomain.SendTextInput) (*igdomain.SendResult, error) {
	if len(in.Text) > igdomain.MaxTextBytes {
		return nil, igdomain.ErrTextTooLong
	}
	env := sendapi.Text(sendapi.ToUser(in.RecipientIGSID), in.Text).
		WithQuickReplies(sendapi.QuickReplies(quickReplyOptions(in.QuickReplies),
			igdomain.MaxQuickReplies, igdomain.MaxQuickReplyTitleRunes)).
		ReplyingTo(in.ReplyToMID)
	out, err := s.send(ctx, bucketText, igUserID, token, env)
	if err != nil {
		return nil, err
	}
	return &igdomain.SendResult{RecipientID: out.RecipientID, MessageID: out.MessageID}, nil
}

func (s *messagingService) SendMedia(ctx context.Context, igUserID, token string, in igdomain.SendMediaInput) (*igdomain.SendResult, error) {
	kind, err := sendapi.AttachmentTypeFor(in.Kind)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.URL) == "" {
		return nil, fmt.Errorf("instagram: media send requires a publicly reachable URL")
	}
	env := sendapi.Media(sendapi.ToUser(in.RecipientIGSID), kind, in.URL).ReplyingTo(in.ReplyToMID)
	out, err := s.send(ctx, bucketMedia, igUserID, token, env)
	if err != nil {
		return nil, err
	}
	return &igdomain.SendResult{RecipientID: out.RecipientID, MessageID: out.MessageID}, nil
}

func (s *messagingService) SendReaction(ctx context.Context, igUserID, token, recipientIGSID, targetMID, reaction string) error {
	_, err := s.send(ctx, bucketText, igUserID, token, sendapi.React(sendapi.ToUser(recipientIGSID), targetMID, reaction))
	return err
}

func (s *messagingService) RemoveReaction(ctx context.Context, igUserID, token, recipientIGSID, targetMID string) error {
	_, err := s.send(ctx, bucketText, igUserID, token, sendapi.Unreact(sendapi.ToUser(recipientIGSID), targetMID))
	return err
}

func (s *messagingService) SendTyping(ctx context.Context, igUserID, token, recipientIGSID string, on bool) error {
	action := sendapi.ActionTypingOff
	if on {
		action = sendapi.ActionTypingOn
	}
	_, err := s.send(ctx, bucketText, igUserID, token, sendapi.Action(sendapi.ToUser(recipientIGSID), action))
	return err
}

func (s *messagingService) MarkSeen(ctx context.Context, igUserID, token, recipientIGSID string) error {
	_, err := s.send(ctx, bucketText, igUserID, token, sendapi.Action(sendapi.ToUser(recipientIGSID), sendapi.ActionMarkSeen))
	return err
}

func (s *messagingService) SendPrivateReply(ctx context.Context, igUserID, token, igCommentID, text string) (*igdomain.SendResult, error) {
	if len(text) > igdomain.MaxTextBytes {
		return nil, igdomain.ErrTextTooLong
	}
	out, err := s.send(ctx, bucketText, igUserID, token, sendapi.Text(sendapi.ToComment(igCommentID), text))
	if err != nil {
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

type messageSenderResponse struct {
	From struct {
		ID       string `json:"id"`
		Username string `json:"username"`
	} `json:"from"`
}

func (s *messagingService) GetMessageSender(ctx context.Context, token, mid string) (*igdomain.MessageSender, error) {
	q := url.Values{}
	q.Set("fields", "from")

	var out messageSenderResponse
	if err := s.client.Do(ctx, meta.Request{
		Method: http.MethodGet,
		Path:   "/" + url.PathEscape(mid),
		Token:  token,
		Query:  q,
	}, &out); err != nil {
		return nil, err
	}
	return &igdomain.MessageSender{ID: out.From.ID, Username: out.From.Username}, nil
}

func (s *messagingService) GetConversations(ctx context.Context, igUserID, token string, limit int) error {
	if err := s.throttle.Allow(bucketConversations, igUserID); err != nil {
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

func quickReplyOptions(options []igdomain.QuickReplyOption) []sendapi.Option {
	out := make([]sendapi.Option, 0, len(options))
	for _, o := range options {
		out = append(out, sendapi.Option{Title: o.Title, Payload: o.Payload})
	}
	return out
}
