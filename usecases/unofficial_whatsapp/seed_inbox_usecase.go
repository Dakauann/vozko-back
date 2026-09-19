package unofficial_whatsapp

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strconv"
	"time"

	"vozko/domain/balance"
	"vozko/domain/conversation"
	"vozko/domain/shared"
	uw "vozko/domain/unofficial_whatsapp"
	balance_usecase "vozko/usecases/balance"
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
//
// Direct, rather than through MessageHistoryManager, and this deliberately
// departs from the "always the shared manager" rule in handle_webhook_usecase.
// The manager's three services are deduplication (there is no provider id here
// to deduplicate on), persistence (this port already does it) and websocket
// fan-out (two hundred conversations times eight messages is sixteen hundred
// broadcasts nobody is watching, landing exactly as the operator's own list
// refetches anyway).
type InboxPlaceholderWriter interface {
	// CountByEntry is the guard, not a statistic. See resolveEmptyConversation.
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

	// Scripted is seeded conversations that carry a written thread rather than
	// a blank placeholder. A subset of Seeded, never added to it.
	Scripted int `json:"scripted,omitempty"`
	// ScriptFailed is targets the script was meant for and did not reach: the
	// model refused, timed out, answered unusably, or the balance was too low.
	// They are still seeded, plain. Counted so a batch where every script
	// failed is distinguishable from one that never asked for any.
	ScriptFailed int `json:"scriptFailed,omitempty"`
	// ScriptSkippedNoName is rows with no name under an opening that renders
	// one. "Oi , tudo bem?" is worse than an empty chat, so those are seeded
	// plain on purpose rather than by failure.
	ScriptSkippedNoName int `json:"scriptSkippedNoName,omitempty"`
}

// SeedInboxUseCase opens a conversation for each of a list of numbers so leads
// that arrived through an import are answerable from the inbox.
//
// It is the cold-outbound flow with the outbound removed. It resolves contacts
// and conversations through the SAME ConversationResolver that the inbound
// webhook, the composer and campaigns use, because a second "create a contact"
// path would duplicate every person it touched the moment they replied. It never
// calls the provider, so it cannot spend an instance's send budget or emit a
// ban signal over a list the size of an import.
//
// What makes a seeded conversation VISIBLE is the message, not the conversation
// row: the inbox union requires last_message_at, and the hydration join requires
// a message to exist. Both were measured against Postgres before this was
// written. Writing through the ordinary repository is what keeps that true
// without any query in infra changing.
//
// With a script it writes a short exchange instead of a blank placeholder. That
// costs the workspace's own balance, so the ORDER of Execute's phases is the
// guard: nothing reaches the model that has not already been resolved and found
// empty.
type SeedInboxUseCase struct {
	instances     uw.InstanceRepository
	contacts      uw.ContactRepository
	conversations uw.ConversationRepository
	leads         LeadLinker
	messages      InboxPlaceholderWriter

	// The three below are OPTIONAL and every use of them is nil-safe, so a
	// deployment without an AI service or without balance tracking behaves
	// exactly as this did before scripting existed.
	scripter uw.ConversationScripter
	balance  balance.CachedBalanceChecker
	clock    shared.Clock
}

func NewSeedInboxUseCase(
	instances uw.InstanceRepository,
	contacts uw.ContactRepository,
	conversations uw.ConversationRepository,
	leads LeadLinker,
	messages InboxPlaceholderWriter,
	scripter uw.ConversationScripter,
	balanceChecker balance.CachedBalanceChecker,
) *SeedInboxUseCase {
	return &SeedInboxUseCase{
		instances:     instances,
		contacts:      contacts,
		conversations: conversations,
		leads:         leads,
		messages:      messages,
		scripter:      scripter,
		balance:       balanceChecker,
		clock:         shared.SystemClock{},
	}
}

