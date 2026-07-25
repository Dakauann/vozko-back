package dialer

import "vozko/domain/workspace"

// memberRingResolver answers the session registry's ring-channel policy question
// from the workspace member repository. It is deliberately fed the CACHED repo, so
// each lookup is a cache hit (invalidated when the member updates their selection),
// never a DB round trip on the connect / offer hot path.
type memberRingResolver struct {
	repo workspace.Repository
}

// NewMemberRingResolver builds the resolver. Pass the cached workspace repository.
func NewMemberRingResolver(repo workspace.Repository) RingChannelResolver {
	return &memberRingResolver{repo: repo}
}

// RingsChannel reports whether the member wants the given channel to ring. It fails
// open: an unknown/unreadable member, or one with no explicit selection, rings every
// channel (the default), so a lookup miss never silences an endpoint.
func (m *memberRingResolver) RingsChannel(workspaceID, userID, channel string) bool {
	if m == nil || m.repo == nil {
		return true
	}
	member, err := m.repo.GetMember(workspaceID, userID)
	if err != nil || member == nil {
		return true
	}
	if len(member.RingChannels) == 0 {
		return true
	}
	for _, c := range member.RingChannels {
		if string(c) == channel {
			return true
		}
	}
	return false
}
