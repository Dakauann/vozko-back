package campaign

// The send status of one campaign target, and the algebra over it.
//
// The vocabulary is a union across channels rather than a per-channel enum,
// because everything downstream — the export filter, the entries table, the
// metrics tiles, the CSV column — renders statuses without caring which
// transport produced them. Which subset a channel actually uses is declared by
// that channel as a StatusSet.

type SendStatus string

const (
	SendStatusPending   SendStatus = "PENDING"
	SendStatusSent      SendStatus = "SENT"
	SendStatusDelivered SendStatus = "DELIVERED"
	SendStatusRead      SendStatus = "READ"
	SendStatusFailed    SendStatus = "FAILED"
	// SendStatusNotEligiblePossibleSpam is our own cooldown refusing the send:
	// this lead was already reached from this number inside the workspace's
	// spam-protection window. Nothing was attempted and nobody was charged.
	SendStatusNotEligiblePossibleSpam SendStatus = "NOT_ELIGIBLE_POSSIBLE_SPAM"
	// SendStatusSkippedNotOnWhatsApp exists only on the unofficial channel,
	// where a number can be checked against WhatsApp before anything is sent.
	//
	// It is a LIST-QUALITY fact and deliberately not a failure: a purchased list
	// is routinely 20-40% dead, and reporting that as failures makes a working
	// integration look broken and hides the real failures underneath it.
	SendStatusSkippedNotOnWhatsApp SendStatus = "SKIPPED_NOT_ON_WHATSAPP"
)

// IsTerminal reports whether this status can still change on its own.
//
// PENDING and SENT can: a pending entry is waiting for the queue, and a sent one
// is waiting for a delivery receipt. Everything else is where it will stay
// unless the campaign is reset.
func (s SendStatus) IsTerminal() bool {
	switch s {
	case SendStatusDelivered, SendStatusRead, SendStatusFailed,
		SendStatusNotEligiblePossibleSpam, SendStatusSkippedNotOnWhatsApp:
		return true
	default:
		return false
	}
}

// StatusSet is one channel's declared vocabulary.
//
// A channel lists everything it can produce (All) and, separately, the buckets
// that represent work that never left the building (NonDispatch). Dispatched is
// DERIVED by subtraction rather than listed, and that is the whole point of the
// type: a status added to All without being added to NonDispatch joins the
// dispatched set automatically, instead of being silently dropped from every
// filter, export and metric that asks "what did we actually send".
type StatusSet struct {
	All         []SendStatus
	NonDispatch []SendStatus
}

// Dispatched returns the statuses that represent a message that really left.
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

// Valid reports whether this channel can produce that status.
func (s StatusSet) Valid(v SendStatus) bool {
	for _, known := range s.All {
		if known == v {
			return true
		}
	}
	return false
}

// Strings renders a status set for a repository IN clause.
//
// Exists so no SQL ever spells the statuses as literals. A hand-written IN list
// is a second declaration of the vocabulary, and the two drift the first time a
// status is added.
func Strings(statuses []SendStatus) []string {
	out := make([]string, 0, len(statuses))
	for _, s := range statuses {
		out = append(out, string(s))
	}
	return out
}
