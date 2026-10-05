package node_executors

import (
	"context"
	"errors"
	"testing"

	template_domain "vozko/domain/whatsapp/template"
)

type approvedTemplates struct {
	template_domain.Repository
}

func (approvedTemplates) FindByID(id string) (*template_domain.Template, error) {
	return &template_domain.Template{ID: id, Name: "lembrete", Status: template_domain.TemplateStatusApproved, WABAId: "waba-1", Category: "UTILITY"}, nil
}

type recordingTemplateSends struct {
	sends []template_domain.BilledSendInput
	err   error
}

func (r *recordingTemplateSends) Execute(_ context.Context, in template_domain.BilledSendInput) (*template_domain.BilledSendResult, error) {
	r.sends = append(r.sends, in)
	if r.err != nil {
		return nil, r.err
	}
	return &template_domain.BilledSendResult{MessageID: "wamid.t"}, nil
}

func templateSender(sends template_domain.BilledTemplateSendUseCase, history *recordingHistory) *whatsappSender {
	return &whatsappSender{deps: SenderDeps{
		ClientFactory:     stubMediaClientFactory{},
		LeadRepo:          stubMediaLeadRepo{},
		WhatsAppEntryRepo: stubMediaEntryRepo{},
		TemplateRepo:      approvedTemplates{},
		HistoryManager:    history,
		TemplateSends:     sends,
	}}
}

func TestAWorkflowTemplateIsSentThroughTheBilledSend(t *testing.T) {
	sends := &recordingTemplateSends{}
	history := &recordingHistory{}
	run := waMediaRun()

	out, phone, err := templateSender(sends, history).SendTemplate(context.Background(), run, "t1", "", nil, nil)

	if err != nil || out.MessageID != "wamid.t" || phone != "phone-1" {
		t.Fatalf("out %+v phone %q err %v", out, phone, err)
	}
	got := sends.sends[0]
	if len(sends.sends) != 1 || got.WorkspaceID != "ws-1" || got.BusinessPhoneID != "phone-1" || got.TemplateID != "t1" ||
		got.ToNumber != "5511999999999" || got.EntryID != run.EntryID || got.IdempotencyKey == "" {
		t.Fatalf("billed send %+v", sends.sends)
	}
	if len(history.records) != 1 || history.records[0].MessageID != "wamid.t" {
		t.Fatalf("history %+v", history.records)
	}
}

func TestAWorkflowLoopThatSendsTheTemplateAgainIsChargedAgain(t *testing.T) {
	sends := &recordingTemplateSends{}
	sender := templateSender(sends, &recordingHistory{})
	run := waMediaRun()

	for visit := 0; visit < 2; visit++ {
		if _, _, err := sender.SendTemplate(context.Background(), run, "t1", "", nil, nil); err != nil {
			t.Fatal(err)
		}
	}

	if sends.sends[0].IdempotencyKey == sends.sends[1].IdempotencyKey {
		t.Fatalf("both visits of the node used key %q; the second send would go out unpaid", sends.sends[0].IdempotencyKey)
	}
}

func TestAWorkflowTemplateFailsClosedWithoutTheBilledSend(t *testing.T) {
	history := &recordingHistory{}

	if _, _, err := templateSender(nil, history).SendTemplate(context.Background(), waMediaRun(), "t1", "", nil, nil); err == nil || len(history.records) != 0 {
		t.Fatalf("err %v history %d; nothing may go out unbilled", err, len(history.records))
	}
}

func TestARefusedWorkflowTemplateIsNotRecordedAsSent(t *testing.T) {
	sends := &recordingTemplateSends{err: errors.New("meta refused")}
	history := &recordingHistory{}

	if _, _, err := templateSender(sends, history).SendTemplate(context.Background(), waMediaRun(), "t1", "", nil, nil); err == nil || len(history.records) != 0 {
		t.Fatalf("err %v history %d", err, len(history.records))
	}
}
