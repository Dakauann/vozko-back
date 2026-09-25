package scheduled_message_usecase

import (
	"context"
	"sync"
	"time"

	sm "vozko/domain/scheduled_message"
	"vozko/domain/shared"
	"vozko/domain/workspace"
	wo "vozko/domain/whatsapp_outreach"
)

type fakeTemplates struct {
	mu       sync.Mutex
	checks   []wo.ConversationTemplateInput
	sends    []wo.ConversationTemplateInput
	checkErr error
	sendErr  error
}

func (f *fakeTemplates) Check(_ context.Context, in wo.ConversationTemplateInput) (*wo.ConversationTemplate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checks = append(f.checks, in)
	if f.checkErr != nil {
		return nil, f.checkErr
	}
	return &wo.ConversationTemplate{Name: "follow_up", Preview: "Oi Ana, tudo certo?"}, nil
}

func (f *fakeTemplates) Send(_ context.Context, in wo.ConversationTemplateInput) (*wo.SentConversationTemplate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sends = append(f.sends, in)
	if f.sendErr != nil {
		return nil, f.sendErr
	}
	return &wo.SentConversationTemplate{AttemptID: "att-1", MessageID: "wamid.1", Recorded: true}, nil
}

func (f *fakeTemplates) sendCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sends)
}

type fakePermissions struct {
	mu      sync.Mutex
	err     error
	checked []string
}

func (p *fakePermissions) Execute(userID, workspaceID string, resource workspace.Resource, action workspace.Action) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.checked = append(p.checked, userID+"|"+workspaceID+"|"+string(resource)+":"+string(action))
	return p.err
}

func (p *fakePermissions) calls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.checked)
}

func templateMessage(id string, at time.Time) *sm.ScheduledMessage {
	return &sm.ScheduledMessage{
		ID:              id,
		WorkspaceID:     "ws-1",
		EntryID:         "entry-1",
		EntryType:       shared.EntryTypeWhatsApp,
		CreatedByUserID: "user-1",
		Kind:            sm.KindTemplate,
		Template:        &sm.TemplateContent{ID: "tpl-1", Name: "follow_up", BodyParams: []string{"Ana"}},
		ScheduledAt:     at,
		Status:          sm.StatusPending,
	}
}
