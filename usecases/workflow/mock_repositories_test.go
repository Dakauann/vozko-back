package workflow_usecase

import (
	"sync"
	"time"

	"vozko/domain/shared"
	"vozko/domain/workflow"
)

type MockWorkflowRepository struct {
	mu         sync.Mutex
	workflows  map[string]*workflow.Workflow
	versions   map[string]map[int]*workflow.WorkflowVersion
	CreateErr  error
	UpdateErr  error
	DeleteErr  error
	FindErr    error
	PublishErr error
	VersionErr error
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
	m.publishLikeTheBootRepair(&cp)
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
	m.publishLikeTheBootRepair(&cp)
	m.workflows[w.ID] = &cp
	return nil
}

func (m *MockWorkflowRepository) SaveDraft(w *workflow.Workflow, expectedRevision int) (int, error) {
	if err := workflow.RequireDraftRevision(expectedRevision); err != nil {
		return 0, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.UpdateErr != nil {
		return 0, m.UpdateErr
	}
	stored, ok := m.workflows[w.ID]
	if !ok {
		return 0, workflow.ErrWorkflowNotFound
	}
	if stored.Version != expectedRevision {
		return 0, &workflow.DraftConflict{WorkflowID: w.ID, Expected: expectedRevision, Current: stored.Version}
	}
	cp := *w
	cp.Status, cp.PublishedVersion, cp.DepartmentID = stored.Status, stored.PublishedVersion, stored.DepartmentID
	cp.Version = expectedRevision + 1
	m.workflows[w.ID] = &cp
	return cp.Version, nil
}

func (m *MockWorkflowRepository) UpdateDepartment(id, departmentID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.UpdateErr != nil {
		return m.UpdateErr
	}
	stored, ok := m.workflows[id]
	if !ok {
		return workflow.ErrWorkflowNotFound
	}
	stored.DepartmentID = departmentID
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
	m.publishLikeTheBootRepair(w)
	return m.withPublication(w), nil
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
			items = append(items, m.withPublication(w))
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
		if w.WorkspaceID != wsID || w.Status != workflow.WorkflowStatusActive || w.PublishedVersion == nil {
			continue
		}
		v := m.versions[w.ID][*w.PublishedVersion]
		if v == nil {
			continue
		}
		running := w.Running(v)
		if running.HasTriggerType(triggerType) {
			result = append(result, running)
		}
	}
	return result, nil
}

func (m *MockWorkflowRepository) UpdateStatus(id string, status workflow.WorkflowStatus) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.UpdateErr != nil {
		return m.UpdateErr
	}
	w, ok := m.workflows[id]
	if !ok {
		return workflow.ErrWorkflowNotFound
	}
	w.Status = status
	return nil
}

func (m *MockWorkflowRepository) PublishVersion(in workflow.PublishVersion) (*workflow.WorkflowVersion, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.PublishErr != nil {
		return nil, m.PublishErr
	}
	w, ok := m.workflows[in.WorkflowID]
	if !ok {
		return nil, workflow.ErrWorkflowNotFound
	}
	next := len(m.versions[in.WorkflowID]) + 1
	v := m.storeVersion(in.WorkflowID, next, in.Type, in.Graph, in.GraphHash, in.PublishedBy)
	w.PublishedVersion = &next
	w.Status = workflow.WorkflowStatusActive
	if in.RewriteDraft && w.Version == in.DraftRevision {
		w.Graph = in.Draft
		w.Type = in.Type
	}
	cp := *v
	return &cp, nil
}

func (m *MockWorkflowRepository) PublishedInWorkspace(workspaceID string) ([]workflow.PublishedWorkflow, error) {
	drafts, err := m.FindByWorkspaceID(workspaceID)
	if err != nil {
		return nil, err
	}
	out := make([]workflow.PublishedWorkflow, 0, len(drafts))
	for _, w := range drafts {
		published, err := workflow.PublishedOf(m, w)
		out = append(out, workflow.PublishedWorkflow{Draft: w, Published: published, Err: err})
	}
	return out, nil
}

