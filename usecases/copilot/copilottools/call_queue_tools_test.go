package copilottools

import (
	"context"
	"strings"
	"testing"
	"time"

	"vozko/domain/callrouting"
	"vozko/domain/copilot"
	wd "vozko/domain/workspace/workspace_department"
)

const (
	queueUUID      = "3b2a1c0d-9e8f-4a7b-8c6d-5e4f3a2b1c0d"
	salesDeptUUID  = "0a1b2c3d-4e5f-4a6b-8c7d-8e9f0a1b2c3d"
	anaUUID        = "1c2d3e4f-5a6b-4c7d-8e9f-0a1b2c3d4e5f"
	brunoUUID      = "2d3e4f5a-6b7c-4d8e-9f0a-1b2c3d4e5f6a"
	queueTestClock = "2026-09-30T14:00:00Z"
)

type queueBook struct {
	queues  map[string]*callrouting.Queue
	created *callrouting.Queue
	updated *callrouting.Queue
	deleted string
}

func (b *queueBook) List(_ context.Context, workspaceID string) ([]*callrouting.Queue, error) {
	var out []*callrouting.Queue
	for _, q := range b.queues {
		if q.WorkspaceID == workspaceID {
			out = append(out, q)
		}
	}
	return out, nil
}

func (b *queueBook) Get(_ context.Context, workspaceID, id string) (*callrouting.Queue, error) {
	q, ok := b.queues[id]
	if !ok || q.WorkspaceID != workspaceID {
		return nil, callrouting.ErrQueueNotFound
	}
	clone := *q
	return &clone, nil
}

func (b *queueBook) Create(_ context.Context, q callrouting.Queue) (*callrouting.Queue, error) {
	q.ApplyDefaults()
	if err := q.Validate(); err != nil {
		return nil, err
	}
	q.ID = "new-queue"
	b.created = &q
	return &q, nil
}

func (b *queueBook) Update(_ context.Context, q callrouting.Queue) (*callrouting.Queue, error) {
	if err := q.Validate(); err != nil {
		return nil, err
	}
	b.updated = &q
	return &q, nil
}

func (b *queueBook) Delete(_ context.Context, workspaceID, id string) error {
	if _, err := b.Get(context.Background(), workspaceID, id); err != nil {
		return err
	}
	b.deleted = id
	return nil
}

type liveBoard struct{ live []callrouting.QueueLive }

func (l liveBoard) Live(context.Context, string) ([]callrouting.QueueLive, error) { return l.live, nil }

type presetShelf struct{}

func (presetShelf) Presets() []callrouting.HoldPreset {
	return []callrouting.HoldPreset{{ID: callrouting.DefaultHoldPreset, Name: "Piano calmo", Mood: "calmo"}, {ID: "jazz_leve", Name: "Jazz leve", Mood: "leve"}}
}

type nameBook map[string]string

func (n nameBook) ResolveUsernames(ids []string) map[string]string {
	out := map[string]string{}
	for _, id := range ids {
		if name, ok := n[id]; ok {
			out[id] = name
		}
	}
	return out
}

type departmentBook map[string]*wd.Department

func (d departmentBook) Get(workspaceID, id string) (*wd.Department, error) {
	if dept, ok := d[id]; ok && dept.WorkspaceID == workspaceID {
		return dept, nil
	}
	return nil, wd.ErrDepartmentNotFound
}

func queueFixture() (*queueBook, CallQueueDeps) {
	book := &queueBook{queues: map[string]*callrouting.Queue{queueUUID: {
		ID: queueUUID, WorkspaceID: "ws-1", Name: "Suporte", Strategy: callrouting.StrategyLongestIdle,
		MemberUserIDs: []string{anaUUID}, RingSeconds: 15, MaxWaitSeconds: 300, WrapUpSeconds: 10,
		HoldMusic: callrouting.HoldMusicRef{PresetID: callrouting.DefaultHoldPreset},
	}}}
	clock, _ := time.Parse(time.RFC3339, queueTestClock)
	return book, CallQueueDeps{
		Queues: book,
		Monitor: liveBoard{live: []callrouting.QueueLive{{
			QueueID: queueUUID, Name: "Suporte",
			Waiting: []callrouting.WaitingCaller{{CallID: "c1", Since: clock.Add(-90 * time.Second)}},
			Agents:  []callrouting.AgentStatus{{UserID: anaUUID, Name: "Ana", State: callrouting.AgentOnCall}},
		}}},
		Music:       presetShelf{},
		Names:       nameBook{anaUUID: "Ana", brunoUUID: "Bruno"},
		Departments: departmentBook{salesDeptUUID: {ID: salesDeptUUID, WorkspaceID: "ws-1", Name: "Vendas"}},
		Now:         func() time.Time { return clock },
	}
}

var queueAdminCtx = copilot.Context{WorkspaceID: "ws-1", UserID: "user-1", Departments: &wd.DepartmentFilter{IsOwnerOrAdmin: true}}

func TestQueuesShowTheirSetupAndWhatIsHappeningNow(t *testing.T) {
	_, deps := queueFixture()
	res := NewListCallQueuesTool(deps).Execute(context.Background(), queueAdminCtx, nil)
	if res.Status != copilot.StatusOK {
		t.Fatalf("res = %+v", res)
	}
	data := res.Data.(map[string]interface{})
	queue := data["queues"].([]map[string]interface{})[0]
	if queue["answered_by"] != "Ana" || queue["hold_music"] != "Piano calmo" || queue["distribution"] != "toca para quem está livre há mais tempo" {
		t.Fatalf("queue = %+v", queue)
	}
	now := queue["now"].(map[string]interface{})
	if now["callers_waiting"] != 1 || now["longest_wait_seconds"] != 90 {
		t.Fatalf("now = %+v", now)
	}
	if people := now["people"].([]map[string]string); people[0]["state"] != "em ligação" {
		t.Fatalf("people = %+v", people)
	}
	if presets := data["hold_music_presets"].([]map[string]string); len(presets) != 2 {
		t.Fatalf("presets = %+v", presets)
	}
}

