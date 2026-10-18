package smartcampaign

import "vozko/domain/whatsapp/metaerror"

type Outcome struct {
	Processed int         `json:"processed"`
	Delivered int         `json:"delivered"`
	Read      int         `json:"read"`
	Failures  map[int]int `json:"failures,omitempty"`
}

func (o Outcome) Failed() int {
	total := 0
	for _, n := range o.Failures {
		total += n
	}
	return total
}

func (o Outcome) Share(n int) float64 {
	if o.Processed == 0 {
		return 0
	}
	return float64(n) / float64(o.Processed)
}

func (o Outcome) contactSkips() int {
	skipped := 0
	for code, n := range o.Failures {
		if metaerror.AboutContact(code) {
			skipped += n
		}
	}
	return skipped
}

func (o Outcome) Judged() int {
	return o.Processed - o.contactSkips()
}

func (o Outcome) TemplateFailures() int {
	return o.Failed() - o.contactSkips()
}

func (o Outcome) FailureRate() float64 {
	if o.Judged() <= 0 {
		return 0
	}
	return float64(o.TemplateFailures()) / float64(o.Judged())
}
