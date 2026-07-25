package dialer

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	media_domain "vozko/domain/media"
	wsc "vozko/domain/workspace_config"
)

// Narrow read ports (satisfied by the real repositories; trivial fakes in tests).
type holdMusicConfigReader interface {
	GetByWorkspaceID(ctx context.Context, workspaceID string) (*wsc.WorkspaceConfig, error)
}

type holdMusicMediaReader interface {
	GetMediaByID(mediaID string) (*media_domain.Media, error)
}

const (
	holdMusicConfigTimeout = 2 * time.Second
	holdMusicFetchTimeout  = 15 * time.Second
	holdMusicFetchMaxBytes = 26 << 20
	// holdMusicMaxPCMBytes trims a decoded loop to 5 minutes of 8kHz mono PCM16
	// (the transcoder already bounds uploads; this is the belt for legacy files).
	holdMusicMaxPCMBytes = 300 * 8000 * 2
	// holdMusicRetryAfter is how long a failed media load is remembered before
	// the next hold retries it (covers transient CDN/network failures without a
	// refetch storm on every hold).
	holdMusicRetryAfter = 5 * time.Minute
)

type workspaceHoldMusicEntry struct {
	track    string
	pcm      []byte // nil = load failed (negative cache until failedAt+retry)
	failedAt time.Time
}

// WorkspaceHoldMusicProvider resolves what a held/parked caller hears for a
// given workspace: the workspace's selected track (a shipped builtin or an
// uploaded hold_music media), else the process-wide fallback (HOLD_MUSIC_PATH or
// the generated comfort tone). Media PCM is decoded ONCE per (workspace, track)
// and cached immutable, mirroring the keyboard-bed boot cache; because media are
// immutable (a new upload is a new id) and the selection is re-read per hold,
// the cache needs NO invalidation plumbing: changing the selection simply stops
// matching the cached track.
//
// The first hold after selecting an UPLOADED track warms the cache in the
// background and plays the fallback audio; every later hold gets the music. A
// builtin selection is served synchronously (local asset, loaded once).
type WorkspaceHoldMusicProvider struct {
	configs  holdMusicConfigReader
	medias   holdMusicMediaReader
	fetch    func(ctx context.Context, url string) ([]byte, error)
	decode   func(r io.Reader) ([]byte, error)
	fallback HoldAudioFactory
	logger   *log.Logger

	mu      sync.Mutex
	cache   map[string]workspaceHoldMusicEntry
	loading map[string]bool
	now     func() time.Time
}

func NewWorkspaceHoldMusicProvider(
	configs holdMusicConfigReader,
	medias holdMusicMediaReader,
	decode func(r io.Reader) ([]byte, error),
	fallback HoldAudioFactory,
	logger *log.Logger,
) *WorkspaceHoldMusicProvider {
	if logger == nil {
		logger = log.Default()
	}
	return &WorkspaceHoldMusicProvider{
		configs:  configs,
		medias:   medias,
		fetch:    defaultHoldMusicFetch,
		decode:   decode,
		fallback: fallback,
		logger:   logger,
		cache:    make(map[string]workspaceHoldMusicEntry),
		loading:  make(map[string]bool),
		now:      time.Now,
	}
}

// SetFetcher overrides the HTTP download (tests).
func (p *WorkspaceHoldMusicProvider) SetFetcher(fetch func(ctx context.Context, url string) ([]byte, error)) {
	if fetch != nil {
		p.fetch = fetch
	}
}

// Source implements dialer_infra.HoldAudioFactory.
func (p *WorkspaceHoldMusicProvider) Source(workspaceID string) HoldAudioSource {
	if p == nil {
		return NewComfortToneSource()
	}
	if workspaceID == "" || p.configs == nil {
		return p.fallbackSource(workspaceID)
	}

	ctx, cancel := context.WithTimeout(context.Background(), holdMusicConfigTimeout)
	defer cancel()
	cfg, err := p.configs.GetByWorkspaceID(ctx, workspaceID)
	if err != nil || cfg == nil {
		if err != nil {
			p.logger.Printf("[HoldMusic] ws=%s config read failed (%v); using fallback audio", workspaceID, err)
		}
		return p.fallbackSource(workspaceID)
	}
	track := strings.TrimSpace(cfg.HoldMusicTrack)
	if track == "" {
		return p.fallbackSource(workspaceID)
	}

	if key, ok := strings.CutPrefix(track, wsc.BuiltinHoldMusicPrefix); ok {
		if pcm := BuiltinHoldTrackPCM(key); len(pcm) > 0 {
			return NewLoopingPCMSource(pcm)
		}
		return p.fallbackSource(workspaceID)
	}

	p.mu.Lock()
	if e, ok := p.cache[workspaceID]; ok && e.track == track {
		if len(e.pcm) > 0 {
			pcm := e.pcm
			p.mu.Unlock()
			return NewLoopingPCMSource(pcm)
		}
		if p.now().Sub(e.failedAt) < holdMusicRetryAfter {
			p.mu.Unlock()
			return p.fallbackSource(workspaceID)
		}
		// Failed long enough ago: fall through and retry the load.
	}
	if !p.loading[workspaceID] {
		p.loading[workspaceID] = true
		go p.load(workspaceID, track)
	}
	p.mu.Unlock()
	return p.fallbackSource(workspaceID)
}

