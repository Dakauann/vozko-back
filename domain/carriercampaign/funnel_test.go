package carriercampaign

import (
	"math"
	"testing"

	"vozko/domain/rcs"
)

func assertRate(t *testing.T, name string, got *float64, want float64, present bool) {
	t.Helper()
	if !present {
		if got != nil {
			t.Errorf("%s = %v, want absent", name, *got)
		}
		return
	}
	if got == nil {
		t.Errorf("%s absent, want %v", name, want)
		return
	}
	if math.IsNaN(*got) || math.IsInf(*got, 0) || math.Abs(*got-want) > 1e-9 {
		t.Errorf("%s = %v, want %v", name, *got, want)
	}
}

func TestFunnelRates(t *testing.T) {
	f := Funnel{
		Targeted:              1200,
		Skipped:               200,
		Sent:                  1000,
		Accepted:              980,
		Rejected:              20,
		Delivered:             800,
		Read:                  400,
		Failed:                100,
		Expired:               80,
		Taps:                  120,
		Clicks:                80,
		Replies:               40,
		FellBackWhatsApp:      150,
		FellBackSMS:           50,
		OptOuts:               8,
		Conversions:           16,
		ConversionValueMicros: 4_000_000_000,
		SpendMicros:           320_000_000,
	}
	r := f.Rates()
	assertRate(t, "delivery", r.Delivery, 0.8, true)
	assertRate(t, "read", r.Read, 0.5, true)
	assertRate(t, "tap through", r.TapThrough, 0.15, true)
	assertRate(t, "click through", r.ClickThrough, 0.1, true)
	assertRate(t, "reply", r.Reply, 0.05, true)
	assertRate(t, "fallback", r.Fallback, 0.2, true)
	assertRate(t, "opt out", r.OptOut, 0.01, true)
	assertRate(t, "conversion", r.Conversion, 0.02, true)
	if r.CostPerConversionMicros == nil || *r.CostPerConversionMicros != 20_000_000 {
		t.Errorf("cost per conversion = %v", r.CostPerConversionMicros)
	}
}

func TestFunnelRatesWithoutDenominators(t *testing.T) {
	r := Funnel{}.Rates()
	for name, got := range map[string]*float64{
		"delivery": r.Delivery, "read": r.Read, "tap": r.TapThrough, "click": r.ClickThrough,
		"reply": r.Reply, "fallback": r.Fallback, "opt out": r.OptOut, "conversion": r.Conversion,
	} {
		assertRate(t, name, got, 0, false)
	}
	if r.CostPerConversionMicros != nil {
		t.Errorf("cost per conversion = %v", *r.CostPerConversionMicros)
	}

	sentOnly := Funnel{Sent: 10, SpendMicros: 100}.Rates()
	assertRate(t, "delivery of sent only", sentOnly.Delivery, 0, true)
	assertRate(t, "read without delivered", sentOnly.Read, 0, false)
	if sentOnly.CostPerConversionMicros != nil {
		t.Error("no conversions means no cost per conversion")
	}

	negative := Funnel{Sent: -3, Delivered: 2}.Rates()
	assertRate(t, "negative denominator", negative.Delivery, 0, false)
}

func TestFunnelAdd(t *testing.T) {
	a := Funnel{Targeted: 1, Skipped: 2, Sent: 3, Accepted: 4, Rejected: 5, Delivered: 6, Read: 7, Failed: 8, Expired: 9, Taps: 10, Clicks: 11, Replies: 12, FellBackWhatsApp: 13, FellBackSMS: 14, OptOuts: 15, Conversions: 16, ConversionValueMicros: 17, SpendMicros: 18}
	got := a.Add(a)
	want := Funnel{Targeted: 2, Skipped: 4, Sent: 6, Accepted: 8, Rejected: 10, Delivered: 12, Read: 14, Failed: 16, Expired: 18, Taps: 20, Clicks: 22, Replies: 24, FellBackWhatsApp: 26, FellBackSMS: 28, OptOuts: 30, Conversions: 32, ConversionValueMicros: 34, SpendMicros: 36}
	if got != want {
		t.Fatalf("got %+v", got)
	}
}

func TestVariantStats(t *testing.T) {
	variants := []Variant{textVariant("Curta", "a"), cardVariant("Cartão")}
	stats := VariantStats(variants, map[int]Funnel{1: {Sent: 10, Delivered: 5}, 7: {Sent: 99}})
	if len(stats) != 2 {
		t.Fatalf("got %d stats", len(stats))
	}
	if stats[0].Index != 0 || stats[0].Name != "Curta" || stats[0].Funnel != (Funnel{}) || stats[0].Rates.Delivery != nil {
		t.Fatalf("variant without data: %+v", stats[0])
	}
	if stats[1].Index != 1 || stats[1].Name != "Cartão" || stats[1].Funnel.Sent != 10 {
		t.Fatalf("variant with data: %+v", stats[1])
	}
	assertRate(t, "variant delivery", stats[1].Rates.Delivery, 0.5, true)
}

func TestButtonStats(t *testing.T) {
	content := rcs.Content{
		Cards: []rcs.Card{
			{Title: "A", Suggestions: []rcs.Suggestion{{Kind: rcs.SuggestOpenURL, Label: "Comprar", URL: "https://loja.example.com"}, {Kind: rcs.SuggestReply, Label: "Quero"}}},
			{Title: "B", Suggestions: []rcs.Suggestion{{Kind: rcs.SuggestDial, Label: "Ligar", Phone: "+5511999990000"}}},
		},
		Suggestions: []rcs.Suggestion{{Kind: rcs.SuggestReply, Label: "Sair"}},
	}
	taps := map[rcs.ButtonKey]int64{{Card: 0, Index: 1}: 7, {Card: -1, Index: 0}: 2, {Card: 9, Index: 9}: 50}
	clicks := map[rcs.ButtonKey]int64{{Card: 0, Index: 0}: 30}
	got := ButtonStats(content, taps, clicks)
	want := []ButtonStat{
		{Key: rcs.ButtonKey{Card: -1, Index: 0}, Label: "Sair", Kind: rcs.SuggestReply, Taps: 2},
		{Key: rcs.ButtonKey{Card: 0, Index: 0}, Label: "Comprar", Kind: rcs.SuggestOpenURL, Clicks: 30},
		{Key: rcs.ButtonKey{Card: 0, Index: 1}, Label: "Quero", Kind: rcs.SuggestReply, Taps: 7},
		{Key: rcs.ButtonKey{Card: 1, Index: 0}, Label: "Ligar", Kind: rcs.SuggestDial},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d buttons: %+v", len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("button %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestCarrierStat(t *testing.T) {
	c := CarrierStat{Operator: "Vivo", Sent: 200, Delivered: 150, FellBack: 30}
	assertRate(t, "carrier fallback", c.FallbackRate(), 0.15, true)
	assertRate(t, "carrier delivery", c.DeliveryRate(), 0.75, true)
	empty := CarrierStat{Operator: "Claro"}
	assertRate(t, "empty carrier fallback", empty.FallbackRate(), 0, false)
	assertRate(t, "empty carrier delivery", empty.DeliveryRate(), 0, false)
}
