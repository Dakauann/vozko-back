package emailwiring_test

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"

	"vozko/domain/emailaddress"
	em "vozko/domain/emailinbox"
	"vozko/domain/livedecision"
	"vozko/domain/shared"
	conversation_repository "vozko/infra/repositories/conversation"
	stage_repository "vozko/infra/repositories/stage"
	conversation_usecase "vozko/usecases/conversation"
	emuc "vozko/usecases/emailinbox"
	"vozko/usecases/shared/automationrefs"
)

type liveRefs struct {
	mu   sync.Mutex
	refs []livedecision.EntryRef
}

func (l *liveRefs) Queue(ref livedecision.EntryRef) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.refs = append(l.refs, ref)
}

func (l *liveRefs) has(ref livedecision.EntryRef) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, queued := range l.refs {
		if queued == ref {
			return true
		}
	}
	return false
}

type workspaceRefs struct{}

func (workspaceRefs) OwnedBy(context.Context, string, automationrefs.Refs) error { return nil }

func (w *world) awaitingAnalysis(entryID string) bool {
	w.t.Helper()
	awaiting, err := conversation_usecase.NewAnalysisScheduleReader(w.shared).AwaitingAnalysis([]string{entryID}, string(shared.EntryTypeEmail))
	w.must(err)
	return awaiting[entryID]
}

func TestEmailE2E_AReplyOnAConfiguredInboxReachesAnalysisStagingAndMemory(t *testing.T) {
	w := newWorld(t)
	live := &liveRefs{}
	w.automation.forwardTo(conversation_usecase.NewInboundAutomation(nil, nil,
		conversation_usecase.NewAnalysisScheduler(w.shared, live), conversation_repository.NewRepository(w.db)))
	ws := w.sendingTenant("Loja Atendimento", roomyWallet)
	ana := w.seedLeads(ws, 1, "atendimento")[0]
	res, err := w.sendFromWorkflow(ws, ana, "atendimento:"+ana.ID)
	w.must(err)
	replyTo := w.onlySentTo(emailaddress.Address(ana.Email)).Message.ReplyTo[0]
	conv, err := w.inbox.Conversations.FindByID(w.ctx, res.ConversationID)
	w.must(err)
	ref := livedecision.EntryRef{EntryID: conv.ID, EntryType: string(shared.EntryTypeEmail)}

	w.receive(w.replyFrom(ana, replyTo, "Quero saber o prazo de entrega.", 0))
	if w.awaitingAnalysis(conv.ID) || live.has(ref) {
		t.Fatal("an inbox without analysis, staging or memory scheduled the conversation")
	}

	pipeline := uuid.NewString()
	inboxes := emuc.NewInboxes(w.inbox.Inboxes, w.senders, w.domains, workspaceRefs{})
	_, err = inboxes.Update(w.ctx, ws.ID, conv.SenderID, em.InboxSettings{
		PipelineID: &pipeline, EnableAnalysis: true, EnableAutoStaging: true, EnableAutoMemory: true,
	})
	w.must(err)

	w.receive(w.replyFrom(ana, replyTo, "Pode me mandar o boleto?", 0))
	dispatched := w.automation.last()
	if !dispatched.Config.EnableAnalysis || !dispatched.Config.EnableAutoStaging || !dispatched.Config.EnableAutoMemory {
		t.Fatalf("the reply carried %+v, want the inbox switches", dispatched.Config)
	}
	if dispatched.LeadID == nil || *dispatched.LeadID != ana.ID {
		t.Fatalf("the reply carried lead %v, want %s so memory has a subject", dispatched.LeadID, ana.ID)
	}
	if !w.awaitingAnalysis(conv.ID) {
		t.Fatal("the reply did not schedule the conversation analysis")
	}
	if !live.has(ref) {
		t.Fatal("the reply did not queue the live stage decision")
	}

	owners := conversation_usecase.NewCampaignWorkspaceResolver(nil, nil)
	owners.(interface {
		SetEntryOwnerResolver(shared.EntryType, conversation_usecase.EntryOwnerResolver)
	}).SetEntryOwnerResolver(shared.EntryTypeEmail, w.inbox.Conversations)
	container, err := owners.GetEntryCampaignID(conv.ID, string(shared.EntryTypeEmail))
	if err != nil || container != conv.SenderID {
		t.Fatalf("container = %q, %v, want the sender inbox %s for the initial stage", container, err, conv.SenderID)
	}
	funnel, err := stage_repository.ChannelPipelineResolvers(w.db)[shared.EntryTypeEmail].PipelineIDForContainer(w.ctx, container)
	if err != nil || funnel != pipeline {
		t.Fatalf("funnel = %q, %v, want the inbox funnel %s", funnel, err, pipeline)
	}
}
