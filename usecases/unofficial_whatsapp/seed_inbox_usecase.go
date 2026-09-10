package unofficial_whatsapp

import (
	"context"
	"errors"
	"log"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	uw "vozko/domain/unofficial_whatsapp"
)

// ErrNoConnectedInstance means the workspace has no live number to seed onto.
//
// A refusal rather than a fallback to a disconnected number: a conversation on
// a dead session is an inbox row nobody can answer, and an operator who imported
// a list would spend a while working out why every reply fails.
var ErrNoConnectedInstance = errors.New("unofficial whatsapp: the workspace has no connected number")

// InboxPlaceholderWriter is the narrow slice of the conversation message
// repository that seeding needs.
//
// Narrow on purpose. The full MessageRepository carries forty methods about
// delivery receipts, media, unread counts and provider ids, none of which this
// use case may reach for. Both methods below are satisfied by the existing
// repository, so this is a view of a port rather than a second one.
type InboxPlaceholderWriter interface {
	// CountByEntry is the guard, not a statistic. See seedOne.
	CountByEntry(entryID string, entryType shared.EntryType) (int64, error)
	Create(message *conversation.Message) error
}

// SeedOutcome is what one batch did, in the terms an operator would use.
type SeedOutcome struct {
	// Seeded is conversations that now render in the inbox and did not before.
	Seeded int `json:"seeded"`
	// AlreadyActive is conversations left untouched because they already had
	// messages. Counted rather than folded into Seeded: "1.000 seeded" is a lie
	// when 700 of those chats were already in the inbox.
	AlreadyActive int `json:"alreadyActive"`
	// Failed is targets that errored. Reported, never silent.
	Failed int `json:"failed"`
}

// SeedInboxUseCase opens an empty conversation for each of a list of numbers so
// leads that arrived through an import are answerable from the inbox.
//
// It is the cold-outbound flow with the outbound removed. It resolves contacts
// and conversations through the SAME ConversationResolver that the inbound
// webhook, the composer and campaigns use, because a second "create a contact"
// path would duplicate every person it touched the moment they replied. It never
// calls the provider, so it cannot spend an instance's send budget or emit a
// ban signal over a list the size of an import.
//
// What makes a seeded conversation VISIBLE is the placeholder message, not the
// conversation row: the inbox union requires last_message_at, and the hydration
// join requires a message to exist. Both were measured against Postgres before
// this was written. Writing the placeholder through the ordinary repository is
// what keeps that true without any query in infra changing.
type SeedInboxUseCase struct {
	instances     uw.InstanceRepository
	contacts      uw.ContactRepository
	conversations uw.ConversationRepository
	leads         LeadLinker
	messages      InboxPlaceholderWriter
}

func NewSeedInboxUseCase(
	instances uw.InstanceRepository,
	contacts uw.ContactRepository,
	conversations uw.ConversationRepository,
	leads LeadLinker,
	messages InboxPlaceholderWriter,
) *SeedInboxUseCase {
	return &SeedInboxUseCase{
		instances:     instances,
		contacts:      contacts,
		conversations: conversations,
		leads:         leads,
		messages:      messages,
	}
}

// instanceLookupPageSize bounds the instance list this reads to choose from.
// A workspace has a handful of numbers; this is a ceiling, not a page.
const instanceLookupPageSize = 200

// Execute seeds one batch.
func (uc *SeedInboxUseCase) Execute(ctx context.Context, in uw.SeedRequest) (*SeedOutcome, error) {
	// Normalised here rather than trusted from the caller. This runs off a queue
	// message, so the producer may be an older build, and the rules that decide
	// what is addressable belong to the domain rather than to whoever published.
	in.Normalize()
	if err := in.Validate(); err != nil {
		return nil, err
	}

	instance, err := uc.pickInstance(ctx, in.WorkspaceID)
	if err != nil {
		return nil, err
	}

	resolver := NewConversationResolver(uc.contacts, uc.conversations, uc.leads)

	out := &SeedOutcome{}
	for _, target := range in.Targets {
		switch uc.seedOne(ctx, resolver, instance, target) {
		case seedResultSeeded:
			out.Seeded++
		case seedResultAlreadyActive:
			out.AlreadyActive++
		default:
			out.Failed++
		}
	}
	return out, nil
}

type seedResult int

const (
	seedResultFailed seedResult = iota
	seedResultSeeded
	seedResultAlreadyActive
)

