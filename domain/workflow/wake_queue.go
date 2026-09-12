package workflow

const (
	Exchange = "workflow_wake_exchange"

	TopicRunWake = "workflow.run.wake"
)

type RunWakeMessage struct {
	RunID  string `json:"run_id"`
	WakeAt int64  `json:"wake_at"`
}

type WakeScheduler interface {
	ScheduleRunWake(runID string, wakeAt int64) error
}

type ConsumeRunWakeUseCase interface {
	Start() error
}
