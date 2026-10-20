package conversation

import "time"

func (s SentBy) AnswersTheCustomer() bool {
	if !s.Valid() {
		return false
	}
	for _, kind := range ReplySenderKinds() {
		if s.kind == kind {
			return true
		}
	}
	return false
}

func (s ConversationStatus) EndsTheWait() bool {
	return s == ConversationStatusFinished
}

func WaitingSince(status ConversationStatus, awaitingReplySince *time.Time) *time.Time {
	if awaitingReplySince == nil || awaitingReplySince.IsZero() || status.EndsTheWait() {
		return nil
	}
	since := awaitingReplySince.UTC()
	return &since
}
