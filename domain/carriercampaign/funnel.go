package carriercampaign

import "vozko/domain/rcs"

type Funnel struct {
	Targeted              int64 `json:"targeted"`
	Skipped               int64 `json:"skipped"`
	Sent                  int64 `json:"sent"`
	Accepted              int64 `json:"accepted"`
	Rejected              int64 `json:"rejected"`
	Delivered             int64 `json:"delivered"`
	Read                  int64 `json:"read"`
	Failed                int64 `json:"failed"`
	Expired               int64 `json:"expired"`
	Taps                  int64 `json:"taps"`
	Clicks                int64 `json:"clicks"`
	Replies               int64 `json:"replies"`
	FellBackWhatsApp      int64 `json:"fell_back_whatsapp"`
	FellBackSMS           int64 `json:"fell_back_sms"`
	OptOuts               int64 `json:"opt_outs"`
	Conversions           int64 `json:"conversions"`
	ConversionValueMicros int64 `json:"conversion_value_micros"`
	SpendMicros           int64 `json:"spend_micros"`
}

type Rates struct {
	Delivery                *float64 `json:"delivery"`
	Read                    *float64 `json:"read"`
	TapThrough              *float64 `json:"tap_through"`
	ClickThrough            *float64 `json:"click_through"`
	Reply                   *float64 `json:"reply"`
	Fallback                *float64 `json:"fallback"`
	OptOut                  *float64 `json:"opt_out"`
	Conversion              *float64 `json:"conversion"`
	CostPerConversionMicros *int64   `json:"cost_per_conversion_micros"`
}

func ratio(part, whole int64) *float64 {
	if whole <= 0 || part < 0 {
		return nil
	}
	r := float64(part) / float64(whole)
	return &r
}

func (f Funnel) Rates() Rates {
	r := Rates{
		Delivery:     ratio(f.Delivered, f.Sent),
		Read:         ratio(f.Read, f.Delivered),
		TapThrough:   ratio(f.Taps, f.Delivered),
		ClickThrough: ratio(f.Clicks, f.Delivered),
		Reply:        ratio(f.Replies, f.Delivered),
		Fallback:     ratio(f.FellBackWhatsApp+f.FellBackSMS, f.Sent),
		OptOut:       ratio(f.OptOuts, f.Delivered),
		Conversion:   ratio(f.Conversions, f.Delivered),
	}
	if f.Conversions > 0 && f.SpendMicros >= 0 {
		cost := f.SpendMicros / f.Conversions
		r.CostPerConversionMicros = &cost
	}
	return r
}

func (f Funnel) Add(o Funnel) Funnel {
	return Funnel{
		Targeted:              f.Targeted + o.Targeted,
		Skipped:               f.Skipped + o.Skipped,
		Sent:                  f.Sent + o.Sent,
		Accepted:              f.Accepted + o.Accepted,
		Rejected:              f.Rejected + o.Rejected,
		Delivered:             f.Delivered + o.Delivered,
		Read:                  f.Read + o.Read,
		Failed:                f.Failed + o.Failed,
		Expired:               f.Expired + o.Expired,
		Taps:                  f.Taps + o.Taps,
		Clicks:                f.Clicks + o.Clicks,
		Replies:               f.Replies + o.Replies,
		FellBackWhatsApp:      f.FellBackWhatsApp + o.FellBackWhatsApp,
		FellBackSMS:           f.FellBackSMS + o.FellBackSMS,
		OptOuts:               f.OptOuts + o.OptOuts,
		Conversions:           f.Conversions + o.Conversions,
		ConversionValueMicros: f.ConversionValueMicros + o.ConversionValueMicros,
		SpendMicros:           f.SpendMicros + o.SpendMicros,
	}
}

type VariantStat struct {
	Index  int    `json:"index"`
	Name   string `json:"name"`
	Funnel Funnel `json:"funnel"`
	Rates  Rates  `json:"rates"`
}

func VariantStats(variants []Variant, funnels map[int]Funnel) []VariantStat {
	out := make([]VariantStat, 0, len(variants))
	for i, v := range variants {
		f := funnels[i]
		out = append(out, VariantStat{Index: i, Name: v.Name, Funnel: f, Rates: f.Rates()})
	}
	return out
}

type ButtonStat struct {
	Key    rcs.ButtonKey      `json:"key"`
	Label  string             `json:"label"`
	Kind   rcs.SuggestionKind `json:"kind"`
	Taps   int64              `json:"taps"`
	Clicks int64              `json:"clicks"`
}

func ButtonStats(content rcs.Content, taps, clicks map[rcs.ButtonKey]int64) []ButtonStat {
	buttons := content.Buttons()
	out := make([]ButtonStat, 0, len(buttons))
	for _, b := range buttons {
		out = append(out, ButtonStat{
			Key:    b.Key,
			Label:  b.Suggestion.Label,
			Kind:   b.Suggestion.Kind,
			Taps:   taps[b.Key],
			Clicks: clicks[b.Key],
		})
	}
	return out
}

type CarrierStat struct {
	Operator  string `json:"operator"`
	Sent      int64  `json:"sent"`
	Delivered int64  `json:"delivered"`
	FellBack  int64  `json:"fell_back"`
}

func (c CarrierStat) DeliveryRate() *float64 {
	return ratio(c.Delivered, c.Sent)
}

func (c CarrierStat) FallbackRate() *float64 {
	return ratio(c.FellBack, c.Sent)
}
