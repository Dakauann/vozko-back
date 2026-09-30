package holdmusic

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/callrouting"
)

type uploadStub struct {
	pcm []byte
	err error
	ws  string
}

func (u *uploadStub) LoadPCM(_ context.Context, workspaceID, _ string) ([]byte, error) {
	u.ws = workspaceID
	return u.pcm, u.err
}

func TestEveryPresetShipsAsAboutThirtySecondsOfTelephoneAudio(t *testing.T) {
	lib, err := NewLibrary(nil)
	if err != nil {
		t.Fatalf("NewLibrary: %v", err)
	}
	presets := lib.Presets()
	if len(presets) != 20 {
		t.Fatalf("presets = %d", len(presets))
	}
	for _, preset := range presets {
		pcm, ok := lib.PresetAudio(preset.ID)
		seconds := float64(len(pcm)) / 2 / 8000
		if !ok || seconds < 28 || seconds > 32 {
			t.Errorf("%s: %.1f seconds", preset.ID, seconds)
		}
	}
	if _, ok := lib.PresetAudio(callrouting.DefaultHoldPreset); !ok {
		t.Fatal("the default preset is missing")
	}
}

func TestEveryCallerSharesTheSameDecodedPreset(t *testing.T) {
	lib, _ := NewLibrary(nil)
	first, _ := lib.PCM(context.Background(), "ws1", callrouting.HoldMusicRef{PresetID: "bossa_nova"})
	second, _ := lib.PCM(context.Background(), "ws2", callrouting.HoldMusicRef{PresetID: "bossa_nova"})
	if &first[0] != &second[0] {
		t.Fatal("each caller got a private copy of the preset")
	}
}

func TestHoldMusicResolvesPresetsDefaultsAndUploads(t *testing.T) {
	uploads := &uploadStub{pcm: []byte{1, 2}}
	lib, _ := NewLibrary(uploads)

	if pcm, err := lib.PCM(context.Background(), "ws1", callrouting.HoldMusicRef{}); err != nil || len(pcm) == 0 {
		t.Fatalf("default: %v", err)
	}
	if pcm, err := lib.PCM(context.Background(), "ws1", callrouting.HoldMusicRef{MediaID: "m1"}); err != nil || len(pcm) != 2 || uploads.ws != "ws1" {
		t.Fatalf("upload: %v %v", pcm, err)
	}
	if _, err := lib.PCM(context.Background(), "ws1", callrouting.HoldMusicRef{PresetID: "heavy_metal"}); !errors.Is(err, callrouting.ErrHoldMusicNotFound) {
		t.Fatalf("unknown preset err = %v", err)
	}
	uploads.err = errors.New("not audio")
	if _, err := lib.PCM(context.Background(), "ws1", callrouting.HoldMusicRef{MediaID: "m1"}); !errors.Is(err, callrouting.ErrHoldMusicNotFound) {
		t.Fatalf("bad upload err = %v", err)
	}
}