const (
	// instanceLookupPageSize bounds the instance list this reads to choose from.
	// A workspace has a handful of numbers; this is a ceiling, not a page.
	instanceLookupPageSize = 200

	// scriptCallTimeout bounds ONE model call.
	//
	// The consumer hands this a context.Background(), which is right for
	// database writes and wrong for a provider: without a bound, one hung
	// request holds a queue consumer forever and the rest of the import never
	// seeds.
	scriptCallTimeout = 60 * time.Second

	// seedMetadataSource marks every row this writes.
	//
	// It makes seeded messages findable and deletable later without guessing
	// from the text. Readers take named fields, so an unknown key is inert.
	seedMetadataSource = "lead_import"

	// minTurnGapSeconds and turnGapSpreadSeconds space a seeded thread.
	//
	// Real messages are minutes apart. A thread whose four messages share one
	// timestamp reads as a paste, and one laid back at a fixed interval reads
	// as a machine. The spread is derived from the number, never from rand, so
	// a re-run produces an identical thread.
	minTurnGapSeconds    = 90
	turnGapSpreadSeconds = 151 // 90..240 inclusive

	// threadRecencySpreadSeconds staggers where each thread ENDS.
	//
	// Without it every conversation in a batch shares one last_message_at to
	// the second, which is the single most obvious tell that an inbox was
	// generated rather than worked. Half an hour is wide enough to break the
	// tie and short enough that every seeded thread still reads as recent.
	// Derived from the number like everything else here, so a re-import
	// reproduces the same instant rather than a second, different one.
	threadRecencySpreadSeconds = 1800
)

// Execute seeds one batch, in four phases, and the order is the guard.
func (uc *SeedInboxUseCase) Execute(ctx context.Context, in uw.SeedRequest) (*SeedOutcome, error) {
	// Normalised here rather than trusted from the caller. This runs off a queue
	// message, so the producer may be an older build, and the rules that decide
	// what is addressable belong to the domain rather than to whoever published.
	in.Normalize()
	if err := in.Validate(); err != nil {
		return nil, err
	}

	// 1. The number every conversation in this batch belongs to.
	instance, err := uc.pickInstance(ctx, in.WorkspaceID)
	if err != nil {
		return nil, err
	}

	out := &SeedOutcome{}

	// 2. Resolve and classify. No model call has happened yet, which is the
	//    whole point: scripting a thread for a conversation that turns out to
	//    be live is money spent on output that gets thrown away.
	pending := uc.resolveTargets(ctx, instance, in.Targets, out)

	// 3. Script, if this batch asked for it and is allowed to spend.
	uc.scriptPending(ctx, in, pending, out)

	// 4. Write: the thread where there is one, the placeholder everywhere else.
	uc.writePending(pending, in.Script, out)

	return out, nil
}

// seedCandidate is one target that got as far as an empty conversation.
type seedCandidate struct {
	target         uw.SeedTarget
	conversationID string
	// opening is the operator's own first message, rendered. Empty when this
	// batch carries no script.
	opening string
	// turns is what the model wrote, already passed through AcceptTurns. Empty
	// means this target falls back to the plain placeholder.
	turns []uw.ScriptTurn
	// eligible is whether this target may be sent to the model at all.
	eligible bool
	// skippedNoName separates "we chose not to script this" from "the script
	// failed", so the outcome can say which.
	skippedNoName bool
}

// resolveTargets is phase 2: every target becomes a contact, a lead and a
// conversation, and the ones that already have history are set aside.
//
// Best effort per target, for the same reason bridgeContactLead is: one number
// that cannot be resolved must not cost the others in the batch their inbox
// entries.
func (uc *SeedInboxUseCase) resolveTargets(
	ctx context.Context,
	instance *uw.Instance,
	targets []uw.SeedTarget,
	out *SeedOutcome,
) []*seedCandidate {
	resolver := NewConversationResolver(uc.contacts, uc.conversations, uc.leads)

	pending := make([]*seedCandidate, 0, len(targets))
	for _, target := range targets {
		conversationID, result := uc.resolveEmptyConversation(ctx, resolver, instance, target)
		switch result {
		case seedResultAlreadyActive:
			out.AlreadyActive++
		case seedResultFailed:
			out.Failed++
		default:
			pending = append(pending, &seedCandidate{target: target, conversationID: conversationID})
		}
	}
	return pending
}

type seedResult int

const (
	seedResultFailed seedResult = iota
	seedResultEmpty
	seedResultAlreadyActive
)

