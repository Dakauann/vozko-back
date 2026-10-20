package webhook

import "testing"

func TestFacebookFieldRouting(t *testing.T) {
	cases := map[string]string{
		"feed":                         TopicFacebookFeed,
		"mention":                      TopicFacebookFeed,
		"videos":                       TopicFacebookFeed,
		"leadgen":                      TopicFacebookLeadgen,
		"messaging_policy_enforcement": TopicFacebookPage,
		"ratings":                      TopicFacebookPage,
		"":                             TopicFacebookPage,
	}
	for field, want := range cases {
		if got := TopicForFacebookField(field); got != want {
			t.Errorf("%q -> %s, want %s", field, got, want)
		}
	}
}

func TestFlowsEventsHaveTheirOwnTopic(t *testing.T) {
	if !IsWhatsAppFlowsField("flows") || IsWhatsAppFlowsField("messages") || TopicWhatsAppFlows != "webhook.whatsapp.flows" {
		t.Fatal("flows events route to webhook.whatsapp.flows and nothing else does")
	}
}
