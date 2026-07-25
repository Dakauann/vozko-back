package dialer

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	media_domain "vozko/domain/media"
	wsc "vozko/domain/workspace_config"
)

type fakeHoldConfigs struct{ track string }

func (f fakeHoldConfigs) GetByWorkspaceID(_ context.Context, ws string) (*wsc.WorkspaceConfig, error) {
	return &wsc.WorkspaceConfig{WorkspaceID: ws, HoldMusicTrack: f.track}, nil
}

type fakeHoldMedias struct{ m *media_domain.Media }

func (f fakeHoldMedias) GetMediaByID(string) (*media_domain.Media, error) {
	if f.m == nil {
		return nil, errors.New("record not found")
	}
	return f.m, nil
}

// markedSource lets assertions distinguish which chain link produced the audio.
type markedSource struct{ frame []byte }

func (s markedSource) NextFrame() []byte { return s.frame }

func fallbackFactory(mark byte) HoldAudioFactory {
	frame := bytes.Repeat([]byte{mark}, sipBytesPerFrame)
	return func(string) HoldAudioSource { return markedSource{frame: frame} }
}

func testDecode(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	// "decoded PCM" = the fetched bytes repeated to fill four frames.
	out := bytes.Repeat(data, sipBytesPerFrame*4)
	return out[:sipBytesPerFrame*4], nil
}

func waitForHoldCache(t *testing.T, p *WorkspaceHoldMusicProvider, ws string) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		p.mu.Lock()
		e, ok := p.cache[ws]
		p.mu.Unlock()
		if ok && (len(e.pcm) > 0 || !e.failedAt.IsZero()) {
			return
		}
		select {
		case <-deadline:
			t.Fatal("hold music cache never settled")
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func TestWorkspaceHoldMusic_EmptySelectionUsesFallback(t *testing.T) {
	p := NewWorkspaceHoldMusicProvider(fakeHoldConfigs{track: ""}, fakeHoldMedias{}, testDecode, fallbackFactory('F'), nil)
	src := p.Source("ws-1")
	if got := src.NextFrame()[0]; got != 'F' {
		t.Fatalf("empty selection must resolve the fallback chain, got frame byte %q", got)
	}
}

func TestWorkspaceHoldMusic_BuiltinServedSynchronously(t *testing.T) {
	builtinPCMMu.Lock()
	builtinPCMCache["lofi"] = bytes.Repeat([]byte{'B'}, sipBytesPerFrame*2)
	builtinPCMMu.Unlock()
	t.Cleanup(func() {
		builtinPCMMu.Lock()
		builtinPCMCache = map[string][]byte{}
		builtinPCMMu.Unlock()
	})

	p := NewWorkspaceHoldMusicProvider(fakeHoldConfigs{track: "builtin:lofi"}, fakeHoldMedias{}, testDecode, fallbackFactory('F'), nil)
	if got := p.Source("ws-1").NextFrame()[0]; got != 'B' {
		t.Fatalf("builtin selection must play the builtin PCM immediately, got %q", got)
	}
}

func TestWorkspaceHoldMusic_MediaWarmsThenPlays(t *testing.T) {
	p := NewWorkspaceHoldMusicProvider(
		fakeHoldConfigs{track: "media-1"},
		fakeHoldMedias{m: &media_domain.Media{ID: "media-1", WorkspaceID: "ws-1", Type: media_domain.MediaTypeHoldMusic, URL: "https://cdn.test/m1.mp3"}},
		testDecode,
		fallbackFactory('F'),
		nil,
	)
	p.SetFetcher(func(context.Context, string) ([]byte, error) { return []byte{'M'}, nil })

	// First hold: cache cold, plays fallback while the track warms in background.
	if got := p.Source("ws-1").NextFrame()[0]; got != 'F' {
		t.Fatalf("first hold must play the fallback while warming, got %q", got)
	}
	waitForHoldCache(t, p, "ws-1")
	// Every later hold plays the workspace's music.
	if got := p.Source("ws-1").NextFrame()[0]; got != 'M' {
		t.Fatalf("warmed hold must play the workspace track, got %q", got)
	}
}

func TestWorkspaceHoldMusic_CrossWorkspaceMediaIsRefused(t *testing.T) {
	p := NewWorkspaceHoldMusicProvider(
		fakeHoldConfigs{track: "media-1"},
		fakeHoldMedias{m: &media_domain.Media{ID: "media-1", WorkspaceID: "ws-OTHER", Type: media_domain.MediaTypeHoldMusic, URL: "https://cdn.test/m1.mp3"}},
		testDecode,
		fallbackFactory('F'),
		nil,
	)
	p.SetFetcher(func(context.Context, string) ([]byte, error) { return []byte{'M'}, nil })

	_ = p.Source("ws-1")
	waitForHoldCache(t, p, "ws-1")
	if got := p.Source("ws-1").NextFrame()[0]; got != 'F' {
		t.Fatalf("another tenant's media must NEVER play; got %q", got)
	}
}

func TestHoldMusicTrackValidator(t *testing.T) {
	builtinPCMMu.Lock()
	builtinPCMCache = map[string][]byte{}
	builtinPCMMu.Unlock()

	own := &media_domain.Media{ID: "m-1", WorkspaceID: "ws-1", Type: media_domain.MediaTypeHoldMusic}
	cases := []struct {
		name  string
		track string
		media *media_domain.Media
		ok    bool
	}{
		{"empty clears", "", nil, true},
		{"known builtin", "builtin:lofi", nil, true},
		{"unknown builtin", "builtin:nope", nil, false},
		{"own hold music media", "m-1", own, true},
		{"cross workspace media", "m-1", &media_domain.Media{ID: "m-1", WorkspaceID: "ws-2", Type: media_domain.MediaTypeHoldMusic}, false},
		{"wrong media type", "m-1", &media_domain.Media{ID: "m-1", WorkspaceID: "ws-1", Type: media_domain.MediaTypeAudio}, false},
		{"missing media", "m-404", nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := NewHoldMusicTrackValidator(fakeHoldMedias{m: c.media})
			err := v.ValidateHoldMusicTrack(context.Background(), "ws-1", c.track)
			if c.ok && err != nil {
				t.Fatalf("want valid, got %v", err)
			}
			if !c.ok && !errors.Is(err, wsc.ErrInvalidHoldMusicTrack) {
				t.Fatalf("want ErrInvalidHoldMusicTrack, got %v", err)
			}
		})
	}
}
