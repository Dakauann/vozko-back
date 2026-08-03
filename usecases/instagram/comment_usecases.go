package instagram

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"vozko/domain/conversation"
	igdomain "vozko/domain/instagram"
	"vozko/domain/shared"
	"vozko/infra/meta"
)

// ListCommentsInput is one page of comments on a post.
type ListCommentsInput struct {
	WorkspaceID string
	AccountID   string
	IGMediaID   string
	Limit       int
	After       string
}

// ListCommentsUseCase lists comments with their replies expanded.
//
// Graph caps this edge at 50 per query, returns only top-level comments unless
// `replies` is field-expanded, and cannot be filtered by timestamp, so this is a
// live paginated read, and the local mirror is kept current from webhooks rather
// than by polling.
type ListCommentsUseCase struct {
	accountResolver
	comments     igdomain.CommentService
	commentsRepo igdomain.CommentRepository
}

func NewListCommentsUseCase(
	accounts igdomain.AccountRepository,
	commentSvc igdomain.CommentService,
	commentsRepo igdomain.CommentRepository,
) *ListCommentsUseCase {
	return &ListCommentsUseCase{
		accountResolver: accountResolver{accounts: accounts},
		comments:        commentSvc,
		commentsRepo:    commentsRepo,
	}
}

func (uc *ListCommentsUseCase) Execute(ctx context.Context, in ListCommentsInput) (*igdomain.Page[*igdomain.RemoteComment], error) {
	account, err := uc.resolve(ctx, in.WorkspaceID, in.AccountID)
	if err != nil {
		return nil, err
	}
	if !account.CanManageComments() {
		return nil, fmt.Errorf("instagram account %s cannot read comments (missing %s)",
			account.Username, igdomain.ScopeManageComments)
	}

	page, err := uc.comments.ListComments(ctx, account.AccessToken, in.IGMediaID, in.Limit, in.After)
	if err != nil {
		log.Printf("[instagram] list comments FAILED account=@%s media=%s: %v",
			account.Username, in.IGMediaID, err)
		return nil, err
	}

	replies := 0
	for _, c := range page.Items {
		replies += len(c.Replies)
	}
	// Logged because a media object's comments_count can legitimately exceed what
	// this edge returns: it counts every comment, while the edge returns only
	// top-level ones and additionally applies privacy filtering (a commenter with a
	// private or restricted account is omitted). A visible mismatch is expected
	// behaviour, not necessarily a fault, this line is how to tell them apart.
	log.Printf("[instagram] list comments account=@%s media=%s -> %d top-level + %d replies (hasNext=%t cursor=%t)",
		account.Username, in.IGMediaID, len(page.Items), replies, page.HasNext, page.NextCursor != "")

	uc.mirror(ctx, account, in.IGMediaID, page.Items)
	return page, nil
}

// mirror keeps the local projection current so the moderation view has data even
// when the read budget is exhausted. Best effort by design.
func (uc *ListCommentsUseCase) mirror(ctx context.Context, account *igdomain.Account, igMediaID string, items []*igdomain.RemoteComment) {
	if uc.commentsRepo == nil || len(items) == 0 {
		return
	}
	records := make([]*igdomain.Comment, 0, len(items)*2)
	for _, c := range items {
		if c == nil {
			continue
		}
		records = append(records, remoteToComment(account, igMediaID, c, nil))
		for _, reply := range c.Replies {
			if reply == nil {
				continue
			}
			parent := c.IGCommentID
			records = append(records, remoteToComment(account, igMediaID, reply, &parent))
		}
	}
	if err := uc.commentsRepo.UpsertMany(ctx, records); err != nil {
		log.Printf("[instagram] comment mirror failed media=%s: %v", igMediaID, err)
	}
}

// ReplyToCommentUseCase posts a public threaded reply.
type ReplyToCommentUseCase struct {
	accountResolver
	comments     igdomain.CommentService
	commentsRepo igdomain.CommentRepository
}

