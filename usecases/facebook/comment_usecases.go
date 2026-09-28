package facebook

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"vozko/domain/conversation"
	fbdomain "vozko/domain/facebook"
	"vozko/domain/privatereply"
	"vozko/domain/shared"
	cauc "vozko/usecases/commentautomation"
	"vozko/usecases/metachannel"
)

const (
	defaultCommentPageSize = 50
	maxCommentPageSize     = 100
)

type CommentForgetter interface {
	Forget(ctx context.Context, fbCommentID string)
}

type CommentDeps struct {
	Pages          fbdomain.PageRepository
	Comments       fbdomain.CommentRepository
	Service        fbdomain.CommentService
	Messaging      fbdomain.MessagingService
	Contacts       fbdomain.ContactRepository
	Conversations  fbdomain.ConversationRepository
	Transcript     *metachannel.Transcript
	PrivateReplies *cauc.PrivateReplySender
	ReplyRecords   privatereply.Repository
	Audience       CommentForgetter
}

type CommentUseCases struct {
	d   CommentDeps
	now func() time.Time
}

func NewCommentUseCases(d CommentDeps) *CommentUseCases {
	return &CommentUseCases{d: d, now: func() time.Time { return time.Now().UTC() }}
}

type CommentView struct {
	Remote       *fbdomain.RemoteComment
	PrivateReply *privatereply.Record
	Deadline     *time.Time
	ContactID    *string
}

type PrivateReplyOutcome struct {
	ConversationID string
	MessageID      string
}

func (uc *CommentUseCases) List(ctx context.Context, workspaceID, pageID, fbPostID string, filter fbdomain.CommentFilter, limit int, after string) (*fbdomain.Paged[*CommentView], error) {
	page, err := pageWith(ctx, uc.d.Pages, workspaceID, pageID, fbdomain.CapReadPosts)
	if err != nil {
		return nil, err
	}
	if !page.OwnsPostID(fbPostID) {
		return nil, fbdomain.ErrPostNotFound
	}
	if limit <= 0 || limit > maxCommentPageSize {
		limit = defaultCommentPageSize
	}
	remote, err := uc.d.Service.List(ctx, page.PageToken, fbPostID, filter, limit, after)
	if err != nil {
		return nil, err
	}

	mirror := make([]*fbdomain.Comment, 0, len(remote.Items))
	ids := make([]string, 0, len(remote.Items))
	for _, c := range remote.Items {
		mirror = append(mirror, fbdomain.CommentFromRemote(page, fbPostID, c))
		ids = append(ids, c.FBCommentID)
	}
	if err := uc.d.Comments.UpsertMany(ctx, mirror); err != nil {
		log.Printf("[facebook] comment mirror failed post=%s: %v", fbPostID, err)
	}
	replies, err := uc.d.ReplyRecords.FindMany(ctx, shared.EntryTypeFacebook, ids)
	if err != nil {
		return nil, err
	}
	contacts, err := uc.d.Comments.ContactsFor(ctx, ids)
	if err != nil {
		return nil, err
	}

	out := &fbdomain.Paged[*CommentView]{Items: make([]*CommentView, 0, len(remote.Items)), NextCursor: remote.NextCursor, HasNext: remote.HasNext}
	for _, c := range remote.Items {
		view := &CommentView{Remote: c, PrivateReply: replies[c.FBCommentID], Deadline: privatereply.Deadline(c.CreatedTime)}
		if contactID, ok := contacts[c.FBCommentID]; ok {
			view.ContactID = &contactID
		}
		out.Items = append(out.Items, view)
	}
	return out, nil
}

func (uc *CommentUseCases) CommentAsPage(ctx context.Context, workspaceID, pageID, fbPostID, message string) (string, error) {
	page, err := pageWith(ctx, uc.d.Pages, workspaceID, pageID, fbdomain.CapComment)
	if err != nil {
		return "", err
	}
	if !page.OwnsPostID(fbPostID) {
		return "", fbdomain.ErrPostNotFound
	}
	return uc.publish(ctx, page, fbPostID, fbPostID, nil, message)
}

