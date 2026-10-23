package emailsending

import "testing"

func TestReputationBandsAreTheReviewThresholdsInBasisPoints(t *testing.T) {
	if BounceBand().WarningBasisPoints != 200 || BounceBand().LimitBasisPoints != 500 {
		t.Fatalf("bounce band = %+v", BounceBand())
	}
	if ComplaintBand().WarningBasisPoints != 10 || ComplaintBand().LimitBasisPoints != 30 {
		t.Fatalf("complaint band = %+v", ComplaintBand())
	}
}

func TestReputationLevel(t *testing.T) {
	band := BounceBand()
	cases := []struct {
		rate int
		want ReputationLevel
	}{
		{0, ReputationOK},
		{199, ReputationOK},
		{200, ReputationWarning},
		{499, ReputationWarning},
		{500, ReputationOver},
		{2000, ReputationOver},
	}
	for _, tc := range cases {
		if got := band.Level(tc.rate); got != tc.want {
			t.Errorf("Level(%d) = %q, want %q", tc.rate, got, tc.want)
		}
	}
}
