package workflow_usecase

import (
	"context"
	"time"

	"vozko/domain/workflow"
)

type fakeWebhookRepo struct {
	byToken    map[string]*workflow.WorkflowWebhook
	byWorkflow map[string]*workflow.WorkflowWebhook
	findErr    error
	createErr  error
	updateErr  error
	created    []*workflow.WorkflowWebhook
	updated    []*workflow.WorkflowWebhook
	deleted    []string
}

func newFakeWebhookRepo() *fakeWebhookRepo {
	return &fakeWebhookRepo{
		byToken:    map[string]*workflow.WorkflowWebhook{},
		byWorkflow: map[string]*workflow.WorkflowWebhook{},
	}
}

func (f *fakeWebhookRepo) put(wh *workflow.WorkflowWebhook) {
	f.byToken[wh.Token] = wh
	f.byWorkflow[wh.WorkflowID] = wh
}

func (f *fakeWebhookRepo) Create(wh *workflow.WorkflowWebhook) error {
	if f.createErr != nil {
		return f.createErr
	}
	if wh.ID == "" {
		wh.ID = "wh-" + wh.WorkflowID
	}
	f.put(wh)
	f.created = append(f.created, wh)
	return nil
}

func (f *fakeWebhookRepo) Update(wh *workflow.WorkflowWebhook) error {
	if f.updateErr != nil {
		return f.updateErr
	}
	f.put(wh)
	f.updated = append(f.updated, wh)
	return nil
}

func (f *fakeWebhookRepo) FindByToken(token string) (*workflow.WorkflowWebhook, error) {
	if f.findErr != nil {
		return nil, f.findErr
	}
	return f.byToken[token], nil
}

func (f *fakeWebhookRepo) FindByWorkflowID(workflowID string) (*workflow.WorkflowWebhook, error) {
	if f.findErr != nil {
		return nil, f.findErr
	}
	return f.byWorkflow[workflowID], nil
}

func (f *fakeWebhookRepo) Delete(workflowID string) error {
	f.deleted = append(f.deleted, workflowID)
	if wh := f.byWorkflow[workflowID]; wh != nil {
		delete(f.byToken, wh.Token)
		delete(f.byWorkflow, workflowID)
	}
	return nil
}

type fakeEntryChecker struct {
	owns bool
	err  error
	seen []string
}

func (f *fakeEntryChecker) OwnsEntry(workspaceID, entryID, entryType string) (bool, error) {
	f.seen = append(f.seen, workspaceID+"/"+entryID+"/"+entryType)
	return f.owns, f.err
}

type fakeEntryResolver struct {
	entryID   string
	entryType string
	err       error
	seen      []string
}

func (f *fakeEntryResolver) ResolveByPhone(workspaceID, phone string) (string, string, error) {
	f.seen = append(f.seen, workspaceID+"/"+phone)
	return f.entryID, f.entryType, f.err
}

type fakeLauncher struct {
	launched []*workflow.WorkflowRun
}

func (f *fakeLauncher) Launch(run *workflow.WorkflowRun, w *workflow.Workflow) {
	f.launched = append(f.launched, run)
}

type fakeDedup struct {
	duplicate bool
	keys      []string
}

func (f *fakeDedup) IsDuplicate(key string) bool {
	f.keys = append(f.keys, key)
	return f.duplicate
}

// fakeSharedState is a minimal cache.SharedState for concurrency-slot tests. Only
// TryIncr/Decr carry behavior; the rest satisfy the interface as no-ops.
type fakeSharedState struct {
	allow   bool
	incrs   int
	decrs   int
	incrErr error
	decrErr error
}

func (f *fakeSharedState) TryIncr(key string, max int64) (bool, error) {
	f.incrs++
	if f.incrErr != nil {
		return false, f.incrErr
	}
	return f.allow, nil
}
func (f *fakeSharedState) Decr(key string) (int64, error) {
	f.decrs++
	return 0, f.decrErr
}
func (f *fakeSharedState) SetNX(string, string, time.Duration) (bool, error) { return true, nil }
func (f *fakeSharedState) SetString(string, string, time.Duration) error     { return nil }
func (f *fakeSharedState) GetString(string) (string, error)                  { return "", nil }
func (f *fakeSharedState) Del(...string) error                               { return nil }
func (f *fakeSharedState) Exists(string) (bool, error)                       { return false, nil }
func (f *fakeSharedState) Incr(string) (int64, error)                        { return 0, nil }
func (f *fakeSharedState) IncrWithTTL(string, time.Duration) (int64, error)  { return 0, nil }
func (f *fakeSharedState) SAdd(string, ...string) error                      { return nil }
func (f *fakeSharedState) SRem(string, ...string) error                      { return nil }
func (f *fakeSharedState) SMembers(string) ([]string, error)                 { return nil, nil }
func (f *fakeSharedState) Publish(string, []byte) error                      { return nil }
func (f *fakeSharedState) Subscribe(context.Context, string, func([]byte))   {}
func (f *fakeSharedState) HSet(string, string, string) error                 { return nil }
func (f *fakeSharedState) HDel(string, string) error                         { return nil }
func (f *fakeSharedState) HGetAll(string) (map[string]string, error)         { return nil, nil }
func (f *fakeSharedState) HIncrBy(string, string, int64) (int64, error)      { return 0, nil }
func (f *fakeSharedState) IncrBy(string, int64) (int64, error)               { return 0, nil }
func (f *fakeSharedState) DecrBy(string, int64) (int64, error)               { return 0, nil }
func (f *fakeSharedState) TryIncrBy(string, int64, int64) (bool, error)      { return true, nil }
func (f *fakeSharedState) Expire(string, time.Duration) (bool, error)        { return true, nil }

func webhookWorkflow() *workflow.Workflow {
	return &workflow.Workflow{
		ID:          "wf1",
		WorkspaceID: "ws1",
		Status:      workflow.WorkflowStatusActive,
		Graph: workflow.Graph{
			Nodes: []workflow.Node{
				{ID: "twh", Type: workflow.NodeTypeTriggerWebhook},
				{ID: "e1", Type: workflow.NodeTypeEnd},
			},
			Edges: []workflow.Edge{{Source: "twh", Target: "e1"}},
		},
	}
}
