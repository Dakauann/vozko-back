package attendance

import (
	"math"
	"slices"
)

type FRTSample struct {
	ActorKind string
	Seconds   float64
}

func BuildFRTStats(samples []FRTSample) FRTStats {
	out := FRTStats{}
	if len(samples) == 0 {
		return out
	}

	seconds := make([]float64, 0, len(samples))
	var sum, humanSum, aiSum float64
	for _, sample := range samples {
		seconds = append(seconds, sample.Seconds)
		sum += sample.Seconds
		if CountsAsAutomation(sample.ActorKind) {
			out.AISamples++
			aiSum += sample.Seconds
			continue
		}
		out.HumanSamples++
		humanSum += sample.Seconds
	}

	out.SampleCount = int64(len(samples))
	out.AvgFRTMins = secondsToRoundedMinutes(sum / float64(out.SampleCount))
	slices.Sort(seconds)
	out.MedianFRTMins = secondsToRoundedMinutes(seconds[len(seconds)/2])
	if out.HumanSamples > 0 {
		out.HumanAvgMins = secondsToRoundedMinutes(humanSum / float64(out.HumanSamples))
	}
	if out.AISamples > 0 {
		out.AIAvgMins = secondsToRoundedMinutes(aiSum / float64(out.AISamples))
	}
	return out
}

func secondsToRoundedMinutes(seconds float64) float64 {
	return math.Round(seconds/60*100) / 100
}
