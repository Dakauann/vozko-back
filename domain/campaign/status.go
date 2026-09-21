package campaign

type SendStatus string

const (
	SendStatusPending                 SendStatus = "PENDING"
	SendStatusSent                    SendStatus = "SENT"
	SendStatusDelivered               SendStatus = "DELIVERED"
	SendStatusRead                    SendStatus = "READ"
	SendStatusFailed                  SendStatus = "FAILED"
	SendStatusNotEligiblePossibleSpam SendStatus = "NOT_ELIGIBLE_POSSIBLE_SPAM"
	SendStatusSkippedNotOnWhatsApp    SendStatus = "SKIPPED_NOT_ON_WHATSAPP"
)

func (s SendStatus) IsTerminal() bool {
	switch s {
	case SendStatusDelivered, SendStatusRead, SendStatusFailed,
		SendStatusNotEligiblePossibleSpam, SendStatusSkippedNotOnWhatsApp:
		return true
	default:
		return false
	}
}

type StatusSet struct {
	All         []SendStatus
	NonDispatch []SendStatus
}

func (s StatusSet) Dispatched() []SendStatus {
	excluded := make(map[SendStatus]struct{}, len(s.NonDispatch))
	for _, st := range s.NonDispatch {
		excluded[st] = struct{}{}
	}

	out := make([]SendStatus, 0, len(s.All))
	for _, st := range s.All {
		if _, skip := excluded[st]; skip {
			continue
		}
		out = append(out, st)
	}
	return out
}

func (s StatusSet) Valid(v SendStatus) bool {
	for _, known := range s.All {
		if known == v {
			return true
		}
	}
	return false
}

func Strings(statuses []SendStatus) []string {
	out := make([]string, 0, len(statuses))
	for _, s := range statuses {
		out = append(out, string(s))
	}
	return out
}
