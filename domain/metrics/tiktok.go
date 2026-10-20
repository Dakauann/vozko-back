package metrics

type TikTokMetricsRecorder interface {
	AddTikTokEvent(stage, outcome string)
}
