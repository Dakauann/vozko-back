package emailcampaign_usecase

import (
	"context"
	"strings"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/emailsending"
	"vozko/usecases/campaignqueue"
)

func sentMessageOf(call gatewayCall, id string) emailsending.Message {
	return emailsending.Message{
		ID: id, WorkspaceID: call.req.WorkspaceID, RunKind: call.req.RunKind, RunID: call.req.RunID,
		RecipientID: call.req.RecipientID, Address: call.req.To, Status: emailsending.MessageSent,
	}
}

func TestACampaignSendIsRecalledExactlyAsItWasComposed(t *testing.T) {
	w, c := launched(t, nil, person{email: "a@x.com", subscribed: true})
	rc := pendingOf(w, c, "a@x.com")
	if res := w.svc.handle(campaignqueue.Message{CampaignID: c.ID, EntryID: rc.ID}); res.Outcome != campaignqueue.OutcomeDone {
		t.Fatalf("outcome = %+v", res)
	}
	call := w.gateway.calls[0]
	original, found, err := w.svc.Original(context.Background(), sentMessageOf(call, "msg-1"))
	if err != nil || !found {
		t.Fatalf("recall = %v %v", found, err)
	}
	if original.Subject != call.content.Subject || original.Text == "" || strings.Contains(original.Text, fakeUnsubscribeURL) {
		t.Fatalf("recalled %q / %q, the send composed %q / %q", original.Subject, original.Text, call.content.Subject, call.content.Text)
	}
	if !strings.Contains(call.content.Text, fakeUnsubscribeURL) {
		t.Fatal("the sent email lost its unsubscribe link")
	}
	if original.SentBy.Kind() != conversation.SenderCampaign || original.SentBy != conversation.SentByCampaign(c.ID) ||
		original.CampaignID != c.ID || original.CampaignName != "Black Friday" {
		t.Fatalf("original = %+v", original)
	}
}

func TestATestSendIsRecalledWithTheTestMergeValues(t *testing.T) {
	w := newWorld(t)
	c := w.draft(nil)
	if _, err := w.svc.TestSend(context.Background(), w.actor(), c.ID, []string{"a@loja.com.br"}); err != nil {
		t.Fatal(err)
	}
	call := w.gateway.calls[0]
	original, found, err := w.svc.Original(context.Background(), sentMessageOf(call, "msg-1"))
	if err != nil || !found {
		t.Fatalf("recall = %v %v", found, err)
	}
	if original.Subject != call.content.Subject || original.Text == "" || strings.Contains(original.Text, fakeUnsubscribeURL) || original.CampaignID != c.ID {
		t.Fatalf("recalled %+v, the test composed %+v", original, call.content)
	}
}

func TestOnlyCampaignAndTestSendsOfTheSameWorkspaceAreRecalled(t *testing.T) {
	w, c := launched(t, nil, person{email: "a@x.com", subscribed: true})
	rc := pendingOf(w, c, "a@x.com")
	w.svc.handle(campaignqueue.Message{CampaignID: c.ID, EntryID: rc.ID})
	sent := sentMessageOf(w.gateway.calls[0], "msg-1")
	cases := []struct {
		name   string
		mutate func(*emailsending.Message)
	}{
		{"a one-to-one send keeps its own transcript", func(m *emailsending.Message) { m.RunKind = emailsending.RunTriggered }},
		{"an inbox reply keeps its own transcript", func(m *emailsending.Message) { m.RunKind = emailsending.RunReply }},
		{"another workspace never reads this campaign", func(m *emailsending.Message) { m.WorkspaceID = "99999999-0000-0000-0000-000000000009" }},
		{"a deleted campaign", func(m *emailsending.Message) { m.RunID = "70000000-0000-0000-0000-000000000007" }},
		{"a recipient that is gone", func(m *emailsending.Message) { m.RecipientID = "80000000-0000-0000-0000-000000000008" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := sent
			tc.mutate(&m)
			if _, found, err := w.svc.Original(context.Background(), m); err != nil || found {
				t.Fatalf("recall = %v %v, want nothing to recall", found, err)
			}
		})
	}
}

func TestTheViewerOriginalCarriesNoWorkingUnsubscribeWhileTheSentEmailDoes(t *testing.T) {
	w, c := launched(t, nil, person{email: "a@x.com", subscribed: true})
	rc := pendingOf(w, c, "a@x.com")
	w.svc.handle(campaignqueue.Message{CampaignID: c.ID, EntryID: rc.ID})
	call := w.gateway.calls[0]
	sent := sentMessageOf(call, "msg-1")
	original, found, err := w.svc.Original(context.Background(), sent)
	if err != nil || !found {
		t.Fatalf("recall = %v %v", found, err)
	}
	viewed, err := w.svc.OriginalHTML(context.Background(), sent, *original.Merge)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(call.content.HTML, fakeUnsubscribeURL) {
		t.Fatal("the sent email lost its unsubscribe link")
	}
	if strings.Contains(viewed, fakeUnsubscribeURL) || !strings.Contains(viewed, `href="#"`) {
		t.Fatalf("the viewer original still unsubscribes the customer: %s", viewed)
	}
	if viewed != strings.ReplaceAll(call.content.HTML, fakeUnsubscribeURL, "#") {
		t.Fatal("the viewer original differs from the sent email beyond the unsubscribe link")
	}
}
