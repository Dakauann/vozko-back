package opportunity_usecase

import (
	"log"

	"vozko/domain/conversation"
	"vozko/domain/opportunity"
)

type ConversationDirectory interface {
	BuildInboxEntry(entryID, entryType string) (*conversation.InboxEntry, error)
}

type LinkedConversation struct {
	opportunity.ConversationLink
	LeadName   string `json:"leadName,omitempty"`
	LeadNumber string `json:"leadNumber,omitempty"`
}

func (s *Service) ListConversations(workspaceID, opportunityID string) ([]LinkedConversation, error) {
	links, err := s.links.ListByOpportunity(workspaceID, opportunityID)
	if err != nil {
		return nil, err
	}
	out := make([]LinkedConversation, 0, len(links))
	for _, link := range links {
		out = append(out, s.describe(link))
	}
	return out, nil
}

func (s *Service) describe(link opportunity.ConversationLink) LinkedConversation {
	linked := LinkedConversation{ConversationLink: link}
	if s.conversations == nil {
		return linked
	}
	entry, err := s.conversations.BuildInboxEntry(link.EntryID, link.EntryType)
	if err != nil || entry == nil {
		log.Printf("[opportunity] naming linked conversation %s:%s failed: %v", link.EntryType, link.EntryID, err)
		return linked
	}
	linked.LeadName, linked.LeadNumber = entry.LeadName, entry.LeadNumber
	return linked
}