func NewReplyToCommentUseCase(
	accounts igdomain.AccountRepository,
	commentSvc igdomain.CommentService,
	commentsRepo igdomain.CommentRepository,
) *ReplyToCommentUseCase {
	return &ReplyToCommentUseCase{
		accountResolver: accountResolver{accounts: accounts},
		comments:        commentSvc,
		commentsRepo:    commentsRepo,
	}
}

func (uc *ReplyToCommentUseCase) Execute(ctx context.Context, workspaceID, accountID, igCommentID, message string) (string, error) {
	account, err := uc.resolve(ctx, workspaceID, accountID)
	if err != nil {
		return "", err
	}
	if !account.CanManageComments() {
		return "", fmt.Errorf("instagram account %s cannot reply to comments (missing %s)",
			account.Username, igdomain.ScopeManageComments)
	}
	if strings.TrimSpace(message) == "" {
		return "", fmt.Errorf("instagram: reply text is required")
	}

	newID, err := uc.comments.ReplyToComment(ctx, account.AccessToken, igCommentID, message)
	if err != nil {
		return "", err
	}

	// Record our own reply immediately so the thread updates without waiting for
	// the webhook, and so IsOurs is set correctly (it decides whether the reply
	// can later be deleted).
	if uc.commentsRepo != nil && newID != "" {
		parent := igCommentID
		mediaID := uc.parentMediaID(ctx, account, igCommentID)
		now := time.Now().UTC()
		if err := uc.commentsRepo.Upsert(ctx, &igdomain.Comment{
			WorkspaceID:       account.WorkspaceID,
			IGAccountID:       account.ID,
			IGCommentID:       newID,
			IGMediaID:         mediaID,
			ParentIGCommentID: &parent,
			FromIGSID:         account.IGUserID,
			FromUsername:      account.Username,
			Text:              message,
			IsOurs:            true,
			Timestamp:         &now,
		}); err != nil {
			log.Printf("[instagram] reply mirror failed comment=%s: %v", newID, err)
		}
	}
	return newID, nil
}

func (uc *ReplyToCommentUseCase) parentMediaID(ctx context.Context, account *igdomain.Account, igCommentID string) string {
	if uc.commentsRepo == nil {
		return ""
	}
	parent, err := uc.commentsRepo.FindByIGCommentID(ctx, account.ID, igCommentID)
	if err != nil || parent == nil {
		return ""
	}
	return parent.IGMediaID
}

// ModerateCommentUseCase hides, unhides and deletes comments.
type ModerateCommentUseCase struct {
	accountResolver
	comments     igdomain.CommentService
	commentsRepo igdomain.CommentRepository
}

func NewModerateCommentUseCase(
	accounts igdomain.AccountRepository,
	commentSvc igdomain.CommentService,
	commentsRepo igdomain.CommentRepository,
) *ModerateCommentUseCase {
	return &ModerateCommentUseCase{
		accountResolver: accountResolver{accounts: accounts},
		comments:        commentSvc,
		commentsRepo:    commentsRepo,
	}
}

// SetHidden hides or unhides a comment.
//
// Hiding requires the MEDIA OWNER's token, which we have, so this is the
// moderation action that works on anyone's comment, unlike deletion.
func (uc *ModerateCommentUseCase) SetHidden(ctx context.Context, workspaceID, accountID, igCommentID string, hidden bool) error {
	account, err := uc.resolve(ctx, workspaceID, accountID)
	if err != nil {
		return err
	}
	if !account.CanManageComments() {
		return fmt.Errorf("instagram account %s cannot moderate comments (missing %s)",
			account.Username, igdomain.ScopeManageComments)
	}
	if err := uc.comments.SetHidden(ctx, account.AccessToken, igCommentID, hidden); err != nil {
		return err
	}
	if uc.commentsRepo != nil {
		if err := uc.commentsRepo.SetHidden(ctx, account.ID, igCommentID, hidden); err != nil {
			log.Printf("[instagram] hide mirror failed comment=%s: %v", igCommentID, err)
		}
	}
	return nil
}

