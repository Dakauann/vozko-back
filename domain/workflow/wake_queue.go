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

// AutomationGate answers whether automation is still on for a conversation.
//
// A run that PARKS — on a timer or waiting for a reply — outlives the decision
// that started it. The inbound path refuses to start a run when the operator
// has switched automation off, but nothing re-asked when a parked run woke up,
// so a workflow scheduled in the morning still messaged the contact hours after
// automation was turned off. Live case: automation disabled 11:13, the run sent
// at 15:21, interrupting the attendant mid-conversation with the patient.
//
// A port rather than a repository dependency: the engine must not learn what a
// campaign entry is. Implementations live in infra and answer for their own
// channel.
//
// Returning true on doubt is deliberate. A read failure must not silently
// cancel runs for every conversation; the inbound guard has the same posture,
// and treating "unknown" as "off" would turn a transient database error into
// mass cancellation.
type AutomationGate interface {
	AutomationEnabled(entryID, entryType string) bool
}
