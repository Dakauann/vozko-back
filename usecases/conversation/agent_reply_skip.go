package conversation_usecase

import (
	"vozko/domain/campaign"
	"vozko/domain/conversation"
	"vozko/domain/lead"
	wo "vozko/domain/whatsapp_outreach"
)

const (
	replySkipRateLimited         = conversation.ReplySkipRateLimited
	replySkipInsufficientBalance = conversation.ReplySkipInsufficientBalance
	replySkipEmptyReply          = conversation.ReplySkipEmptyReply
	replySkipWindowClosed        = conversation.ReplySkipWindowClosed
	replySkipWithheld            = conversation.ReplySkipWithheld
)

func skippedReply(reason string) error {
	return conversation.ReplySkipped{Reason: reason}
}

func replySkipReason(err error) (string, bool) {
	return conversation.SkipReasonOf(err)
}

func refusedContact(contact *lead.Lead) error {
	if refusal := campaign.ContactRefusal(contact); refusal != nil {
		return skippedReply(wo.ErrorCode(refusal))
	}
	return nil
}

func failedReply(err error) error {
	if _, skipped := replySkipReason(err); skipped {
		return nil
	}
	return err
}