func (uc *CommentUseCases) Reply(ctx context.Context, workspaceID, pageID, fbCommentID, message string) (string, error) {
	page, parent, err := uc.comment(ctx, workspaceID, pageID, fbCommentID, fbdomain.CapComment)
	if err != nil {
		return "", err
	}
	return uc.publish(ctx, page, fbCommentID, parent.FBPostID, &parent.FBCommentID, message)
}

func (uc *CommentUseCases) publish(ctx context.Context, page *fbdomain.Page, target, fbPostID string, parentID *string, message string) (string, error) {
	message = strings.TrimSpace(message)
	if message == "" {
		return "", fbdomain.ErrCommentEmpty
	}
	id, err := uc.d.Service.Create(ctx, page.PageToken, target, message)
	if err != nil {
		return "", err
	}
	now, from := uc.now(), page.FBPageID
	mirror := &fbdomain.Comment{
		WorkspaceID: page.WorkspaceID, PageID: page.ID, FBCommentID: id, FBPostID: fbPostID, ParentFBCommentID: parentID,
		FromID: &from, FromName: page.Name, FromIsPage: true, IsOurs: true, Message: message, CreatedTime: &now,
	}
	if err := uc.d.Comments.UpsertMany(ctx, []*fbdomain.Comment{mirror}); err != nil {
		log.Printf("[facebook] own comment %s not mirrored: %v", id, err)
	}
	return id, nil
}

func (uc *CommentUseCases) Edit(ctx context.Context, workspaceID, pageID, fbCommentID, message string) error {
	page, stored, err := uc.comment(ctx, workspaceID, pageID, fbCommentID, fbdomain.CapComment)
	if err != nil {
		return err
	}
	if !stored.IsOurs {
		return fbdomain.ErrCommentNotOurs
	}
	message = strings.TrimSpace(message)
	if message == "" {
		return fbdomain.ErrCommentEmpty
	}
	if err := uc.d.Service.Edit(ctx, page.PageToken, fbCommentID, message); err != nil {
		return err
	}
	return uc.d.Comments.Edit(ctx, fbCommentID, message, uc.now())
}

func (uc *CommentUseCases) SetHidden(ctx context.Context, workspaceID, pageID, fbCommentID string, hidden bool) error {
	page, _, err := uc.comment(ctx, workspaceID, pageID, fbCommentID, fbdomain.CapModerate)
	if err != nil {
		return err
	}
	if err := uc.d.Service.SetHidden(ctx, page.PageToken, fbCommentID, hidden); err != nil {
		return err
	}
	return uc.d.Comments.SetHidden(ctx, fbCommentID, hidden)
}

func (uc *CommentUseCases) SetLiked(ctx context.Context, workspaceID, pageID, fbCommentID string, liked bool) error {
	page, _, err := uc.comment(ctx, workspaceID, pageID, fbCommentID, fbdomain.CapComment)
	if err != nil {
		return err
	}
	if err := uc.d.Service.SetLiked(ctx, page.PageToken, fbCommentID, liked); err != nil {
		return err
	}
	return uc.d.Comments.SetLiked(ctx, fbCommentID, liked)
}

func (uc *CommentUseCases) Delete(ctx context.Context, workspaceID, pageID, fbCommentID string) error {
	page, _, err := uc.comment(ctx, workspaceID, pageID, fbCommentID, fbdomain.CapModerate)
	if err != nil {
		return err
	}
	if err := uc.d.Service.Delete(ctx, page.PageToken, fbCommentID); err != nil {
		return err
	}
	if err := uc.d.Comments.MarkRemoved(ctx, fbCommentID, uc.now()); err != nil {
		log.Printf("[facebook] deleted comment %s not tombstoned: %v", fbCommentID, err)
	}
	uc.d.Audience.Forget(ctx, fbCommentID)
	return nil
}

