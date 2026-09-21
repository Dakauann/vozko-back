package conversation_usecase

import (
	"context"
	"log"
	"strings"
	"time"

	"vozko/domain/cache"
	"vozko/domain/shared"
)

type AnalysisSubject struct {
	EntryID   string
	EntryType shared.EntryType

	WorkspaceID   string
	ContainerID   string
	ContainerName string

	ContactLabel string

	LeadID  string
	AgentID string

	EnableAnalysis    bool
	EnableAutoStaging bool
	EnableAutoMemory  bool
	AIModel           string
}

func (s *AnalysisSubject) WantsWork() bool {
	return s != nil && (s.EnableAnalysis || s.EnableAutoStaging || (s.EnableAutoMemory && s.LeadID != ""))
}

type AnalysisSubjectResolver func(ctx context.Context, entryID string) (*AnalysisSubject, error)

type AnalysisScheduler struct {
	sharedState cache.SharedState
}

func NewAnalysisScheduler(sharedState cache.SharedState) *AnalysisScheduler {
	return &AnalysisScheduler{sharedState: sharedState}
}

func (s *AnalysisScheduler) ScheduleAnalysis(entryID string, entryType shared.EntryType) {
	if s == nil || s.sharedState == nil || entryID == "" || entryType == "" {
		return
	}
	value := encodeAnalysisDebounceValue(entryType, time.Now().UTC())
	if err := s.sharedState.HSet(AnalysisDebounceRedisKey, entryID, value); err != nil {
		log.Printf("[analysis] failed to stamp debounce for %s entry %s: %v", entryType, entryID, err)
	}
}

type analysisDebounceEntry struct {
	EntryType shared.EntryType
	At        time.Time
}

func encodeAnalysisDebounceValue(entryType shared.EntryType, at time.Time) string {
	return string(entryType) + "|" + at.UTC().Format(time.RFC3339)
}

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