func TestCreatingAQueueNamesWhoAnswersAndChecksItFirst(t *testing.T) {
	book, deps := queueFixture()
	tool := NewCreateCallQueueTool(deps)
	args := map[string]interface{}{"name": "Vendas", "department_id": salesDeptUUID, "ring_seconds": 20}
	if err := tool.(copilot.Validator).Validate(context.Background(), queueAdminCtx, args); err != nil {
		t.Fatalf("validate: %v", err)
	}
	fields := tool.(copilot.Describer).Describe(context.Background(), queueAdminCtx, args)
	if !hasField(fields, "answeredBy", "todo o departamento Vendas") || !hasField(fields, "ringSeconds", "20") {
		t.Fatalf("fields = %+v", fields)
	}
	if res := tool.Execute(context.Background(), queueAdminCtx, args); res.Status != copilot.StatusOK || book.created.DepartmentID != salesDeptUUID || book.created.RingSeconds != 20 {
		t.Fatalf("res = %+v created = %+v", res, book.created)
	}
}

func TestAQueueIsNeverAnsweredByADepartmentAndPeopleAtOnce(t *testing.T) {
	_, deps := queueFixture()
	err := NewCreateCallQueueTool(deps).(copilot.Validator).Validate(context.Background(), queueAdminCtx, map[string]interface{}{
		"name": "Vendas", "department_id": salesDeptUUID, "member_ids": []interface{}{anaUUID},
	})
	if err == nil || !strings.Contains(err.Error(), "não pelos dois") {
		t.Fatalf("err = %v", err)
	}
}

func TestAQueueOutsideTheUsersDepartmentsIsRefused(t *testing.T) {
	_, deps := queueFixture()
	scoped := copilot.Context{WorkspaceID: "ws-1", UserID: "user-1", Departments: &wd.DepartmentFilter{DepartmentIDs: []string{"other"}, WorkspaceHasDepartments: true}}
	err := NewCreateCallQueueTool(deps).(copilot.Validator).Validate(context.Background(), scoped, map[string]interface{}{"name": "Vendas", "department_id": salesDeptUUID})
	if err == nil || !strings.Contains(err.Error(), "fora do seu alcance") {
		t.Fatalf("err = %v", err)
	}
}

func TestUpdatingAQueueKeepsWhatWasNotMentioned(t *testing.T) {
	book, deps := queueFixture()
	res := NewUpdateCallQueueTool(deps).Execute(context.Background(), queueAdminCtx, map[string]interface{}{
		"queue_id": queueUUID, "member_ids": []interface{}{anaUUID, brunoUUID}, "hold_music_preset": "jazz_leve",
	})
	if res.Status != copilot.StatusOK {
		t.Fatalf("res = %+v", res)
	}
	got := book.updated
	if got.Name != "Suporte" || got.RingSeconds != 15 || len(got.MemberUserIDs) != 2 || got.HoldMusic.PresetID != "jazz_leve" {
		t.Fatalf("updated = %+v", got)
	}
	if book.queues[queueUUID].MemberUserIDs[0] != anaUUID || len(book.queues[queueUUID].MemberUserIDs) != 1 {
		t.Fatal("the stored queue changed before the update was saved")
	}
}

func TestSwitchingAQueueToADepartmentDropsThePeopleList(t *testing.T) {
	book, deps := queueFixture()
	res := NewUpdateCallQueueTool(deps).Execute(context.Background(), queueAdminCtx, map[string]interface{}{"queue_id": queueUUID, "department_id": salesDeptUUID})
	if res.Status != copilot.StatusOK || book.updated.DepartmentID != salesDeptUUID || len(book.updated.MemberUserIDs) != 0 {
		t.Fatalf("res = %+v updated = %+v", res, book.updated)
	}
}

func TestAnUnknownHoldMusicIsRefusedBeforeApproval(t *testing.T) {
	_, deps := queueFixture()
	err := NewUpdateCallQueueTool(deps).(copilot.Validator).Validate(context.Background(), queueAdminCtx, map[string]interface{}{"queue_id": queueUUID, "hold_music_preset": "rock"})
	if err == nil || !strings.Contains(err.Error(), "música de espera desconhecida") {
		t.Fatalf("err = %v", err)
	}
}

func TestDeletingAQueueWarnsAboutVoiceFlows(t *testing.T) {
	book, deps := queueFixture()
	tool := NewDeleteCallQueueTool(deps)
	fields := tool.(copilot.Describer).Describe(context.Background(), queueAdminCtx, map[string]interface{}{"queue_id": queueUUID})
	if !hasField(fields, "queue", "Suporte") || !hasField(fields, "risks", "Fluxos de voz que mandam ligações para esta fila deixam de transferir.") {
		t.Fatalf("fields = %+v", fields)
	}
	if res := tool.Execute(context.Background(), queueAdminCtx, map[string]interface{}{"queue_id": queueUUID}); res.Status != copilot.StatusOK || book.deleted != queueUUID {
		t.Fatalf("res = %+v", res)
	}
}

func hasField(fields []copilot.Field, key, value string) bool {
	for _, f := range fields {
		if f.Key == key && f.Value == value {
			return true
		}
	}
	return false
}
