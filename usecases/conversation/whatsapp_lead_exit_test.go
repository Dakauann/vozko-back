package conversation_usecase

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/lead"
	"vozko/domain/leadexit"
	"vozko/domain/shared"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
	"vozko/domain/workflow"
)

func sairTap() *conversation.WhatsAppMessage {
	return &conversation.WhatsAppMessage{
		From: "5511987654321", ID: "wamid.in", Timestamp: "1792152000", Type: "text",
		Text:    &conversation.WhatsAppTextMessage{Body: "SAIR"},
		Button:  &conversation.WhatsAppButton{Text: "SAIR", Payload: "vz1:1"},
		Context: &conversation.WhatsAppContext{ID: "wamid.tmpl"},
	}
}

func TestATemplateTapKeepsItsPayloadTextAndContextOnTheStoredMessage(t *testing.T) {
	uc := &handleWhatsAppMessageUseCase{}
	record := uc.inboundTextRecord(sairTap(), inboundTarget{entryID: "entry-1", entryType: shared.EntryTypeWhatsApp}, nil)

	if record.Text != "SAIR" {
		t.Fatalf("the chat must still show the button text: %q", record.Text)
	}
	var facts struct {
		Button  struct{ Payload, Text string }
		Context struct{ ID string }
	}
	if err := json.Unmarshal(record.Metadata, &facts); err != nil {
		t.Fatal(err)
	}
	if facts.Button.Payload != "vz1:1" || facts.Button.Text != "SAIR" || facts.Context.ID != "wamid.tmpl" {
		t.Fatalf("metadata = %s", record.Metadata)
	}
}

func TestAnInteractiveReplyKeepsItsChoiceAndALocationKeepsItsCoordinates(t *testing.T) {
	uc := &handleWhatsAppMessageUseCase{}
	reply := &conversation.WhatsAppMessage{From: "5511987654321", ID: "wamid.r", Text: &conversation.WhatsAppTextMessage{Body: "Sim"},
		Interactive: &conversation.WhatsAppInteractive{Type: "button_reply", ButtonReply: &conversation.WhatsAppButtonReply{ID: "yes", Title: "Sim"}}}
	var facts struct {
		Interactive struct{ Type, ID, Title string }
	}
	if err := json.Unmarshal(uc.inboundTextRecord(reply, inboundTarget{entryID: "e"}, nil).Metadata, &facts); err != nil {
		t.Fatal(err)
	}
	if facts.Interactive.Type != "button_reply" || facts.Interactive.ID != "yes" || facts.Interactive.Title != "Sim" {
		t.Fatalf("facts = %+v", facts)
	}

	located := &conversation.WhatsAppMessage{From: "5511987654321", ID: "wamid.l", Text: &conversation.WhatsAppTextMessage{Body: "aqui"},
		Location: &conversation.WhatsAppLocation{Latitude: -23.5, Longitude: -46.6}}
	stored := &conversation.Message{Channel: conversation.MessageChannelWhatsApp, Metadata: uc.inboundTextRecord(located, inboundTarget{entryID: "e"}, nil).Metadata}
	if loc, ok := conversation.LocationOf(stored); !ok || loc.Point.Lat != -23.5 {
		t.Fatalf("location lost: %+v %v", loc, ok)
	}

	plain := &conversation.WhatsAppMessage{From: "5511987654321", ID: "wamid.p", Text: &conversation.WhatsAppTextMessage{Body: "oi"}}
	if got := uc.inboundTextRecord(plain, inboundTarget{entryID: "e"}, nil).Metadata; got != nil {
		t.Fatalf("a plain text got metadata: %s", got)
	}
}

type capturingExitStep struct {
	inbound leadexit.Inbound
	replier leadexit.Replier
	result  leadexit.Result
	err     error
	reply   string
}

func (s *capturingExitStep) Run(ctx context.Context, in leadexit.Inbound, replier leadexit.Replier) (leadexit.Result, error) {
	s.inbound, s.replier = in, replier
	if s.reply != "" {
		if err := replier.Reply(ctx, s.reply); err != nil {
			return leadexit.Result{}, err
		}
	}
	return s.result, s.err
}

type capturedHistory struct {
	mu      sync.Mutex
	records []conversation.MessageHistoryRecord
}

func (h *capturedHistory) Record(_ context.Context, r conversation.MessageHistoryRecord) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r)
	return nil
}

