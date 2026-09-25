package conversation_usecase

import (
	"testing"

	"vozko/domain/conversation"
)

func sentBy(sender conversation.SentBy, messageType conversation.MessageType) *conversation.Message {
	return &conversation.Message{SentBy: sender, MessageType: messageType}
}

func TestSendersAreNamedFromOneBatchedLookupPerKind(t *testing.T) {
	users := &ownerUsersStub{}
	agents := &ownerAgentsStub{}
	svc := ownerFixture(agents, users)

	messages := []*conversation.Message{
		sentBy(conversation.SentByPerson("user-1"), conversation.MessageTypeOperator),
		sentBy(conversation.SentByPerson("user-1"), conversation.MessageTypeMedia),
		sentBy(conversation.SentByAI("agent-1"), conversation.MessageTypeAIResponse),
		sentBy(conversation.SentByAI("agent-1"), conversation.MessageTypeToolCall),
		sentBy(conversation.SentByWorkflow("wf-1"), conversation.MessageTypeMedia),
		sentBy(conversation.SentByCampaign("c-1"), conversation.MessageTypeTemplate),
		sentBy(conversation.SentBySystem(), conversation.MessageTypeSystem),
		sentBy(conversation.SentExternally(), conversation.MessageTypeAudio),
	}
	svc.identifySenders("e-1", groupEntryType, messages)

	want := []string{"ana", "ana", "Sofia", "Sofia", "Triagem", "Campanha", "Sistema", ""}
	for i, name := range want {
		if messages[i].SenderName != name {
			t.Errorf("message %d (%s): name = %q, want %q", i, messages[i].SentBy.Kind(), messages[i].SenderName, name)
		}
	}
	if len(users.asked) != 1 || len(agents.asked) != 1 {
		t.Errorf("users asked %v, agents asked %v; want one distinct id each", users.asked, agents.asked)
	}
}

func TestUnknownActorsKeepTheirKindsGenericName(t *testing.T) {
	svc := ownerFixture(&ownerAgentsStub{}, &ownerUsersStub{})

	messages := []*conversation.Message{
		sentBy(conversation.SentByPerson("gone"), conversation.MessageTypeOperator),
		sentBy(conversation.SentByAI("gone"), conversation.MessageTypeAIResponse),
		sentBy(conversation.SentByWorkflow("gone"), conversation.MessageTypeMedia),
	}
	svc.identifySenders("e-1", groupEntryType, messages)

	for i, name := range []string{"Operador", "Assistente", "Fluxo"} {
		if messages[i].SenderName != name {
			t.Errorf("message %d: name = %q, want %q", i, messages[i].SenderName, name)
		}
	}
}

func TestContactMessagesTakeTheLeadThenTheGroupAuthor(t *testing.T) {
	lead := senderIdentity{Name: "Equipe Vozko", Avatar: "https://cdn.test/group.jpg"}
	authors := map[string]ContactDisplay{"+5511900000001": {Name: "Ana"}}

	fromAna := sentBy(conversation.SentByContact("+5511900000001"), conversation.MessageTypeUserMessage)
	fromAna.From = "+5511900000001"
	fromGroup := sentBy(conversation.SentByContact("+5511988887777"), conversation.MessageTypeAudio)
	fromGroup.From = "+5511988887777"

	for _, msg := range []*conversation.Message{fromAna, fromGroup} {
		nameSender(msg, lead, nil)
		applyAuthor(msg, authors)
	}

	if fromAna.SenderName != "Ana" || fromAna.SenderAvatar != "" {
		t.Errorf("group author = %q %q, want Ana without the group picture", fromAna.SenderName, fromAna.SenderAvatar)
	}
	if fromGroup.SenderName != "Equipe Vozko" || fromGroup.SenderAvatar != "https://cdn.test/group.jpg" {
		t.Errorf("lead = %q %q", fromGroup.SenderName, fromGroup.SenderAvatar)
	}
}

func TestBusinessOnlyPagesSkipTheLeadLookup(t *testing.T) {
	svc := ownerFixture(&ownerAgentsStub{}, &ownerUsersStub{})

	svc.identifySenders("", groupEntryType, []*conversation.Message{
		sentBy(conversation.SentByAI("agent-1"), conversation.MessageTypeAIResponse),
	})
}
