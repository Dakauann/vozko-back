package conversation_usecase

import (
	"errors"
	"testing"

	"vozko/domain/balance"
	"vozko/domain/conversation"
	"vozko/domain/lead"
	whatsappTemplate "vozko/domain/whatsapp/template"
	wce "vozko/domain/whatsapp_campaign_entry"
)

type grantStub bool

func (g grantStub) Execute(string, string) (bool, error) { return bool(g), nil }

type templateRepoStub struct {
	whatsappTemplate.Repository
	tmpl *whatsappTemplate.Template
}

func (r templateRepoStub) FindByID(string) (*whatsappTemplate.Template, error) { return r.tmpl, nil }

type entryRepoStub struct{ wce.Repository }

func (entryRepoStub) FindByID(id string) (*wce.WhatsAppCampaignEntry, error) {
	return &wce.WhatsAppCampaignEntry{ID: id, LeadID: "lead-1"}, nil
}

func (entryRepoStub) GetCampaignForEntry(string) (*wce.EntryCampaignInfo, error) {
	return &wce.EntryCampaignInfo{BusinessPhoneID: "phone-1"}, nil
}

type leadRepoStub struct{ lead.Repository }

func (leadRepoStub) FindByID(string, string) (*lead.Lead, error) {
	return &lead.Lead{ID: "lead-1", Number: "5584994409624"}, nil
}

type clientFactoryStub struct {
	conversation.WhatsAppClientFactory
	waba string
}

func (f clientFactoryStub) WABAIdForPhone(string) (string, error) { return f.waba, nil }

type consumeStub struct {
	balance.ConsumeWhatsappTemplateUseCase
	charged int
}

func (c *consumeStub) Execute(string, string, string) (*balance.Transaction, error) {
	c.charged++
	return nil, errors.New("stop after the charge")
}

func templateSender(granted bool, templateWABA string, consume *consumeStub) *TemplateSenderService {
	tmpl := &whatsappTemplate.Template{ID: "t1", Name: "pedido", Status: whatsappTemplate.TemplateStatusApproved, WABAId: templateWABA, Category: "UTILITY"}
	return NewTemplateSenderService(clientFactoryStub{waba: "waba-1"}, templateRepoStub{tmpl: tmpl}, nil, leadRepoStub{}, entryRepoStub{}, nil, consume, nil, grantStub(granted))
}

func TestSendTemplateRefusesATemplateTheWorkspaceWasNotGranted(t *testing.T) {
	consume := &consumeStub{}
	_, err := templateSender(false, "waba-1", consume).SendTemplate("e1", "whatsapp", "t1", nil, "u1", "ws1")
	if !errors.Is(err, conversation.ErrTemplateNotGranted) || consume.charged != 0 {
		t.Fatalf("err %v charged %d", err, consume.charged)
	}
}

func TestSendTemplateRefusesATemplateOfAnotherWhatsAppAccount(t *testing.T) {
	consume := &consumeStub{}
	_, err := templateSender(true, "waba-2", consume).SendTemplate("e1", "whatsapp", "t1", nil, "u1", "ws1")
	if !errors.Is(err, whatsappTemplate.ErrTemplatePhoneMismatch) || consume.charged != 0 {
		t.Fatalf("err %v charged %d", err, consume.charged)
	}
}

func TestSendTemplateFailsClosedWithoutAGrantCheck(t *testing.T) {
	consume := &consumeStub{}
	tmpl := &whatsappTemplate.Template{ID: "t1", Status: whatsappTemplate.TemplateStatusApproved, WABAId: "waba-1"}
	sender := NewTemplateSenderService(clientFactoryStub{waba: "waba-1"}, templateRepoStub{tmpl: tmpl}, nil, leadRepoStub{}, entryRepoStub{}, nil, consume, nil, nil)
	if _, err := sender.SendTemplate("e1", "whatsapp", "t1", nil, "u1", "ws1"); !errors.Is(err, conversation.ErrTemplateNotGranted) || consume.charged != 0 {
		t.Fatalf("err %v charged %d", err, consume.charged)
	}
}
