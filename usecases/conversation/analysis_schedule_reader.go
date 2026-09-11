package conversation_usecase

import (
	"vozko/domain/cache"
	"vozko/domain/conversation"
	"vozko/domain/shared"
)

// Which conversations are waiting for their inactivity window to elapse.
//
// A conversation is stamped the moment somebody replies, and the debounce job
// hands it to the analysis engine only once it has been quiet for a few
// minutes. Between those two moments there is no queue row to find: the engine
// has never heard of the conversation, so the inbox looked exactly like a
// conversation nobody was going to analyse. This reads the stamp, so the CRM
// can say "an analysis is coming" from the first reply rather than five minutes
// later.
//
// It lives here because this package owns the debounce key and its encoding.
// Nothing outside needs to learn either.

type analysisScheduleReader struct {
	state cache.SharedState
}

// NewAnalysisScheduleReader reads the debounce stamps the send path writes.
func NewAnalysisScheduleReader(state cache.SharedState) conversation.AnalysisScheduleReader {
	return &analysisScheduleReader{state: state}
}

// AwaitingAnalysis reports which of these conversations carry a stamp.
//
// ONE read for the whole page, and the entry ids are filtered in Go rather than
// asked for field by field: the hash holds only conversations touched in the
// last few minutes, so it is small, and a per-entry HGET would be one round
// trip per row of the inbox.
//
// Ids with no stamp are absent rather than false, matching the analysis
// provider beside it, so the map is the size of the answer and not of the page.
// A Redis failure returns an empty map and no error: this decorates a row, and
// an inbox that refused to render because a cache was briefly down would be a
// far worse outcome than a missing chip.
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
		// The stamp names the channel it was written for. A conversation id is
		// unique on its own, but decoding confirms the value is a stamp this
		// code wrote rather than a leftover in a shared key, and an entry type
		// that disagrees is not this page's row.
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
