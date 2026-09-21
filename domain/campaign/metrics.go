package campaign

type Counts struct {
	Total                   int64 `json:"total"`
	Pending                 int64 `json:"pending"`
	Sent                    int64 `json:"sent"`
	Delivered               int64 `json:"delivered"`
	Read                    int64 `json:"read"`
	Failed                  int64 `json:"failed"`
	NotEligiblePossibleSpam int64 `json:"notEligiblePossibleSpam"`
	SkippedNotOnWhatsApp    int64 `json:"skippedNotOnWhatsApp,omitempty"`
}

func (c Counts) Processed() int64 {
	return c.Sent + c.Delivered + c.Read + c.Failed +
		c.NotEligiblePossibleSpam + c.SkippedNotOnWhatsApp
}

func (c Counts) Dispatches() int64 {
	dispatched := c.Total - c.Pending - c.Failed -
		c.NotEligiblePossibleSpam - c.SkippedNotOnWhatsApp
	if dispatched < 0 {
		return 0
	}
	return dispatched
}

type CategoryDispatches struct {
	Marketing      int64 `json:"marketing"`
	Utility        int64 `json:"utility"`
	Authentication int64 `json:"authentication"`
}

type Metrics struct {
	TotalNumbers            int64               `json:"totalNumbers"`
	Pending                 int64               `json:"pending"`
	Sent                    int64               `json:"sent"`
	Delivered               int64               `json:"delivered"`
	Read                    int64               `json:"read"`
	Failed                  int64               `json:"failed"`
	NotEligiblePossibleSpam int64               `json:"notEligiblePossibleSpam"`
	SkippedNotOnWhatsApp    int64               `json:"skippedNotOnWhatsApp,omitempty"`
	Processed               int64               `json:"processed"`
	Dispatches              int64               `json:"dispatches"`
	CompletionRate          float64             `json:"completionRate"`
	SuccessRate             float64             `json:"successRate"`
	ByCategory              *CategoryDispatches `json:"byCategory,omitempty"`
}

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
