package conversation

import "errors"

const (
	ReplySkipRateLimited         = "rate_limited"
	ReplySkipInsufficientBalance = "insufficient_balance"
	ReplySkipEmptyReply          = "empty_reply"
	ReplySkipWindowClosed        = "window_closed"
	ReplySkipWithheld            = "withheld"
)

type ReplySkipped struct {
	Reason string
}

func (s ReplySkipped) Error() string {
	return "agent reply skipped: " + s.Reason
}

func SkipReasonOf(err error) (string, bool) {
	var skipped ReplySkipped
	if errors.As(err, &skipped) {
		return skipped.Reason, true
	}
	return "", false
}

var ErrAgentTurnNotStarted = errors.New("the agent turn stopped before the model ran")

var retryableReplySkips = map[string]bool{ReplySkipRateLimited: true}

func TurnRetryable(err error) bool {
	if reason, skipped := SkipReasonOf(err); skipped {
		return retryableReplySkips[reason]
	}
	return errors.Is(err, ErrAgentTurnNotStarted)
}
