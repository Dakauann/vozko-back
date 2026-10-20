package metrics

type MercadoLivreMetricsRecorder interface {
	AddMercadoLivreEvent(stage, outcome string)
}
