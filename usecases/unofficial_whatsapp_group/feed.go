package unofficial_whatsapp_group_usecase

import (
	"context"
	"errors"
	"strings"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	uw "vozko/domain/unofficial_whatsapp"
	uwg "vozko/domain/unofficial_whatsapp_group"
)

var errFeedUnavailable = errors.New("the group message feed is not configured")

type Thread interface {
	GetHistory(entryID string, entryType shared.EntryType, limit int) ([]*conversation.Message, bool, int64, error)
	GetHistoryBefore(entryID string, entryType shared.EntryType, before time.Time, limit int) ([]*conversation.Message, bool, error)
}

type ContactFinder interface {
	FindByHandles(ctx context.Context, instanceID string, handles []string) ([]*uw.Contact, error)
}

type GroupMessagesInput struct {
	Before *time.Time
	Limit  int
}

func (s *Service) GroupMessages(ctx context.Context, a Actor, id string, in GroupMessagesInput) (*GroupMessagesPage, error) {
	n, g, err := s.visibleGroup(ctx, a, id)
	if err != nil {
		return nil, err
	}
	instanceID, conversationID, err := s.groupConversation(ctx, g, n)
	if err != nil {
		return nil, err
	}
	page := &GroupMessagesPage{ConversationID: conversationID, Messages: []GroupMessage{}}
	if conversationID == "" {
		return page, nil
	}
	if s.d.Thread == nil {
		return nil, errFeedUnavailable
	}
	messages, hasMore, err := s.readThread(conversationID, in)
	if err != nil {
		return nil, err
	}
	names, err := s.feedNames(ctx, g.ID, instanceID, n, messages)
	if err != nil {
		return nil, err
	}
	for _, m := range messages {
		if m != nil {
			page.Messages = append(page.Messages, feedMessage(m, names))
		}
	}
	if hasMore && len(page.Messages) > 0 {
		oldest := page.Messages[0].CreatedAt
		page.HasMore, page.NextBefore = true, &oldest
	}
	return page, nil
}

func (s *Service) groupConversation(ctx context.Context, g *uwg.Group, n numbers) (string, string, error) {
	instanceID, ok := s.readableSeat(ctx, g, n)
	if !ok {
		return "", "", nil
	}
	conversationID, err := s.d.Messages.GroupConversation(ctx, instanceID, g.JID)
	if errors.Is(err, uwg.ErrNoGroupConversation) {
		return instanceID, "", nil
	}
	if err != nil {
		return "", "", err
	}
	return instanceID, conversationID, nil
}

func (s *Service) readThread(conversationID string, in GroupMessagesInput) ([]*conversation.Message, bool, error) {
	limit := conversation.HistoryPageSize(in.Limit)
	if in.Before != nil {
		return s.d.Thread.GetHistoryBefore(conversationID, shared.EntryTypeUnofficialWhatsApp, *in.Before, limit)
	}
	messages, hasMore, _, err := s.d.Thread.GetHistory(conversationID, shared.EntryTypeUnofficialWhatsApp, limit)
	return messages, hasMore, err
}

func (s *Service) feedNames(ctx context.Context, groupID, instanceID string, n numbers, messages []*conversation.Message) (uwg.FeedNames, error) {
	names := uwg.FeedNames{Own: ownNumbers(n.everyone)}
	handles := inboundHandles(messages)
	if len(handles) == 0 {
		return names, nil
	}
	members, err := s.d.Members.Names(ctx, groupID)
	if err != nil {
		return names, err
	}
	names.Members = members
	if s.d.Contacts == nil {
		return names, nil
	}
	contacts, err := s.d.Contacts.FindByHandles(ctx, instanceID, handles)
	if err != nil {
		return names, err
	}
	names.Contacts = make(map[string]uwg.FeedContact, len(contacts))
	for _, c := range contacts {
		if c != nil && !c.IsGroup {
			names.Contacts[c.Handle()] = uwg.FeedContact{Name: c.DisplayName(), Picture: c.PictureURL}
		}
	}
	return names, nil
}

func inboundHandles(messages []*conversation.Message) []string {
	seen := map[string]struct{}{}
	var handles []string
	for _, m := range messages {
		if m == nil || isOutbound(m) {
			continue
		}
		handle := strings.TrimSpace(m.From)
		if handle == "" {
			continue
		}
		if _, dup := seen[handle]; dup {
			continue
		}
		seen[handle] = struct{}{}
		handles = append(handles, handle)
	}
	return handles
}

func isOutbound(m *conversation.Message) bool {
	return m.ResolvedDirection() == conversation.MessageDirectionOutbound
}

func feedMessage(m *conversation.Message, names uwg.FeedNames) GroupMessage {
	outbound := isOutbound(m)
	author := names.Author(outbound, m.From)
	from := strings.TrimSpace(m.From)
	direction := conversation.MessageDirectionInbound
	if outbound {
		from, direction = "", conversation.MessageDirectionOutbound
	}
	return GroupMessage{
		ID: m.ID, EntryID: m.EntryID, Channel: m.Channel, MessageType: m.MessageType, Direction: direction,
		From: from, SenderName: author.Name, SenderAvatar: author.Picture, OurTeam: author.OurTeam,
		Text: m.Text, Transcript: m.Transcript, MediaID: m.MediaID, MediaType: m.MediaType, Media: m.Media,
		ReplyToMessageID: m.ReplyToMessageID, DeliveryStatus: m.DeliveryStatus, Read: m.Read, Metadata: m.Metadata,
		EditedAt: m.EditedAt, RetractedAt: m.RetractedAt, CreatedAt: m.CreatedAt,
	}
}
