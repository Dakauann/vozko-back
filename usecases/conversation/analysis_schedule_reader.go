package conversation_usecase

import (
	"vozko/domain/cache"
	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type analysisScheduleReader struct {
	state cache.SharedState
}

func NewAnalysisScheduleReader(state cache.SharedState) conversation.AnalysisScheduleReader {
	return &analysisScheduleReader{state: state}
}

func (r *analysisScheduleReader) AwaitingAnalysis(entryIDs []string, entryType string) (map[string]bool, error) {
	out := map[string]bool{}
	if r == nil || r.state == nil || len(entryIDs) == 0 {
		return out, nil
	}

	stamps, err := r.state.HGetAll(AnalysisDebounceRedisKey)
	if err != nil || len(stamps) == 0 {
		return out, nil
	}

	for _, entryID := range entryIDs {
		raw, ok := stamps[entryID]
		if !ok {
			continue
		}
		stamp, ok := decodeAnalysisDebounceValue(raw)
		if !ok {
			continue
		}
		if entryType != "" && stamp.EntryType != shared.EntryType(entryType) {
			continue
		}
		out[entryID] = true
	}
	return out, nil
}
