package audience_usecase

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	ca "vozko/domain/audience"
	"vozko/domain/cache"
)

// The debounce hint store (plan §6.2): one Redis hash, one field per
// container, value "v1|{workspaceID}|{firstSeen}|{lastSeen}|{count}".
//
// It mirrors the conversation analysis_subject.go stamp in shape and in
// posture: the hint says WHEN to look, the database says WHAT is pending.
// Stamp is a read-modify-write without a lock on purpose; a lost update
// costs at most one backstop interval of latency and never a comment.

const (
	hintHashKey  = "comment_analysis:debounce:pending"
	hintVersion  = "v1"
	hintTimeFmt  = time.RFC3339
	hintFieldSep = "|"
)

type redisScheduler struct {
	state cache.SharedState
}

// NewScheduler builds the hint store over the shared Redis state.
func NewScheduler(state cache.SharedState) ca.Scheduler {
	return &redisScheduler{state: state}
}

func (s *redisScheduler) Stamp(_ context.Context, ref ca.ContainerRef, workspaceID string, now time.Time) error {
	if err := ref.Validate(); err != nil {
		return err
	}
	all, err := s.state.HGetAll(hintHashKey)
	if err != nil {
		return err
	}
	var current ca.Hint
	if raw, ok := all[ref.Key()]; ok {
		if parsed, perr := parseHint(ref, raw); perr == nil {
			current = parsed
		}
	}
	next := current.Stamp(ref, workspaceID, now)
	return s.state.HSet(hintHashKey, ref.Key(), encodeHint(next))
}

func (s *redisScheduler) Hints(_ context.Context) ([]ca.Hint, error) {
	all, err := s.state.HGetAll(hintHashKey)
	if err != nil {
		return nil, err
	}
	out := make([]ca.Hint, 0, len(all))
	for field, raw := range all {
		ref, err := ca.ParseContainerKey(field)
		if err != nil {
			// A field this code did not write. Drop it so it cannot wedge the
			// hash forever; the backstop covers whatever it referred to.
			_ = s.state.HDel(hintHashKey, field)
			continue
		}
		h, err := parseHint(ref, raw)
		if err != nil {
			// Same reasoning: a corrupt hint is treated as due (zero times)
			// rather than skipped, so the container is still looked at.
			h = ca.Hint{Ref: ref}
		}
		out = append(out, h)
	}
	return out, nil
}

func (s *redisScheduler) Clear(_ context.Context, ref ca.ContainerRef) error {
	return s.state.HDel(hintHashKey, ref.Key())
}

func encodeHint(h ca.Hint) string {
	return strings.Join([]string{
		hintVersion,
		h.WorkspaceID,
		h.FirstSeen.UTC().Format(hintTimeFmt),
		h.LastSeen.UTC().Format(hintTimeFmt),
		strconv.Itoa(h.Count),
	}, hintFieldSep)
}

func parseHint(ref ca.ContainerRef, raw string) (ca.Hint, error) {
	parts := strings.Split(raw, hintFieldSep)
	if len(parts) != 5 || parts[0] != hintVersion {
		return ca.Hint{}, fmt.Errorf("comment analysis: malformed hint %q", raw)
	}
	first, err := time.Parse(hintTimeFmt, parts[2])
	if err != nil {
		return ca.Hint{}, err
	}
	last, err := time.Parse(hintTimeFmt, parts[3])
	if err != nil {
		return ca.Hint{}, err
	}
	count, err := strconv.Atoi(parts[4])
	if err != nil {
		return ca.Hint{}, err
	}
	return ca.Hint{Ref: ref, WorkspaceID: parts[1], FirstSeen: first, LastSeen: last, Count: count}, nil
}
