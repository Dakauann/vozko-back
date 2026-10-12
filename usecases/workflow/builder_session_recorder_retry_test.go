package workflow_usecase

import (
	"context"
	"testing"

	"vozko/domain/workflow"
)

type recordedSessions struct {
	messages []workflow.BuilderMessage
}

func (r *recordedSessions) CreateSession(context.Context, *workflow.BuilderSession) error { return nil }
func (r *recordedSessions) UpdateSession(context.Context, *workflow.BuilderSession) error { return nil }
func (r *recordedSessions) AppendMessage(_ context.Context, _ string, m workflow.BuilderMessage) error {
	r.messages = append(r.messages, m)
	return nil
}
func (r *recordedSessions) ListByWorkflow(context.Context, string, string) ([]*workflow.BuilderSession, error) {
	return nil, nil
}
func (r *recordedSessions) ListByWorkspace(context.Context, string, int) ([]*workflow.BuilderSession, error) {
	return nil, nil
}
func (r *recordedSessions) GetByID(context.Context, string, string) (*workflow.BuilderSession, error) {
	return nil, nil
}

func TestTheBuilderTranscriptDropsTheTextOfAFailedAttempt(t *testing.T) {
	repo := &recordedSessions{}
	rec := NewBuilderSessionRecorder(repo, "ws-1", "wf-1")
	rec.Outbound([]byte(`{"type":"assistant_delta","payload":{"text":"Vou ligar "}}`))
	rec.Outbound([]byte(`{"type":"assistant_retry","payload":{"attempt":1,"of":3,"text":"Vou ligar "}}`))
	rec.Outbound([]byte(`{"type":"assistant_delta","payload":{"text":"Vou ligar os nós."}}`))
	rec.Outbound([]byte(`{"type":"assistant_done","payload":{"tools":0}}`))

	if len(repo.messages) != 1 || repo.messages[0].Text != "Vou ligar os nós." {
		t.Fatalf("transcript = %+v", repo.messages)
	}
}
