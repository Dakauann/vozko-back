package conversation_usecase

import (
	"context"
	"fmt"
	"log"
	"strings"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/domain/user"
)

// operatorSendUseCase delivers a message a human authored, on any channel.
//
// It is the single answer to "what happens when an operator sends", assembled
// from the WebSocket hub's frame handler where it used to live inline. Three
// things were trapped in that handler and are now reachable by every send
// surface: which signature format the channel renders, how a media send differs
// from a text send, and the four side effects a delivered reply owes its
// conversation.
type operatorSendUseCase struct {
	sender    conversation.MessageSender
	users     user.UserRepository
	finalizer conversation.OperatorSendFinalizer
	billing   conversation.ServiceMessageBilling
}

// NewOperatorSendUseCase wires the use case.
//
// All three dependencies are required. A nil sender cannot deliver anything and
// a nil finalizer would deliver messages that leave the conversation in the
// wrong status and off the activity timeline — a silent loss, which is worse
// than refusing to start.
func NewOperatorSendUseCase(
	sender conversation.MessageSender,
	users user.UserRepository,
	finalizer conversation.OperatorSendFinalizer,
	billing conversation.ServiceMessageBilling,
) (conversation.OperatorSendUseCase, error) {
	missing := []string{}
	if sender == nil {
		missing = append(missing, "message sender")
	}
	if users == nil {
		missing = append(missing, "user repository")
	}
	if finalizer == nil {
		missing = append(missing, "operator send finalizer")
	}
	if billing == nil {
		// Every operator reply on an official WhatsApp number is a message Meta
		// charges for from 1 October 2026. Without this the send goes out and
		// the fee is nobody's, which is the failure the whole billing pass
		// exists to stop.
		missing = append(missing, "service message billing")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("operator send use case: missing %s", strings.Join(missing, ", "))
	}

	return &operatorSendUseCase{sender: sender, users: users, finalizer: finalizer, billing: billing}, nil
}

func (uc *operatorSendUseCase) Execute(ctx context.Context, in conversation.OperatorSendInput) (*conversation.Message, error) {
	in.EntryID = strings.TrimSpace(in.EntryID)
	in.EntryType = strings.TrimSpace(in.EntryType)
	in.Text = strings.TrimSpace(in.Text)

	if in.EntryID == "" {
		return nil, conversation.ErrEntryIDRequired
	}
	if !shared.EntryType(in.EntryType).IsKnown() {
		return nil, conversation.ErrEntryTypeInvalid
	}
	if in.Text == "" && in.MediaID == "" && in.Buttons == nil {
		return nil, conversation.ErrMessageContentRequired
	}

	// Refuse before sending, because after sending there is nothing to refuse:
	// Meta charges on delivery and the message is gone. A plan that prices
	// service messages at zero never reaches the balance, so this is invisible
	// to everyone who is not paying per message.
	//
	// Skipped when the caller could not name a workspace. Every caller does
	// today (the live composer, the scheduled dispatcher, the audience
	// senders), and refusing an operator's reply because a hint was missing
	// would be a worse failure than not charging for it.
	if in.WorkspaceID != "" {
		if err := uc.billing.AllowSend(in.WorkspaceID); err != nil {
			return nil, err
		}
	}

	message, err := uc.send(in)
	if err != nil {
		return nil, err
	}

	// Best-effort by contract: the message is already with the customer, so a
	// failing side effect is reported, never propagated as a send failure.
	if err := uc.finalizer.FinalizeOperatorSend(ctx, conversation.FinalizeOperatorSendInput{
		EntryID:     in.EntryID,
		EntryType:   in.EntryType,
		WorkspaceID: in.WorkspaceID,
		ActorUserID: in.SenderUserID,
		Message:     message,
	}); err != nil {
		log.Printf("[OperatorSend] finalize failed for %s (%s): %v", in.EntryID, in.EntryType, err)
	}

	return message, nil
}

// send routes to the one of three shapes this input describes.
func (uc *operatorSendUseCase) send(in conversation.OperatorSendInput) (*conversation.Message, error) {
	if in.Buttons != nil {
		// An interactive prompt is not signed: the signature would land in the
		// body above the buttons and read as part of the question.
		return uc.sender.SendButtonMessage(in.EntryID, in.EntryType, in.SenderUserID, in.ReplyToMessageID, *in.Buttons)
	}

	text := in.Text
	if in.Signed {
		text = conversation.SignOutbound(shared.EntryType(in.EntryType), uc.senderName(in.SenderUserID), text)
	}

	if in.MediaID != "" {
		// Media carries the text as its caption, which is why signing happens
		// before this branch rather than inside each one.
		return uc.sender.SendMediaMessage(
			in.EntryID, in.EntryType, in.MediaID, in.MediaType,
			in.SenderUserID, in.ReplyToMessageID, text,
		)
	}
	return uc.sender.SendTextMessage(in.EntryID, in.EntryType, text, in.SenderUserID, in.ReplyToMessageID)
}

// senderName resolves the operator's display name for the signature.
//
// Resolve-and-continue: a lookup failure costs the signature prefix and nothing
// else. SignOutbound treats an empty name as "do not sign", so a dead user
// repository can never produce a message that opens with a stray "*:".
func (uc *operatorSendUseCase) senderName(userID string) string {
	record, err := uc.users.FindByID(userID)
	if err != nil || record == nil {
		log.Printf("[OperatorSend] could not resolve sender %s for the signature: %v", userID, err)
		return ""
	}
	return record.Username
}

var _ conversation.OperatorSendUseCase = (*operatorSendUseCase)(nil)
