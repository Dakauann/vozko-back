package attendance

import "testing"

func TestBuildFRTStatsOnNoSamplesIsEmpty(t *testing.T) {
	got := BuildFRTStats(nil)
	if got != (FRTStats{}) {
		t.Fatalf("BuildFRTStats(nil) = %+v, want the zero value", got)
	}
}

func TestBuildFRTStatsSplitsHumansFromAutomation(t *testing.T) {
	got := BuildFRTStats([]FRTSample{
		{ActorKind: ActorKindHuman, Seconds: 60},
		{ActorKind: ActorKindHuman, Seconds: 180},
		{ActorKind: ActorKindAI, Seconds: 6},
		{ActorKind: "workflow", Seconds: 18},
	})

	if got.SampleCount != 4 || got.HumanSamples != 2 || got.AISamples != 2 {
		t.Fatalf("BuildFRTStats() counts = %d total / %d human / %d ai, want 4/2/2", got.SampleCount, got.HumanSamples, got.AISamples)
	}
	if got.HumanAvgMins != 2 {
		t.Fatalf("BuildFRTStats() HumanAvgMins = %v, want 2", got.HumanAvgMins)
	}
	if got.AIAvgMins != 0.2 {
		t.Fatalf("BuildFRTStats() AIAvgMins = %v, want 0.2", got.AIAvgMins)
	}
	if got.AvgFRTMins != 1.1 {
		t.Fatalf("BuildFRTStats() AvgFRTMins = %v, want 1.1", got.AvgFRTMins)
	}
}

func TestBuildFRTStatsMedianIsTheUpperMiddleOfTheSortedSamples(t *testing.T) {
	cases := []struct {
		name    string
		seconds []float64
		want    float64
	}{
		{name: "odd count takes the middle", seconds: []float64{600, 60, 120}, want: 2},
		{name: "even count takes the upper middle", seconds: []float64{240, 60, 120, 600}, want: 4},
		{name: "a single sample is its own median", seconds: []float64{90}, want: 1.5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			samples := make([]FRTSample, 0, len(tc.seconds))
			for _, s := range tc.seconds {
				samples = append(samples, FRTSample{ActorKind: ActorKindHuman, Seconds: s})
			}
			if got := BuildFRTStats(samples).MedianFRTMins; got != tc.want {
				t.Fatalf("MedianFRTMins = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestBuildFRTStatsDoesNotReorderTheCallersSamples(t *testing.T) {
	samples := []FRTSample{{Seconds: 300}, {Seconds: 60}}
	BuildFRTStats(samples)
	if samples[0].Seconds != 300 {
		t.Fatalf("BuildFRTStats() sorted the caller's slice in place")
	}
}

func TestBuildFRTStatsScalesToALargeSample(t *testing.T) {
	samples := make([]FRTSample, 200_000)
	for i := range samples {
		samples[i] = FRTSample{ActorKind: ActorKindHuman, Seconds: float64(len(samples) - i)}
	}
	got := BuildFRTStats(samples)
	if got.SampleCount != int64(len(samples)) {
		t.Fatalf("SampleCount = %d, want %d", got.SampleCount, len(samples))
	}
}