func (m *MockWorkflowRepository) FindVersion(id string, version int) (*workflow.WorkflowVersion, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FindErr != nil {
		return nil, m.FindErr
	}
	if m.VersionErr != nil {
		return nil, m.VersionErr
	}
	v := m.versions[id][version]
	if v == nil {
		return nil, nil
	}
	cp := *v
	g := v.Graph.Clone()
	cp.Graph = &g
	return &cp, nil
}

func (m *MockWorkflowRepository) ListVersions(id string, _ shared.QueryOptions) (*shared.PaginatedResult[*workflow.WorkflowVersion], error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	items := []*workflow.WorkflowVersion{}
	for n := len(m.versions[id]); n >= 1; n-- {
		if v := m.versions[id][n]; v != nil {
			cp := *v
			cp.Graph = nil
			items = append(items, &cp)
		}
	}
	return &shared.PaginatedResult[*workflow.WorkflowVersion]{Items: items, Page: 1, PageSize: 20, TotalItems: int64(len(items)), TotalPages: 1}, nil
}

func (m *MockWorkflowRepository) VersionCount(id string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.versions[id])
}

func (m *MockWorkflowRepository) storeVersion(id string, number int, t workflow.WorkflowType, g workflow.Graph, hash, by string) *workflow.WorkflowVersion {
	if m.versions == nil {
		m.versions = make(map[string]map[int]*workflow.WorkflowVersion)
	}
	if m.versions[id] == nil {
		m.versions[id] = make(map[int]*workflow.WorkflowVersion)
	}
	snapshot := g.Clone()
	v := &workflow.WorkflowVersion{WorkflowID: id, Version: number, Type: t, Graph: &snapshot, GraphHash: hash, PublishedBy: by, PublishedAt: time.Now().UTC()}
	m.versions[id][number] = v
	return v
}

func (m *MockWorkflowRepository) publishLikeTheBootRepair(w *workflow.Workflow) {
	if w.PublishedVersion != nil || (w.Status != workflow.WorkflowStatusActive && w.Status != workflow.WorkflowStatusPaused) {
		return
	}
	one := 1
	m.storeVersion(w.ID, one, w.Type, w.Graph, workflow.GraphHash(&w.Graph), "")
	w.PublishedVersion = &one
}

func (m *MockWorkflowRepository) withPublication(w *workflow.Workflow) *workflow.Workflow {
	cp := *w
	if cp.PublishedVersion != nil {
		if v := m.versions[w.ID][*cp.PublishedVersion]; v != nil {
			at := v.PublishedAt
			cp.PublishedAt = &at
			cp.PublishedGraphHash = v.GraphHash
		}
	}
	return &cp
}

type MockWorkflowRunRepository struct {
	mu                 sync.Mutex
	runs               map[string]*workflow.WorkflowRun
	CreateErr          error
	UpdateErr          error
	FindErr            error
	finishBeforeCancel bool
}

func (m *MockWorkflowRunRepository) CancelRun(runID, reason string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.runs[runID]
	if !ok {
		return false, nil
	}
	if m.finishBeforeCancel {
		r.SetCompleted()
	}
	if r.Status.IsTerminal() {
		return false, nil
	}
	r.SetCancelled()
	r.Error = reason
	return true, nil
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
	if stored, ok := m.runs[run.ID]; ok && stored.Status.IsTerminal() {
		return workflow.ErrRunTerminal
	}
	cp := *run
	m.runs[run.ID] = &cp
	return nil
}

func (m *MockWorkflowRunRepository) RunStatus(id string) (workflow.RunStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.runs[id]
	if !ok {
		return "", workflow.ErrRunNotFound
	}
	return r.Status, nil
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

func (m *MockWorkflowRunLogRepository) PageByRunID(runID string, offset, limit int) ([]*workflow.WorkflowRunLog, error) {
	all, _ := m.FindByRunID(runID)
	if offset >= len(all) {
		return []*workflow.WorkflowRunLog{}, nil
	}
	return all[offset:min(offset+limit, len(all))], nil
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
