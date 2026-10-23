package carriermessaging

import (
	"errors"
	"testing"
	"time"
)

func TestFrequencyCap(t *testing.T) {
	cap := DefaultFrequencyCap()
	cases := []struct {
		today, week int
		want        bool
	}{
		{0, 0, true},
		{1, 5, true},
		{2, 2, false},
		{1, 6, false},
	}
	for _, tc := range cases {
		if got := cap.Allows(tc.today, tc.week); got != tc.want {
			t.Fatalf("Allows(%d, %d) = %v, want %v", tc.today, tc.week, got, tc.want)
		}
	}
}

func TestFrequencyCapValidation(t *testing.T) {
	for _, bad := range []FrequencyCap{{PerDay: 0, PerWeek: 6}, {PerDay: 3, PerWeek: 2}, {PerDay: -1, PerWeek: 1}} {
		if err := bad.Validate(); !errors.Is(err, ErrSettingsInvalid) {
			t.Fatalf("Validate(%+v) = %v", bad, err)
		}
	}
	if err := DefaultFrequencyCap().Validate(); err != nil {
		t.Fatalf("default = %v", err)
	}
}

func TestOptOutKeywords(t *testing.T) {
	for _, text := range []string{"SAIR", "sair", " Parar ", "stop", "Cancelar", "sair."} {
		if !IsOptOutKeyword(text) {
			t.Fatalf("%q should opt out", text)
		}
	}
	for _, text := range []string{"quero sair da lista amanhã", "parar o pedido 123", "", "sairr"} {
		if IsOptOutKeyword(text) {
			t.Fatalf("%q should not opt out", text)
		}
	}
}

func TestQuietHoursDefaultIsEightToEightEveryDay(t *testing.T) {
	spec := DefaultQuietHours("America/Sao_Paulo")
	sched, err := spec.Compile()
	if err != nil {
		t.Fatalf("Compile = %v", err)
	}
	loc, _ := time.LoadLocation("America/Sao_Paulo")
	if !sched.IsOpen(time.Date(2026, 10, 25, 8, 0, 0, 0, loc)) {
		t.Fatal("Sunday 08:00 is open")
	}
	if sched.IsOpen(time.Date(2026, 10, 21, 20, 0, 0, 0, loc)) {
		t.Fatal("20:00 is closed")
	}
	if sched.IsOpen(time.Date(2026, 10, 21, 7, 59, 0, 0, loc)) {
		t.Fatal("07:59 is closed")
	}
}

func TestHealthVerdict(t *testing.T) {
	cases := []struct {
		delivered, optOuts int
		want               bool
	}{
		{199, 100, false},
		{200, 4, false},
		{200, 5, true},
		{1000, 21, true},
		{1000, 20, false},
	}
	for _, tc := range cases {
		if got := OptOutRateBreached(tc.delivered, tc.optOuts); got != tc.want {
			t.Fatalf("OptOutRateBreached(%d, %d) = %v, want %v", tc.delivered, tc.optOuts, got, tc.want)
		}
	}
}

func TestSettingsDefaults(t *testing.T) {
	s := DefaultSettings("ws-1", "America/Sao_Paulo")
	if s.FrequencyCap != DefaultFrequencyCap() || s.AttributionDays != DefaultAttributionDays || s.QuietHours.Timezone != "America/Sao_Paulo" {
		t.Fatalf("defaults = %+v", s)
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("Validate = %v", err)
	}
	s.AttributionDays = 0
	if err := s.Validate(); !errors.Is(err, ErrSettingsInvalid) {
		t.Fatalf("Validate zero attribution = %v", err)
	}
}

func TestFloodingAfterThirtyMessagesInAnHour(t *testing.T) {
	if IsFlooding(FloodThreshold) {
		t.Fatal("exactly the threshold is not a flood")
	}
	if !IsFlooding(FloodThreshold + 1) {
		t.Fatal("one past the threshold is a flood")
	}
	if FloodWindow != time.Hour || FloodThreshold != 30 {
		t.Fatalf("flood rule = %d in %v", FloodThreshold, FloodWindow)
	}
}