func exitTarget() whatsAppExitTarget {
	meta, _ := json.Marshal(map[string]string{"template_name": "promo_outubro", "language": "pt_BR"})
	return whatsAppExitTarget{
		agentCtx: &agentContext{
			wcCampaign: &wc.Campaign{ID: "camp-1", WorkspaceID: "ws-1", BusinessPhoneID: "phone-1"},
			wcEntry:    &wce.WhatsAppCampaignEntry{ID: "entry-1"},
		},
		leadRecord: &lead.Lead{ID: "lead-1", WorkspaceID: "ws-1", Number: "5511987654321"},
		entryID:    "entry-1", entryType: shared.EntryTypeWhatsApp, conversationID: "conv-1", businessNumber: "551140001234",
		receivedPhoneID: "phone-1",
		history:         []*conversation.Message{{MessageType: conversation.MessageTypeTemplate, Metadata: meta, To: "5511987654321"}},
	}
}

func TestATemplateTapReachesTheExitStepWithEverythingItNeeds(t *testing.T) {
	step := &capturingExitStep{result: leadexit.Result{OptedOut: true}}
	uc := &handleWhatsAppMessageUseCase{exitStep: step}

	result, err := uc.runLeadExit(context.Background(), sairTap(), exitTarget())

	if err != nil || !result.OptedOut {
		t.Fatalf("result = %+v, %v", result, err)
	}
	in := step.inbound
	if in.WorkspaceID != "ws-1" || in.LeadID != "lead-1" || in.SenderNumber != "5511987654321" || in.EntryID != "entry-1" || in.EntryType != "whatsapp" ||
		in.Channel != "whatsapp" || in.MessageID != "wamid.in" || in.Text != "SAIR" || in.ContextMessageID != "wamid.tmpl" ||
		in.Tap == nil || in.Tap.Payload != "vz1:1" || in.Tap.Text != "SAIR" || !in.ReceivedAt.Equal(time.Unix(1792152000, 0)) {
		t.Fatalf("inbound = %+v", in)
	}
	if in.LastTemplate == nil || in.LastTemplate.Template.Name != "promo_outubro" {
		t.Fatalf("last template = %+v", in.LastTemplate)
	}
}

func TestTheConfirmationGoesOutThroughTheConversationsOwnSender(t *testing.T) {
	client := &fallbackPolicyWhatsAppClient{}
	history := &capturedHistory{}
	step := &capturingExitStep{reply: "Entendido."}
	uc := &handleWhatsAppMessageUseCase{exitStep: step, whatsappClientFactory: stubAudioClientFactory{client: client}, historyManager: history}

	if _, err := uc.runLeadExit(context.Background(), sairTap(), exitTarget()); err != nil {
		t.Fatal(err)
	}

	if len(client.sent) != 1 || client.sent[0].To != "5511987654321" || client.sent[0].Body != "Entendido." {
		t.Fatalf("sent = %+v", client.sent)
	}
	if len(history.records) != 1 {
		t.Fatalf("records = %+v", history.records)
	}
	got := history.records[0]
	if got.SentBy.Kind() != conversation.SenderSystem || got.EntryID != "entry-1" || got.Text != "Entendido." || got.MessageID != "fallback-msg" || got.To != "5511987654321" {
		t.Fatalf("record = %+v", got)
	}
}

type refusingBilling struct {
	conversation.ServiceMessageBilling
}

func (refusingBilling) AllowSend(string) error { return errors.New("no balance") }

func TestAConfirmationTheWorkspaceCannotPayForIsNotSent(t *testing.T) {
	client := &fallbackPolicyWhatsAppClient{}
	step := &capturingExitStep{reply: "Entendido."}
	uc := &handleWhatsAppMessageUseCase{exitStep: step, whatsappClientFactory: stubAudioClientFactory{client: client}, historyManager: &capturedHistory{},
		serviceMessageBilling: refusingBilling{}}

	if _, err := uc.runLeadExit(context.Background(), sairTap(), exitTarget()); err == nil {
		t.Fatal("the step must see the refusal")
	}
	if len(client.sent) != 0 {
		t.Fatalf("sent = %+v", client.sent)
	}
}

func TestWithoutTheExitStepTheInboundFailsClosed(t *testing.T) {
	uc := &handleWhatsAppMessageUseCase{}
	if _, err := uc.runLeadExit(context.Background(), sairTap(), exitTarget()); !errors.Is(err, errLeadExitStepMissing) {
		t.Fatalf("got %v", err)
	}
}

