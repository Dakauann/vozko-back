package emailsending

import "testing"

func TestDomainStatsAlarming(t *testing.T) {
	cases := []struct {
		stats DomainStats
		want  bool
	}{
		{DomainStats{Available: true, SpamRatePerMil: 3}, true},
		{DomainStats{Available: true, SpamRatePerMil: 12}, true},
		{DomainStats{Available: true, SpamRatePerMil: 2}, false},
		{DomainStats{Available: false, SpamRatePerMil: 9}, false},
	}
	for _, tc := range cases {
		if got := tc.stats.Alarming(); got != tc.want {
			t.Errorf("%+v: got %v, want %v", tc.stats, got, tc.want)
		}
	}
}