func (uc *CommentUseCases) SendPrivateReply(ctx context.Context, workspaceID, pageID, fbCommentID string, sentBy conversation.SentBy, text string) (*PrivateReplyOutcome, error) {
	page, stored, err := uc.comment(ctx, workspaceID, pageID, fbCommentID, fbdomain.CapMessaging)
	if err != nil {
		return nil, err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, fbdomain.ErrCommentEmpty
	}
	if fbdomain.Descriptor(false).Capabilities.TextTooLong(text) {
		return nil, fbdomain.ErrTextTooLong
	}

	delivery, err := uc.d.PrivateReplies.Send(ctx, shared.EntryTypeFacebook, page.ID, fbCommentID, uc.commentTime(ctx, page, stored),
		func(ctx context.Context) (*cauc.Delivery, error) {
			result, err := uc.d.Messaging.Send(ctx, page.FBPageID, page.PageToken, fbdomain.OutboundMessage{
				Recipient: fbdomain.Recipient{CommentID: fbCommentID}, Text: text, Metadata: fbdomain.OutboundMetadata(true),
			})
			if err != nil {
				return nil, err
			}
			return &cauc.Delivery{RecipientRef: result.RecipientID, MessageID: result.MessageID}, nil
		})
	if err != nil {
		return nil, err
	}
	return uc.recordPrivateReply(ctx, page, fbCommentID, delivery, sentBy, text)
}

func (uc *CommentUseCases) recordPrivateReply(ctx context.Context, page *fbdomain.Page, fbCommentID string, delivery *cauc.Delivery, sentBy conversation.SentBy, text string) (*PrivateReplyOutcome, error) {
	if delivery.RecipientRef == "" {
		return nil, fmt.Errorf("facebook: private reply to %s was sent but Meta returned no recipient", fbCommentID)
	}
	contact, err := uc.d.Contacts.FindOrCreate(ctx, page.WorkspaceID, page.ID, delivery.RecipientRef)
	if err != nil {
		return nil, err
	}
	conv, err := uc.d.Conversations.FindOrCreate(ctx, page.WorkspaceID, page.ID, contact.ID)
	if err != nil {
		return nil, err
	}
	if err := uc.d.Comments.LinkContact(ctx, fbCommentID, contact.ID); err != nil {
		log.Printf("[facebook] comment %s not linked to contact %s: %v", fbCommentID, contact.ID, err)
	}
	now := uc.now()
	if err := uc.d.Transcript.Record(ctx, conv.ID, sentBy, metachannel.Party{From: page.FBPageID, To: contact.PSID}, metachannel.HistoryInput{
		MessageType: conversation.MessageTypeOperator, ProviderMessageID: delivery.MessageID, Text: text, Timestamp: now,
		Metadata: metachannel.MergeMetadata(nil, map[string]any{MetadataPrefix + "_private_reply_to": fbCommentID}),
	}); err != nil {
		return nil, err
	}
	if err := uc.d.Conversations.RecordOutbound(ctx, conv.ID, now); err != nil {
		log.Printf("[facebook] private reply outbound not recorded conversation=%s: %v", conv.ID, err)
	}
	return &PrivateReplyOutcome{ConversationID: conv.ID, MessageID: delivery.MessageID}, nil
}

func (uc *CommentUseCases) commentTime(ctx context.Context, page *fbdomain.Page, stored *fbdomain.Comment) *time.Time {
	if stored.CreatedTime != nil {
		return stored.CreatedTime
	}
	remote, err := uc.d.Service.Get(ctx, page.PageToken, stored.FBCommentID)
	if err != nil {
		log.Printf("[facebook] comment %s time unavailable for the private reply deadline: %v", stored.FBCommentID, err)
		return nil
	}
	return remote.CreatedTime
}

func (uc *CommentUseCases) comment(ctx context.Context, workspaceID, pageID, fbCommentID string, capability fbdomain.Capability) (*fbdomain.Page, *fbdomain.Comment, error) {
	page, err := pageWith(ctx, uc.d.Pages, workspaceID, pageID, capability)
	if err != nil {
		return nil, nil, err
	}
	stored, err := uc.d.Comments.FindByFBCommentID(ctx, fbCommentID)
	if err != nil {
		return nil, nil, err
	}
	if stored.PageID != page.ID || stored.RemovedAt != nil {
		return nil, nil, fbdomain.ErrCommentNotFound
	}
	return page, stored, nil
}