// resolveEmptyConversation finds this number's conversation and reports whether
// it is empty enough to seed.
func (uc *SeedInboxUseCase) resolveEmptyConversation(
	ctx context.Context,
	resolver *ConversationResolver,
	instance *uw.Instance,
	target uw.SeedTarget,
) (string, seedResult) {
	// The JID is BUILT rather than verified against WhatsApp. Seeding sends
	// nothing, so the ban signal that makes verification worth a provider round
	// trip in StartConversationUseCase does not apply here, and a hundred
	// thousand rows would be five hundred provider calls. The cost is that a
	// migrated account is addressed by its old form; the contact still reconciles
	// on the phone number when they write in, so it costs a failed send rather
	// than a duplicate person.
	jid := uw.UserJID(target.Number)
	if jid == "" {
		return "", seedResultFailed
	}

	resolved, err := resolver.Resolve(ctx, instance, ResolveInput{
		JID:         jid,
		PhoneNumber: target.Number,
		Name:        target.Name,
	})
	if err != nil {
		log.Printf("[unofficial-whatsapp] could not seed a conversation for %s: %v", target.Number, err)
		return "", seedResultFailed
	}

	// The guard that protects live chats.
	//
	// resolved.AlreadyExisted is NOT enough: a conversation can already exist and
	// still be empty (an operator opened it and never sent, a campaign resolved
	// it and then failed to send), and those are exactly the ones seeding should
	// rescue. The question is whether the thread has HISTORY, and only a count
	// answers it. Writing into a live conversation would put messages nobody
	// sent into a real thread and bump last_message_at, dragging a months old
	// chat to the top of the inbox.
	existing, err := uc.messages.CountByEntry(resolved.Conversation.ID, shared.EntryTypeUnofficialWhatsApp)
	if err != nil {
		// A history that cannot be read is left alone. Treating the failure as
		// "empty" is how a live thread gets written into during a database blip.
		log.Printf("[unofficial-whatsapp] could not count history for conversation %s: %v",
			resolved.Conversation.ID, err)
		return "", seedResultFailed
	}
	if existing > 0 {
		return resolved.Conversation.ID, seedResultAlreadyActive
	}
	return resolved.Conversation.ID, seedResultEmpty
}

// scriptPending is phase 3: the only phase that spends money.
//
// Every refusal here is silent to the operator's import and loud in the
// outcome: a target that is not scripted still gets a conversation, so nothing
// is ever dropped over a model or a balance.
func (uc *SeedInboxUseCase) scriptPending(
	ctx context.Context,
	in uw.SeedRequest,
	pending []*seedCandidate,
	out *SeedOutcome,
) {
	if in.Script == nil || uc.scripter == nil || len(pending) == 0 {
		return
	}
	script := *in.Script
	usesName := script.UsesName()

	eligible := make([]*seedCandidate, 0, len(pending))
	for _, candidate := range pending {
		candidate.opening = script.OpeningFor(candidate.target)
		// A row with no name under a body that renders one is seeded plain, on
		// purpose. "Oi , tudo bem?" is worse than an empty chat, and the
		// operator saw the count of unnamed rows in the import preview before
		// committing.
		if usesName && candidate.target.Name == "" {
			candidate.skippedNoName = true
			out.ScriptSkippedNoName++
			continue
		}
		candidate.eligible = true
		eligible = append(eligible, candidate)
	}
	if len(eligible) == 0 {
		return
	}

	// The balance floor, read ONCE per batch. Twenty-five reads for
	// twenty-five targets is twenty-four calls that cannot change the answer,
	// and the floor is about the workspace, not the target.
	if !balance_usecase.NewAIFloorGuard(uc.balance, "inbox seed scripting").Allow(in.WorkspaceID) {
		out.ScriptFailed += len(eligible)
		return
	}

	for start := 0; start < len(eligible); start += uw.ScriptSubjectsPerCall {
		end := start + uw.ScriptSubjectsPerCall
		if end > len(eligible) {
			end = len(eligible)
		}
		uc.scriptChunk(ctx, in.WorkspaceID, script, eligible[start:end], out)
	}
}

