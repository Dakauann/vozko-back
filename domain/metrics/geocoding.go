package metrics

type GeocodingMetricsRecorder interface {
	SetGeocodingBacklog(status string, n int64)
	AddGeocodingOutcomes(outcome string, n int)
	AddGeocodingStale(n int)
}
