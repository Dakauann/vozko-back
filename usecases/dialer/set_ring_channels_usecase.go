package dialer_usecase

import "vozko/domain/workspace"

// MemberRingStore is the narrow member persistence the ring-channels update needs.
type MemberRingStore interface {
	GetMember(workspaceID, userID string) (*workspace.Member, error)
	UpdateMemberRingChannels(memberID string, channels []workspace.RingChannel) error
}

// RingChannelApplier pushes a member's new selection to their live dialer endpoints,
// so the change takes effect immediately without a reconnect.
type RingChannelApplier interface {
	ApplyRingChannels(workspaceID, userID string, channels []string)
}

// SetRingChannelsUseCase updates a member's ring-channel selection (the AOR-level
// policy for which of their endpoints ring) and applies it to their live sessions.
// Authorization (self vs admin) is enforced by the route/middleware; this use case
// trusts the already-resolved (workspaceID, userID) target.
type SetRingChannelsUseCase struct {
	members MemberRingStore
	applier RingChannelApplier
}

func NewSetRingChannelsUseCase(members MemberRingStore, applier RingChannelApplier) *SetRingChannelsUseCase {
	return &SetRingChannelsUseCase{members: members, applier: applier}
}

// Execute validates and persists the selection for the member, then syncs their live
// endpoints. Duplicate/unknown channels are de-duplicated / rejected.
func (uc *SetRingChannelsUseCase) Execute(workspaceID, userID string, channels []workspace.RingChannel) error {
	clean := make([]workspace.RingChannel, 0, len(channels))
	seen := make(map[workspace.RingChannel]bool, len(channels))
	for _, c := range channels {
		if !workspace.IsValidRingChannel(c) {
			return workspace.ErrInvalidRingChannel
		}
		if seen[c] {
			continue
		}
		seen[c] = true
		clean = append(clean, c)
	}

	member, err := uc.members.GetMember(workspaceID, userID)
	if err != nil {
		return err
	}
	if member == nil {
		return workspace.ErrMemberNotFound
	}
	if err := uc.members.UpdateMemberRingChannels(member.ID, clean); err != nil {
		return err
	}

	strs := make([]string, len(clean))
	for i, c := range clean {
		strs[i] = string(c)
	}
	uc.applier.ApplyRingChannels(workspaceID, userID, strs)
	return nil
}

// Get returns a member's current selection, falling back to the default (all
// channels) when they have made no explicit choice.
func (uc *SetRingChannelsUseCase) Get(workspaceID, userID string) ([]workspace.RingChannel, error) {
	member, err := uc.members.GetMember(workspaceID, userID)
	if err != nil {
		return nil, err
	}
	if member == nil {
		return nil, workspace.ErrMemberNotFound
	}
	if len(member.RingChannels) == 0 {
		return workspace.DefaultRingChannels(), nil
	}
	return member.RingChannels, nil
}