// Delete removes a comment.
//
// Graph requires a token from the COMMENT CREATOR, so this only works for replies
// we authored. Attempting it on someone else's comment would fail upstream, so it
// is rejected here with a message that points at hiding instead.
func (uc *ModerateCommentUseCase) Delete(ctx context.Context, workspaceID, accountID, igCommentID string) error {
	account, err := uc.resolve(ctx, workspaceID, accountID)
	if err != nil {
		return err
	}
	if !account.CanManageComments() {
		return fmt.Errorf("instagram account %s cannot moderate comments (missing %s)",
			account.Username, igdomain.ScopeManageComments)
	}

	if uc.commentsRepo != nil {
		stored, err := uc.commentsRepo.FindByIGCommentID(ctx, account.ID, igCommentID)
		if err == nil && stored != nil && !stored.CanDelete() {
			return fmt.Errorf("instagram only allows deleting comments you authored; hide this one instead")
		}
	}

	if err := uc.comments.Delete(ctx, account.AccessToken, igCommentID); err != nil {
		return err
	}
	if uc.commentsRepo != nil {
		if err := uc.commentsRepo.Delete(ctx, account.ID, igCommentID); err != nil &&
			!errors.Is(err, igdomain.ErrCommentNotFound) {
			log.Printf("[instagram] delete mirror failed comment=%s: %v", igCommentID, err)
		}
	}
	return nil
}

// SendPrivateReplyUseCase DMs the author of a public comment.
//
// Instagram permits exactly ONE private reply per comment, ever, and only within
// 7 days. The allowance is claimed in Postgres BEFORE the HTTP call, so a retry
// after an ambiguous timeout cannot silently burn it.
type SendPrivateReplyUseCase struct {
	accountResolver
	messaging      igdomain.MessagingService
	commentsRepo   igdomain.CommentRepository
	privateReplies igdomain.PrivateReplyRepository
	contacts       igdomain.ContactRepository
	conversations  igdomain.ConversationRepository
	// history records the reply in the CRM transcript. Optional, but without it
	// the conversation this reply opens stays invisible: the inbox lists only
	// conversations that have a last message.
	history conversation.MessageHistoryManager
}

// SetHistoryManager wires transcript recording for private replies.
func (uc *SendPrivateReplyUseCase) SetHistoryManager(h conversation.MessageHistoryManager) {
	uc.history = h
}

func NewSendPrivateReplyUseCase(
	accounts igdomain.AccountRepository,
	messaging igdomain.MessagingService,
	commentsRepo igdomain.CommentRepository,
	privateReplies igdomain.PrivateReplyRepository,
	contacts igdomain.ContactRepository,
	conversations igdomain.ConversationRepository,
) *SendPrivateReplyUseCase {
	return &SendPrivateReplyUseCase{
		accountResolver: accountResolver{accounts: accounts},
		messaging:       messaging,
		commentsRepo:    commentsRepo,
		privateReplies:  privateReplies,
		contacts:        contacts,
		conversations:   conversations,
	}
}

func (uc *SendPrivateReplyUseCase) Execute(ctx context.Context, workspaceID, accountID, igCommentID, text string) error {
	account, err := uc.resolve(ctx, workspaceID, accountID)
	if err != nil {
		return err
	}
	// Private replies are gated by the COMMENTS scope, not a messaging scope.
	if !account.CanManageComments() {
		return fmt.Errorf("instagram account %s cannot send private replies (missing %s)",
			account.Username, igdomain.ScopeManageComments)
	}
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("instagram: private reply text is required")
	}
	if len(text) > igdomain.MaxTextBytes {
		return igdomain.ErrTextTooLong
	}

	// Enforce the 7-day window before spending the allowance.
	if uc.commentsRepo != nil {
		stored, err := uc.commentsRepo.FindByIGCommentID(ctx, account.ID, igCommentID)
		if err == nil && stored != nil && stored.Timestamp != nil {
			if time.Since(*stored.Timestamp) > igdomain.PrivateReplyWindow {
				return igdomain.ErrPrivateReplyExpired
			}
		}
	}

	// Claim first. If this returns false the allowance is already gone.
	claimed, err := uc.privateReplies.Claim(ctx, igCommentID, account.ID)
	if err != nil {
		return err
	}
	if !claimed {
		return igdomain.ErrPrivateReplyUsed
	}

	result, err := uc.messaging.SendPrivateReply(ctx, account.IGUserID, account.AccessToken, igCommentID, text)
	if err != nil {
		code := 0
		if apiErr, ok := meta.AsError(err); ok {
			code = apiErr.Code
		}
		// The row deliberately stays claimed: we cannot know whether Meta
		// processed the send, so handing the allowance back could double-send.
		if markErr := uc.privateReplies.MarkFailed(ctx, igCommentID, code, err.Error()); markErr != nil {
			log.Printf("[instagram] mark private reply failed comment=%s: %v", igCommentID, markErr)
		}
		return err
	}

	if err := uc.privateReplies.MarkSent(ctx, igCommentID, result.RecipientID, result.MessageID); err != nil {
		log.Printf("[instagram] mark private reply sent comment=%s: %v", igCommentID, err)
	}

	// The response carries the commenter's IGSID, which is the handle needed for
	// every subsequent normal DM, so the conversation is created now.
	conv := uc.ensureConversation(ctx, account, result.RecipientID)
	uc.recordInTranscript(ctx, account, conv, result, text)
	return nil
}

