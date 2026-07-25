package prometheus

import "vozko/usecases/conversation/loopguard"

func (p *PrometheusService) LoopGuardChecked(layer, action string) {
	if p == nil || p.LoopGuardCheckedTotal == nil {
		return
	}
	p.LoopGuardCheckedTotal.WithLabelValues(safeLabel(layer), safeLabel(action)).Inc()
}

func (p *PrometheusService) LoopGuardBlocked(reason string) {
	if p == nil || p.LoopGuardBlockedTotal == nil {
		return
	}
	p.LoopGuardBlockedTotal.WithLabelValues(safeLabel(reason)).Inc()
}

var _ loopguard.MetricsRecorder = &PrometheusService{}
