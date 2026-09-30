package holdmusic

import (
	"context"
	"embed"
	"fmt"
	"strings"

	"github.com/zaf/g711"

	"vozko/domain/callrouting"
	"vozko/domain/workflow"
)

//go:embed presets/*.ulaw
var presetFiles embed.FS

var catalog = []callrouting.HoldPreset{
	{ID: "piano_calmo", Name: "Piano calmo", Mood: "calm"},
	{ID: "violao_acustico", Name: "Violão acústico", Mood: "calm"},
	{ID: "ambiente_relaxante", Name: "Ambiente relaxante", Mood: "calm"},
	{ID: "spa_natureza", Name: "Spa e natureza", Mood: "calm"},
	{ID: "cordas_classicas", Name: "Cordas clássicas", Mood: "elegant"},
	{ID: "jazz_lounge", Name: "Jazz lounge", Mood: "elegant"},
	{ID: "corporativo_sereno", Name: "Corporativo sereno", Mood: "professional"},
	{ID: "corporativo_animado", Name: "Corporativo animado", Mood: "professional"},
	{ID: "cinematico_inspirador", Name: "Cinemático inspirador", Mood: "professional"},
	{ID: "bossa_nova", Name: "Bossa nova", Mood: "brazilian"},
	{ID: "mpb_suave", Name: "MPB suave", Mood: "brazilian"},
	{ID: "samba_leve", Name: "Samba leve", Mood: "brazilian"},
	{ID: "forro_instrumental", Name: "Forró instrumental", Mood: "brazilian"},
	{ID: "lofi", Name: "Lo-fi", Mood: "modern"},
	{ID: "eletronico_chill", Name: "Eletrônico chill", Mood: "modern"},
	{ID: "synthwave", Name: "Synthwave", Mood: "modern"},
	{ID: "pop_acustico", Name: "Pop acústico", Mood: "upbeat"},
	{ID: "soul_funk_leve", Name: "Soul e funk leve", Mood: "upbeat"},
	{ID: "reggae_suave", Name: "Reggae suave", Mood: "upbeat"},
	{ID: "natal", Name: "Natal", Mood: "seasonal"},
}

type Library struct {
	presets map[string][]byte
	uploads workflow.VoiceAudio
}

var _ callrouting.HoldMusicLibrary = (*Library)(nil)

func NewLibrary(uploads workflow.VoiceAudio) (*Library, error) {
	presets := make(map[string][]byte, len(catalog))
	for _, preset := range catalog {
		encoded, err := presetFiles.ReadFile("presets/" + preset.ID + ".ulaw")
		if err != nil {
			return nil, fmt.Errorf("hold music preset %s: %w", preset.ID, err)
		}
		presets[preset.ID] = g711.DecodeUlaw(encoded)
	}
	return &Library{presets: presets, uploads: uploads}, nil
}

func (l *Library) Presets() []callrouting.HoldPreset {
	return append([]callrouting.HoldPreset(nil), catalog...)
}

func (l *Library) PresetAudio(presetID string) ([]byte, bool) {
	pcm, ok := l.presets[presetID]
	return pcm, ok
}

func (l *Library) PCM(ctx context.Context, workspaceID string, ref callrouting.HoldMusicRef) ([]byte, error) {
	if err := ref.Validate(); err != nil {
		return nil, err
	}
	if mediaID := strings.TrimSpace(ref.MediaID); mediaID != "" {
		if l.uploads == nil {
			return nil, callrouting.ErrHoldMusicNotFound
		}
		pcm, err := l.uploads.LoadPCM(ctx, workspaceID, mediaID)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", callrouting.ErrHoldMusicNotFound, err)
		}
		return pcm, nil
	}
	presetID := strings.TrimSpace(ref.PresetID)
	if presetID == "" {
		presetID = callrouting.DefaultHoldPreset
	}
	pcm, ok := l.presets[presetID]
	if !ok {
		return nil, fmt.Errorf("%w: preset %q", callrouting.ErrHoldMusicNotFound, presetID)
	}
	return pcm, nil
}
