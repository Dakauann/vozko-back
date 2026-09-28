package campaign

import "testing"

func TestStartableNeedsPendingNumbers(t *testing.T) {
	var none *Metrics
	cases := []struct {
		metrics *Metrics
		want    error
	}{
		{none, ErrNothingToSend},
		{&Metrics{}, ErrNothingToSend},
		{&Metrics{TotalNumbers: 2, Processed: 2}, ErrAlreadySent},
		{&Metrics{TotalNumbers: 2, Pending: 1, Processed: 1}, nil},
	}
	for _, c := range cases {
		if err := c.metrics.Startable(); err != c.want {
			t.Fatalf("%+v: %v", c.metrics, err)
		}
	}
}