// scriptChunk is one model call. A chunk that fails leaves its targets
// unscripted and costs them nothing but their thread.
func (uc *SeedInboxUseCase) scriptChunk(
	ctx context.Context,
	workspaceID string,
	script uw.SeedScript,
	chunk []*seedCandidate,
	out *SeedOutcome,
) {
	subjects := make([]uw.ScriptSubject, 0, len(chunk))
	for i, candidate := range chunk {
		// The ref is the position in this chunk, never the phone number: the
		// model is given no reason to echo a real number back, and a ref it
		// invents matches nothing rather than matching the wrong person.
		subjects = append(subjects, uw.ScriptSubject{
			Ref:          i + 1,
			Name:         candidate.target.Name,
			FirstMessage: candidate.opening,
		})
	}

	// Bounded here rather than by the caller: this is the only line in the
	// whole feature that talks to a provider, and the consumer above it
	// legitimately holds a context with no deadline.
	callCtx, cancel := context.WithTimeout(ctx, scriptCallTimeout)
	defer cancel()

	result, err := uc.scripter.Script(callCtx, uw.ScriptRequest{
		WorkspaceID: workspaceID,
		Context:     script.Context,
		MaxMessages: script.MaxMessages,
		Subjects:    subjects,
	})
	if err != nil || result == nil {
		log.Printf("[unofficial-whatsapp] scripting %d seeded conversations for workspace %s failed, seeding them plain: %v",
			len(chunk), workspaceID, err)
		out.ScriptFailed += len(chunk)
		return
	}

	byRef := make(map[int][]uw.ScriptTurn, len(result.Threads))
	for _, thread := range result.Threads {
		byRef[thread.Ref] = thread.Turns
	}

	for i, candidate := range chunk {
		// AcceptTurns is the enforcement half of the prompt: whatever the model
		// returned, this is what may be written. A ref it invented is simply
		// absent here, and that target falls back.
		turns := script.AcceptTurns(byRef[i+1])
		if len(turns) == 0 {
			out.ScriptFailed++
			continue
		}
		candidate.turns = turns
	}
}

// writePending is phase 4, and the only phase that writes.
func (uc *SeedInboxUseCase) writePending(
	pending []*seedCandidate,
	script *uw.SeedScript,
	out *SeedOutcome,
) {
	now := uc.now()
	for _, candidate := range pending {
		var err error
		if len(candidate.turns) > 0 && script != nil {
			err = uc.writeThread(candidate, now)
		} else {
			err = uc.messages.Create(newInboxPlaceholder(candidate.conversationID, now))
		}
		if err != nil {
			log.Printf("[unofficial-whatsapp] could not seed conversation %s: %v", candidate.conversationID, err)
			out.Failed++
			continue
		}
		out.Seeded++
		if len(candidate.turns) > 0 {
			out.Scripted++
		}
	}
}

