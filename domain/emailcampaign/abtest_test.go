package emailcampaign

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"vozko/domain/emailaddress"
)

func validAB() ABTest {
	return ABTest{VariantBSubject: "Outra chamada", TestPercent: 20, WinnerMetric: WinnerOpens, DecideAfterMinutes: 60}
}

func TestABTestValidate(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*ABTest)
		wantErr error
	}{
		{name: "valid", mutate: func(*ABTest) {}},
		{name: "lowest share", mutate: func(a *ABTest) { a.TestPercent = 10 }},
		{name: "highest share", mutate: func(a *ABTest) { a.TestPercent = 50 }},
		{name: "share too small", mutate: func(a *ABTest) { a.TestPercent = 9 }, wantErr: ErrABTestPercent},
		{name: "share too large", mutate: func(a *ABTest) { a.TestPercent = 51 }, wantErr: ErrABTestPercent},
		{name: "clicks metric", mutate: func(a *ABTest) { a.WinnerMetric = WinnerClicks }},
		{name: "unknown metric", mutate: func(a *ABTest) { a.WinnerMetric = "revenue" }, wantErr: ErrABTestMetric},
		{name: "shortest wait", mutate: func(a *ABTest) { a.DecideAfterMinutes = 30 }},
		{name: "longest wait", mutate: func(a *ABTest) { a.DecideAfterMinutes = 1440 }},
		{name: "wait too short", mutate: func(a *ABTest) { a.DecideAfterMinutes = 29 }, wantErr: ErrABTestWindow},
		{name: "wait too long", mutate: func(a *ABTest) { a.DecideAfterMinutes = 1441 }, wantErr: ErrABTestWindow},
		{name: "empty subject", mutate: func(a *ABTest) { a.VariantBSubject = " " }, wantErr: ErrABTestSubject},
		{name: "unknown merge tag in subject", mutate: func(a *ABTest) { a.VariantBSubject = "Oi {{apelido}}" }, wantErr: ErrABTestSubject},
		{name: "a winner is chosen by the system only", mutate: func(a *ABTest) { a.Winner = VariantB }, wantErr: ErrABTestWinnerPreset},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ab := validAB()
			tc.mutate(&ab)
			if err := ab.Validate(); !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestAssignIsStableAndSplitsTheTestShareEvenly(t *testing.T) {
	ab := validAB()
	counts := map[Variant]int{}
	const total = 20000
	for i := 0; i < total; i++ {
		address := emailaddress.Address(fmt.Sprintf("person%d@example.com", i))
		first := ab.Assign(address)
		if ab.Assign(address) != first {
			t.Fatalf("assignment of %s changed between calls", address)
		}
		counts[first]++
	}
	share := func(v Variant) float64 { return float64(counts[v]) / total }
	if a, b := share(VariantA), share(VariantB); a < 0.08 || a > 0.12 || b < 0.08 || b > 0.12 {
		t.Fatalf("a=%.3f b=%.3f, want about 0.10 each", a, b)
	}
	if w := share(VariantWinner); w < 0.77 || w > 0.83 {
		t.Fatalf("winner share %.3f, want about 0.80", w)
	}
}

func TestChooseWinner(t *testing.T) {
	cases := []struct {
		name   string
		metric WinnerMetric
		a, b   VariantResult
		want   Variant
	}{
		{name: "b opens more", metric: WinnerOpens, a: VariantResult{Delivered: 100, Opens: 20}, b: VariantResult{Delivered: 100, Opens: 30}, want: VariantB},
		{name: "rates not raw counts", metric: WinnerOpens, a: VariantResult{Delivered: 50, Opens: 20}, b: VariantResult{Delivered: 200, Opens: 30}, want: VariantA},
		{name: "tie goes to a", metric: WinnerOpens, a: VariantResult{Delivered: 100, Opens: 10}, b: VariantResult{Delivered: 200, Opens: 20}, want: VariantA},
		{name: "clicks decide", metric: WinnerClicks, a: VariantResult{Delivered: 100, Opens: 90, Clicks: 1}, b: VariantResult{Delivered: 100, Opens: 10, Clicks: 5}, want: VariantB},
		{name: "nothing delivered is a tie", metric: WinnerClicks, a: VariantResult{}, b: VariantResult{}, want: VariantA},
		{name: "a delivered nothing", metric: WinnerOpens, a: VariantResult{}, b: VariantResult{Delivered: 10, Opens: 1}, want: VariantB},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ChooseWinner(tc.metric, tc.a, tc.b); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestDecisionTimeAndSubjects(t *testing.T) {
	started := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	ab := validAB()
	if !ab.DecideAt(started).Equal(started.Add(time.Hour)) {
		t.Fatalf("DecideAt = %v", ab.DecideAt(started))
	}
	c := Campaign{Subject: "Chamada A", ABTest: &ab}
	if c.SubjectFor(VariantA) != "Chamada A" || c.SubjectFor(VariantB) != "Outra chamada" {
		t.Fatal("variant subjects are wrong")
	}
	if c.SubjectFor(VariantWinner) != "Chamada A" {
		t.Fatal("before a decision the remainder uses subject a")
	}
	ab.Winner = VariantB
	if c.SubjectFor(VariantWinner) != "Outra chamada" {
		t.Fatal("the remainder uses the winning subject")
	}
	plain := Campaign{Subject: "Só uma"}
	if plain.SubjectFor(VariantWinner) != "Só uma" || plain.SubjectFor("") != "Só uma" {
		t.Fatal("a campaign without a test always uses its subject")
	}
}
