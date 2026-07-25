package workflow

import (
	"encoding/json"
	"fmt"
	"time"
)

type RunStatus string

const (
	RunStatusRunning   RunStatus = "running"
	RunStatusWaiting   RunStatus = "waiting"
	RunStatusCompleted RunStatus = "completed"
	RunStatusError     RunStatus = "error"
	RunStatusCancelled RunStatus = "cancelled"
)

func (s RunStatus) Valid() bool {
	switch s {
	case RunStatusRunning, RunStatusWaiting, RunStatusCompleted, RunStatusError, RunStatusCancelled:
		return true
	}
	return false
}

func (s RunStatus) IsTerminal() bool {
	switch s {
	case RunStatusCompleted, RunStatusError, RunStatusCancelled:
		return true
	}
	return false
}

type LogStatus string

const (
	LogStatusExecuted LogStatus = "executed"
	LogStatusSkipped  LogStatus = "skipped"
	LogStatusFailed   LogStatus = "failed"
)

type RunState struct {
	Vars map[string]interface{} `json:"vars"`
}

func NewRunState() RunState {
	return RunState{Vars: make(map[string]interface{})}
}

func (s *RunState) Get(key string) (interface{}, bool) {
	if s.Vars == nil {
		return nil, false
	}
	val, ok := s.Vars[key]
	return val, ok
}

func (s *RunState) GetString(key string) string {
	val, ok := s.Get(key)
	if !ok {
		return ""
	}
	str, ok := val.(string)
	if ok {
		return str
	}
	return fmt.Sprintf("%v", val)
}

// GetInt reads an integer state value, tolerating the float64 that a numeric
// value becomes after a JSON persistence round-trip (state survives waits as JSON).
func (s *RunState) GetInt(key string) int {
	val, ok := s.Get(key)
	if !ok {
		return 0
	}
	switch n := val.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}

func (s *RunState) Set(key string, value interface{}) {
	if s.Vars == nil {
		s.Vars = make(map[string]interface{})
	}
	s.Vars[key] = value
}

func (s *RunState) Delete(key string) {
	delete(s.Vars, key)
}

func (s *RunState) JSON() ([]byte, error) {
	return json.Marshal(s.Vars)
}

func RunStateFromJSON(data []byte) (RunState, error) {
	state := NewRunState()
	if len(data) == 0 {
		return state, nil
	}
	if err := json.Unmarshal(data, &state.Vars); err != nil {
		return state, err
	}
	return state, nil
}

type WaitReason string

const (
	WaitReasonDuration WaitReason = "duration"
	WaitReasonReply    WaitReason = "reply"
	WaitReasonEvent    WaitReason = "event"
	WaitReasonBalance  WaitReason = "insufficient_balance"
	WaitReasonRetry    WaitReason = "retry"
)

type WorkflowRun struct {
	ID            string     `json:"id"`
	WorkflowID    string     `json:"workflowId"`
	WorkspaceID   string     `json:"workspaceId"`
	EntryID       string     `json:"entryId"`
	EntryType     string     `json:"entryType"`
	Status        RunStatus  `json:"status"`
	TriggerNodeID string     `json:"triggerNodeId,omitempty"`
	CurrentNodeID string     `json:"currentNodeId,omitempty"`
	State         RunState   `json:"state"`
	WakeAt        *int64     `json:"wakeAt,omitempty"`
	WaitReason    WaitReason `json:"waitReason,omitempty"`
	RetryCount    int        `json:"retryCount"`
	Error         string     `json:"error,omitempty"`
	StartedAt     time.Time  `json:"startedAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
	CompletedAt   *time.Time `json:"completedAt,omitempty"`
}

func (r *WorkflowRun) Validate() error {
	if r.WorkflowID == "" {
		return ErrWorkflowIDRequired
	}
	if r.WorkspaceID == "" {
		return ErrWorkspaceIDRequired
	}
	if r.EntryID == "" {
		return ErrEntryIDRequired
	}
	if r.EntryType == "" {
		return ErrEntryTypeRequired
	}
	return nil
}

func (r *WorkflowRun) SetWaiting(wakeAt int64, reason WaitReason) {
	r.Status = RunStatusWaiting
	r.WakeAt = &wakeAt
	r.WaitReason = reason
	r.UpdatedAt = time.Now().UTC()
}

func (r *WorkflowRun) SetRunning() {
	r.Status = RunStatusRunning
	r.WakeAt = nil
	r.WaitReason = ""
	r.UpdatedAt = time.Now().UTC()
}

func (r *WorkflowRun) SetCompleted() {
	r.Status = RunStatusCompleted
	now := time.Now().UTC()
	r.CompletedAt = &now
	r.UpdatedAt = now
}

func (r *WorkflowRun) SetError(errMsg string) {
	r.Status = RunStatusError
	r.Error = errMsg
	r.UpdatedAt = time.Now().UTC()
}

func (r *WorkflowRun) SetCancelled() {
	r.Status = RunStatusCancelled
	now := time.Now().UTC()
	r.CompletedAt = &now
	r.UpdatedAt = now
}

type WorkflowRunLog struct {
	ID         string                 `json:"id"`
	RunID      string                 `json:"runId"`
	NodeID     string                 `json:"nodeId"`
	NodeType   NodeType               `json:"nodeType"`
	Status     LogStatus              `json:"status"`
	Input      map[string]interface{} `json:"input,omitempty"`
	Output     map[string]interface{} `json:"output,omitempty"`
	Error      string                 `json:"error,omitempty"`
	ExecutedAt time.Time              `json:"executedAt"`
}
