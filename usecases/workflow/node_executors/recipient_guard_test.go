package node_executors

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"vozko/domain/conversation"
	lead_domain "vozko/domain/lead"
	"vozko/domain/shared"
	wo "vozko/domain/whatsapp_outreach"
	"vozko/domain/workflow"
)

type countingWhatsAppClient struct {
	conversation.WhatsAppClient
	sends int
}

func (c *countingWhatsAppClient) SendTextMessage(context.Context, conversation.SendTextMessageInput) (*conversation.SendTextMessageOutput, error) {
	c.sends++
	return &conversation.SendTextMessageOutput{MessageID: "wamid.text"}, nil
}

func (c *countingWhatsAppClient) SendButtonMessage(context.Context, conversation.SendButtonMessageInput) (*conversation.SendTextMessageOutput, error) {
	c.sends++
	return &conversation.SendTextMessageOutput{MessageID: "wamid.buttons"}, nil
}

func (c *countingWhatsAppClient) SendListMessage(context.Context, conversation.SendListMessageInput) (*conversation.SendTextMessageOutput, error) {
	c.sends++
	return &conversation.SendTextMessageOutput{MessageID: "wamid.list"}, nil
}

func (c *countingWhatsAppClient) SendImageMessage(context.Context, conversation.SendImageMessageInput) (*conversation.SendTextMessageOutput, error) {
	c.sends++
	return &conversation.SendTextMessageOutput{MessageID: "wamid.image"}, nil
}

func (c *countingWhatsAppClient) UploadMedia(context.Context, []byte, string, string) (string, error) {
	return "wa-media-1", nil
}

func guardedSender(contact *lead_domain.Lead, client *countingWhatsAppClient, history *recordingHistory) *whatsappSender {
	return &whatsappSender{deps: SenderDeps{
		ClientFactory:     stubMediaClientFactory{client: client},
		LeadRepo:          &knownLeadRepo{byID: contact},
		WhatsAppEntryRepo: stubMediaEntryRepo{},
		HistoryManager:    history,
		MediaFetcher:      unguardedTestFetcher(),
	}}
}

func everyWhatsAppSend(t *testing.T) map[string]func(*whatsappSender) error {
	server := mediaServer(t, "image/png", []byte("\x89PNG\r\n\x1a\nimage"))
	return map[string]func(*whatsappSender) error{
		"text": func(s *whatsappSender) error {
			_, _, err := s.SendText(context.Background(), waMediaRun(), "Oi", conversation.MessageTypeAIResponse)
			return err
		},
		"buttons": func(s *whatsappSender) error {
			_, _, err := s.SendButtons(context.Background(), waMediaRun(), "Escolha", []conversation.InteractiveButton{{ID: "1", Title: "Sim"}})
			return err
		},
		"buttons with input": func(s *whatsappSender) error {
			_, _, err := s.SendButtonsWithInput(context.Background(), waMediaRun(), conversation.SendButtonMessageInput{BodyText: "Escolha"})
			return err
		},
		"list": func(s *whatsappSender) error {
			_, _, err := s.SendListWithInput(context.Background(), waMediaRun(), conversation.SendListMessageInput{BodyText: "Escolha"})
			return err
		},
		"media": func(s *whatsappSender) error {
			_, _, err := s.SendMedia(context.Background(), waMediaRun(), server.URL+"/foto.png", "")
			return err
		},
	}
}

