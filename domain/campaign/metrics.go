package campaign

// Counts is the per-status tally of one campaign's entries.
//
// The JSON shape is load-bearing: it is what the inbox status chips and the
// campaigns list already render, so the field names here are the ones the
// frontend has been reading since before this package existed.
type Counts struct {
	Total                   int64 `json:"total"`
	Pending                 int64 `json:"pending"`
	Sent                    int64 `json:"sent"`
	Delivered               int64 `json:"delivered"`
	Read                    int64 `json:"read"`
	Failed                  int64 `json:"failed"`
	NotEligiblePossibleSpam int64 `json:"notEligiblePossibleSpam"`
	// SkippedNotOnWhatsApp is omitted entirely on channels that cannot check a
	// number before sending, which is every channel but the unofficial one. An
	// absent field reads correctly as "this question does not apply here",
	// where a zero would read as "we checked and none were dead".
	SkippedNotOnWhatsApp int64 `json:"skippedNotOnWhatsApp,omitempty"`
}

// Processed is everything that has reached a resting state.
func (c Counts) Processed() int64 {
	return c.Sent + c.Delivered + c.Read + c.Failed +
		c.NotEligiblePossibleSpam + c.SkippedNotOnWhatsApp
}

// Dispatches ("disparos") counts the entries that actually left our system.
//
// Defined SUBTRACTIVELY — the whole campaign minus the buckets that represent
// work never attempted — and NOT by summing the delivery lifecycle
// (SENT/DELIVERED/READ). The difference matters when a status is added later: a
// subtractive definition includes it automatically, while a sum silently drops
// it from every export, filter and tile that asks what we sent.
//
// The excluded buckets are exactly the ones where nothing was transmitted:
// not-yet-sent (PENDING), our own spam cooldown, a number that is not on
// WhatsApp, and a send the provider refused (FAILED).
func (c Counts) Dispatches() int64 {
	dispatched := c.Total - c.Pending - c.Failed -
		c.NotEligiblePossibleSpam - c.SkippedNotOnWhatsApp
	if dispatched < 0 {
		return 0
	}
	return dispatched
}

// CategoryDispatches is dispatch volume split by WhatsApp template category.
//
// Official channel only — it is a fact about templates, and the unofficial
// channel has none. It stays in this package rather than in the official
// channel's because Metrics carries it, and Metrics is shared.
type CategoryDispatches struct {
	Marketing      int64 `json:"marketing"`
	Utility        int64 `json:"utility"`
	Authentication int64 `json:"authentication"`
}

// Metrics is what a campaign card, a campaign header and the workspace summary
// bar all render.
//
// One shape for every channel, so a component that draws these tiles never
// learns which transport produced them.
type Metrics struct {
	TotalNumbers            int64 `json:"totalNumbers"`
	Pending                 int64 `json:"pending"`
	Sent                    int64 `json:"sent"`
	Delivered               int64 `json:"delivered"`
	Read                    int64 `json:"read"`
	Failed                  int64 `json:"failed"`
	NotEligiblePossibleSpam int64 `json:"notEligiblePossibleSpam"`
	SkippedNotOnWhatsApp    int64 `json:"skippedNotOnWhatsApp,omitempty"`
	Processed               int64 `json:"processed"`
	// Dispatches is CURRENT entry status, not a ledger: a campaign reset zeroes
	// it. A channel that bills should read its charges from the balance ledger
	// instead and overwrite this field.
	Dispatches     int64   `json:"dispatches"`
	CompletionRate float64 `json:"completionRate"`
	SuccessRate    float64 `json:"successRate"`
	// ByCategory is set only where template categories exist.
	ByCategory *CategoryDispatches `json:"byCategory,omitempty"`
}

// NewMetrics derives the rendered metrics from a tally.
//
// A nil tally yields a zeroed Metrics rather than nil, because every caller
// renders the result and a nil would make a campaign with no entries crash the
// card that shows it.
func NewMetrics(counts *Counts) *Metrics {
	if counts == nil {
		return &Metrics{}
	}

	metrics := &Metrics{
		TotalNumbers:            counts.Total,
		Pending:                 counts.Pending,
		Sent:                    counts.Sent,
		Delivered:               counts.Delivered,
		Read:                    counts.Read,
		Failed:                  counts.Failed,
		NotEligiblePossibleSpam: counts.NotEligiblePossibleSpam,
		SkippedNotOnWhatsApp:    counts.SkippedNotOnWhatsApp,
		Processed:               counts.Processed(),
		Dispatches:              counts.Dispatches(),
	}

	if metrics.TotalNumbers > 0 {
		metrics.CompletionRate = float64(metrics.Processed) / float64(metrics.TotalNumbers) * 100
	}
	if metrics.Processed > 0 {
		metrics.SuccessRate = float64(counts.Sent+counts.Delivered+counts.Read) /
			float64(metrics.Processed) * 100
	}

	return metrics
}
