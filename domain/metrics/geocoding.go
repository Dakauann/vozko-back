package metrics

import "time"

const (
	GeocodingQuotaMonthly = "monthly"
	GeocodingQuotaDaily   = "daily"

	GeocodingProviderOK          = "ok"
	GeocodingProviderNotFound    = "not_found"
	GeocodingProviderAmbiguous   = "ambiguous"
	GeocodingProviderUnavailable = "unavailable"
	GeocodingProviderError       = "error"
)

type GeocodingMetricsRecorder interface {
	SetGeocodingBacklog(status string, n int64)
	AddGeocodingOutcomes(outcome string, n int)
	ObserveGeocodingProvider(provider, result string, elapsed time.Duration)
	IncGeocodingQuotaHit(quota string)
	AddGeocodingStale(n int)
	IncGeocodingProviderPause(provider, reason string)
	SetGeocodingProviderPausedUntil(provider, reason string, until time.Time)
	IncGeocodingAnswerReused()
}
