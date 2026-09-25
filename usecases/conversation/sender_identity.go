package conversation_usecase

import (
	"log"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type senderIdentity struct {
	Name   string
	Avatar string
}

type entryLead struct {
	Name    string
	Number  string
	Picture string
}

func (l entryLead) identity() senderIdentity {
	if l.Name != "" {
		return senderIdentity{Name: l.Name, Avatar: l.Picture}
	}
	return senderIdentity{Name: l.Number, Avatar: l.Picture}
}

var genericSenderNames = map[conversation.SenderKind]string{
	conversation.SenderHuman:    "Operador",
	conversation.SenderAI:       "Assistente",
	conversation.SenderWorkflow: "Fluxo",
	conversation.SenderCampaign: "Campanha",
	conversation.SenderSystem:   "Sistema",
}

func (s *HistoryProviderService) identifySenders(entryID string, entryType shared.EntryType, messages []*conversation.Message) {
	lead := s.leadOf(entryID, entryType, messages)
	actors := s.senderActors(messages)
	authors := s.authorsFor(entryType, entryID, lead.Number, messages)
	for _, msg := range messages {
		if msg.SentBy.Valid() {
			nameSender(msg, lead.identity(), actors)
		} else {
			msg.SenderName, msg.SenderAvatar = s.getSenderInfo(msg.From, msg.MessageType, lead.Name, lead.Number, lead.Picture)
		}
		applyAuthor(msg, authors)
	}
}

func nameSender(msg *conversation.Message, lead senderIdentity, actors map[string]senderIdentity) {
	switch kind := msg.SentBy.Kind(); {
	case msg.SentBy.IsContact():
		msg.SenderName, msg.SenderAvatar = lead.Name, lead.Avatar
	case kind == conversation.SenderExternal:
		msg.SenderName, msg.SenderAvatar = "", ""
	default:
		if who, ok := actors[msg.SentBy.ID()]; ok && who.Name != "" {
			msg.SenderName, msg.SenderAvatar = who.Name, who.Avatar
			return
		}
		msg.SenderName, msg.SenderAvatar = genericSenderNames[kind], ""
	}
}

func (s *HistoryProviderService) leadOf(entryID string, entryType shared.EntryType, messages []*conversation.Message) entryLead {
	if entryID == "" || !anyFromCustomer(messages) {
		return entryLead{}
	}
	name, number, picture, _, _, _, err := s.GetEntryInfo(entryID, string(entryType))
	if err != nil {
		log.Printf("[HistoryProvider] could not resolve the lead of %s:%s: %v", entryType, entryID, err)
		return entryLead{}
	}
	return entryLead{Name: name, Number: number, Picture: picture}
}

func anyFromCustomer(messages []*conversation.Message) bool {
	for _, msg := range messages {
		if msg.FromCustomer() {
			return true
		}
	}
	return false
}

func (s *HistoryProviderService) senderActors(messages []*conversation.Message) map[string]senderIdentity {
	idsByKind := map[conversation.SenderKind][]string{}
	for _, msg := range messages {
		if !msg.SentBy.Valid() || msg.SentBy.ID() == "" {
			continue
		}
		kind := msg.SentBy.Kind()
		idsByKind[kind] = append(idsByKind[kind], msg.SentBy.ID())
	}
	return s.actorIdentities(
		idsByKind[conversation.SenderHuman],
		idsByKind[conversation.SenderAI],
		idsByKind[conversation.SenderWorkflow],
	)
}
