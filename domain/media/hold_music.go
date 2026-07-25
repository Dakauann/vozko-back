package media

import (
	"context"
	"errors"
)

// HoldMusicHardCap is the product ceiling on custom hold music tracks per
// workspace, regardless of what the plan grants.
const HoldMusicHardCap = 10

var (
	// ErrHoldMusicNotIncluded: the workspace has no active plan, or its plan
	// grants zero custom hold music tracks.
	ErrHoldMusicNotIncluded = errors.New("hold music uploads are not included in the workspace plan")
	// ErrHoldMusicQuotaReached: the workspace already holds its maximum number of
	// custom tracks; one must be deleted to free a slot.
	ErrHoldMusicQuotaReached = errors.New("hold music track limit reached; delete a track to free a slot")
	ErrMediaNotFound         = errors.New("media not found")
	// ErrNotHoldMusic: the delete endpoint only removes hold music tracks. Other
	// media types may be referenced by workflows/campaigns and there is no
	// reference counting yet, so they are not deletable here.
	ErrNotHoldMusic = errors.New("only hold music tracks can be deleted")
)

// HoldMusicQuotaGate answers whether a workspace may upload another custom hold
// music track: plan MaxHoldMusicTracks (clamped to HoldMusicHardCap) versus the
// current hold_music media count. Plan driven only, mirroring the branch
// provisioning gate shape.
type HoldMusicQuotaGate interface {
	CanUploadHoldMusic(workspaceID string) error
}

// DeleteHoldMusicUseCase removes a workspace's own hold music track (freeing a
// quota slot) and clears the workspace selection if it pointed at the deleted
// track.
type DeleteHoldMusicUseCase interface {
	DeleteHoldMusic(ctx context.Context, workspaceID, mediaID string) error
}
