package businessphone

func (s Status) BlocksSending() bool {
	switch s {
	case StatusBanned, StatusRestricted, StatusDisconnected, StatusSuspended, StatusRateLimited, StatusOnboardingFailed, StatusPending, StatusVerifying:
		return true
	default:
		return false
	}
}
