package callrouting

import (
	"context"
	"strings"
)

const DefaultHoldPreset = "piano_calmo"

type HoldMusicRef struct {
	PresetID string `json:"presetId,omitempty"`
	MediaID  string `json:"mediaId,omitempty"`
}

func (r HoldMusicRef) IsZero() bool {
	return strings.TrimSpace(r.PresetID) == "" && strings.TrimSpace(r.MediaID) == ""
}

func (r HoldMusicRef) Validate() error {
	if strings.TrimSpace(r.PresetID) != "" && strings.TrimSpace(r.MediaID) != "" {
		return ErrHoldMusicAmbiguous
	}
	return nil
}

type HoldPreset struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Mood string `json:"mood"`
}

type HoldMusicLibrary interface {
	Presets() []HoldPreset
	PresetAudio(presetID string) ([]byte, bool)
	PCM(ctx context.Context, workspaceID string, ref HoldMusicRef) ([]byte, error)
}
