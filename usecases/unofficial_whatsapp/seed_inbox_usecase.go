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
	"vozko/domain/media"
	"vozko/domain/shared"
	uw "vozko/domain/unofficial_whatsapp"
	balance_usecase "vozko/usecases/balance"
)

var ErrNoConnectedInstance = errors.New("unofficial whatsapp: the workspace has no connected number")

type InboxPlaceholderWriter interface {
	CountByEntry(entryID string, entryType shared.EntryType) (int64, error)
	Create(message *conversation.Message) error
}

type SeedAssetReader interface {
	GetMediaByID(mediaID string) (*media.Media, error)
}

type SeedAttachmentWriter interface {
	Create(media *conversation.ConversationMedia) error
}

type SeedOutcome struct {
	Seeded        int `json:"seeded"`
	AlreadyActive int `json:"alreadyActive"`
	Failed        int `json:"failed"`

	Scripted            int `json:"scripted,omitempty"`
	ScriptFailed        int `json:"scriptFailed,omitempty"`
	ScriptSkippedNoName int `json:"scriptSkippedNoName,omitempty"`
}

type SeedInboxUseCase struct {
	instances     uw.InstanceRepository
	contacts      uw.ContactRepository
	conversations uw.ConversationRepository
	leads         LeadLinker
	messages      InboxPlaceholderWriter

	scripter uw.ConversationScripter
	balance  balance.CachedBalanceChecker
	clock    shared.Clock

	assets      SeedAssetReader
	attachments SeedAttachmentWriter
}

func (uc *SeedInboxUseCase) WithAttachments(assets SeedAssetReader, attachments SeedAttachmentWriter) *SeedInboxUseCase {
	uc.assets = assets
	uc.attachments = attachments
	return uc
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
	instanceLookupPageSize = 200

	scriptCallTimeout = 60 * time.Second

	seedMetadataSource = "lead_import"

	minTurnGapSeconds    = 90
	turnGapSpreadSeconds = 151

	threadRecencySpreadSeconds = 1800
)

func (uc *SeedInboxUseCase) Execute(ctx context.Context, in uw.SeedRequest) (*SeedOutcome, error) {
	in.Normalize()
	if err := in.Validate(); err != nil {
		return nil, err
	}

	instance, err := uc.pickInstance(ctx, in.WorkspaceID)
	if err != nil {
		return nil, err
	}

	out := &SeedOutcome{}

	pending := uc.resolveTargets(ctx, instance, in.Targets, out)

	uc.scriptPending(ctx, in, pending, out)

	uc.writePending(pending, in.Script, uc.resolveAsset(in), out)

	return out, nil
}

type seedCandidate struct {
	target         uw.SeedTarget
	conversationID string
	opening        string
	turns          []uw.ScriptTurn
	eligible       bool
	skippedNoName  bool
}

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

func (uc *SeedInboxUseCase) resolveEmptyConversation(
	ctx context.Context,
	resolver *ConversationResolver,
	instance *uw.Instance,
	target uw.SeedTarget,
) (string, seedResult) {
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

	existing, err := uc.messages.CountByEntry(resolved.Conversation.ID, shared.EntryTypeUnofficialWhatsApp)
	if err != nil {
		log.Printf("[unofficial-whatsapp] could not count history for conversation %s: %v",
			resolved.Conversation.ID, err)
		return "", seedResultFailed
	}
	if existing > 0 {
		return resolved.Conversation.ID, seedResultAlreadyActive
	}
	return resolved.Conversation.ID, seedResultEmpty
}

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

	if !balance_usecase.NewSpendGuard(uc.balance, "inbox seed scripting").Allow(in.WorkspaceID) {
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

func (uc *SeedInboxUseCase) scriptChunk(
	ctx context.Context,
	workspaceID string,
	script uw.SeedScript,
	chunk []*seedCandidate,
	out *SeedOutcome,
) {
	subjects := make([]uw.ScriptSubject, 0, len(chunk))
	for i, candidate := range chunk {
		subjects = append(subjects, uw.ScriptSubject{
			Ref:          i + 1,
			Name:         candidate.target.Name,
			FirstMessage: candidate.opening,
		})
	}

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
		turns := script.AcceptTurns(byRef[i+1])
		if len(turns) == 0 {
			out.ScriptFailed++
			continue
		}
		candidate.turns = turns
	}
}

func (uc *SeedInboxUseCase) writePending(
	pending []*seedCandidate,
	script *uw.SeedScript,
	asset *seedAsset,
	out *SeedOutcome,
) {
	now := uc.now()
	for _, candidate := range pending {
		var err error
		if len(candidate.turns) > 0 && script != nil {
			err = uc.writeThread(candidate, asset, now)
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

func (uc *SeedInboxUseCase) writeThread(candidate *seedCandidate, asset *seedAsset, now time.Time) error {
	total := len(candidate.turns) + 1
	at := threadTimestamps(candidate.target.Number, total, now)
	metadata := seedMetadata(now)

	messages := make([]*conversation.Message, 0, total)
	opening := &conversation.Message{
		EntryID:     candidate.conversationID,
		EntryType:   shared.EntryTypeUnofficialWhatsApp,
		Channel:     conversation.MessageChannelUnofficialWhatsApp,
		MessageType: conversation.MessageTypeOperator,
		Direction:   conversation.MessageDirectionOutbound,
		Text:        candidate.opening,
		Metadata:    metadata,
		CreatedAt:   at[0],
	}
	uc.attach(opening, asset, now)
	messages = append(messages, opening)

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
			msg.MessageType = conversation.MessageTypeUserMessage
			msg.Direction = conversation.MessageDirectionInbound
		} else {
			msg.MessageType = conversation.MessageTypeOperator
			msg.Direction = conversation.MessageDirectionOutbound
		}
		messages = append(messages, msg)
	}

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

func (uc *SeedInboxUseCase) now() time.Time {
	if uc.clock == nil {
		return time.Now().UTC()
	}
	return uc.clock.Now().UTC()
}

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
