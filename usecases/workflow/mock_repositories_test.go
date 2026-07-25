package workflow_usecase

import (
	"sync"
	"time"

	"vozko/domain/shared"
	"vozko/domain/workflow"
)

type MockWorkflowRepository struct {
	mu        sync.Mutex
	workflows map[string]*workflow.Workflow
	CreateErr error
	UpdateErr error
	DeleteErr error
	FindErr   error
}

func NewMockWorkflowRepository() *MockWorkflowRepository {
	return &MockWorkflowRepository{
		workflows: make(map[string]*workflow.Workflow),
	}
}

func (m *MockWorkflowRepository) Create(w *workflow.Workflow) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.CreateErr != nil {
		return m.CreateErr
	}
	cp := *w
	m.workflows[w.ID] = &cp
	return nil
}

func (m *MockWorkflowRepository) Update(w *workflow.Workflow) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.UpdateErr != nil {
		return m.UpdateErr
	}
	cp := *w
	m.workflows[w.ID] = &cp
	return nil
}

func (m *MockWorkflowRepository) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.DeleteErr != nil {
		return m.DeleteErr
	}
	delete(m.workflows, id)
	return nil
}

func (m *MockWorkflowRepository) FindByID(id string) (*workflow.Workflow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FindErr != nil {
		return nil, m.FindErr
	}
	w, ok := m.workflows[id]
	if !ok {
		return nil, nil
	}
	cp := *w
	return &cp, nil
}

func (m *MockWorkflowRepository) FindByWorkspaceID(wsID string) ([]*workflow.Workflow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []*workflow.Workflow
	for _, w := range m.workflows {
		if w.WorkspaceID == wsID {
			cp := *w
			result = append(result, &cp)
		}
	}
	return result, nil
}

func (m *MockWorkflowRepository) List(input workflow.ListWorkflowsInput) (*shared.PaginatedResult[*workflow.Workflow], error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var items []*workflow.Workflow
	for _, w := range m.workflows {
		if w.WorkspaceID == input.WorkspaceID {
			cp := *w
			items = append(items, &cp)
		}
	}
	return &shared.PaginatedResult[*workflow.Workflow]{
		Items:      items,
		Page:       1,
		PageSize:   20,
		TotalItems: int64(len(items)),
		TotalPages: 1,
	}, nil
}

func (m *MockWorkflowRepository) FindActiveByTrigger(wsID string, triggerType workflow.TriggerType) ([]*workflow.Workflow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []*workflow.Workflow
	for _, w := range m.workflows {
		if w.WorkspaceID == wsID && w.Status == workflow.WorkflowStatusActive && w.HasTriggerType(triggerType) {
			cp := *w
			result = append(result, &cp)
		}
	}
	return result, nil
}

type MockWorkflowRunRepository struct {
	mu        sync.Mutex
	runs      map[string]*workflow.WorkflowRun
	CreateErr error
	UpdateErr error
	FindErr   error
}

func NewMockWorkflowRunRepository() *MockWorkflowRunRepository {
	return &MockWorkflowRunRepository{
		runs: make(map[string]*workflow.WorkflowRun),
	}
}

func (m *MockWorkflowRunRepository) Create(run *workflow.WorkflowRun) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.CreateErr != nil {
		return m.CreateErr
	}
	cp := *run
	m.runs[run.ID] = &cp
	return nil
}

func (m *MockWorkflowRunRepository) Update(run *workflow.WorkflowRun) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.UpdateErr != nil {
		return m.UpdateErr
	}
	cp := *run
	m.runs[run.ID] = &cp
	return nil
}

func (m *MockWorkflowRunRepository) FindByID(id string) (*workflow.WorkflowRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.runs[id]
	if !ok {
		return nil, nil
	}
	cp := *r
	return &cp, nil
}

func (m *MockWorkflowRunRepository) FindActiveByEntry(workflowID, entryID string) (*workflow.WorkflowRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.runs {
		if r.WorkflowID == workflowID && r.EntryID == entryID && !r.Status.IsTerminal() {
			cp := *r
			return &cp, nil
		}
	}
	return nil, nil
}

