package leadexit_usecase

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/whatsapp/template"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
)

type fakeEntries map[string]*wce.WhatsAppCampaignEntry

func (f fakeEntries) FindByID(id string) (*wce.WhatsAppCampaignEntry, error) {
	if e, ok := f[id]; ok {
		return e, nil
	}
	return nil, wce.ErrEntryNotFound
}

func (f fakeEntries) FindByMessageID(messageID string) (*wce.WhatsAppCampaignEntry, error) {
	for _, e := range f {
		if e.MessageID == messageID {
			return e, nil
		}
	}
	return nil, wce.ErrEntryNotFound
}

type fakeCampaigns map[string]*wc.Campaign

func (f fakeCampaigns) FindByID(id string) (*wc.Campaign, error) {
	if c, ok := f[id]; ok {
		return c, nil
	}
	return nil, wc.ErrCampaignNotFound
}

type fakeMessages struct {
	byWamid map[string]*conversation.Message
	err     error
}

func (f fakeMessages) GetByWhatsAppMessageID(wamid string) (*conversation.Message, error) {
	if f.err != nil {
		return nil, f.err
	}
	if m, ok := f.byWamid[wamid]; ok {
		return m, nil
	}
	return nil, conversation.ErrMessageNotFound
}

type fakeTemplateNames map[string]*template.Template

func (f fakeTemplateNames) FindByID(id string) (*template.Template, error) {
	if found, ok := f[id]; ok {
		return found, nil
	}
	return nil, template.ErrTemplateNotFound
}

func sentTemplatesFixture(t *testing.T, messages fakeMessages) *SentTemplates {
	t.Helper()
	resolver, err := NewSentTemplates(SentTemplatesDeps{
		Entries: fakeEntries{
			"entry-1": {ID: "entry-1", CampaignID: "camp-1", LeadID: "lead-1", MessageID: "wamid.campaign"},
			"entry-2": {ID: "entry-2", CampaignID: "receptive-1", LeadID: "lead-1"},
		},
		Campaigns: fakeCampaigns{
			"camp-1":      {ID: "camp-1", WorkspaceID: workspaceA, TemplateID: "tmpl-1"},
			"receptive-1": {ID: "receptive-1", WorkspaceID: workspaceA},
		},
		Messages:  messages,
		Leads:     newMemLeads(storedLead("lead-1", leadNumber)),
		Templates: fakeTemplateNames{"tmpl-1": {ID: "tmpl-1", Name: "promo_outubro", Language: "pt_BR"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return resolver
}

func storedTemplate(entryID, wamid string, sentBy conversation.SentBy) *conversation.Message {
	meta, _ := json.Marshal(map[string]string{"template_name": "promo_outubro", "language": "pt_BR"})
	return &conversation.Message{EntryID: entryID, MessageType: conversation.MessageTypeTemplate, To: leadNumber, Metadata: meta, WhatsAppMessageID: &wamid, SentBy: sentBy}
}

func TestACampaignTemplateResolvesThroughItsStoredMessage(t *testing.T) {
	resolver := sentTemplatesFixture(t, fakeMessages{byWamid: map[string]*conversation.Message{
		"wamid.campaign": storedTemplate("entry-1", "wamid.campaign", conversation.SentByCampaign("camp-1")),
	}})

	sent, err := resolver.SentTemplate(context.Background(), "wamid.campaign")

	if err != nil || sent == nil {
		t.Fatalf("got %+v, %v", sent, err)
	}
	if sent.WorkspaceID != workspaceA || sent.TemplateID != "tmpl-1" || sent.CampaignID != "camp-1" || sent.Template != promoRef ||
		sent.RecipientNumber != leadNumber || sent.MessageID != "wamid.campaign" {
		t.Fatalf("sent = %+v", sent)
	}
}

func TestAConversationTemplateResolvesByNameAndLanguage(t *testing.T) {
	resolver := sentTemplatesFixture(t, fakeMessages{byWamid: map[string]*conversation.Message{
		"wamid.person": storedTemplate("entry-2", "wamid.person", conversation.SentByPerson("user-1")),
	}})

	sent, err := resolver.SentTemplate(context.Background(), "wamid.person")

	if err != nil || sent == nil || sent.WorkspaceID != workspaceA || sent.TemplateID != "" || sent.CampaignID != "" || sent.Template != promoRef {
		t.Fatalf("got %+v, %v", sent, err)
	}
}

func TestACampaignEntryWithoutItsStoredMessageStillResolves(t *testing.T) {
	resolver := sentTemplatesFixture(t, fakeMessages{})

	sent, err := resolver.SentTemplate(context.Background(), "wamid.campaign")

	if err != nil || sent == nil || sent.WorkspaceID != workspaceA || sent.TemplateID != "tmpl-1" || sent.CampaignID != "camp-1" ||
		sent.RecipientNumber != leadNumber || !sent.Template.Empty() {
		t.Fatalf("got %+v, %v", sent, err)
	}
}

func TestAQuotedTextOrAnUnknownMessageIsNoTemplate(t *testing.T) {
	wamid := "wamid.text"
	resolver := sentTemplatesFixture(t, fakeMessages{byWamid: map[string]*conversation.Message{
		wamid: {EntryID: "entry-2", MessageType: conversation.MessageTypeAIResponse, Text: "oi", WhatsAppMessageID: &wamid},
	}})
	for _, id := range []string{wamid, "wamid.unknown"} {
		if sent, err := resolver.SentTemplate(context.Background(), id); err != nil || sent != nil {
			t.Fatalf("%s: got %+v, %v", id, sent, err)
		}
	}
}

func TestAStorageFailureResolvingTheRepliedMessageIsAnError(t *testing.T) {
	resolver := sentTemplatesFixture(t, fakeMessages{err: errors.New("connection reset")})
	if _, err := resolver.SentTemplate(context.Background(), "wamid.campaign"); err == nil {
		t.Fatal("want the error")
	}
}

func TestTheSentTemplateResolverNeedsEveryRepository(t *testing.T) {
	if _, err := NewSentTemplates(SentTemplatesDeps{Leads: newMemLeads()}); err == nil {
		t.Fatal("want a refusal")
	}
}
