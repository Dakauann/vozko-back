package conversation_usecase

import (
	"context"
	"log"
	"strings"
	"time"

	"vozko/domain/cache"
	"vozko/domain/shared"
)

// AI conversation analysis was WhatsApp-only, and not by design.
//
// The debounce job hardcoded `entryType := "whatsapp"`, read its configuration
// from whatsapp_campaigns, and, most decisively, returned early when the
// contact had no phone number. An Instagram contact is an IGSID and a Telegram
// contact is a numeric user id, so the job would have bailed even if it had been
// called, which it never was. The result was an EnableAnalysis switch on both
// channels' account rows that did nothing at all.
//
// The three things the job actually needs are below. None of them is
// WhatsApp-specific once named honestly: a tenant, a container that carries the
// automation config, and a label for the person on the other side.

// AnalysisSubject is everything the analysis/auto-stage job needs about one
// conversation, resolved per channel.
type AnalysisSubject struct {
	EntryID   string
	EntryType shared.EntryType

	WorkspaceID string
	// ContainerID is the config-carrying parent: a WhatsApp campaign, or the
	// account row for channels with no campaign concept. It scopes the stage list
	// the auto-tagger chooses from.
	ContainerID string
	// ContainerName appears in the analysis prompt so the model knows what the
	// conversation is about.
	ContainerName string

	// ContactLabel identifies the person in the transcript. WhatsApp uses the
	// phone number; channels without one use the handle or the provider id. It
	// only has to be stable and recognisable, it is prompt text, not a key,
	// which is why demanding a phone number was the wrong precondition.
	ContactLabel string

	// LeadID is the CRM lead behind the conversation, when one is linked.
	// Auto-memorization is scoped to leads, so without it the memory pass is
	// silently skipped: a conversation with no lead has nowhere to remember to.
	LeadID string
	// AgentID attributes auto-memorized facts to the container's agent
	// (actor "ai:{id}"). Empty falls back to the system actor, which is the
	// honest attribution for a background job with no agent configured.
	AgentID string

	EnableAnalysis    bool
	EnableAutoStaging bool
	EnableAutoMemory  bool
	// AIModel overrides the default. Empty means the job's default.
	AIModel string
}

// WantsWork reports whether this subject has anything enabled at all.
func (s *AnalysisSubject) WantsWork() bool {
	return s != nil && (s.EnableAnalysis || s.EnableAutoStaging || (s.EnableAutoMemory && s.LeadID != ""))
}

// AnalysisSubjectResolver loads one channel's subject.
//
// Returning (nil, nil) means "this entry exists but should not be analysed",
// deleted, or belonging to a container with everything switched off, which is a
// normal outcome, not a failure.
type AnalysisSubjectResolver func(ctx context.Context, entryID string) (*AnalysisSubject, error)

// AnalysisScheduler stamps a conversation for deferred analysis.
//
// It is a service rather than a method on each channel's usecase so every
// channel schedules the same way, into the same hash, with the same encoding,
// which is what makes the debounce job able to stay channel-agnostic.
type AnalysisScheduler struct {
	sharedState cache.SharedState
}

func NewAnalysisScheduler(sharedState cache.SharedState) *AnalysisScheduler {
	return &AnalysisScheduler{sharedState: sharedState}
}

// ScheduleAnalysis records that a conversation just moved.
//
// Best effort by design: analysis is an enrichment, and failing an inbound
// webhook because Redis blinked would redeliver a message that was already
// stored.
func (s *AnalysisScheduler) ScheduleAnalysis(entryID string, entryType shared.EntryType) {
	if s == nil || s.sharedState == nil || entryID == "" || entryType == "" {
		return
	}
	value := encodeAnalysisDebounceValue(entryType, time.Now().UTC())
	if err := s.sharedState.HSet(AnalysisDebounceRedisKey, entryID, value); err != nil {
		log.Printf("[analysis] failed to stamp debounce for %s entry %s: %v", entryType, entryID, err)
	}
}

// analysisDebounceEntry is one pending row in the debounce hash.
type analysisDebounceEntry struct {
	EntryType shared.EntryType
	At        time.Time
}

// encodeAnalysisDebounceValue renders the hash value.
//
// The entry type has to travel with the timestamp because the hash is keyed by
// entry id alone, and an entry id says nothing about its channel. Encoding it in
// the value avoids migrating a live Redis key.
func encodeAnalysisDebounceValue(entryType shared.EntryType, at time.Time) string {
	return string(entryType) + "|" + at.UTC().Format(time.RFC3339)
}

// decodeAnalysisDebounceValue parses a hash value.
//
// A bare timestamp with no channel prefix is WhatsApp: that is the format this
// hash held before other channels existed, and rows written by the previous
// build are still in flight at deploy time. Dropping them would silently skip
// one debounce window's worth of analyses on every release.
func decodeAnalysisDebounceValue(raw string) (analysisDebounceEntry, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return analysisDebounceEntry{}, false
	}

	entryType := shared.EntryTypeWhatsApp
	timestamp := raw
	if idx := strings.IndexByte(raw, '|'); idx >= 0 {
		candidate := shared.EntryType(raw[:idx])
		timestamp = raw[idx+1:]
		if !candidate.Valid() {
			return analysisDebounceEntry{}, false
		}
		entryType = candidate
	}

	at, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		return analysisDebounceEntry{}, false
	}
	return analysisDebounceEntry{EntryType: entryType, At: at.UTC()}, true
}