// recordInTranscript writes the private reply into the CRM conversation.
//
// Without this the reply is delivered on Instagram but absent from the CRM, and
// the conversation it opened does not appear in the inbox at all, the inbox
// lists conversations by their last message, and this would be a conversation
// with none. The operator would then meet the customer's answer with no record
// of what was said to them.
func (uc *SendPrivateReplyUseCase) recordInTranscript(
	ctx context.Context,
	account *igdomain.Account,
	conv *igdomain.Conversation,
	result *igdomain.SendResult,
	text string,
) {
	if uc.history == nil || conv == nil {
		return
	}
	providerID := ""
	if result != nil {
		providerID = result.MessageID
	}
	if err := uc.history.Record(ctx, conversation.MessageDirectionOutbound, conversation.MessageHistoryRecord{
		EntryID:           conv.ID,
		EntryType:         shared.EntryTypeInstagram,
		Channel:           conversation.MessageChannelInstagram,
		MessageType:       conversation.MessageTypeOperator,
		ProviderMessageID: providerID,
		From:              account.IGUserID,
		To:                result.RecipientID,
		Text:              text,
		Timestamp:         time.Now().UTC(),
	}); err != nil {
		log.Printf("[instagram] private reply transcript record failed conversation=%s: %v", conv.ID, err)
	}
}

func (uc *SendPrivateReplyUseCase) ensureConversation(ctx context.Context, account *igdomain.Account, igsid string) *igdomain.Conversation {
	if igsid == "" || uc.contacts == nil || uc.conversations == nil {
		return nil
	}
	contact, err := uc.contacts.FindOrCreate(ctx, account.WorkspaceID, account.ID, igsid)
	if err != nil {
		log.Printf("[instagram] private reply contact create failed igsid=%s: %v", igsid, err)
		return nil
	}
	conv, err := uc.conversations.FindOrCreate(ctx, account.WorkspaceID, account.ID, contact.ID)
	if err != nil {
		log.Printf("[instagram] private reply conversation create failed igsid=%s: %v", igsid, err)
		return nil
	}
	return conv
}

func remoteToComment(account *igdomain.Account, igMediaID string, c *igdomain.RemoteComment, parent *string) *igdomain.Comment {
	if parent == nil && c.ParentID != "" {
		p := c.ParentID
		parent = &p
	}
	return &igdomain.Comment{
		WorkspaceID:       account.WorkspaceID,
		IGAccountID:       account.ID,
		IGCommentID:       c.IGCommentID,
		IGMediaID:         igMediaID,
		ParentIGCommentID: parent,
		FromIGSID:         c.FromIGSID,
		FromUsername:      c.FromUsername,
		Text:              c.Text,
		LikeCount:         c.LikeCount,
		Hidden:            c.Hidden,
		IsOurs:            c.IsOurs,
		Timestamp:         c.Timestamp,
	}
}
