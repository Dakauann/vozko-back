package whatsapp_campaign_usecase

import (
	"reflect"
	"sync"
	"testing"

	"vozko/domain/balance"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
)

// idempotentConsumeTemplate models the FIXED debit dependency: a charge is keyed
// on the reference it is given and applied at most once per reference. It records
// every reference so tests can assert the consumer keys debits per recipient.
type idempotentConsumeTemplate struct {
	cost    int64
	mu      sync.Mutex
	charged map[string]bool
	refs    []string
}

func (m *idempotentConsumeTemplate) Execute(_ string, reference string, _ string) (*balance.Transaction, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.charged == nil {
		m.charged = map[string]bool{}
	}
	m.refs = append(m.refs, reference)
	m.charged[reference] = true
	return &balance.Transaction{}, nil
}
func (m *idempotentConsumeTemplate) Refund(_ string, _ string, _ string) error { return nil }
func (m *idempotentConsumeTemplate) GetTemplateCostMicros(_ string, _ string) (int64, error) {
	return m.cost, nil
}

func (m *idempotentConsumeTemplate) chargeCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.charged)
}
func (m *idempotentConsumeTemplate) distinctRefs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.charged))
	for r := range m.charged {
		out = append(out, r)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}

// CORRECT BEHAVIOR (red until the fix): two recipients of a campaign, each
// redelivered once, must be charged exactly once each. Today every recipient is
// debited under one shared CampaignID reference, so the idempotent dependency
// collapses them to a single charge — this fails until debits are keyed per
// recipient.
func TestWhatsAppRedelivery_ChargesEachRecipientExactlyOnce(t *testing.T) {
	h := newTestHarness()
	h.consumer.WhatsAppClientFactory = &mockWhatsAppClientFactory{client: h.waClient, returnReal: true}
	mock := &idempotentConsumeTemplate{cost: 500}
	h.consumer.ConsumeWhatsappTemplate = mock
	h.cachedBalanceChecker.balanceMicros = 1_000_000

	campID := "wa-idem-redelivery"
	topic := makeTopic(campID)
	h.campaignRepo.Create(&wc.Campaign{
		ID: campID, Status: wc.CampaignStatusRunning, WorkspaceID: "ws-1", BusinessPhoneID: "bp-1", TemplateID: "tmpl-1",
	})
	h.templateRepo.templates["tmpl-1"] = approvedMarketingTemplate("tmpl-1")
	h.entryRepo.entries["entry-1"] = &wce.WhatsAppCampaignEntry{ID: "entry-1", LeadID: "lead-1"}
	h.entryRepo.entries["entry-2"] = &wce.WhatsAppCampaignEntry{ID: "entry-2", LeadID: "lead-2"}
	_ = h.consumer.SubscribeToCampaign(campID)

	p1 := makePayload(campID, "entry-1", "5584999990001")
	p2 := makePayload(campID, "entry-2", "5584999990002")
	h.queueSub.deliver(topic, p1)
	h.queueSub.deliver(topic, p2)
	h.queueSub.deliver(topic, p1) // redelivery of entry-1
	h.queueSub.deliver(topic, p2) // redelivery of entry-2

	if got := mock.chargeCount(); got != 2 {
		t.Fatalf("entry-1 and entry-2 must each be charged exactly once across redeliveries; got %d distinct charges (references=%v)", got, mock.distinctRefs())
	}
}

// CORRECT BEHAVIOR (red until the fix): each recipient must be debited under its
// own EntryID. Today every recipient is debited under the CampaignID.
func TestWhatsAppDebit_UsesPerEntryReference(t *testing.T) {
	h := newTestHarness()
	h.consumer.WhatsAppClientFactory = &mockWhatsAppClientFactory{client: h.waClient, returnReal: true}
	mock := &idempotentConsumeTemplate{cost: 500}
	h.consumer.ConsumeWhatsappTemplate = mock
	h.cachedBalanceChecker.balanceMicros = 1_000_000

	campID := "wa-perentry-ref"
	topic := makeTopic(campID)
	h.campaignRepo.Create(&wc.Campaign{
		ID: campID, Status: wc.CampaignStatusRunning, WorkspaceID: "ws-1", BusinessPhoneID: "bp-1", TemplateID: "tmpl-1",
	})
	h.templateRepo.templates["tmpl-1"] = approvedMarketingTemplate("tmpl-1")
	h.entryRepo.entries["entry-1"] = &wce.WhatsAppCampaignEntry{ID: "entry-1", LeadID: "lead-1"}
	h.entryRepo.entries["entry-2"] = &wce.WhatsAppCampaignEntry{ID: "entry-2", LeadID: "lead-2"}
	_ = h.consumer.SubscribeToCampaign(campID)

	h.queueSub.deliver(topic, makePayload(campID, "entry-1", "5584999990001"))
	h.queueSub.deliver(topic, makePayload(campID, "entry-2", "5584999990002"))

	got := mock.distinctRefs()
	want := []string{"entry-1", "entry-2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("each recipient must be debited under its own EntryID; got references %v, want %v", got, want)
	}
}
