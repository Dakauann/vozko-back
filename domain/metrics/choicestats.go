package metrics

type ChoiceStatsMetricsRecorder interface {
	IncChoiceStatsWrite(op, outcome string)
}
