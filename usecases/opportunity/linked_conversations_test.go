package opportunity_usecase

import (
	"errors"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/opportunity"
)

type conversationsByID map[string]*conversation.InboxEntry

func (c conversationsByID) BuildInboxEntry(entryID, entryType string) (*conversation.InboxEntry, error) {
	entry, ok := c[entryType+":"+entryID]
	if !ok {
		return nil, errors.New("not found")
	}
	return entry, nil
}

func linkedService(links []opportunity.ConversationLink, conversations ConversationDirectory) *Service {
	service := newService(newFakeOppRepo())
	service.links = &fakeLinks{byOpportunity: links}
	service.conversations = conversations
	return service
}

func TestLinkedConversationsShowTheContactTheyBelongTo(t *testing.T) {
	service := linkedService(
		[]opportunity.ConversationLink{{OpportunityID: "o1", EntryID: "e1", EntryType: "unofficial_whatsapp"}},
		conversationsByID{"unofficial_whatsapp:e1": {LeadName: "DakauannT", LeadNumber: "558494409624"}},
	)

	linked, err := service.ListConversations("ws1", "o1")

	if err != nil || len(linked) != 1 {
		t.Fatalf("linked = %+v, err = %v", linked, err)
	}
	if linked[0].LeadName != "DakauannT" || linked[0].LeadNumber != "558494409624" || linked[0].EntryID != "e1" {
		t.Fatalf("linked = %+v", linked[0])
	}
}

func TestAConversationThatCannotBeReadIsStillListed(t *testing.T) {
	service := linkedService(
		[]opportunity.ConversationLink{{OpportunityID: "o1", EntryID: "gone", EntryType: "whatsapp"}},
		conversationsByID{},
	)

	linked, err := service.ListConversations("ws1", "o1")

	if err != nil || len(linked) != 1 || linked[0].EntryID != "gone" || linked[0].LeadName != "" {
		t.Fatalf("linked = %+v, err = %v", linked, err)
	}
}

func TestAFailedLinkListingIsAnError(t *testing.T) {
	service := linkedService(nil, conversationsByID{})
	service.links = &fakeLinks{err: errFakeBoom}

	if _, err := service.ListConversations("ws1", "o1"); !errors.Is(err, errFakeBoom) {
		t.Fatalf("err = %v", err)
	}
}