func TestEveryWorkflowWhatsAppSendRefusesALeadWhoCannotReceiveIt(t *testing.T) {
	cases := map[string]struct {
		contact *lead_domain.Lead
		want    error
		code    string
	}{
		"opted out": {optedOutLead(), wo.ErrLeadOptedOut, "lead_opted_out"},
		"blocked":   {blockedLead(), wo.ErrLeadBlocked, "lead_blocked"},
	}
	for name, tc := range cases {
		for send, call := range everyWhatsAppSend(t) {
			t.Run(name+" "+send, func(t *testing.T) {
				client, history := &countingWhatsAppClient{}, &recordingHistory{}

				err := call(guardedSender(tc.contact, client, history))

				if !errors.Is(err, errRecipientRefused) || !errors.Is(err, tc.want) || !strings.Contains(err.Error(), tc.code) {
					t.Fatalf("err = %v, want a refusal naming %s for the run log", err, tc.code)
				}
				if client.sends != 0 || len(history.records) != 0 {
					t.Fatalf("sent %d, recorded %d; nothing may go out", client.sends, len(history.records))
				}
			})
		}
	}
}

func TestEveryWorkflowWhatsAppSendStillReachesAReachableLead(t *testing.T) {
	for send, call := range everyWhatsAppSend(t) {
		t.Run(send, func(t *testing.T) {
			client := &countingWhatsAppClient{}
			if err := call(guardedSender(reachableLead(), client, &recordingHistory{})); err != nil || client.sends != 1 {
				t.Fatalf("err = %v sends = %d, want one send", err, client.sends)
			}
		})
	}
}

type entryLeads struct {
	leadID string
	err    error
	asked  []shared.EntryRef
}

func (e *entryLeads) LeadOfEntry(_ context.Context, _ string, ref shared.EntryRef) (string, error) {
	e.asked = append(e.asked, ref)
	if e.err != nil {
		return "", e.err
	}
	if e.leadID == "" {
		return "", lead_domain.ErrLeadNotFound
	}
	return e.leadID, nil
}

func igSender(adapter *fakeAdapter, entries *entryLeads, leads *knownLeadRepo) *channelSender {
	return &channelSender{
		adapters:   conversation.NewAdapterRegistry(adapter),
		history:    &recordingHistory{},
		entryLeads: entries,
		leads:      leads,
	}
}

func everyChannelSend() map[string]func(*channelSender) error {
	return map[string]func(*channelSender) error{
		"text": func(s *channelSender) error {
			_, err := s.SendText(context.Background(), igRun(), "Oi", conversation.MessageTypeAIResponse)
			return err
		},
		"media": func(s *channelSender) error {
			_, err := s.SendMedia(context.Background(), igRun(), "https://cdn/foto.png", "")
			return err
		},
		"segments": func(s *channelSender) error {
			_, err := s.SendSegments(context.Background(), igRun(), []string{"Oi", "Tudo bem?"}, func(time.Duration) {})
			return err
		},
	}
}

func TestEveryOtherChannelSendRefusesAnOptedOutOrBlockedLead(t *testing.T) {
	for name, contact := range map[string]*lead_domain.Lead{"opted out": optedOutLead(), "blocked": blockedLead()} {
		for send, call := range everyChannelSend() {
			t.Run(name+" "+send, func(t *testing.T) {
				adapter := &fakeAdapter{entryType: shared.EntryTypeInstagram, windowOpen: true}
				entries := &entryLeads{leadID: "lead-1"}

				err := call(igSender(adapter, entries, &knownLeadRepo{byID: contact}))

				if !errors.Is(err, errRecipientRefused) || adapter.sendCalls != 0 {
					t.Fatalf("err = %v sends = %d, want a refusal and nothing sent", err, adapter.sendCalls)
				}
				if len(entries.asked) != 1 || entries.asked[0] != (shared.EntryRef{EntryID: "conv-1", EntryType: shared.EntryTypeInstagram}) {
					t.Fatalf("asked = %+v", entries.asked)
				}
			})
		}
	}
}

func TestAChannelConversationWithoutALeadOrWithAReachableOneIsSent(t *testing.T) {
	for name, entries := range map[string]*entryLeads{"no lead": {}, "a reachable lead": {leadID: "lead-1"}} {
		t.Run(name, func(t *testing.T) {
			adapter := &fakeAdapter{entryType: shared.EntryTypeInstagram, windowOpen: true}
			if _, err := igSender(adapter, entries, &knownLeadRepo{byID: &lead_domain.Lead{ID: "lead-1", Name: "Ana"}}).
				SendText(context.Background(), igRun(), "Oi", conversation.MessageTypeAIResponse); err != nil || adapter.sendCalls != 1 {
				t.Fatalf("err = %v sends = %d", err, adapter.sendCalls)
			}
		})
	}
}

