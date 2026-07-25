package calls

type CallMetricsRecorder interface {
	IncCallDialing()
	DecCallDialing()
	IncCallOngoing()
	DecCallOngoing()
	IncCallFinished()
}

type CallStateMachine struct {
	metrics  CallMetricsRecorder
	answered bool
	finished bool
}

func NewCallStateMachine(metrics CallMetricsRecorder) *CallStateMachine {
	return &CallStateMachine{metrics: metrics}
}

func (sm *CallStateMachine) Dialing() {
	if sm.metrics != nil {
		sm.metrics.IncCallDialing()
	}
}

func (sm *CallStateMachine) LeaveDialing() {
	if sm.metrics == nil {
		return
	}
	if !sm.answered {
		sm.metrics.DecCallDialing()
	}
}

func (sm *CallStateMachine) Answered() {
	sm.answered = true
	if sm.metrics != nil {
		sm.metrics.DecCallDialing()
		sm.metrics.IncCallOngoing()
	}
}

func (sm *CallStateMachine) LeaveOngoing() {
	if sm.metrics != nil {
		sm.metrics.DecCallOngoing()
	}
}

func (sm *CallStateMachine) Finished() {
	if sm.finished {
		return
	}
	sm.finished = true
	if sm.metrics != nil {
		sm.metrics.IncCallFinished()
	}
}