func (m *MockWorkflowRunRepository) FindActiveByEntryAndTrigger(workflowID, entryID, triggerNodeID string) (*workflow.WorkflowRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FindErr != nil {
		return nil, m.FindErr
	}
	for _, r := range m.runs {
		if r.WorkflowID == workflowID && r.EntryID == entryID && r.TriggerNodeID == triggerNodeID && !r.Status.IsTerminal() {
			cp := *r
			return &cp, nil
		}
	}
	return nil, nil
}

func (m *MockWorkflowRunRepository) FindWaitingReplyByEntry(entryID string) (*workflow.WorkflowRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.runs {
		if r.EntryID == entryID && r.Status == workflow.RunStatusWaiting && r.WaitReason == workflow.WaitReasonReply {
			cp := *r
			return &cp, nil
		}
	}
	return nil, nil
}

func (m *MockWorkflowRunRepository) List(input workflow.ListRunsInput) (*shared.PaginatedResult[*workflow.WorkflowRun], error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var items []*workflow.WorkflowRun
	for _, r := range m.runs {
		if input.WorkflowID != "" && r.WorkflowID != input.WorkflowID {
			continue
		}
		if input.EntryID != "" && r.EntryID != input.EntryID {
			continue
		}
		cp := *r
		items = append(items, &cp)
	}
	return &shared.PaginatedResult[*workflow.WorkflowRun]{
		Items:      items,
		Page:       1,
		PageSize:   20,
		TotalItems: int64(len(items)),
		TotalPages: 1,
	}, nil
}

func (m *MockWorkflowRunRepository) FindWakeableRuns(now int64, limit int) ([]*workflow.WorkflowRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []*workflow.WorkflowRun
	for _, r := range m.runs {
		if r.Status == workflow.RunStatusWaiting && r.WakeAt != nil && *r.WakeAt <= now {
			cp := *r
			result = append(result, &cp)
			if len(result) >= limit {
				break
			}
		}
	}
	return result, nil
}

func (m *MockWorkflowRunRepository) FindStuckRuns(cutoff int64) ([]*workflow.WorkflowRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	nowMs := time.Now().UTC().UnixMilli()
	var result []*workflow.WorkflowRun
	for _, r := range m.runs {
		if (r.Status == workflow.RunStatusRunning || r.Status == workflow.RunStatusWaiting) && r.UpdatedAt.Unix() < cutoff {

			if r.Status == workflow.RunStatusWaiting && r.WakeAt != nil && *r.WakeAt > nowMs {
				continue
			}
			cp := *r
			result = append(result, &cp)
		}
	}
	return result, nil
}

func (m *MockWorkflowRunRepository) CancelByWorkflow(workflowID string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var count int64
	for _, r := range m.runs {
		if r.WorkflowID == workflowID && !r.Status.IsTerminal() {
			r.SetCancelled()
			count++
		}
	}
	return count, nil
}

func (m *MockWorkflowRunRepository) CountByWorkflow(workflowID string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var count int64
	for _, r := range m.runs {
		if r.WorkflowID == workflowID {
			count++
		}
	}
	return count, nil
}

func (m *MockWorkflowRunRepository) CountActiveByWorkspace(wsID string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var count int64
	for _, r := range m.runs {
		if r.WorkspaceID == wsID && !r.Status.IsTerminal() {
			count++
		}
	}
	return count, nil
}

type MockWorkflowRunLogRepository struct {
	mu   sync.Mutex
	logs []*workflow.WorkflowRunLog
}

func NewMockWorkflowRunLogRepository() *MockWorkflowRunLogRepository {
	return &MockWorkflowRunLogRepository{}
}

func (m *MockWorkflowRunLogRepository) Create(log *workflow.WorkflowRunLog) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *log
	m.logs = append(m.logs, &cp)
	return nil
}

func (m *MockWorkflowRunLogRepository) FindByRunID(runID string) ([]*workflow.WorkflowRunLog, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []*workflow.WorkflowRunLog
	for _, l := range m.logs {
		if l.RunID == runID {
			cp := *l
			result = append(result, &cp)
		}
	}
	return result, nil
}

func (m *MockWorkflowRunLogRepository) CountByRun(runID string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var count int64
	for _, l := range m.logs {
		if l.RunID == runID {
			count++
		}
	}
	return count, nil
}