// seedOne opens the conversation for one number and gives it a placeholder.
//
// Best effort per target, for the same reason bridgeContactLead is: one number
// that cannot be resolved must not cost the other four hundred and ninety-nine
// in the batch their inbox entries.
func (uc *SeedInboxUseCase) seedOne(
	ctx context.Context,
	resolver *ConversationResolver,
	instance *uw.Instance,
	target uw.SeedTarget,
) seedResult {
	// The JID is BUILT rather than verified against WhatsApp. Seeding sends
	// nothing, so the ban signal that makes verification worth a provider round
	// trip in StartConversationUseCase does not apply here, and a hundred
	// thousand rows would be five hundred provider calls. The cost is that a
	// migrated account is addressed by its old form; the contact still reconciles
	// on the phone number when they write in, so it costs a failed send rather
	// than a duplicate person.
	jid := uw.UserJID(target.Number)
	if jid == "" {
		return seedResultFailed
	}

	resolved, err := resolver.Resolve(ctx, instance, ResolveInput{
		JID:         jid,
		PhoneNumber: target.Number,
		Name:        target.Name,
	})
	if err != nil {
		log.Printf("[unofficial-whatsapp] could not seed a conversation for %s: %v", target.Number, err)
		return seedResultFailed
	}

	// The guard that protects live chats.
	//
	// resolved.AlreadyExisted is NOT enough: a conversation can already exist and
	// still be empty (an operator opened it and never sent, a campaign resolved
	// it and then failed to send), and those are exactly the ones seeding should
	// rescue. The question is whether the thread has HISTORY, and only a count
	// answers it. Writing a placeholder into a live conversation would put a
	// blank bubble in a real thread and bump last_message_at, dragging a months
	// old chat to the top of the inbox.
	existing, err := uc.messages.CountByEntry(resolved.Conversation.ID, shared.EntryTypeUnofficialWhatsApp)
	if err != nil {
		// A history that cannot be read is left alone. Treating the failure as
		// "empty" is how a live thread gets written into during a database blip.
		log.Printf("[unofficial-whatsapp] could not count history for conversation %s: %v",
			resolved.Conversation.ID, err)
		return seedResultFailed
	}
	if existing > 0 {
		return seedResultAlreadyActive
	}

	if err := uc.messages.Create(newInboxPlaceholder(resolved.Conversation.ID)); err != nil {
		log.Printf("[unofficial-whatsapp] could not write the inbox placeholder for conversation %s: %v",
			resolved.Conversation.ID, err)
		return seedResultFailed
	}
	return seedResultSeeded
}

// newInboxPlaceholder builds the message that makes an empty conversation
// visible.
//
// System, and empty. System because it is the one type every AI history builder
// already skips and the one type InboundMessageTypes() excludes, so the
// placeholder can never read as something the lead said, never raise the unread
// badge, and never fool CountInboundByEntry into thinking a first message has
// arrived. Empty because the chat is meant to look untouched; the clients render
// it, so this is deliberately the smallest mark that can be left.
//
// Read is set even though a system message cannot count as unread today. It is
// one predicate change away from doing so, and a seeded inbox that lights up
// with a thousand unread badges is a bad way to discover that.
func newInboxPlaceholder(conversationID string) *conversation.Message {
	now := time.Now().UTC()
	return &conversation.Message{
		EntryID:     conversationID,
		EntryType:   shared.EntryTypeUnofficialWhatsApp,
		Channel:     conversation.MessageChannelUnofficialWhatsApp,
		MessageType: conversation.MessageTypeSystem,
		Text:        "",
		Read:        true,
		ReadAt:      &now,
		CreatedAt:   now,
	}
}

// pickInstance chooses the number the seeded conversations belong to.
//
// Oldest connected first, and both halves matter. Connected because a
// conversation on a dead session cannot be answered. Oldest because the choice
// has to be STABLE: the repository's own listing is newest-first, so picking
// from the top would silently move where the next import lands the day a
// workspace connects a second number, splitting one imported list across two
// inboxes for no reason anybody could see.
func (uc *SeedInboxUseCase) pickInstance(ctx context.Context, workspaceID string) (*uw.Instance, error) {
	connected := uw.StatusConnected
	page, err := uc.instances.ListByWorkspace(ctx, uw.ListInstancesInput{
		WorkspaceID: workspaceID,
		Status:      &connected,
		Options: shared.QueryOptions{
			Pagination: shared.Pagination{Page: 1, PageSize: instanceLookupPageSize},
		},
	})
	if err != nil {
		return nil, err
	}

	var chosen *uw.Instance
	if page != nil {
		for _, candidate := range page.Items {
			// Status is re-checked rather than trusted from the filter: the
			// filter is one argument away from being dropped, and this is the
			// predicate the whole choice rests on.
			if candidate == nil || !candidate.SessionLive() {
				continue
			}
			if chosen == nil || candidate.CreatedAt.Before(chosen.CreatedAt) {
				chosen = candidate
			}
		}
	}
	if chosen == nil {
		return nil, ErrNoConnectedInstance
	}
	return chosen, nil
}