func TestAMessageOutsideAKnownConversationIsNotChecked(t *testing.T) {
	step := &capturingExitStep{}
	uc := &handleWhatsAppMessageUseCase{exitStep: step}
	target := exitTarget()
	target.entryID = ""

	if _, err := uc.runLeadExit(context.Background(), sairTap(), target); err != nil || step.replier != nil {
		t.Fatalf("ran without a conversation: %v", err)
	}
}

func TestAStepFailureFailsTheInboundSoItIsRetried(t *testing.T) {
	uc := &handleWhatsAppMessageUseCase{exitStep: &capturingExitStep{err: errors.New("database down")}}
	if _, err := uc.runLeadExit(context.Background(), sairTap(), exitTarget()); err == nil {
		t.Fatal("want the error")
	}
}

func TestTheTapSelectionWinsAndInteractiveRepliesKeepTheirs(t *testing.T) {
	tap := workflow.TemplateButtonSelection("SAIR", 1, true)
	if got := selectedOptionFromMessage(sairTap(), tap); got != tap {
		t.Fatalf("got %+v", got)
	}
	reply := &conversation.WhatsAppMessage{Interactive: &conversation.WhatsAppInteractive{ButtonReply: &conversation.WhatsAppButtonReply{ID: "yes", Title: "Sim"}}}
	if got := selectedOptionFromMessage(reply, nil); got == nil || got.ID != "yes" || got.Kind != "button" {
		t.Fatalf("interactive = %+v", got)
	}
	list := &conversation.WhatsAppMessage{Interactive: &conversation.WhatsAppInteractive{ListReply: &conversation.WhatsAppListReply{ID: "opt-2", Title: "Plano B"}}}
	if got := selectedOptionFromMessage(list, nil); got == nil || got.ID != "opt-2" || got.Kind != "list" {
		t.Fatalf("list = %+v", got)
	}
	if got := selectedOptionFromMessage(&conversation.WhatsAppMessage{}, nil); got != nil {
		t.Fatalf("plain = %+v", got)
	}
}

type triggerRows struct {
	conversation.MessageRepository
	rows map[string]string
}

func (r triggerRows) GetByWhatsAppMessageID(wamid string) (*conversation.Message, error) {
	if id, ok := r.rows[wamid]; ok {
		return &conversation.Message{ID: id}, nil
	}
	return nil, conversation.ErrMessageNotFound
}

func TestTheWorkflowRunNamesTheStoredInboundRowAndTheTappedButton(t *testing.T) {
	ev := &recordingEvaluator{}
	uc := &handleWhatsAppMessageUseCase{triggerEvaluator: ev, messageRepo: triggerRows{rows: map[string]string{"wamid.in": "row-1"}}}
	actx := exitTarget().agentCtx
	actx.automation = conversation.ChannelAutomation{EnableWorkflow: true}

	uc.fireWorkflowTriggers(actx, uc.messageEvent(actx, "entry-1", shared.EntryTypeWhatsApp, "5511987654321", "wamid.in", "SAIR", "text", "",
		workflow.TemplateButtonSelection("SAIR", 1, true)), nil)

	if len(ev.events) == 0 {
		t.Fatal("no trigger fired")
	}
	data := ev.events[0].Data
	if data[workflow.DataKeyTriggerMessageID] != "row-1" || data[workflow.DataKeySelectedOptionID] != "template_button:1" ||
		data[workflow.DataKeySelectedOptionKind] != workflow.OptionKindTemplateButton || data[workflow.DataKeySelectedOptionTitle] != "SAIR" {
		t.Fatalf("data = %+v", data)
	}
}

func TestTheExitStepNamesTheStoredInboundRowForAWorkflowItStarts(t *testing.T) {
	step := &capturingExitStep{}
	uc := &handleWhatsAppMessageUseCase{exitStep: step, messageRepo: triggerRows{rows: map[string]string{"wamid.in": "row-1"}}}

	if _, err := uc.runLeadExit(context.Background(), sairTap(), exitTarget()); err != nil {
		t.Fatal(err)
	}
	if step.inbound.TriggerMessageID != "row-1" {
		t.Fatalf("trigger message = %q", step.inbound.TriggerMessageID)
	}
}
