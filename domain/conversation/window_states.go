package conversation

import "context"

type WindowStatesAdapter interface {
	WindowStates(ctx context.Context, entryIDs []string) (map[string]WindowState, error)
}

func (w WindowState) Equal(o WindowState) bool {
	if w.Open != o.Open || w.Reason != o.Reason || w.ProviderDetail != o.ProviderDetail || w.Tier != o.Tier {
		return false
	}
	if (w.ExpiresAt == nil) != (o.ExpiresAt == nil) {
		return false
	}
	return w.ExpiresAt == nil || w.ExpiresAt.Equal(*o.ExpiresAt)
}

func WithUnavailable(entryIDs []string, known map[string]WindowState) map[string]WindowState {
	out := make(map[string]WindowState, len(entryIDs))
	for _, id := range entryIDs {
		if id == "" {
			continue
		}
		if state, ok := known[id]; ok {
			out[id] = state
			continue
		}
		out[id] = ClosedWindow(WindowReasonChannelUnavailable)
	}
	return out
}