func (p *WorkspaceHoldMusicProvider) fallbackSource(workspaceID string) HoldAudioSource {
	if p.fallback != nil {
		if src := p.fallback(workspaceID); src != nil {
			return src
		}
	}
	return NewComfortToneSource()
}

func (p *WorkspaceHoldMusicProvider) load(workspaceID, track string) {
	pcm, err := p.loadTrackPCM(workspaceID, track)

	p.mu.Lock()
	delete(p.loading, workspaceID)
	entry := workspaceHoldMusicEntry{track: track, pcm: pcm}
	if err != nil {
		entry.pcm = nil
		entry.failedAt = p.now()
	}
	p.cache[workspaceID] = entry
	p.mu.Unlock()

	if err != nil {
		p.logger.Printf("[HoldMusic] ws=%s track=%s load failed (%v); holds use the fallback audio until retry", workspaceID, track, err)
	} else {
		p.logger.Printf("[HoldMusic] ws=%s track=%s cached (%d bytes of telephony PCM)", workspaceID, track, len(pcm))
	}
}

func (p *WorkspaceHoldMusicProvider) loadTrackPCM(workspaceID, mediaID string) ([]byte, error) {
	if p.medias == nil || p.decode == nil {
		return nil, fmt.Errorf("hold music media loading not wired")
	}
	m, err := p.medias.GetMediaByID(mediaID)
	if err != nil || m == nil {
		return nil, fmt.Errorf("media %s not found: %w", mediaID, err)
	}
	// The media repo looks up by id only; the workspace scope check here is what
	// prevents one tenant pointing its hold music at another tenant's file.
	if m.WorkspaceID != workspaceID {
		return nil, fmt.Errorf("media %s does not belong to workspace %s", mediaID, workspaceID)
	}
	if m.Type != media_domain.MediaTypeHoldMusic {
		return nil, fmt.Errorf("media %s is %q, not hold_music", mediaID, m.Type)
	}

	ctx, cancel := context.WithTimeout(context.Background(), holdMusicFetchTimeout)
	defer cancel()
	raw, err := p.fetch(ctx, m.URL)
	if err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}
	pcm, err := p.decode(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if len(pcm) > holdMusicMaxPCMBytes {
		pcm = pcm[:holdMusicMaxPCMBytes]
	}
	if len(pcm) < sipBytesPerFrame {
		return nil, fmt.Errorf("decoded loop shorter than one telephony frame")
	}
	return pcm, nil
}

func defaultHoldMusicFetch(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, holdMusicFetchMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > holdMusicFetchMaxBytes {
		return nil, fmt.Errorf("file exceeds the %dMB hold music cap", holdMusicFetchMaxBytes>>20)
	}
	return data, nil
}

// HoldMusicTrackValidator implements the workspace_config domain port: a track
// is valid when it is empty, a known builtin key, or a hold_music media owned by
// the workspace.
type HoldMusicTrackValidator struct {
	medias holdMusicMediaReader
}

func NewHoldMusicTrackValidator(medias holdMusicMediaReader) *HoldMusicTrackValidator {
	return &HoldMusicTrackValidator{medias: medias}
}

func (v *HoldMusicTrackValidator) ValidateHoldMusicTrack(ctx context.Context, workspaceID, track string) error {
	track = strings.TrimSpace(track)
	if track == "" {
		return nil
	}
	if key, ok := strings.CutPrefix(track, wsc.BuiltinHoldMusicPrefix); ok {
		if IsBuiltinHoldTrack(key) {
			return nil
		}
		return wsc.ErrInvalidHoldMusicTrack
	}
	if v.medias == nil {
		return wsc.ErrInvalidHoldMusicTrack
	}
	m, err := v.medias.GetMediaByID(track)
	if err != nil || m == nil {
		return wsc.ErrInvalidHoldMusicTrack
	}
	if m.WorkspaceID != workspaceID || m.Type != media_domain.MediaTypeHoldMusic {
		return wsc.ErrInvalidHoldMusicTrack
	}
	return nil
}

var _ wsc.HoldMusicTrackValidator = (*HoldMusicTrackValidator)(nil)
