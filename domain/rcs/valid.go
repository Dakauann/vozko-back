package rcs

func (c BillingClass) Valid() bool {
	return c == ClassBasic || c == ClassSingle
}

func (s SenderStatus) Valid() bool {
	switch s {
	case SenderDraft, SenderSubmitted, SenderAwaitingConfirmation, SenderLaunched, SenderPaused, SenderRejected:
		return true
	}
	return false
}

func (p PausedBy) Valid() bool {
	return p == PausedByPlatform || p == PausedByCarrier
}

func (r PauseReason) Valid() bool {
	switch r {
	case PauseManual, PauseComplaints, PauseOptOutRate, PauseUnpaidBalance, PauseCarrier:
		return true
	}
	return false
}
