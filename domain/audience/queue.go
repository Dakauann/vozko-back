package audience

// The alert send path, named where it belongs.
//
// Both names live here rather than as literals in the composition root, the way
// domain/rag and domain/shortlink already declare theirs. The exchange and the
// topic are one fact about this feature: a publisher and a consumer that
// disagree about either one produce a queue nothing ever reads, and splitting
// them across two layers is how they come to disagree.
const (
	AlertExchange = "audience_alert_exchange"

	// TopicAlertSend carries a CLAIMED alert to whoever sends it.
	//
	// The split it exists for is between what must happen exactly once and what
	// is merely slow. Claiming a rule is one conditional write that decides
	// which replica owns a firing, so it stays on the analysis path. The model
	// call for the briefing and the outbound WhatsApp send do not: the flush job
	// walks every due container of every workspace sequentially in one
	// goroutine, and doing either there made one workspace's alert delay every
	// other workspace's analysis in the same tick.
	//
	// Because the claim already happened before anything is published, a message
	// on this topic is owned: it cannot become a second alert.
	TopicAlertSend = "audience.alert.send"
)
