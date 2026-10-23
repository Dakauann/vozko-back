package metrics

type GoogleSheetsMetricsRecorder interface {
	AddGoogleSheetsEvent(stage, outcome string)
	ObserveGoogleSheetsBatch(rows int, seconds float64)
}