// writeThread writes the operator's opening and everything the model wrote.
//
// Chronological, so the last Create carries the newest timestamp and
// touchEntryMessageClocks — which is monotonic — lands last_message_at on it.
// A partial failure is a failure: a half-written thread is worse than an empty
// chat, so the caller counts the target as failed and the rows that did land
// still make the conversation visible.
func (uc *SeedInboxUseCase) writeThread(candidate *seedCandidate, now time.Time) error {
	total := len(candidate.turns) + 1
	at := threadTimestamps(candidate.target.Number, total, now)
	metadata := seedMetadata(now)

	messages := make([]*conversation.Message, 0, total)
	messages = append(messages, &conversation.Message{
		EntryID:   candidate.conversationID,
		EntryType: shared.EntryTypeUnofficialWhatsApp,
		Channel:   conversation.MessageChannelUnofficialWhatsApp,
		// Operator, because that is what it is: the message a person at the
		// company would have typed. It also sets last_agent_message_at, which
		// is what stops the idle auto-close window treating a thread we spoke
		// in last as one we never answered.
		MessageType: conversation.MessageTypeOperator,
		// STATED rather than derived. This channel names its content honestly,
		// and a direction derived from the type puts an owner's own reply on
		// the customer's side of the thread.
		Direction: conversation.MessageDirectionOutbound,
		Text:      candidate.opening,
		Metadata:  metadata,
		CreatedAt: at[0],
	})

	for i, turn := range candidate.turns {
		msg := &conversation.Message{
			EntryID:   candidate.conversationID,
			EntryType: shared.EntryTypeUnofficialWhatsApp,
			Channel:   conversation.MessageChannelUnofficialWhatsApp,
			Text:      turn.Text,
			Metadata:  metadata,
			CreatedAt: at[i+1],
		}
		if turn.FromLead {
			// A genuinely inbound TYPE, not a system marker: the inbox counts
			// unread from these, the analysis reads them as the customer, and
			// a seeded thread the CRM cannot see as a conversation is a seeded
			// thread that teaches nobody anything.
			msg.MessageType = conversation.MessageTypeUserMessage
			msg.Direction = conversation.MessageDirectionInbound
		} else {
			msg.MessageType = conversation.MessageTypeOperator
			msg.Direction = conversation.MessageDirectionOutbound
		}
		messages = append(messages, msg)
	}

	// Read, except the last one when it is the lead's.
	//
	// The blank placeholder is marked read to avoid a thousand unread badges,
	// and that is right for an empty pill with nothing to read. A scripted
	// thread ending on the lead's turn is a genuinely unanswered message;
	// showing it as answered would be the lie. It is affordable because this is
	// capped at two hundred, not a hundred thousand.
	readAt := now
	for i, msg := range messages {
		trailingInbound := i == len(messages)-1 && msg.MessageType.IsInbound()
		if trailingInbound {
			continue
		}
		msg.Read = true
		msg.ReadAt = &readAt
	}

	for _, msg := range messages {
		if err := uc.messages.Create(msg); err != nil {
			return err
		}
	}
	return nil
}

// threadTimestamps lays a thread of count messages back from now.
//
// The newest message lands within the last half hour, so the conversation is
// near the top of an inbox sorted by last_message_at without every thread in a
// batch sharing one timestamp. Every gap inside the thread is between
// minTurnGapSeconds and minTurnGapSeconds+turnGapSpreadSeconds.
//
// All of it is derived from the phone number rather than drawn at random, so
// re-running the same import reproduces an identical thread instead of a
// second, differently-spaced one.
func threadTimestamps(number string, count int, now time.Time) []time.Time {
	at := make([]time.Time, count)
	if count == 0 {
		return at
	}
	at[count-1] = now.Add(-time.Duration(
		shared.VariantIndexFor(number+":end", threadRecencySpreadSeconds)) * time.Second)
	for i := count - 2; i >= 0; i-- {
		gap := minTurnGapSeconds + shared.VariantIndexFor(number+":"+strconv.Itoa(i), turnGapSpreadSeconds)
		at[i] = at[i+1].Add(-time.Duration(gap) * time.Second)
	}
	return at
}

// seedMetadata marks a row as ours.
//
// Best effort: a marshal that somehow fails costs the marker, not the message.
// Nothing reads this to decide behaviour, so an absent key degrades to a row
// that is harder to find later rather than one that misbehaves.
func seedMetadata(now time.Time) json.RawMessage {
	body, err := json.Marshal(map[string]string{
		"seed":     seedMetadataSource,
		"seededAt": now.UTC().Format(time.RFC3339),
	})
	if err != nil {
		return nil
	}
	return body
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
//
// It carries the same seed marker a scripted thread does, so "everything this
// import opened" is one query whether or not the conversations were scripted.
func newInboxPlaceholder(conversationID string, now time.Time) *conversation.Message {
	now = now.UTC()
	return &conversation.Message{
		EntryID:     conversationID,
		EntryType:   shared.EntryTypeUnofficialWhatsApp,
		Channel:     conversation.MessageChannelUnofficialWhatsApp,
		MessageType: conversation.MessageTypeSystem,
		Text:        "",
		Read:        true,
		ReadAt:      &now,
		Metadata:    seedMetadata(now),
		CreatedAt:   now,
	}
}

// now is the injected clock, or the real one. Injected so the thread's spacing
// and its metadata timestamp are assertable without sleeping.
func (uc *SeedInboxUseCase) now() time.Time {
	if uc.clock == nil {
		return time.Now().UTC()
	}
	return uc.clock.Now().UTC()
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
