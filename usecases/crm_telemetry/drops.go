package crm_telemetry_usecase

import (
	"log"
	"sync/atomic"
)

// LogDropRecorder is a process-local drop counter + log (safe when Prometheus not extended).
type LogDropRecorder struct {
	publishErrors  atomic.Int64
	consumeErrors  atomic.Int64
	dropped        atomic.Int64
}

func NewLogDropRecorder() *LogDropRecorder {
	return &LogDropRecorder{}
}

func (r *LogDropRecorder) IncTelemetryPublishError(kind string) {
	if r == nil {
		return
	}
	r.publishErrors.Add(1)
	log.Printf("[crm_telemetry][metric] publish_error kind=%s total=%d", kind, r.publishErrors.Load())
}

func (r *LogDropRecorder) IncTelemetryConsumeError(kind, reason string) {
	if r == nil {
		return
	}
	r.consumeErrors.Add(1)
	log.Printf("[crm_telemetry][metric] consume_error kind=%s reason=%s total=%d", kind, reason, r.consumeErrors.Load())
}

func (r *LogDropRecorder) IncTelemetryDropped(kind, reason string) {
	if r == nil {
		return
	}
	r.dropped.Add(1)
	log.Printf("[crm_telemetry][metric] dropped kind=%s reason=%s total=%d", kind, reason, r.dropped.Load())
}

func (r *LogDropRecorder) Snapshot() (publish, consume, drop int64) {
	if r == nil {
		return 0, 0, 0
	}
	return r.publishErrors.Load(), r.consumeErrors.Load(), r.dropped.Load()
}