func TestAChannelSendFailsClosedWhenTheLeadCannotBeChecked(t *testing.T) {
	cases := map[string]*channelSender{
		"the entry lookup fails": igSender(&fakeAdapter{entryType: shared.EntryTypeInstagram, windowOpen: true}, &entryLeads{err: errors.New("db down")}, &knownLeadRepo{}),
		"the lead read fails":    igSender(&fakeAdapter{entryType: shared.EntryTypeInstagram, windowOpen: true}, &entryLeads{leadID: "lead-1"}, &knownLeadRepo{err: errors.New("db down")}),
		"no entry lookup":        {adapters: conversation.NewAdapterRegistry(&fakeAdapter{entryType: shared.EntryTypeInstagram, windowOpen: true}), leads: &knownLeadRepo{}},
		"no lead reader":         {adapters: conversation.NewAdapterRegistry(&fakeAdapter{entryType: shared.EntryTypeInstagram, windowOpen: true}), entryLeads: &entryLeads{}},
	}
	for name, sender := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := sender.SendText(context.Background(), igRun(), "Oi", conversation.MessageTypeAIResponse); !errors.Is(err, errRecipientRefused) {
				t.Fatalf("err = %v, want a refusal", err)
			}
		})
	}
}

func TestASendTextStepToAnOptedOutLeadFails(t *testing.T) {
	client := &countingWhatsAppClient{}
	exec := &sendTextExecutor{sender: newChannelSenderFromWhatsApp(guardedSender(optedOutLead(), client, &recordingHistory{}))}
	node := &workflow.Node{ID: "n1", Type: workflow.NodeTypeActionSendText, Config: map[string]interface{}{"text": "Promoção!"}}
	state := workflow.NewRunState()

	_, err := exec.Execute(&workflow.NodeContext{Node: node, Run: waMediaRun(), State: &state})

	if !errors.Is(err, wo.ErrLeadOptedOut) || client.sends != 0 {
		t.Fatalf("err = %v sends = %d, want the step to fail with lead_opted_out", err, client.sends)
	}
}

func TestATemplateOrMediaStepToAnOptedOutLeadFailsInsteadOfReportingNotSent(t *testing.T) {
	sends := &recordingTemplateSends{}
	tmpl := &sendTemplateExecutor{sender: senderWithLeads(&knownLeadRepo{byID: optedOutLead()}, sends, &recordingHistory{})}
	state := workflow.NewRunState()
	node := &workflow.Node{ID: "n2", Type: workflow.NodeTypeActionSendTemplate, Config: map[string]interface{}{"template_id": "t1"}}
	if _, err := tmpl.Execute(&workflow.NodeContext{Node: node, Run: waMediaRun(), State: &state}); !errors.Is(err, wo.ErrLeadOptedOut) || len(sends.sends) != 0 {
		t.Fatalf("template step err = %v sends = %d", err, len(sends.sends))
	}

	client := &countingWhatsAppClient{}
	guarded := guardedSender(optedOutLead(), client, &recordingHistory{})
	media := &sendMediaExecutor{sender: guarded, channel: newChannelSenderFromWhatsApp(guarded)}
	mediaNode := &workflow.Node{ID: "n3", Type: workflow.NodeTypeActionSendMedia, Config: map[string]interface{}{"media_url": "https://cdn/foto.png"}}
	if _, err := media.Execute(&workflow.NodeContext{Node: mediaNode, Run: waMediaRun(), State: &state}); !errors.Is(err, wo.ErrLeadOptedOut) || client.sends != 0 {
		t.Fatalf("media step err = %v sends = %d", err, client.sends)
	}
}
